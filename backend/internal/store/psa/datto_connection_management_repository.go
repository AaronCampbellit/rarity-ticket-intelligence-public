package psa

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

func (r *DattoRepository) WithLegacyCredentialResolver(
	resolver datto.CredentialResolver,
) *DattoRepository {
	r.legacyCredentials = resolver
	return r
}

func (r *DattoRepository) ListManagedConnections(
	ctx context.Context,
	mspID string,
) ([]datto.ManagedConnection, error) {
	rows, err := r.db.Query(ctx, dattoManagedConnectionSelect+`
WHERE msp_id = $1
ORDER BY name, id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]datto.ManagedConnection, 0)
	for rows.Next() {
		item, err := scanDattoManagedConnection(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *DattoRepository) GetManagedConnection(
	ctx context.Context,
	mspID string,
	id string,
) (datto.ManagedConnection, error) {
	item, err := scanDattoManagedConnection(r.db.QueryRow(ctx, dattoManagedConnectionSelect+`
WHERE id = $1 AND msp_id = $2
`, id, mspID))
	if errors.Is(err, pgx.ErrNoRows) {
		return datto.ManagedConnection{}, scope.ErrNotFound
	}
	return item, err
}

const dattoManagedConnectionSelect = `
SELECT id::text, name, COALESCE(api_url, ''),
       (credential_secret_ref IS NOT NULL OR credential_secret_version IS NOT NULL),
       extract(epoch FROM sync_interval)::bigint,
       enabled, health_state, last_completed_at, COALESCE(last_error_code, ''),
       version, updated_at
FROM datto_connections
`

func scanDattoManagedConnection(
	scanner interface{ Scan(...any) error },
) (datto.ManagedConnection, error) {
	var item datto.ManagedConnection
	err := scanner.Scan(
		&item.ID, &item.Name, &item.APIURL, &item.CredentialConfigured,
		&item.SyncIntervalSeconds, &item.Enabled, &item.HealthState,
		&item.LastCompletedAt, &item.LastErrorCode, &item.Version, &item.UpdatedAt,
	)
	return item, err
}

func (r *DattoRepository) CreateManagedConnection(
	ctx context.Context,
	accepted datto.ConnectionMutation,
) error {
	defer wipeDattoProtected(accepted.ProtectedCredential)
	sealed, err := r.sealDattoCredential(
		ctx, accepted.Connection.ID, accepted.ProtectedCredential,
	)
	if err != nil {
		return err
	}
	return r.dattoManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Connection
		_, err := tx.Exec(ctx, `
INSERT INTO datto_connections (
  id, msp_id, name, credential_secret_ref,
  credential_secret_version, credential_secret_nonce,
  credential_secret_ciphertext, api_url, sync_interval, enabled,
  health_state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, NULL, $4, $5, $6, $7,
  ($8 * interval '1 second'), $9, $10, 1, $11, $12::uuid, $11, $12::uuid
)
`, item.ID, accepted.MSPID, item.Name, sealed.Version, sealed.Nonce,
			sealed.Ciphertext, item.APIURL, item.SyncIntervalSeconds,
			item.Enabled, item.HealthState, accepted.Audit.OccurredAt,
			accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *DattoRepository) UpdateManagedConnection(
	ctx context.Context,
	accepted datto.ConnectionMutation,
) error {
	return r.dattoManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Connection
		tag, err := tx.Exec(ctx, `
UPDATE datto_connections
SET name = $4, sync_interval = ($5 * interval '1 second'),
    enabled = $6, health_state = $7, version = version + 1,
    configuration_generation = configuration_generation + 1,
    lease_until = NULL, updated_at = $8, updated_by = $9::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, item.ID, accepted.MSPID, accepted.ExpectedVersion, item.Name,
			item.SyncIntervalSeconds, item.Enabled, item.HealthState,
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

func (r *DattoRepository) ReplaceManagedCredential(
	ctx context.Context,
	accepted datto.ConnectionMutation,
) error {
	defer wipeDattoProtected(accepted.ProtectedCredential)
	sealed, err := r.sealDattoCredential(
		ctx, accepted.Connection.ID, accepted.ProtectedCredential,
	)
	if err != nil {
		return err
	}
	return r.dattoManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Connection
		tag, err := tx.Exec(ctx, `
UPDATE datto_connections
SET credential_secret_ref = NULL, credential_secret_version = $4,
    credential_secret_nonce = $5, credential_secret_ciphertext = $6,
    api_url = $7, version = version + 1,
    configuration_generation = configuration_generation + 1,
    lease_until = NULL, health_state = 'pending',
    last_error_code = NULL, updated_at = $8, updated_by = $9::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, item.ID, accepted.MSPID, accepted.ExpectedVersion, sealed.Version,
			sealed.Nonce, sealed.Ciphertext, item.APIURL,
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

func (r *DattoRepository) Resolve(
	ctx context.Context,
	reference string,
) ([]byte, error) {
	id, generation, ok := parseDattoReference(reference)
	if !ok {
		if r.legacyCredentials != nil {
			return r.legacyCredentials.Resolve(ctx, reference)
		}
		return nil, datto.ErrInvalidSourceCredentials
	}
	var legacy *string
	var keyVersion *int
	var nonce, ciphertext []byte
	err := r.db.QueryRow(ctx, `
SELECT credential_secret_ref, credential_secret_version,
       credential_secret_nonce, credential_secret_ciphertext
FROM datto_connections
WHERE id = $1 AND configuration_generation = $2 AND enabled
`, id, generation).Scan(&legacy, &keyVersion, &nonce, &ciphertext)
	if err != nil {
		return nil, datto.ErrInvalidSourceCredentials
	}
	if legacy != nil {
		if r.legacyCredentials == nil {
			return nil, datto.ErrInvalidSourceCredentials
		}
		return r.legacyCredentials.Resolve(ctx, *legacy)
	}
	if keyVersion == nil || r.secrets == nil {
		return nil, datto.ErrInvalidSourceCredentials
	}
	return r.secrets.Open(
		ctx, dattoCredentialPurpose(id),
		secrets.SealedValue{
			Version: *keyVersion, Nonce: nonce, Ciphertext: ciphertext,
		},
	)
}

func (r *DattoRepository) sealDattoCredential(
	ctx context.Context,
	id string,
	value []byte,
) (secrets.SealedValue, error) {
	if r.secrets == nil || len(value) == 0 {
		return secrets.SealedValue{}, datto.ErrInvalidConnectionManagement
	}
	return r.secrets.Seal(ctx, dattoCredentialPurpose(id), value)
}

func (r *DattoRepository) dattoManagementTransaction(
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

func parseDattoReference(reference string) (string, int64, bool) {
	const prefix = "db://datto/"
	if !strings.HasPrefix(reference, prefix) {
		return "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(reference, prefix), "/")
	if len(parts) != 3 || parts[2] != "credential" {
		return "", 0, false
	}
	generation, err := strconv.ParseInt(parts[1], 10, 64)
	return parts[0], generation, err == nil && generation > 0
}

func dattoCredentialPurpose(id string) string {
	return "datto.connection.credential." + id
}

func wipeDattoProtected(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
