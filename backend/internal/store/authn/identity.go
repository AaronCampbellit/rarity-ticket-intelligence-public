package authn

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
)

type IdentityStore struct {
	pool *pgxpool.Pool
}

var _ identity.Repository = (*IdentityStore)(nil)

func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{pool: pool}
}

func (s *IdentityStore) FindByExternalIdentity(
	ctx context.Context,
	mspID string,
	issuer string,
	subject string,
) (identity.Technician, error) {
	var technician identity.Technician
	err := s.pool.QueryRow(ctx, `
UPDATE external_identities ei
SET last_authenticated_at = now()
FROM technicians t
WHERE ei.msp_id = $1 AND ei.issuer = $2 AND ei.subject = $3
  AND t.id = ei.technician_id AND t.msp_id = ei.msp_id
  AND t.lifecycle_state = 'active'
RETURNING t.id::text, t.msp_id::text, t.email, t.display_name
`, mspID, issuer, subject).Scan(
		&technician.ID, &technician.MSPID,
		&technician.Email, &technician.DisplayName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Technician{}, identity.ErrIdentityNotFound
	}
	return technician, err
}

func (s *IdentityStore) ProvisionAtomic(
	ctx context.Context,
	command identity.ProvisionCommand,
) (identity.Technician, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.Technician{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(
  hashtextextended($1 || E'\n' || $2 || E'\n' || $3, 0)
)
`, command.MSPID, command.Issuer, command.Subject); err != nil {
		return identity.Technician{}, err
	}
	var existing identity.Technician
	err = tx.QueryRow(ctx, `
SELECT t.id::text, t.msp_id::text, t.email, t.display_name
FROM external_identities ei
JOIN technicians t ON t.id = ei.technician_id AND t.msp_id = ei.msp_id
WHERE ei.msp_id = $1 AND ei.issuer = $2 AND ei.subject = $3
  AND t.lifecycle_state = 'active'
`, command.MSPID, command.Issuer, command.Subject).Scan(
		&existing.ID, &existing.MSPID, &existing.Email, &existing.DisplayName,
	)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return identity.Technician{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Technician{}, err
	}
	displayName := strings.TrimSpace(command.DisplayName)
	if displayName == "" {
		displayName = command.Email
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO technicians (
  id, msp_id, email, display_name, lifecycle_state,
  version, created_at, updated_at
) VALUES ($1, $2, $3, $4, 'active', 1, $5, $5)
`, command.TechnicianID, command.MSPID, command.Email,
		displayName, command.OccurredAt); err != nil {
		return identity.Technician{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO external_identities (
  id, msp_id, technician_id, provider, issuer, subject, tenant_id,
  email_at_link, linked_at, last_authenticated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
`, command.ExternalIdentityID, command.MSPID, command.TechnicianID,
		command.Provider, command.Issuer, command.Subject, command.TenantID,
		command.Email, command.OccurredAt); err != nil {
		return identity.Technician{}, err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO role_assignments (
  id, msp_id, client_id, technician_id, role_id, granted_at, granted_by
)
SELECT $1, $2, NULL, $3, r.id, $4, $3
FROM roles r
WHERE r.msp_id = $2 AND r.key = $5
`, command.RoleAssignmentID, command.MSPID, command.TechnicianID,
		command.OccurredAt, command.DefaultRoleKey)
	if err != nil {
		return identity.Technician{}, err
	}
	if tag.RowsAffected() != 1 {
		return identity.Technician{}, identity.ErrJITDisabled
	}
	if err := writeAuthFacts(ctx, tx, command.Audit, command.Event); err != nil {
		return identity.Technician{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Technician{}, err
	}
	return identity.Technician{
		ID: command.TechnicianID, MSPID: command.MSPID,
		Email: command.Email, DisplayName: displayName,
	}, nil
}
