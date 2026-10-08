package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

const graphCursorPurposePrefix = "graph-delta-cursor:"

type GraphRepository struct {
	db                database
	secrets           secrets.Provider
	newID             func() string
	retentionYears    int
	legacyCredentials graphintake.GraphCredentialResolver
	legacyClientState graphintake.ClientStateResolver
}

var _ graphintake.ReconciliationQueue = (*GraphRepository)(nil)
var _ graphintake.NotificationRepository = (*GraphRepository)(nil)
var _ graphintake.NotificationQueue = (*GraphRepository)(nil)
var _ graphintake.SubscriptionQueue = (*GraphRepository)(nil)

func NewGraphRepository(
	db database,
	provider secrets.Provider,
	newID func() string,
) *GraphRepository {
	return &GraphRepository{
		db: db, secrets: provider, newID: newID, retentionYears: 7,
	}
}

func (r *GraphRepository) Plan(
	ctx context.Context,
	limit int,
	_ time.Time,
) (int, error) {
	if r.newID == nil || limit <= 0 {
		return 0, graphintake.ErrInvalidGraphConfiguration
	}
	rows, err := r.db.Query(ctx, `
SELECT connection.id::text
FROM graph_mailbox_connections connection
LEFT JOIN graph_delta_cursors cursor
  ON cursor.connection_id = connection.id AND cursor.folder_id = 'inbox'
WHERE connection.enabled AND cursor.id IS NULL
ORDER BY connection.created_at, connection.id
LIMIT $1
`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	connectionIDs := make([]string, 0)
	for rows.Next() {
		var connectionID string
		if err := rows.Scan(&connectionID); err != nil {
			return 0, err
		}
		connectionIDs = append(connectionIDs, connectionID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	planned := 0
	for _, connectionID := range connectionIDs {
		cursorID := r.newID()
		tag, err := tx.Exec(ctx, `
INSERT INTO graph_delta_cursors (
  id, connection_id, folder_id, cursor_secret_ref, version
) VALUES ($1, $2, 'inbox', $3, 1)
ON CONFLICT (connection_id, folder_id) DO NOTHING
`, cursorID, connectionID, "local://graph-delta/"+cursorID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return 0, err
		}
		planned += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	return planned, nil
}

func (r *GraphRepository) Claim(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]graphintake.ReconciliationJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT cursor.id
  FROM graph_delta_cursors cursor
  JOIN graph_mailbox_connections connection
    ON connection.id = cursor.connection_id AND connection.enabled
  WHERE (
      cursor.last_completed_at IS NULL
      OR cursor.last_completed_at <= $1::timestamptz - interval '5 minutes'
      OR EXISTS (
        SELECT 1 FROM graph_subscriptions subscription
        WHERE subscription.connection_id = connection.id
          AND subscription.recovery_state = 'run_delta'
      )
    )
    AND (cursor.lease_until IS NULL OR cursor.lease_until <= $1)
  ORDER BY COALESCE(cursor.last_completed_at, '-infinity'::timestamptz),
           cursor.connection_id, cursor.folder_id
  LIMIT $2
  FOR UPDATE OF cursor SKIP LOCKED
),
leased AS (
  UPDATE graph_delta_cursors cursor
  SET lease_until = $1 + ($3 * interval '1 microsecond'),
      last_attempt_at = $1,
      last_started_at = $1,
      last_error_code = NULL
  FROM candidates
  WHERE cursor.id = candidates.id
  RETURNING cursor.connection_id, cursor.folder_id
)
SELECT connection.id::text, connection.msp_id::text,
       connection.mailbox_address, leased.folder_id,
       'db://graph/' || connection.id::text || '/' || connection.configuration_generation::text || '/credential'
FROM leased
JOIN graph_mailbox_connections connection
  ON connection.id = leased.connection_id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]graphintake.ReconciliationJob, 0)
	for rows.Next() {
		var job graphintake.ReconciliationJob
		if err := rows.Scan(
			&job.ConnectionID, &job.MSPID, &job.Mailbox,
			&job.Folder, &job.CredentialSecretRef,
		); err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *GraphRepository) Bind(
	connectionID string,
) (graphintake.IntakeRepository, graphintake.ThreadIndex) {
	bound := &boundGraphRepository{
		parent: r, connectionID: strings.TrimSpace(connectionID),
	}
	return bound, bound
}

func (r *GraphRepository) MarkFailed(
	ctx context.Context,
	connectionID string,
	at time.Time,
	errorCode string,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE graph_delta_cursors cursor
SET lease_until = NULL, last_error_code = $3
FROM graph_mailbox_connections connection
WHERE cursor.connection_id = connection.id
  AND connection.id = $1
  AND cursor.last_attempt_at = $2
`, connectionID, at, errorCode)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidDeltaPage
	}
	if _, err := tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET health_state = 'degraded', last_error_code = $3,
    updated_at = $2, version = version + 1
WHERE id = $1
`, connectionID, at, errorCode); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *GraphRepository) ClaimSubscriptions(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]graphintake.SubscriptionJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT connection.id
  FROM graph_mailbox_connections connection
  LEFT JOIN graph_subscriptions subscription
    ON subscription.connection_id = connection.id
  WHERE connection.enabled
    AND (
      (connection.client_state_secret_ref IS NOT NULL
       AND btrim(connection.client_state_secret_ref) <> '')
      OR connection.client_state_secret_version IS NOT NULL
    )
    AND (
      subscription.id IS NULL
      OR subscription.expires_at <= $1::timestamptz + interval '24 hours'
      OR subscription.recovery_state IN (
        'reauthorize', 'recreate_subscription'
      )
    )
    AND (
      connection.subscription_lease_until IS NULL
      OR connection.subscription_lease_until <= $1::timestamptz
    )
  ORDER BY
    CASE WHEN subscription.id IS NULL THEN 0 ELSE 1 END,
    COALESCE(subscription.expires_at, '-infinity'::timestamptz),
    connection.id
  LIMIT $2
  FOR UPDATE OF connection SKIP LOCKED
),
leased AS (
  UPDATE graph_mailbox_connections connection
  SET subscription_lease_until =
        $1::timestamptz + ($3 * interval '1 microsecond'),
      last_subscription_attempt_at = $1::timestamptz,
      last_error_code = NULL
  FROM candidates
  WHERE connection.id = candidates.id
  RETURNING connection.*
)
SELECT COALESCE(subscription.id::text, ''),
       connection.id::text, connection.mailbox_address,
       'db://graph/' || connection.id::text || '/' || connection.configuration_generation::text || '/credential',
       'db://graph/' || connection.id::text || '/' || connection.configuration_generation::text || '/client-state',
       COALESCE(subscription.graph_subscription_id, ''),
       COALESCE(subscription.resource, ''),
       COALESCE(subscription.expires_at, 'epoch'::timestamptz),
       COALESCE(subscription.recovery_state, 'none')
FROM leased connection
LEFT JOIN graph_subscriptions subscription
  ON subscription.connection_id = connection.id
ORDER BY connection.id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]graphintake.SubscriptionJob, 0)
	for rows.Next() {
		var job graphintake.SubscriptionJob
		if err := rows.Scan(
			&job.ID, &job.ConnectionID, &job.Mailbox,
			&job.CredentialSecretRef, &job.ClientStateSecretRef,
			&job.ExternalID, &job.Resource, &job.ExpiresAt,
			&job.Recovery,
		); err != nil {
			return nil, err
		}
		if job.ID == "" {
			job.ID = r.newID()
		}
		if job.Resource == "" {
			job.Resource = "users/" + strings.TrimSpace(job.Mailbox) +
				"/mailFolders('Inbox')/messages"
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *GraphRepository) CompleteSubscription(
	ctx context.Context,
	completion graphintake.SubscriptionCompletion,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO graph_subscriptions (
  id, connection_id, graph_subscription_id,
  client_state_secret_ref, resource, expires_at,
  last_renewed_at, last_lifecycle_event, recovery_state,
  version
)
SELECT $1, connection.id, $3,
       COALESCE(
         connection.client_state_secret_ref,
         'db://graph/' || connection.id::text || '/' ||
           connection.configuration_generation::text || '/client-state'
       ), $4, $5,
       $6, NULL, $7, 1
FROM graph_mailbox_connections connection
WHERE connection.id = $2 AND connection.enabled
  AND connection.subscription_lease_until IS NOT NULL
  AND connection.last_subscription_attempt_at = $6
ON CONFLICT (connection_id)
DO UPDATE SET
  graph_subscription_id = EXCLUDED.graph_subscription_id,
  client_state_secret_ref = EXCLUDED.client_state_secret_ref,
  resource = EXCLUDED.resource,
  expires_at = EXCLUDED.expires_at,
  last_renewed_at = EXCLUDED.last_renewed_at,
  last_lifecycle_event = NULL,
  recovery_state = EXCLUDED.recovery_state,
  version = graph_subscriptions.version + 1
`, completion.ID, completion.ConnectionID, completion.ExternalID,
		completion.Resource, completion.ExpiresAt, completion.CompletedAt,
		completion.Recovery)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidSubscription
	}
	tag, err = tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET subscription_lease_until = NULL,
    last_subscription_success_at = $2,
    health_state = 'healthy', last_error_code = NULL,
    updated_at = $2, version = version + 1
WHERE id = $1 AND subscription_lease_until IS NOT NULL
  AND last_subscription_attempt_at = $2
`, completion.ConnectionID, completion.CompletedAt)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidSubscription
	}
	action := "graph.subscription.renewed"
	if completion.Created {
		action = "graph.subscription.created"
	}
	auditID, eventID, correlationID := r.newID(), r.newID(), r.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, connection.msp_id, 'integration', connection.id,
       $3, 'graph_subscription', subscription.id,
       subscription.version, 'graph', $4,
       jsonb_build_object(
         'expires_at', subscription.expires_at,
         'resource', subscription.resource,
         'recovery_state', subscription.recovery_state
       ),
       jsonb_build_object('connection_id', connection.id)
FROM graph_subscriptions subscription
JOIN graph_mailbox_connections connection
  ON connection.id = subscription.connection_id
WHERE subscription.connection_id = $5
`, auditID, completion.CompletedAt, action, correlationID,
		completion.ConnectionID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, $2, 1, $3, connection.msp_id,
       'integration', connection.id, 'graph_subscription',
       subscription.id, subscription.version, $4, 'graph',
       jsonb_build_object(
         'expires_at', subscription.expires_at,
         'resource', subscription.resource,
         'recovery_state', subscription.recovery_state
       )
FROM graph_subscriptions subscription
JOIN graph_mailbox_connections connection
  ON connection.id = subscription.connection_id
WHERE subscription.connection_id = $5
`, eventID, action, completion.CompletedAt, correlationID,
		completion.ConnectionID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *GraphRepository) FailSubscription(
	ctx context.Context,
	failure graphintake.SubscriptionFailure,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET subscription_lease_until = NULL,
    health_state = 'degraded', last_error_code = $3,
    updated_at = $2, version = version + 1
WHERE id = $1 AND subscription_lease_until IS NOT NULL
  AND last_subscription_attempt_at = $2
`, failure.ConnectionID, failure.FailedAt, failure.ErrorCode)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidSubscription
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *GraphRepository) LoadSubscription(
	ctx context.Context,
	subscriptionID string,
) (graphintake.SubscriptionTarget, error) {
	var target graphintake.SubscriptionTarget
	err := r.db.QueryRow(ctx, `
SELECT connection.id::text, subscription.graph_subscription_id,
       connection.mailbox_address, subscription.client_state_secret_ref
FROM graph_subscriptions subscription
JOIN graph_mailbox_connections connection
  ON connection.id = subscription.connection_id
WHERE subscription.graph_subscription_id = $1
  AND connection.enabled
`, subscriptionID).Scan(
		&target.ConnectionID, &target.SubscriptionID,
		&target.Mailbox, &target.ClientStateSecretRef,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphintake.SubscriptionTarget{}, graphintake.ErrInvalidNotification
	}
	return target, err
}

func (r *GraphRepository) AcceptNotifications(
	ctx context.Context,
	mutation graphintake.NotificationMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	for _, hint := range mutation.Hints {
		tag, err := tx.Exec(ctx, `
INSERT INTO graph_notification_hints (
  id, connection_id, subscription_id, message_id, received_at, state
)
SELECT $1, connection.id, subscription.id, $4, $5, 'pending'
FROM graph_subscriptions subscription
JOIN graph_mailbox_connections connection
  ON connection.id = subscription.connection_id
WHERE connection.id = $2
  AND subscription.graph_subscription_id = $3
  AND connection.enabled
ON CONFLICT (subscription_id, message_id) DO NOTHING
`, hint.ID, hint.ConnectionID, hint.SubscriptionID,
			hint.MessageID, hint.ReceivedAt)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
	}
	for _, lifecycle := range mutation.Lifecycle {
		tag, err := tx.Exec(ctx, `
UPDATE graph_subscriptions subscription
SET last_lifecycle_event = $3, recovery_state = $4,
    version = subscription.version + 1
FROM graph_mailbox_connections connection
WHERE subscription.connection_id = connection.id
  AND connection.id = $1
  AND subscription.graph_subscription_id = $2
  AND connection.enabled
`, lifecycle.ConnectionID, lifecycle.SubscriptionID,
			lifecycle.Event, lifecycle.Recovery)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return graphintake.ErrInvalidNotification
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *GraphRepository) ClaimNotifications(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]graphintake.NotificationJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT hint.id
  FROM graph_notification_hints hint
  JOIN graph_mailbox_connections connection
    ON connection.id = hint.connection_id AND connection.enabled
  WHERE hint.state = 'pending'
    AND (hint.lease_until IS NULL OR hint.lease_until <= $1)
  ORDER BY hint.received_at, hint.connection_id, hint.id
  LIMIT $2
  FOR UPDATE OF hint SKIP LOCKED
),
leased AS (
  UPDATE graph_notification_hints hint
  SET lease_until = $1 + ($3 * interval '1 microsecond'),
      attempt_count = hint.attempt_count + 1
  FROM candidates
  WHERE hint.id = candidates.id
  RETURNING hint.id, hint.connection_id, hint.message_id
)
SELECT leased.id::text, connection.id::text, connection.msp_id::text,
       connection.mailbox_address,
       'db://graph/' || connection.id::text || '/' || connection.configuration_generation::text || '/credential',
       leased.message_id
FROM leased
JOIN graph_mailbox_connections connection
  ON connection.id = leased.connection_id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]graphintake.NotificationJob, 0)
	for rows.Next() {
		var job graphintake.NotificationJob
		if err := rows.Scan(
			&job.HintID, &job.ConnectionID, &job.MSPID,
			&job.Mailbox, &job.CredentialSecretRef, &job.MessageID,
		); err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *GraphRepository) MarkNotificationProcessed(
	ctx context.Context,
	hintID string,
	at time.Time,
) error {
	return r.completeNotification(ctx, `
UPDATE graph_notification_hints
SET state = 'processed', processed_at = $2,
    lease_until = NULL, last_error_code = NULL
WHERE id = $1 AND state = 'pending' AND lease_until IS NOT NULL
`, hintID, at)
}

func (r *GraphRepository) MarkNotificationFailed(
	ctx context.Context,
	hintID string,
	_ time.Time,
	errorCode string,
) error {
	return r.completeNotification(ctx, `
UPDATE graph_notification_hints
SET state = 'failed', processed_at = NULL,
    lease_until = NULL, last_error_code = $2
WHERE id = $1 AND state = 'pending' AND lease_until IS NOT NULL
`, hintID, errorCode)
}

func (r *GraphRepository) completeNotification(
	ctx context.Context,
	query string,
	args ...any,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidNotification
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

type boundGraphRepository struct {
	parent       *GraphRepository
	connectionID string
}

var (
	_ graphintake.IntakeRepository = (*boundGraphRepository)(nil)
	_ graphintake.ThreadIndex      = (*boundGraphRepository)(nil)
)

func (r *boundGraphRepository) Cursor(
	ctx context.Context,
	mailbox string,
	folder string,
) (string, error) {
	var (
		keyVersion int
		nonce      []byte
		ciphertext []byte
	)
	err := r.parent.db.QueryRow(ctx, `
SELECT COALESCE(cursor.cursor_key_version, 0),
       COALESCE(cursor.cursor_nonce, ''::bytea),
       COALESCE(cursor.cursor_ciphertext, ''::bytea)
FROM graph_delta_cursors cursor
JOIN graph_mailbox_connections connection
  ON connection.id = cursor.connection_id
WHERE cursor.connection_id = $1
  AND lower(connection.mailbox_address) = lower($2)
  AND cursor.folder_id = $3
  AND connection.enabled
`, r.connectionID, mailbox, folder).Scan(&keyVersion, &nonce, &ciphertext)
	if err != nil {
		return "", err
	}
	if keyVersion == 0 {
		return "", nil
	}
	plaintext, err := r.parent.secrets.Open(
		ctx, graphCursorPurpose(r.connectionID, folder),
		secrets.SealedValue{
			Version: keyVersion, Nonce: nonce, Ciphertext: ciphertext,
		},
	)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (r *boundGraphRepository) Commit(
	ctx context.Context,
	batch graphintake.IntakeBatch,
) error {
	if r.parent.secrets == nil || strings.TrimSpace(batch.NextCursor) == "" {
		return graphintake.ErrInvalidDeltaPage
	}
	sealed, err := r.parent.secrets.Seal(
		ctx, graphCursorPurpose(r.connectionID, batch.Folder),
		[]byte(batch.NextCursor),
	)
	if err != nil {
		return err
	}
	tx, err := r.parent.db.Begin(ctx)
	if err != nil {
		return err
	}
	for _, message := range batch.Messages {
		if err := r.insertMessage(
			ctx, tx, batch.Mailbox, message, batch.CommittedAt,
		); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	tag, err := tx.Exec(ctx, `
UPDATE graph_delta_cursors cursor
SET cursor_key_version = $5, cursor_nonce = $6,
    cursor_ciphertext = $7, last_completed_at = $4,
    last_error_code = NULL, lease_until = NULL,
    version = cursor.version + 1
FROM graph_mailbox_connections connection
WHERE cursor.connection_id = connection.id
  AND cursor.connection_id = $1
  AND lower(connection.mailbox_address) = lower($2)
  AND cursor.folder_id = $3
  AND connection.enabled
`, r.connectionID, batch.Mailbox, batch.Folder, batch.CommittedAt,
		sealed.Version, sealed.Nonce, sealed.Ciphertext)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return graphintake.ErrInvalidDeltaPage
	}
	if err := r.markHealthy(ctx, tx, batch.CommittedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE graph_subscriptions
SET recovery_state = 'none', version = version + 1
WHERE connection_id = $1 AND recovery_state = 'run_delta'
`, r.connectionID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *boundGraphRepository) CommitMessage(
	ctx context.Context,
	mailbox string,
	message graphintake.NormalizedMessage,
) error {
	tx, err := r.parent.db.Begin(ctx)
	if err != nil {
		return err
	}
	at := message.ReceivedAt.UTC()
	if err := r.insertMessage(ctx, tx, mailbox, message, at); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := r.markHealthy(ctx, tx, at); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *boundGraphRepository) insertMessage(
	ctx context.Context,
	tx transaction,
	mailbox string,
	message graphintake.NormalizedMessage,
	committedAt time.Time,
) error {
	if r.parent.newID == nil || strings.TrimSpace(message.ExternalID) == "" ||
		strings.TrimSpace(message.Sender) == "" ||
		strings.TrimSpace(message.RawMIMERef) == "" {
		return graphintake.ErrInvalidGraphResponse
	}
	messageID := r.parent.newID()
	references, err := json.Marshal(message.References)
	if err != nil {
		return err
	}
	attachments, err := json.Marshal(message.AttachmentRefs)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"mailbox": mailbox, "create_new": message.Thread.CreateNew,
	})
	if err != nil {
		return err
	}
	state := "normalized"
	if message.Thread.WorkRecordID != "" {
		state = "processed"
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO inbound_email_messages (
  id, connection_id, msp_id, client_id, external_message_id,
  graph_conversation_id, internet_message_id, in_reply_to, reference_ids,
  sender_address, subject, received_at, raw_mime_ref, attachment_refs, normalized_payload,
  authentication_result, processing_state, work_record_id,
  thread_match_method, retention_until
)
SELECT $1, connection.id, connection.msp_id, work.client_id, $4,
       NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8,
       $9, $10, $11, $12, $13, $14,
       'passed', $15, work.id, $16, $17
FROM graph_mailbox_connections connection
LEFT JOIN work_records work
  ON work.id = NULLIF($18, '')::uuid
 AND work.msp_id = connection.msp_id
 AND work.deleted_at IS NULL
WHERE connection.id = $2
  AND lower(connection.mailbox_address) = lower($3)
  AND connection.enabled
ON CONFLICT (connection_id, external_message_id) DO NOTHING
`, messageID, r.connectionID, mailbox, message.ExternalID,
		message.ConversationID, message.InternetMessageID,
		message.InReplyTo, references, message.Sender, message.Subject,
		message.ReceivedAt, message.RawMIMERef, attachments, payload, state,
		message.Thread.MatchedBy,
		message.ReceivedAt.AddDate(r.parent.retentionYears, 0, 0),
		message.Thread.WorkRecordID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	auditID, eventID, correlationID := r.parent.newID(), r.parent.newID(), r.parent.newID()
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, correlation_id,
  safe_diff, authorization_context
)
SELECT $1, $2, connection.msp_id, work.client_id,
       'integration', connection.id, 'graph_email.received',
       'inbound_email_message', $3, 1, 'graph', $4,
       jsonb_build_object('external_message_id', $5::text),
       jsonb_build_object('connection_id', connection.id)
FROM graph_mailbox_connections connection
LEFT JOIN work_records work
  ON work.id = NULLIF($6, '')::uuid
 AND work.msp_id = connection.msp_id
WHERE connection.id = $7
`, auditID, committedAt, messageID, correlationID,
		message.ExternalID, message.Thread.WorkRecordID, r.connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
)
SELECT $1, 'graph_email.received', 1, $2,
       connection.msp_id, work.client_id, 'integration', connection.id,
       'inbound_email_message', $3, 1, $4, 'graph',
       jsonb_build_object('external_message_id', $5::text)
FROM graph_mailbox_connections connection
LEFT JOIN work_records work
  ON work.id = NULLIF($6, '')::uuid
 AND work.msp_id = connection.msp_id
WHERE connection.id = $7
`, eventID, committedAt, messageID, correlationID,
		message.ExternalID, message.Thread.WorkRecordID, r.connectionID); err != nil {
		return err
	}
	return nil
}

func (r *boundGraphRepository) markHealthy(
	ctx context.Context,
	tx transaction,
	at time.Time,
) error {
	_, err := tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET health_state = 'healthy', last_success_at = $2,
    last_error_code = NULL, updated_at = $2, version = version + 1
WHERE id = $1 AND enabled
`, r.connectionID, at)
	return err
}

func (r *boundGraphRepository) ByConversationID(
	ctx context.Context,
	value string,
) (string, bool) {
	return r.thread(ctx, `
SELECT work_record_id::text
FROM inbound_email_messages
WHERE connection_id = $1 AND graph_conversation_id = $2
  AND work_record_id IS NOT NULL
ORDER BY received_at DESC
LIMIT 1
`, value)
}

func (r *boundGraphRepository) ByInternetMessageID(
	ctx context.Context,
	value string,
) (string, bool) {
	return r.thread(ctx, `
SELECT work_record_id::text
FROM inbound_email_messages
WHERE connection_id = $1 AND internet_message_id = $2
  AND work_record_id IS NOT NULL
ORDER BY received_at DESC
LIMIT 1
`, value)
}

func (r *boundGraphRepository) ByTicketToken(
	ctx context.Context,
	value string,
) (string, bool) {
	return r.thread(ctx, `
SELECT message.work_record_id::text
FROM inbound_email_messages message
WHERE message.connection_id = $1
  AND message.normalized_payload->>'ticket_token' = $2
  AND message.work_record_id IS NOT NULL
ORDER BY message.received_at DESC
LIMIT 1
`, value)
}

func (r *boundGraphRepository) thread(
	ctx context.Context,
	query string,
	value string,
) (string, bool) {
	var workRecordID string
	err := r.parent.db.QueryRow(
		ctx, query, r.connectionID, value,
	).Scan(&workRecordID)
	return workRecordID, err == nil && workRecordID != ""
}

func graphCursorPurpose(connectionID, folder string) string {
	return graphCursorPurposePrefix + connectionID + ":" + folder
}
