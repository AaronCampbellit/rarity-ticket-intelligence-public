package psa

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

// TeamsConnectionRepository is the protected Teams control plane.  Delivery
// history remains in NotificationRepository so its 24-hour retry contract is
// unaffected by management credential changes.
type TeamsConnectionRepository struct {
	db      database
	secrets secrets.Provider
	legacy  notifications.TeamsSecretResolver
}

var _ notifications.TeamsConnectionManagementRepository = (*TeamsConnectionRepository)(nil)
var _ notifications.TeamsSecretResolver = (*TeamsConnectionRepository)(nil)

func NewTeamsConnectionRepository(db database, provider secrets.Provider, legacy notifications.TeamsSecretResolver) *TeamsConnectionRepository {
	return &TeamsConnectionRepository{db: db, secrets: provider, legacy: legacy}
}

func (r *TeamsConnectionRepository) CreateTeamsConnection(ctx context.Context, accepted notifications.TeamsConnectionMutation) error {
	defer wipeTeamsCredential(accepted.PlaintextWebhookURL)
	if r == nil || r.db == nil || r.secrets == nil {
		return notifications.ErrTeamsCredentialUnavailable
	}
	sealed, err := r.secrets.Seal(ctx, teamsCredentialPurpose(accepted.Connection.ID), accepted.PlaintextWebhookURL)
	if err != nil {
		return err
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		connection := accepted.Connection
		if err := lockTeamsConnection(ctx, tx, connection.MSPID, connection.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO teams_connections (
  id, msp_id, client_id, name, webhook_secret_ref,
  webhook_secret_version, webhook_secret_nonce, webhook_secret_ciphertext,
  enabled, health_state, last_tested_at, last_success_at, last_error_code,
  version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, NULLIF($3, '')::uuid, $4, NULL,
  $5, $6, $7, $8, $9, NULL, NULL, NULL,
  $10, $11, $12::uuid, $11, $12::uuid
)
`, connection.ID, connection.MSPID, connection.ClientID, connection.Name,
			sealed.Version, sealed.Nonce, sealed.Ciphertext, connection.Enabled,
			connection.Health, connection.Version, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *TeamsConnectionRepository) ListTeamsConnections(ctx context.Context, target scope.Target) ([]notifications.ManagedTeamsConnection, error) {
	rows, err := r.db.Query(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), name,
       (
         (webhook_secret_ref IS NOT NULL AND btrim(webhook_secret_ref) <> '')
         OR (webhook_secret_version IS NOT NULL AND webhook_secret_nonce IS NOT NULL AND webhook_secret_ciphertext IS NOT NULL)
       ),
       enabled, health_state, last_tested_at, last_success_at,
       COALESCE(last_error_code, ''), version
FROM teams_connections
WHERE msp_id = $1 AND ($2 = '' OR client_id = $2::uuid)
ORDER BY name, id
`, target.MSPID, target.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := make([]notifications.ManagedTeamsConnection, 0)
	for rows.Next() {
		connection, err := scanManagedTeamsConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (r *TeamsConnectionRepository) GetTeamsConnection(ctx context.Context, target scope.Target, id string) (notifications.ManagedTeamsConnection, error) {
	connection, err := scanManagedTeamsConnection(r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, COALESCE(client_id::text, ''), name,
       (
         (webhook_secret_ref IS NOT NULL AND btrim(webhook_secret_ref) <> '')
         OR (webhook_secret_version IS NOT NULL AND webhook_secret_nonce IS NOT NULL AND webhook_secret_ciphertext IS NOT NULL)
       ),
       enabled, health_state, last_tested_at, last_success_at,
       COALESCE(last_error_code, ''), version
FROM teams_connections
WHERE id = $1 AND msp_id = $2 AND ($3 = '' OR client_id = $3::uuid)
`, id, target.MSPID, target.ClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return notifications.ManagedTeamsConnection{}, scope.ErrNotFound
	}
	if err != nil {
		return notifications.ManagedTeamsConnection{}, err
	}
	return connection, nil
}

func (r *TeamsConnectionRepository) UpdateTeamsConnectionMetadata(ctx context.Context, accepted notifications.TeamsConnectionMutation) error {
	return r.update(ctx, accepted, `
UPDATE teams_connections
SET name = $4, updated_at = $5, updated_by = $6::uuid, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.Connection.Name)
}

func (r *TeamsConnectionRepository) ReplaceTeamsConnectionCredential(ctx context.Context, accepted notifications.TeamsConnectionMutation) error {
	defer wipeTeamsCredential(accepted.PlaintextWebhookURL)
	if r == nil || r.db == nil || r.secrets == nil {
		return notifications.ErrTeamsCredentialUnavailable
	}
	sealed, err := r.secrets.Seal(ctx, teamsCredentialPurpose(accepted.Connection.ID), accepted.PlaintextWebhookURL)
	if err != nil {
		return err
	}
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := lockTeamsConnection(ctx, tx, accepted.Connection.MSPID, accepted.Connection.ID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE teams_connections
SET webhook_secret_ref = NULL, webhook_secret_version = $4,
    webhook_secret_nonce = $5, webhook_secret_ciphertext = $6,
    updated_at = $7, updated_by = $8::uuid, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.Connection.ID, accepted.Connection.MSPID, accepted.ExpectedVersion,
			sealed.Version, sealed.Nonce, sealed.Ciphertext, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *TeamsConnectionRepository) SetTeamsConnectionEnabled(ctx context.Context, accepted notifications.TeamsConnectionMutation) error {
	return r.update(ctx, accepted, `
UPDATE teams_connections
SET enabled = $4, health_state = $5, updated_at = $6, updated_by = $7::uuid, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.Connection.Enabled, accepted.Connection.Health)
}

func (r *TeamsConnectionRepository) RecordTeamsConnectionHealth(ctx context.Context, accepted notifications.TeamsConnectionHealthMutation) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := lockTeamsConnection(ctx, tx, accepted.MSPID, accepted.ConnectionID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
UPDATE teams_connections
SET health_state = $4, last_tested_at = $5, last_success_at = $6,
    last_error_code = NULLIF($7, ''), updated_at = $8, updated_by = $9::uuid,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3
`, accepted.ConnectionID, accepted.MSPID, accepted.ExpectedVersion, accepted.Health,
			accepted.TestedAt, accepted.SucceededAt, accepted.LastErrorCode,
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

func (r *TeamsConnectionRepository) UseTeamsWebhook(ctx context.Context, target scope.Target, connection notifications.ManagedTeamsConnection, use func([]byte) error) error {
	if r == nil || r.db == nil || use == nil || !connection.CredentialConfigured || connection.ID == "" || connection.MSPID != target.MSPID {
		return notifications.ErrTeamsCredentialUnavailable
	}
	var reference *string
	var sealed secrets.SealedValue
	err := r.db.QueryRow(ctx, `
SELECT webhook_secret_ref, webhook_secret_version, webhook_secret_nonce, webhook_secret_ciphertext
FROM teams_connections connection
WHERE connection.id = $1 AND connection.msp_id = $2 AND connection.version = $3
  AND connection.client_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid
  AND ($5 = '' OR connection.client_id = $5::uuid)
`, connection.ID, connection.MSPID, connection.Version, connection.ClientID, target.ClientID).Scan(&reference, &sealed.Version, &sealed.Nonce, &sealed.Ciphertext)
	if err != nil {
		return notifications.ErrTeamsCredentialUnavailable
	}
	if sealed.Version != 0 && len(sealed.Nonce) != 0 && len(sealed.Ciphertext) != 0 {
		if r.secrets == nil {
			return notifications.ErrTeamsCredentialUnavailable
		}
		credential, err := r.secrets.Open(ctx, teamsCredentialPurpose(connection.ID), sealed)
		if err != nil || len(credential) == 0 {
			return notifications.ErrTeamsCredentialUnavailable
		}
		defer wipeTeamsCredential(credential)
		return use(credential)
	}
	if reference == nil || r.legacy == nil {
		return notifications.ErrTeamsCredentialUnavailable
	}
	legacyURL, err := r.legacy.Resolve(ctx, notifications.TeamsConnection{
		ID: connection.ID, MSPID: connection.MSPID, ClientID: connection.ClientID,
		WebhookSecretRef: *reference, Version: connection.Version,
	})
	if err != nil || strings.TrimSpace(legacyURL) == "" {
		return notifications.ErrTeamsCredentialUnavailable
	}
	credential := []byte(legacyURL)
	defer wipeTeamsCredential(credential)
	return use(credential)
}

func (r *TeamsConnectionRepository) Resolve(
	ctx context.Context,
	connection notifications.TeamsConnection,
) (string, error) {
	managed := notifications.ManagedTeamsConnection{
		ID: connection.ID, MSPID: connection.MSPID, ClientID: connection.ClientID,
		CredentialConfigured: true, Version: connection.Version,
	}
	var endpoint string
	err := r.UseTeamsWebhook(
		ctx,
		scope.Target{MSPID: connection.MSPID, ClientID: connection.ClientID},
		managed,
		func(webhook []byte) error {
			endpoint = string(webhook)
			return nil
		},
	)
	if err != nil || strings.TrimSpace(endpoint) == "" {
		return "", notifications.ErrTeamsSecretUnavailable
	}
	return endpoint, nil
}

func (r *TeamsConnectionRepository) update(ctx context.Context, accepted notifications.TeamsConnectionMutation, query string, extras ...any) error {
	return r.withTransaction(ctx, func(tx transaction) error {
		if err := lockTeamsConnection(ctx, tx, accepted.Connection.MSPID, accepted.Connection.ID); err != nil {
			return err
		}
		args := []any{accepted.Connection.ID, accepted.Connection.MSPID, accepted.ExpectedVersion}
		args = append(args, extras...)
		args = append(args, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		tag, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *TeamsConnectionRepository) withTransaction(ctx context.Context, fn func(transaction) error) error {
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

func lockTeamsConnection(ctx context.Context, tx transaction, mspID, connectionID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, mspID, connectionID)
	return err
}

func teamsCredentialPurpose(connectionID string) string { return "teams.connection." + connectionID }
func wipeTeamsCredential(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func scanManagedTeamsConnection(scanner interface{ Scan(...any) error }) (notifications.ManagedTeamsConnection, error) {
	var connection notifications.ManagedTeamsConnection
	err := scanner.Scan(&connection.ID, &connection.MSPID, &connection.ClientID, &connection.Name,
		&connection.CredentialConfigured, &connection.Enabled, &connection.Health,
		&connection.LastTestedAt, &connection.LastSuccessAt, &connection.LastErrorCode, &connection.Version)
	return connection, err
}
