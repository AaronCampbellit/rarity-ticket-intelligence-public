package psa

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ForwardingRepository struct {
	db database
}

var _ intake.ForwardingRepository = (*ForwardingRepository)(nil)
var _ intake.ForwardingManagementRepository = (*ForwardingRepository)(nil)

func NewForwardingRepository(db database) *ForwardingRepository {
	return &ForwardingRepository{db: db}
}

func (r *ForwardingRepository) ListManagedForwarding(
	ctx context.Context,
	mspID string,
) ([]intake.ManagedForwardingConnection, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, intake_address, allowed_sender_domains, max_message_bytes,
       rate_limit_per_minute, enabled, health_state, last_received_at,
       COALESCE(last_error_code, ''), version, updated_at
FROM forwarding_intake_connections
WHERE msp_id = $1
ORDER BY intake_address, id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]intake.ManagedForwardingConnection, 0)
	for rows.Next() {
		connection, err := scanManagedForwarding(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, connection)
	}
	return result, rows.Err()
}

func (r *ForwardingRepository) GetManagedForwarding(
	ctx context.Context,
	mspID string,
	id string,
) (intake.ManagedForwardingConnection, error) {
	connection, err := scanManagedForwarding(r.db.QueryRow(ctx, `
SELECT id::text, intake_address, allowed_sender_domains, max_message_bytes,
       rate_limit_per_minute, enabled, health_state, last_received_at,
       COALESCE(last_error_code, ''), version, updated_at
FROM forwarding_intake_connections
WHERE id = $1 AND msp_id = $2
`, id, mspID))
	if errors.Is(err, pgx.ErrNoRows) {
		return intake.ManagedForwardingConnection{}, scope.ErrNotFound
	}
	return connection, err
}

func (r *ForwardingRepository) CreateManagedForwarding(
	ctx context.Context,
	accepted intake.ForwardingManagementMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	connection := accepted.Connection
	_, err = tx.Exec(ctx, `
INSERT INTO forwarding_intake_connections (
  id, msp_id, intake_address, allowed_sender_domains, max_message_bytes,
  rate_limit_per_minute, enabled, health_state, version,
  created_at, created_by, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10::uuid, $9, $10::uuid)
`, connection.ID, accepted.MSPID, connection.IntakeAddress,
		connection.AllowedSenderDomains, connection.MaxMessageBytes,
		connection.RateLimitPerMinute, connection.Enabled,
		connection.HealthState, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err == nil {
		err = writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (r *ForwardingRepository) UpdateManagedForwarding(
	ctx context.Context,
	accepted intake.ForwardingManagementMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	connection := accepted.Connection
	tag, err := tx.Exec(ctx, `
UPDATE forwarding_intake_connections
SET intake_address = $4, allowed_sender_domains = $5,
    max_message_bytes = $6, rate_limit_per_minute = $7,
    enabled = $8, health_state = $9, version = version + 1,
    updated_at = $10, updated_by = $11::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, connection.ID, accepted.MSPID, accepted.ExpectedVersion,
		connection.IntakeAddress, connection.AllowedSenderDomains,
		connection.MaxMessageBytes, connection.RateLimitPerMinute,
		connection.Enabled, connection.HealthState,
		accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err == nil && tag.RowsAffected() != 1 {
		err = object.ErrVersionConflict
	}
	if err == nil {
		err = writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func scanManagedForwarding(scanner interface{ Scan(...any) error }) (intake.ManagedForwardingConnection, error) {
	var connection intake.ManagedForwardingConnection
	err := scanner.Scan(
		&connection.ID, &connection.IntakeAddress,
		&connection.AllowedSenderDomains, &connection.MaxMessageBytes,
		&connection.RateLimitPerMinute, &connection.Enabled,
		&connection.HealthState, &connection.LastReceivedAt,
		&connection.LastErrorCode, &connection.Version, &connection.UpdatedAt,
	)
	return connection, err
}

func (r *ForwardingRepository) LoadForwardingConnection(
	ctx context.Context,
	target scope.Target,
	connectionID string,
) (intake.ForwardingConnection, error) {
	var connection intake.ForwardingConnection
	err := r.db.QueryRow(ctx, `
SELECT connection.id::text, connection.msp_id::text,
       connection.intake_address, connection.allowed_sender_domains,
       connection.max_message_bytes, connection.rate_limit_per_minute,
       connection.enabled, connection.version
FROM forwarding_intake_connections connection
WHERE connection.id = CASE
  WHEN $1 ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  THEN $1::uuid
END
  AND connection.msp_id = $2
`, connectionID, target.MSPID).Scan(
		&connection.ID, &connection.MSPID, &connection.IntakeAddress,
		&connection.AllowedSenderDomains, &connection.MaxMessageBytes,
		&connection.RateLimitPerMinute, &connection.Enabled, &connection.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return intake.ForwardingConnection{}, scope.ErrNotFound
	}
	if err != nil {
		return intake.ForwardingConnection{}, err
	}
	return connection, nil
}

func (r *ForwardingRepository) FindForwardingEvent(
	ctx context.Context,
	target scope.Target,
	connectionID string,
	externalID string,
) (intake.InboundEvent, error) {
	var event intake.InboundEvent
	err := r.db.QueryRow(ctx, `
SELECT event.id::text, event.msp_id::text,
       event.forwarding_connection_id::text, event.source,
       event.external_id, event.received_at, event.authentication_result,
       event.raw_payload_ref, event.processing_state,
       COALESCE(event.quarantine_reason, ''),
       COALESCE(event.normalized_payload->>'sender', ''),
       COALESCE(event.normalized_payload->>'recipient', '')
FROM inbound_events event
WHERE event.msp_id = $1
  AND event.forwarding_connection_id = $2
  AND event.source = 'forwarded_email'
  AND event.external_id = $3
`, target.MSPID, connectionID, externalID).Scan(
		&event.ID, &event.MSPID, &event.ForwardingConnectionID,
		&event.Source, &event.ExternalID, &event.ReceivedAt,
		&event.AuthenticationResult, &event.RawPayloadRef,
		&event.ProcessingState, &event.QuarantineReason,
		&event.Sender, &event.Recipient,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return intake.InboundEvent{}, scope.ErrNotFound
	}
	if err != nil {
		return intake.InboundEvent{}, err
	}
	return event, nil
}

func (r *ForwardingRepository) AllowForwarding(
	ctx context.Context,
	connectionID string,
	senderKey string,
	limit int,
	now time.Time,
) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM forwarding_rate_limit_windows
WHERE window_started_at < $1
`, now.Add(-5*time.Minute)); err != nil {
		_ = tx.Rollback(ctx)
		return false, err
	}
	window := now.Truncate(time.Minute)
	for _, key := range []string{"_all", senderKey} {
		tag, err := tx.Exec(ctx, `
INSERT INTO forwarding_rate_limit_windows (
  connection_id, sender_key, window_started_at, message_count
) VALUES ($1, $2, $3, 1)
ON CONFLICT (connection_id, sender_key, window_started_at)
DO UPDATE SET message_count =
  forwarding_rate_limit_windows.message_count + 1
WHERE forwarding_rate_limit_windows.message_count < $4
`, connectionID, key, window, limit)
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if tag.RowsAffected() != 1 {
			_ = tx.Rollback(ctx)
			return false, nil
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return false, err
	}
	return true, nil
}

func (r *ForwardingRepository) CreateForwardingAtomic(
	ctx context.Context,
	accepted intake.ForwardingMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	event := accepted.Inbound
	tag, err := tx.Exec(ctx, `
INSERT INTO inbound_events (
  id, msp_id, client_id, forwarding_connection_id,
  source, external_id, received_at, authentication_result,
  raw_payload_ref, normalized_payload, processing_state,
  quarantine_reason, version
) SELECT
  $1, $2, NULL, connection.id, $4, $5, $6, $7, $8,
  jsonb_build_object('sender', $9::text, 'recipient', $10::text),
  $11, NULLIF($12, ''), 1
FROM forwarding_intake_connections connection
WHERE connection.id = $3
  AND connection.msp_id = $2
  AND connection.enabled
  AND connection.version = $13
`, event.ID, event.MSPID, event.ForwardingConnectionID,
		event.Source, event.ExternalID, event.ReceivedAt,
		event.AuthenticationResult, event.RawPayloadRef,
		event.Sender, event.Recipient, event.ProcessingState,
		event.QuarantineReason, accepted.ConnectionVersion)
	if err != nil {
		_ = tx.Rollback(ctx)
		return normalizeForwardingWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE forwarding_intake_connections
SET last_received_at = $3, health_state = 'healthy',
    last_error_code = NULL, updated_at = $3
WHERE id = $1 AND msp_id = $2
`, event.ForwardingConnectionID, event.MSPID, event.ReceivedAt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func normalizeForwardingWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" &&
		postgres.ConstraintName == "inbound_events_forwarding_external_id_idx" {
		return intake.ErrDuplicateForwardingEvent
	}
	return err
}
