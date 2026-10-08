package authn

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
)

type EntraSettingsStore struct {
	pool *pgxpool.Pool
}

var _ identity.EntraSettingsRepository = (*EntraSettingsStore)(nil)

func NewEntraSettingsStore(pool *pgxpool.Pool) *EntraSettingsStore {
	return &EntraSettingsStore{pool: pool}
}

func (s *EntraSettingsStore) GetEntraSettings(ctx context.Context, mspID string) (identity.EntraSettings, error) {
	var result identity.EntraSettings
	var activeTenant, activeClient, activeRedirect *string
	var candidateTenant, candidateClient, candidateRedirect *string
	var candidateVersion *int64
	var activeCredential bool
	err := s.pool.QueryRow(ctx, `
SELECT setup.entra_state, setup.entra_version,
       setup.entra_tenant_id, setup.entra_client_id, setup.entra_redirect_url,
       setup.entra_secret_version IS NOT NULL,
       candidate.version, candidate.tenant_id, candidate.client_id, candidate.redirect_url
FROM installation_setup setup
LEFT JOIN entra_configuration_candidates candidate ON candidate.msp_id = setup.msp_id
WHERE setup.singleton = true AND setup.msp_id = $1
`, mspID).Scan(
		&result.State, &result.Version,
		&activeTenant, &activeClient, &activeRedirect, &activeCredential,
		&candidateVersion, &candidateTenant, &candidateClient, &candidateRedirect,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EntraSettings{}, identity.ErrInvalidEntraSettings
	}
	if err != nil {
		return identity.EntraSettings{}, err
	}
	if candidateVersion != nil {
		result.State, result.Version = "verification_required", *candidateVersion
		result.TenantID, result.ClientID, result.RedirectURL =
			*candidateTenant, *candidateClient, *candidateRedirect
		result.CredentialConfigured = true
		return result, nil
	}
	if activeTenant != nil {
		result.TenantID, result.ClientID, result.RedirectURL =
			*activeTenant, *activeClient, *activeRedirect
	}
	result.CredentialConfigured = activeCredential
	return result, nil
}

func (s *EntraSettingsStore) SaveEntraCandidate(ctx context.Context, accepted identity.EntraSettingsMutation) (identity.EntraSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EntraSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE installation_setup
SET entra_version = entra_version + 1
WHERE singleton = true AND msp_id = $1 AND entra_version = $2
RETURNING entra_version
`, accepted.MSPID, accepted.ExpectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EntraSettings{}, identity.ErrEntraSettingsConflict
	}
	if err != nil {
		return identity.EntraSettings{}, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO entra_configuration_candidates (
  msp_id, version, tenant_id, client_id, secret_version, secret_nonce,
  secret_ciphertext, redirect_url, staged_at, staged_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (msp_id) DO UPDATE
SET version = EXCLUDED.version, tenant_id = EXCLUDED.tenant_id,
    client_id = EXCLUDED.client_id, secret_version = EXCLUDED.secret_version,
    secret_nonce = EXCLUDED.secret_nonce,
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    redirect_url = EXCLUDED.redirect_url, staged_at = EXCLUDED.staged_at,
    staged_by = EXCLUDED.staged_by
`, accepted.MSPID, version, accepted.TenantID, accepted.ClientID,
		accepted.Secret.Version, accepted.Secret.Nonce, accepted.Secret.Ciphertext,
		accepted.RedirectURL, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	if err != nil {
		return identity.EntraSettings{}, err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return identity.EntraSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.EntraSettings{}, err
	}
	return identity.EntraSettings{
		State: "verification_required", Version: version,
		TenantID: accepted.TenantID, ClientID: accepted.ClientID,
		RedirectURL: accepted.RedirectURL, CredentialConfigured: true,
	}, nil
}

func (s *EntraSettingsStore) GetEntraCandidate(ctx context.Context, mspID string, version int64) (identity.EntraCandidate, error) {
	var candidate identity.EntraCandidate
	err := s.pool.QueryRow(ctx, `
SELECT version, tenant_id, client_id, redirect_url,
       secret_version, secret_nonce, secret_ciphertext
FROM entra_configuration_candidates
WHERE msp_id = $1 AND version = $2
`, mspID, version).Scan(
		&candidate.Version, &candidate.TenantID, &candidate.ClientID,
		&candidate.RedirectURL, &candidate.Secret.Version,
		&candidate.Secret.Nonce, &candidate.Secret.Ciphertext,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EntraCandidate{}, identity.ErrEntraSettingsConflict
	}
	return candidate, err
}

func (s *EntraSettingsStore) PromoteEntraCandidate(ctx context.Context, accepted identity.EntraSettingsMutation) (identity.EntraSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EntraSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE installation_setup setup
SET entra_tenant_id = candidate.tenant_id,
    entra_client_id = candidate.client_id,
    entra_secret_version = candidate.secret_version,
    entra_secret_nonce = candidate.secret_nonce,
    entra_secret_ciphertext = candidate.secret_ciphertext,
    entra_redirect_url = candidate.redirect_url,
    entra_state = 'connected',
    entra_version = setup.entra_version + 1
FROM entra_configuration_candidates candidate
WHERE setup.singleton = true AND setup.msp_id = $1
  AND setup.entra_version = $2
  AND candidate.msp_id = setup.msp_id AND candidate.version = $2
RETURNING setup.entra_version
`, accepted.MSPID, accepted.ExpectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EntraSettings{}, identity.ErrEntraSettingsConflict
	}
	if err != nil {
		return identity.EntraSettings{}, err
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM entra_configuration_candidates
WHERE msp_id = $1 AND version = $2
`, accepted.MSPID, accepted.ExpectedVersion); err != nil {
		return identity.EntraSettings{}, err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return identity.EntraSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.EntraSettings{}, err
	}
	return identity.EntraSettings{
		State: "connected", Version: version,
		TenantID: accepted.TenantID, ClientID: accepted.ClientID,
		RedirectURL: accepted.RedirectURL, CredentialConfigured: true,
	}, nil
}

func (s *EntraSettingsStore) DisableEntra(ctx context.Context, accepted identity.EntraSettingsMutation) (identity.EntraSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EntraSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, `
UPDATE installation_setup
SET entra_tenant_id = NULL, entra_client_id = NULL,
    entra_secret_version = NULL, entra_secret_nonce = NULL,
    entra_secret_ciphertext = NULL, entra_redirect_url = NULL,
    entra_state = 'not_connected', entra_version = entra_version + 1
WHERE singleton = true AND msp_id = $1 AND entra_version = $2
RETURNING entra_version
`, accepted.MSPID, accepted.ExpectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EntraSettings{}, identity.ErrEntraSettingsConflict
	}
	if err != nil {
		return identity.EntraSettings{}, err
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM entra_configuration_candidates WHERE msp_id = $1
`, accepted.MSPID); err != nil {
		return identity.EntraSettings{}, err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return identity.EntraSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.EntraSettings{}, err
	}
	return identity.EntraSettings{State: "not_connected", Version: version}, nil
}
