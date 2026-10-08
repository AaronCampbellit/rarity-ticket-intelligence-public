package authn

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/servicekeys"
)

type ServiceKeyRepository struct {
	pool *pgxpool.Pool
}

var _ servicekeys.Repository = (*ServiceKeyRepository)(nil)

func NewServiceKeyRepository(pool *pgxpool.Pool) *ServiceKeyRepository {
	return &ServiceKeyRepository{pool: pool}
}

func (r *ServiceKeyRepository) Create(
	ctx context.Context,
	accepted servicekeys.IssueMutation,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertServiceKey(ctx, tx, accepted.Record, nil); err != nil {
		return err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ServiceKeyRepository) List(
	ctx context.Context,
	target scope.Target,
) ([]servicekeys.Record, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), name,
       key_prefix, capabilities, data_scopes, created_by::text,
       created_at, expires_at, revoked_at
FROM service_api_keys
WHERE msp_id = $1
  AND (NULLIF($2, '')::uuid IS NULL OR client_id = NULLIF($2, '')::uuid)
ORDER BY revoked_at NULLS FIRST, created_at DESC, id DESC
LIMIT 250
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]servicekeys.Record, 0)
	for rows.Next() {
		var record servicekeys.Record
		if err := rows.Scan(
			&record.ID, &record.MSPID, &record.ClientID, &record.Name,
			&record.Prefix, &record.Capabilities, &record.DataScopes,
			&record.CreatedBy, &record.CreatedAt, &record.ExpiresAt,
			&record.RevokedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *ServiceKeyRepository) FindByPrefix(
	ctx context.Context,
	prefix string,
) (servicekeys.Record, error) {
	var record servicekeys.Record
	var digest []byte
	err := r.pool.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), name,
       key_prefix, token_hash, capabilities, data_scopes,
       created_by::text, created_at, expires_at, revoked_at
FROM service_api_keys
WHERE key_prefix = $1
`, prefix).Scan(
		&record.ID, &record.MSPID, &record.ClientID, &record.Name,
		&record.Prefix, &digest, &record.Capabilities, &record.DataScopes,
		&record.CreatedBy, &record.CreatedAt, &record.ExpiresAt,
		&record.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && len(digest) != len(record.TokenHash) {
		return servicekeys.Record{}, servicekeys.ErrInvalidKey
	}
	if err != nil {
		return servicekeys.Record{}, err
	}
	copy(record.TokenHash[:], digest)
	return record, nil
}

func (r *ServiceKeyRepository) FindByID(
	ctx context.Context,
	target scope.Target,
	id string,
) (servicekeys.Record, error) {
	var record servicekeys.Record
	var digest []byte
	err := r.pool.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), name,
       key_prefix, token_hash, capabilities, data_scopes,
       created_by::text, created_at, expires_at, revoked_at
FROM service_api_keys
WHERE id = $1 AND msp_id = $2
  AND (NULLIF($3, '')::uuid IS NULL OR client_id = NULLIF($3, '')::uuid)
`, id, target.MSPID, target.ClientID).Scan(
		&record.ID, &record.MSPID, &record.ClientID, &record.Name,
		&record.Prefix, &digest, &record.Capabilities, &record.DataScopes,
		&record.CreatedBy, &record.CreatedAt, &record.ExpiresAt,
		&record.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && len(digest) != len(record.TokenHash) {
		return servicekeys.Record{}, servicekeys.ErrInvalidKey
	}
	if err != nil {
		return servicekeys.Record{}, err
	}
	copy(record.TokenHash[:], digest)
	return record, nil
}

func (r *ServiceKeyRepository) Revoke(
	ctx context.Context,
	accepted servicekeys.RevokeMutation,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE service_api_keys
SET revoked_at = $3, version = version + 1
WHERE id = $1 AND key_prefix = $2 AND revoked_at IS NULL
`, accepted.KeyID, accepted.Prefix, accepted.RevokedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return servicekeys.ErrInvalidKey
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ServiceKeyRepository) Rotate(
	ctx context.Context,
	accepted servicekeys.RotateMutation,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE service_api_keys
SET revoked_at = $3, version = version + 1
WHERE id = $1 AND key_prefix = $2 AND revoked_at IS NULL
`, accepted.OldKeyID, accepted.OldPrefix, accepted.RotatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return servicekeys.ErrInvalidKey
	}
	if err := insertServiceKey(ctx, tx, accepted.Replacement, accepted.OldKeyID); err != nil {
		return err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertServiceKey(
	ctx context.Context,
	tx pgx.Tx,
	record servicekeys.Record,
	rotatedFrom any,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO service_api_keys (
  id, msp_id, client_id, name, key_prefix, token_hash,
  capabilities, data_scopes, created_at, created_by, expires_at,
  rotated_from_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, record.ID, record.MSPID, nullableAuthID(record.ClientID), record.Name,
		record.Prefix, record.TokenHash[:], record.Capabilities,
		record.DataScopes, record.CreatedAt, record.CreatedBy,
		record.ExpiresAt, rotatedFrom)
	return err
}

func writeAuthFacts(
	ctx context.Context,
	tx pgx.Tx,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_ledger (
  id, occurred_at, msp_id, client_id, actor_type, actor_id, action,
  subject_type, subject_id, subject_version, source, reason, correlation_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`, audit.ID, audit.OccurredAt, audit.MSPID, nullableAuthID(audit.ClientID),
		audit.ActorType, audit.ActorID, audit.Action, audit.SubjectType,
		audit.SubjectID, audit.SubjectVersion, audit.Source,
		nullableAuthText(audit.Reason), audit.CorrelationID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
INSERT INTO event_outbox (
  event_id, event_type, schema_version, occurred_at, msp_id, client_id,
  actor_type, actor_id, subject_type, subject_id, subject_version,
  correlation_id, source, data
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '{}'::jsonb)
`, event.EventID, event.EventType, event.SchemaVersion, event.OccurredAt,
		event.MSPID, nullableAuthID(event.ClientID), event.ActorType,
		event.ActorID, event.SubjectType, event.SubjectID,
		event.SubjectVersion, event.CorrelationID, event.Source)
	return err
}

func nullableAuthID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableAuthText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
