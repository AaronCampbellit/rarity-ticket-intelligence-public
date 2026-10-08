package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

type WebhookInboundRepository struct {
	db database
}

var _ webhooks.InboundRepository = (*WebhookInboundRepository)(nil)

func NewWebhookInboundRepository(db database) *WebhookInboundRepository {
	return &WebhookInboundRepository{db: db}
}

func (r *WebhookInboundRepository) LoadInboundConnection(
	ctx context.Context,
	connectionID string,
) (webhooks.InboundConnection, error) {
	var connection webhooks.InboundConnection
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''),
       'db://webhook/' || id::text || '/' || version::text, direction, enabled,
       version
FROM webhook_connections
WHERE id = CASE
  WHEN $1 ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  THEN $1::uuid
END
`, connectionID).Scan(
		&connection.ID, &connection.MSPID, &connection.ClientID,
		&connection.SecretRef, &connection.Direction, &connection.Enabled,
		&connection.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhooks.InboundConnection{}, scope.ErrNotFound
	}
	if err != nil {
		return webhooks.InboundConnection{}, err
	}
	return connection, nil
}

func (r *WebhookInboundRepository) CreateInboundAtomic(
	ctx context.Context,
	accepted webhooks.InboundMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	event := accepted.System.Inbound
	tag, err := tx.Exec(ctx, `
INSERT INTO webhook_replay_claims (
  connection_id, event_id, expires_at
) SELECT connection.id, $2, $3
FROM webhook_connections connection
WHERE connection.id = $1
  AND connection.msp_id = $4
  AND connection.client_id = $5
  AND connection.enabled
  AND connection.direction IN ('inbound', 'bidirectional')
  AND connection.version = $6
`, accepted.ConnectionID, event.ExternalID, accepted.ReplayExpiresAt,
		event.MSPID, event.ClientID, accepted.ConnectionVersion)
	if err != nil {
		_ = tx.Rollback(ctx)
		return normalizeWebhookInboundWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO inbound_events (
  id, msp_id, client_id, connection_id, source, external_id, received_at,
  authentication_result, raw_payload_ref, processing_state,
  quarantine_reason, version
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), 1
)
`, event.ID, event.MSPID, event.ClientID, event.ConnectionID,
		event.Source, event.ExternalID,
		event.ReceivedAt, event.AuthenticationResult, event.RawPayloadRef,
		event.ProcessingState, event.QuarantineReason); err != nil {
		_ = tx.Rollback(ctx)
		return normalizeWebhookInboundWriteError(err)
	}
	if err := writeMutationFacts(
		ctx, tx, accepted.System.Audit, accepted.System.Event,
	); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func normalizeWebhookInboundWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" &&
		(postgres.ConstraintName == "webhook_replay_claims_pkey" ||
			postgres.ConstraintName == "inbound_events_webhook_external_id_idx") {
		return webhooks.ErrReplay
	}
	return err
}
