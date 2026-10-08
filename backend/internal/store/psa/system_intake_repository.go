package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type SystemIntakeRepository struct {
	db database
}

var _ intake.SystemRepository = (*SystemIntakeRepository)(nil)

func NewSystemIntakeRepository(db database) *SystemIntakeRepository {
	return &SystemIntakeRepository{db: db}
}

func (r *SystemIntakeRepository) FindSystemEvent(
	ctx context.Context,
	target scope.Target,
	source intake.Source,
	externalID string,
) (intake.InboundEvent, error) {
	var event intake.InboundEvent
	err := r.db.QueryRow(ctx, `
SELECT event.id::text, event.msp_id::text, event.client_id::text,
       event.source, event.external_id, event.received_at,
       event.authentication_result, event.raw_payload_ref,
       event.processing_state, COALESCE(event.quarantine_reason, '')
FROM inbound_events event
WHERE event.msp_id = $1
  AND event.client_id = $2::uuid
  AND event.source = $3
  AND event.external_id = $4
`, target.MSPID, target.ClientID, source, externalID).Scan(
		&event.ID, &event.MSPID, &event.ClientID, &event.Source,
		&event.ExternalID, &event.ReceivedAt, &event.AuthenticationResult,
		&event.RawPayloadRef, &event.ProcessingState, &event.QuarantineReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return intake.InboundEvent{}, scope.ErrNotFound
	}
	if err != nil {
		return intake.InboundEvent{}, err
	}
	return event, nil
}

func (r *SystemIntakeRepository) CreateSystemEventAtomic(
	ctx context.Context,
	accepted intake.SystemMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	event := accepted.Inbound
	if _, err := tx.Exec(ctx, `
INSERT INTO inbound_events (
  id, msp_id, client_id, source, external_id, received_at,
  authentication_result, raw_payload_ref, processing_state,
  quarantine_reason, version
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), 1
)
`, event.ID, event.MSPID, event.ClientID, event.Source, event.ExternalID,
		event.ReceivedAt, event.AuthenticationResult, event.RawPayloadRef,
		event.ProcessingState, event.QuarantineReason); err != nil {
		_ = tx.Rollback(ctx)
		return normalizeSystemIntakeWriteError(err)
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func normalizeSystemIntakeWriteError(err error) error {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) &&
		postgres.Code == "23505" &&
		(postgres.ConstraintName == "inbound_events_client_external_id_idx" ||
			postgres.ConstraintName == "inbound_events_global_external_id_idx") {
		return intake.ErrDuplicateSystemEvent
	}
	return err
}
