package psa

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/secrets"
)

var _ graphintake.ManagementRepository = (*GraphRepository)(nil)
var _ graphintake.GraphCredentialResolver = (*GraphRepository)(nil)
var _ graphintake.ClientStateResolver = (*GraphRepository)(nil)

func (r *GraphRepository) WithLegacyResolvers(
	credentials graphintake.GraphCredentialResolver,
	clientState graphintake.ClientStateResolver,
) *GraphRepository {
	r.legacyCredentials = credentials
	r.legacyClientState = clientState
	return r
}

func (r *GraphRepository) ListManagedMailboxes(ctx context.Context, mspID string) ([]graphintake.ManagedMailbox, error) {
	rows, err := r.db.Query(ctx, graphManagedMailboxSelect+`
WHERE msp_id = $1
ORDER BY mailbox_address, id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]graphintake.ManagedMailbox, 0)
	for rows.Next() {
		item, err := scanManagedMailbox(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *GraphRepository) GetManagedMailbox(ctx context.Context, mspID, id string) (graphintake.ManagedMailbox, error) {
	item, err := scanManagedMailbox(r.db.QueryRow(ctx, graphManagedMailboxSelect+`
WHERE id = $1 AND msp_id = $2
`, id, mspID))
	if errors.Is(err, pgx.ErrNoRows) {
		return graphintake.ManagedMailbox{}, scope.ErrNotFound
	}
	return item, err
}

const graphManagedMailboxSelect = `
SELECT id::text, mailbox_address, COALESCE(graph_tenant_id, ''),
       COALESCE(graph_client_id, ''),
       (credential_secret_ref IS NOT NULL OR credential_secret_version IS NOT NULL),
       (client_state_secret_ref IS NOT NULL OR client_state_secret_version IS NOT NULL),
       enabled, health_state, last_success_at, COALESCE(last_error_code, ''),
       version, updated_at
FROM graph_mailbox_connections
`

func scanManagedMailbox(scanner interface{ Scan(...any) error }) (graphintake.ManagedMailbox, error) {
	var item graphintake.ManagedMailbox
	err := scanner.Scan(&item.ID, &item.MailboxAddress, &item.TenantID, &item.ClientID,
		&item.CredentialConfigured, &item.ClientStateConfigured, &item.Enabled,
		&item.HealthState, &item.LastSuccessAt, &item.LastErrorCode, &item.Version,
		&item.UpdatedAt)
	return item, err
}

func (r *GraphRepository) CreateManagedMailbox(ctx context.Context, accepted graphintake.MailboxMutation) error {
	defer wipeGraphProtected(accepted.ProtectedCredential)
	defer wipeGraphProtected(accepted.ClientState)
	credential, err := r.sealGraph(ctx, graphCredentialPurpose(accepted.Mailbox.ID), accepted.ProtectedCredential)
	if err != nil {
		return err
	}
	state, err := r.sealGraph(ctx, graphClientStatePurpose(accepted.Mailbox.ID), accepted.ClientState)
	if err != nil {
		return err
	}
	return r.graphManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Mailbox
		_, err := tx.Exec(ctx, `
INSERT INTO graph_mailbox_connections (
  id, msp_id, mailbox_address, credential_secret_ref,
  credential_secret_version, credential_secret_nonce, credential_secret_ciphertext,
  graph_tenant_id, graph_client_id, client_state_secret_ref,
  client_state_secret_version, client_state_secret_nonce, client_state_secret_ciphertext,
  enabled, health_state, version, created_at, created_by, updated_at, updated_by
) VALUES (
  $1, $2, $3, NULL, $4, $5, $6, $7, $8, NULL, $9, $10, $11,
  $12, $13, 1, $14, $15::uuid, $14, $15::uuid
)
`, item.ID, accepted.MSPID, item.MailboxAddress,
			credential.Version, credential.Nonce, credential.Ciphertext,
			item.TenantID, item.ClientID, state.Version, state.Nonce, state.Ciphertext,
			item.Enabled, item.HealthState, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *GraphRepository) UpdateManagedMailbox(ctx context.Context, accepted graphintake.MailboxMutation) error {
	return r.graphManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Mailbox
		tag, err := tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET mailbox_address = $4, enabled = $5, health_state = $6,
    version = version + 1,
    configuration_generation = configuration_generation + 1,
    updated_at = $7, updated_by = $8::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, item.ID, accepted.MSPID, accepted.ExpectedVersion, item.MailboxAddress,
			item.Enabled, item.HealthState, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
UPDATE graph_subscriptions
SET recovery_state = 'recreate_subscription'
WHERE connection_id = $1
`, item.ID); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *GraphRepository) ReplaceManagedMailboxCredential(ctx context.Context, accepted graphintake.MailboxMutation) error {
	defer wipeGraphProtected(accepted.ProtectedCredential)
	sealed, err := r.sealGraph(ctx, graphCredentialPurpose(accepted.Mailbox.ID), accepted.ProtectedCredential)
	if err != nil {
		return err
	}
	return r.graphManagementTransaction(ctx, func(tx transaction) error {
		item := accepted.Mailbox
		tag, err := tx.Exec(ctx, `
UPDATE graph_mailbox_connections
SET credential_secret_ref = NULL, credential_secret_version = $4,
    credential_secret_nonce = $5, credential_secret_ciphertext = $6,
    graph_tenant_id = $7, graph_client_id = $8,
    version = version + 1,
    configuration_generation = configuration_generation + 1,
    updated_at = $9, updated_by = $10::uuid
WHERE id = $1 AND msp_id = $2 AND version = $3
`, item.ID, accepted.MSPID, accepted.ExpectedVersion, sealed.Version,
			sealed.Nonce, sealed.Ciphertext, item.TenantID, item.ClientID,
			accepted.Audit.OccurredAt, accepted.Audit.ActorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return object.ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `
UPDATE graph_subscriptions
SET recovery_state = 'recreate_subscription'
WHERE connection_id = $1
`, item.ID); err != nil {
			return err
		}
		return writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event)
	})
}

func (r *GraphRepository) Resolve(ctx context.Context, reference string) ([]byte, error) {
	id, version, kind, ok := parseGraphReference(reference)
	if !ok {
		if r.legacyCredentials != nil {
			return r.legacyCredentials.Resolve(ctx, reference)
		}
		return nil, graphintake.ErrInvalidGraphCredentials
	}
	if kind != "credential" {
		return nil, graphintake.ErrInvalidGraphCredentials
	}
	var legacy *string
	var keyVersion *int
	var nonce, ciphertext []byte
	err := r.db.QueryRow(ctx, `
SELECT credential_secret_ref, credential_secret_version,
       credential_secret_nonce, credential_secret_ciphertext
FROM graph_mailbox_connections
WHERE id = $1 AND configuration_generation = $2 AND enabled
`, id, version).Scan(&legacy, &keyVersion, &nonce, &ciphertext)
	if err != nil {
		return nil, graphintake.ErrInvalidGraphCredentials
	}
	if legacy != nil {
		if r.legacyCredentials == nil {
			return nil, graphintake.ErrInvalidGraphCredentials
		}
		return r.legacyCredentials.Resolve(ctx, *legacy)
	}
	if keyVersion == nil || r.secrets == nil {
		return nil, graphintake.ErrInvalidGraphCredentials
	}
	return r.secrets.Open(ctx, graphCredentialPurpose(id), secrets.SealedValue{Version: *keyVersion, Nonce: nonce, Ciphertext: ciphertext})
}

func (r *GraphRepository) ResolveClientState(ctx context.Context, reference string) (string, error) {
	id, version, kind, ok := parseGraphReference(reference)
	if !ok {
		if r.legacyClientState != nil {
			return r.legacyClientState.ResolveClientState(ctx, reference)
		}
		return "", graphintake.ErrInvalidNotification
	}
	if kind != "client-state" {
		return "", graphintake.ErrInvalidNotification
	}
	var legacy *string
	var keyVersion *int
	var nonce, ciphertext []byte
	err := r.db.QueryRow(ctx, `
SELECT client_state_secret_ref, client_state_secret_version,
       client_state_secret_nonce, client_state_secret_ciphertext
FROM graph_mailbox_connections
WHERE id = $1 AND configuration_generation = $2 AND enabled
`, id, version).Scan(&legacy, &keyVersion, &nonce, &ciphertext)
	if err != nil {
		return "", graphintake.ErrInvalidNotification
	}
	if legacy != nil {
		if r.legacyClientState == nil {
			return "", graphintake.ErrInvalidNotification
		}
		return r.legacyClientState.ResolveClientState(ctx, *legacy)
	}
	if keyVersion == nil || r.secrets == nil {
		return "", graphintake.ErrInvalidNotification
	}
	value, err := r.secrets.Open(ctx, graphClientStatePurpose(id), secrets.SealedValue{Version: *keyVersion, Nonce: nonce, Ciphertext: ciphertext})
	if err != nil {
		return "", graphintake.ErrInvalidNotification
	}
	defer wipeGraphProtected(value)
	return string(value), nil
}

func (r *GraphRepository) sealGraph(ctx context.Context, purpose string, value []byte) (secrets.SealedValue, error) {
	if r.secrets == nil || len(value) == 0 {
		return secrets.SealedValue{}, graphintake.ErrInvalidGraphManagement
	}
	return r.secrets.Seal(ctx, purpose, value)
}
func (r *GraphRepository) graphManagementTransaction(ctx context.Context, fn func(transaction) error) error {
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
func parseGraphReference(reference string) (string, int64, string, bool) {
	const prefix = "db://graph/"
	if !strings.HasPrefix(reference, prefix) {
		return "", 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(reference, prefix), "/")
	if len(parts) != 3 {
		return "", 0, "", false
	}
	version, err := strconv.ParseInt(parts[1], 10, 64)
	return parts[0], version, parts[2], err == nil && version > 0
}
func graphCredentialPurpose(id string) string  { return "graph.mailbox.credential." + id }
func graphClientStatePurpose(id string) string { return "graph.mailbox.client-state." + id }
func wipeGraphProtected(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
