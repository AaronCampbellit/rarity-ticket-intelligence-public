package psa

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

type WebhookOutboundRepository struct {
	db      database
	newID   func() string
	secrets secrets.Provider
	legacy  webhooks.InboundSecretResolver
}

var (
	_ webhooks.OutboundQueue         = (*WebhookOutboundRepository)(nil)
	_ webhooks.DeliveryHistory       = (*WebhookOutboundRepository)(nil)
	_ webhooks.ManagementRepository  = (*WebhookOutboundRepository)(nil)
	_ webhooks.InboundSecretResolver = (*WebhookOutboundRepository)(nil)
)

func (r *WebhookOutboundRepository) ListManagedConnections(
	ctx context.Context,
	target scope.Target,
) ([]webhooks.ManagedConnection, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, COALESCE(client_id::text, ''), name, direction,
       COALESCE(endpoint_url, ''),
       (secret_ref IS NOT NULL OR secret_version IS NOT NULL), enabled,
       event_types, extract(epoch FROM retry_window)::bigint, version, updated_at
FROM webhook_connections
WHERE msp_id = $1 AND ($2 = '' OR client_id = $2::uuid)
ORDER BY name, id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]webhooks.ManagedConnection, 0)
	for rows.Next() {
		var connection webhooks.ManagedConnection
		if err := rows.Scan(
			&connection.ID, &connection.ClientID, &connection.Name,
			&connection.Direction, &connection.Endpoint,
			&connection.CredentialConfigured, &connection.Enabled,
			&connection.EventTypes, &connection.RetryWindowSeconds,
			&connection.Version, &connection.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, connection)
	}
	return result, rows.Err()
}

func (r *WebhookOutboundRepository) GetManagedConnection(
	ctx context.Context,
	target scope.Target,
	id string,
) (webhooks.ManagedConnection, error) {
	var connection webhooks.ManagedConnection
	err := r.db.QueryRow(ctx, `
SELECT id::text, COALESCE(client_id::text, ''), name, direction,
       COALESCE(endpoint_url, ''),
       (secret_ref IS NOT NULL OR secret_version IS NOT NULL), enabled,
       event_types, extract(epoch FROM retry_window)::bigint, version, updated_at
FROM webhook_connections
WHERE id = $1 AND msp_id = $2 AND ($3 = '' OR client_id = $3::uuid)
`, id, target.MSPID, target.ClientID).Scan(
		&connection.ID, &connection.ClientID, &connection.Name,
		&connection.Direction, &connection.Endpoint,
		&connection.CredentialConfigured, &connection.Enabled,
		&connection.EventTypes, &connection.RetryWindowSeconds,
		&connection.Version, &connection.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhooks.ManagedConnection{}, scope.ErrNotFound
	}
	return connection, err
}

func (r *WebhookOutboundRepository) CreateManagedConnection(
	ctx context.Context,
	accepted webhooks.ConnectionMutation,
) error {
	defer wipeWebhookSecret(accepted.PlaintextSecret)
	sealed, err := r.sealWebhookSecret(ctx, accepted.Connection.ID, accepted.PlaintextSecret)
	if err != nil {
		return err
	}
	return r.webhookTransaction(ctx, func(tx transaction) error {
		connection := accepted.Connection
		_, err := tx.Exec(ctx, `
INSERT INTO webhook_connections (
  id, msp_id, client_id, name, direction, endpoint_url, secret_ref,
  secret_version, secret_nonce, secret_ciphertext, enabled, event_types,
  retry_window, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, $5, NULLIF($6, ''), NULL,
  $7, $8, $9, $10, $11, ($12 * interval '1 second'), 1,
  $13, $14::uuid, $13, $14::uuid
)
`, connection.ID, accepted.Audit.MSPID, connection.ClientID, connection.Name,
			connection.Direction, connection.Endpoint, sealed.Version, sealed.Nonce,
			sealed.Ciphertext, connection.Enabled, connection.EventTypes,
			connection.RetryWindowSeconds, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WebhookOutboundRepository) UpdateManagedConnection(
	ctx context.Context,
	accepted webhooks.ConnectionMutation,
) error {
	return r.webhookTransaction(ctx, func(tx transaction) error {
		connection := accepted.Connection
		tag, err := tx.Exec(ctx, `
UPDATE webhook_connections
SET name = $4, endpoint_url = NULLIF($5, ''), enabled = $6,
    event_types = $7, retry_window = ($8 * interval '1 second'),
    version = version + 1, updated_at = $9, updated_by = $10::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, connection.ID, accepted.Audit.MSPID, accepted.ExpectedVersion,
			connection.Name, connection.Endpoint, connection.Enabled,
			connection.EventTypes, connection.RetryWindowSeconds,
			accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WebhookOutboundRepository) ReplaceManagedCredential(
	ctx context.Context,
	accepted webhooks.ConnectionMutation,
) error {
	defer wipeWebhookSecret(accepted.PlaintextSecret)
	sealed, err := r.sealWebhookSecret(ctx, accepted.Connection.ID, accepted.PlaintextSecret)
	if err != nil {
		return err
	}
	return r.webhookTransaction(ctx, func(tx transaction) error {
		tag, err := tx.Exec(ctx, `
UPDATE webhook_connections
SET secret_ref = NULL, secret_version = $4, secret_nonce = $5,
    secret_ciphertext = $6, version = version + 1,
    updated_at = $7, updated_by = $8::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.Connection.ID, accepted.Audit.MSPID, accepted.ExpectedVersion,
			sealed.Version, sealed.Nonce, sealed.Ciphertext,
			accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WebhookOutboundRepository) RetryManagedDelivery(
	ctx context.Context,
	accepted webhooks.DeliveryRetryMutation,
) error {
	return r.webhookTransaction(ctx, func(tx transaction) error {
		tag, err := tx.Exec(ctx, `
UPDATE webhook_event_deliveries delivery
SET state = 'pending', attempt_count = 0, first_attempt_at = $5,
    next_attempt_at = $5, delivered_at = NULL, failed_at = NULL,
    last_error_code = NULL, lease_until = NULL
FROM webhook_connections connection
WHERE delivery.connection_id = $1 AND delivery.event_id = $2
  AND delivery.state = 'failed'
  AND connection.id = delivery.connection_id
  AND connection.msp_id = $3
  AND ($4 = '' OR connection.client_id = $4::uuid)
`, accepted.ConnectionID, accepted.EventID, accepted.Audit.MSPID,
			accepted.Audit.ClientID, accepted.RetriedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *WebhookOutboundRepository) sealWebhookSecret(
	ctx context.Context,
	id string,
	plaintext []byte,
) (secrets.SealedValue, error) {
	if r.secrets == nil || len(plaintext) == 0 {
		return secrets.SealedValue{}, webhooks.ErrManagementUnavailable
	}
	return r.secrets.Seal(ctx, webhookSecretPurpose(id), plaintext)
}

func (r *WebhookOutboundRepository) webhookTransaction(
	ctx context.Context,
	fn func(transaction) error,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func webhookSecretPurpose(id string) string { return "webhook.connection." + id }

func wipeWebhookSecret(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func (r *WebhookOutboundRepository) ListManagedDeliveries(
	ctx context.Context,
	target scope.Target,
	limit int,
) ([]webhooks.ManagedDelivery, error) {
	rows, err := r.db.Query(ctx, `
SELECT delivery.connection_id::text, connection.name, delivery.event_id::text,
       event.event_type, delivery.state, delivery.attempt_count,
       delivery.first_attempt_at, delivery.next_attempt_at,
       delivery.delivered_at, delivery.failed_at,
       COALESCE(delivery.last_error_code, ''),
       attempt.attempted_at, COALESCE(attempt.http_status, 0),
       COALESCE(attempt.state, '')
FROM webhook_event_deliveries delivery
JOIN webhook_connections connection
  ON connection.id = delivery.connection_id AND connection.msp_id = delivery.msp_id
JOIN event_outbox event ON event.event_id = delivery.event_id
LEFT JOIN LATERAL (
  SELECT attempted_at, http_status, state
  FROM webhook_delivery_attempts
  WHERE connection_id = delivery.connection_id
    AND event_id = delivery.event_id::text
  ORDER BY attempt DESC
  LIMIT 1
) attempt ON true
WHERE delivery.msp_id = $1 AND ($2 = '' OR delivery.client_id = $2::uuid)
ORDER BY COALESCE(delivery.failed_at, delivery.next_attempt_at, delivery.delivered_at) DESC,
         delivery.event_id DESC
LIMIT $3
`, target.MSPID, target.ClientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]webhooks.ManagedDelivery, 0)
	for rows.Next() {
		var delivery webhooks.ManagedDelivery
		if err := rows.Scan(
			&delivery.ConnectionID, &delivery.Connection, &delivery.EventID,
			&delivery.EventType, &delivery.State, &delivery.AttemptCount,
			&delivery.FirstAttempt, &delivery.NextAttempt, &delivery.DeliveredAt,
			&delivery.FailedAt, &delivery.ErrorCode, &delivery.LastAttempt,
			&delivery.LastStatus, &delivery.LastState,
		); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func NewWebhookOutboundRepository(
	db database,
	newID func() string,
) *WebhookOutboundRepository {
	return &WebhookOutboundRepository{db: db, newID: newID}
}

func NewWebhookManagementRepository(
	db database,
	provider secrets.Provider,
	legacy webhooks.InboundSecretResolver,
	newID func() string,
) *WebhookOutboundRepository {
	return &WebhookOutboundRepository{
		db: db, secrets: provider, legacy: legacy, newID: newID,
	}
}

func (r *WebhookOutboundRepository) Resolve(
	ctx context.Context,
	reference string,
) ([]byte, error) {
	const prefix = "db://webhook/"
	if !strings.HasPrefix(reference, prefix) {
		if r.legacy == nil {
			return nil, webhooks.ErrManagementUnavailable
		}
		return r.legacy.Resolve(ctx, reference)
	}
	parts := strings.Split(strings.TrimPrefix(reference, prefix), "/")
	if len(parts) != 2 {
		return nil, webhooks.ErrManagementUnavailable
	}
	id := parts[0]
	version, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || version < 1 {
		return nil, webhooks.ErrManagementUnavailable
	}
	var legacyReference *string
	var secretVersion *int
	var sealed secrets.SealedValue
	err = r.db.QueryRow(ctx, `
SELECT secret_ref, secret_version, secret_nonce, secret_ciphertext
FROM webhook_connections
WHERE id = $1 AND version = $2 AND enabled
	`, id, version).Scan(
		&legacyReference, &secretVersion, &sealed.Nonce, &sealed.Ciphertext,
	)
	if err != nil {
		return nil, webhooks.ErrManagementUnavailable
	}
	if legacyReference != nil {
		if r.legacy == nil {
			return nil, webhooks.ErrManagementUnavailable
		}
		return r.legacy.Resolve(ctx, *legacyReference)
	}
	if r.secrets == nil {
		return nil, webhooks.ErrManagementUnavailable
	}
	if secretVersion == nil {
		return nil, webhooks.ErrManagementUnavailable
	}
	sealed.Version = *secretVersion
	return r.secrets.Open(ctx, webhookSecretPurpose(id), sealed)
}

func (r *WebhookOutboundRepository) Plan(
	ctx context.Context,
	limit int,
	now time.Time,
) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `
WITH candidates AS MATERIALIZED (
  SELECT event.event_id, event.event_type, event.msp_id, event.client_id
  FROM event_outbox event
  LEFT JOIN webhook_event_plans plan ON plan.event_id = event.event_id
  WHERE plan.event_id IS NULL
  ORDER BY event.occurred_at, event.event_id
  LIMIT $1
  FOR UPDATE OF event SKIP LOCKED
),
deliveries AS (
  INSERT INTO webhook_event_deliveries (
    connection_id, event_id, msp_id, client_id, state,
    retry_window_seconds, first_attempt_at, next_attempt_at, created_at
  )
  SELECT connection.id, event.event_id, event.msp_id, event.client_id,
         'pending', extract(epoch FROM connection.retry_window)::integer,
         $2, $2, $2
  FROM candidates event
  JOIN webhook_connections connection
    ON connection.msp_id = event.msp_id
   AND (connection.client_id IS NULL OR connection.client_id = event.client_id)
   AND connection.enabled
   AND connection.direction IN ('outbound', 'bidirectional')
   AND connection.endpoint_url IS NOT NULL
   AND event.event_type = ANY(connection.event_types)
  ON CONFLICT (connection_id, event_id) DO NOTHING
  RETURNING event_id
)
INSERT INTO webhook_event_plans (event_id, planned_at)
SELECT event_id, $2 FROM candidates
ON CONFLICT (event_id) DO NOTHING
`, limit, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *WebhookOutboundRepository) Claim(
	ctx context.Context,
	limit int,
	now time.Time,
	lease time.Duration,
) ([]webhooks.OutboundJob, error) {
	rows, err := r.db.Query(ctx, `
WITH candidates AS (
  SELECT delivery.connection_id, delivery.event_id
  FROM webhook_event_deliveries delivery
  JOIN webhook_connections connection
    ON connection.id = delivery.connection_id
   AND connection.msp_id = delivery.msp_id
   AND connection.enabled
   AND connection.direction IN ('outbound', 'bidirectional')
  WHERE delivery.state = 'pending'
    AND delivery.next_attempt_at <= $1
    AND (delivery.lease_until IS NULL OR delivery.lease_until <= $1)
  ORDER BY delivery.next_attempt_at, delivery.connection_id, delivery.event_id
  LIMIT $2
  FOR UPDATE OF delivery SKIP LOCKED
),
leased AS (
  UPDATE webhook_event_deliveries delivery
  SET lease_until = $1 + ($3 * interval '1 microsecond'),
      attempt_count = delivery.attempt_count + 1
  FROM candidates
  WHERE delivery.connection_id = candidates.connection_id
    AND delivery.event_id = candidates.event_id
  RETURNING delivery.*
)
SELECT connection.id::text, connection.msp_id::text,
       COALESCE(connection.client_id::text, ''), connection.endpoint_url,
       'db://webhook/' || connection.id::text || '/' || connection.version::text,
       event.event_id::text, event.event_type,
       convert_to(jsonb_build_object(
         'event_id', event.event_id,
         'event_type', event.event_type,
         'schema_version', event.schema_version,
         'occurred_at', event.occurred_at,
         'msp_id', event.msp_id,
         'client_id', event.client_id,
         'subject_type', event.subject_type,
         'subject_id', event.subject_id,
         'subject_version', event.subject_version,
         'correlation_id', event.correlation_id,
         'data', event.data
       )::text, 'UTF8'),
       leased.attempt_count, leased.first_attempt_at,
       leased.retry_window_seconds
FROM leased
JOIN webhook_connections connection
  ON connection.id = leased.connection_id
 AND connection.msp_id = leased.msp_id
JOIN event_outbox event ON event.event_id = leased.event_id
`, now, limit, leaseMicroseconds(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]webhooks.OutboundJob, 0)
	for rows.Next() {
		var (
			job                webhooks.OutboundJob
			retryWindowSeconds int64
		)
		if err := rows.Scan(
			&job.Connection.ID, &job.Connection.MSPID,
			&job.Connection.ClientID, &job.Connection.Endpoint,
			&job.Connection.SecretRef,
			&job.Event.ID, &job.Event.Type, &job.Event.Body,
			&job.Attempt, &job.FirstAttemptAt, &retryWindowSeconds,
		); err != nil {
			return nil, err
		}
		job.RetryWindow = time.Duration(retryWindowSeconds) * time.Second
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *WebhookOutboundRepository) MarkDelivered(
	ctx context.Context,
	completion webhooks.OutboundCompletion,
) error {
	return r.updateCompletion(ctx, `
UPDATE webhook_event_deliveries
SET state = 'delivered', delivered_at = $4, failed_at = NULL,
    last_error_code = NULL, lease_until = NULL
WHERE connection_id = $1 AND event_id = $2
  AND state = 'pending' AND attempt_count = $3
`, completion.ConnectionID, completion.EventID, completion.Attempt,
		completion.CompletedAt)
}

func (r *WebhookOutboundRepository) MarkRetry(
	ctx context.Context,
	completion webhooks.OutboundCompletion,
) error {
	return r.updateCompletion(ctx, `
UPDATE webhook_event_deliveries
SET state = 'pending', next_attempt_at = $5, last_error_code = $6,
    lease_until = NULL
WHERE connection_id = $1 AND event_id = $2
  AND state = 'pending' AND attempt_count = $3
`, completion.ConnectionID, completion.EventID, completion.Attempt,
		completion.CompletedAt, completion.NextAttemptAt, completion.ErrorCode)
}

func (r *WebhookOutboundRepository) MarkFailed(
	ctx context.Context,
	completion webhooks.OutboundCompletion,
) error {
	return r.updateCompletion(ctx, `
UPDATE webhook_event_deliveries
SET state = 'failed', failed_at = $4, delivered_at = NULL,
    last_error_code = $5, lease_until = NULL
WHERE connection_id = $1 AND event_id = $2
  AND state = 'pending' AND attempt_count = $3
`, completion.ConnectionID, completion.EventID, completion.Attempt,
		completion.CompletedAt, completion.ErrorCode)
}

func (r *WebhookOutboundRepository) updateCompletion(
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
		return webhooks.ErrDeliveryStateConflict
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func (r *WebhookOutboundRepository) Record(
	ctx context.Context,
	attempt webhooks.DeliveryAttempt,
) error {
	if r.newID == nil {
		return webhooks.ErrDeliveryFailed
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO webhook_delivery_attempts (
  id, connection_id, msp_id, client_id, event_id, attempt,
  attempted_at, state, http_status, error_code
)
SELECT $1, connection.id, connection.msp_id, connection.client_id,
       $3, $4, $5, $6, NULLIF($7, 0), NULLIF($8, '')
FROM webhook_connections connection
WHERE connection.id = $2
ON CONFLICT (connection_id, event_id, attempt) DO NOTHING
`, r.newID(), attempt.ConnectionID, attempt.EventID, attempt.Attempt,
		attempt.AttemptedAt, attempt.State, attempt.HTTPStatus,
		attempt.ErrorCode); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
