package authn

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type BreakGlassStore struct {
	pool  *pgxpool.Pool
	newID func() string
}

var _ identity.BreakGlassRepository = (*BreakGlassStore)(nil)
var _ identity.BreakGlassManagementRepository = (*BreakGlassStore)(nil)

func NewBreakGlassStore(pool *pgxpool.Pool, newID func() string) *BreakGlassStore {
	return &BreakGlassStore{pool: pool, newID: newID}
}

func (s *BreakGlassStore) CreateBreakGlassAccountAtomic(ctx context.Context, accepted identity.BreakGlassAccountMutation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if accepted.Email != "" {
		if _, err := tx.Exec(ctx, `
INSERT INTO technicians (
  id, msp_id, email, display_name, created_at, updated_at
) VALUES ($1,$2,lower(btrim($3)),$4,$5,$5)
`, accepted.Account.TechnicianID, accepted.MSPID, accepted.Email,
			accepted.DisplayName, accepted.Account.CreatedAt); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO role_assignments (
  id, msp_id, technician_id, role_id, granted_at, granted_by
)
SELECT $1,$2,$3,role.id,$4,$5
FROM roles role
WHERE role.msp_id = $2 AND role.key = 'global-admin' AND role.system_role
`, accepted.AssignmentID, accepted.MSPID, accepted.Account.TechnicianID,
			accepted.Account.CreatedAt, accepted.CreatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return identity.ErrInvalidBreakGlassAccount
		}
	}
	_, err = tx.Exec(ctx, `
INSERT INTO break_glass_accounts (
  id, msp_id, technician_id, username, password_hash, allowed_cidrs,
  enabled, created_at, created_by, updated_at, version
) VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8,$7,1)
`, accepted.Account.ID, accepted.MSPID, accepted.Account.TechnicianID,
		accepted.Account.Username, accepted.PasswordHash, accepted.Account.AllowedCIDRs,
		accepted.Account.CreatedAt, accepted.CreatedBy)
	if err != nil {
		return err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *BreakGlassStore) ListBreakGlassAccounts(ctx context.Context, mspID string) ([]identity.ManagedBreakGlassAccount, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id::text, technician_id::text, username, allowed_cidrs::text[], enabled,
       created_at, last_used_at, COALESCE(last_used_ip::text, ''), version
FROM break_glass_accounts
WHERE msp_id = $1
ORDER BY enabled DESC, username
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]identity.ManagedBreakGlassAccount, 0)
	for rows.Next() {
		var account identity.ManagedBreakGlassAccount
		if err := rows.Scan(&account.ID, &account.TechnicianID, &account.Username,
			&account.AllowedCIDRs, &account.Enabled, &account.CreatedAt,
			&account.LastUsedAt, &account.LastUsedIP, &account.Version); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *BreakGlassStore) DisableBreakGlassAccountAtomic(ctx context.Context, accepted identity.BreakGlassAccountMutation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT id::text FROM break_glass_accounts
WHERE msp_id = $1 AND enabled
FOR UPDATE
`, accepted.MSPID)
	if err != nil {
		return err
	}
	enabled := 0
	for rows.Next() {
		enabled++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if enabled <= 1 {
		return identity.ErrLastBreakGlassAccount
	}
	tag, err := tx.Exec(ctx, `
UPDATE break_glass_accounts
SET enabled = false, updated_at = $4, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3 AND enabled
`, accepted.Account.ID, accepted.MSPID, accepted.Account.Version-1, accepted.Audit.OccurredAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrBreakGlassVersionConflict
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *BreakGlassStore) ResetBreakGlassPasswordAtomic(ctx context.Context, accepted identity.BreakGlassAccountMutation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE break_glass_accounts
SET password_hash = $4, updated_at = $5, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3 AND enabled
`, accepted.Account.ID, accepted.MSPID, accepted.Account.Version-1,
		accepted.PasswordHash, accepted.Audit.OccurredAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrBreakGlassVersionConflict
	}
	if err := revokeLocalAdministratorSessions(ctx, tx, accepted); err != nil {
		return err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *BreakGlassStore) UpdateBreakGlassNetworksAtomic(ctx context.Context, accepted identity.BreakGlassAccountMutation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
UPDATE break_glass_accounts
SET allowed_cidrs = $4, updated_at = $5, version = version + 1
WHERE id = $1 AND msp_id = $2 AND version = $3 AND enabled
`, accepted.Account.ID, accepted.MSPID, accepted.Account.Version-1,
		accepted.Account.AllowedCIDRs, accepted.Audit.OccurredAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrBreakGlassVersionConflict
	}
	if err := revokeLocalAdministratorSessions(ctx, tx, accepted); err != nil {
		return err
	}
	if err := writeAuthFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func revokeLocalAdministratorSessions(ctx context.Context, tx pgx.Tx, accepted identity.BreakGlassAccountMutation) error {
	_, err := tx.Exec(ctx, `
UPDATE sessions
SET revoked_at = $3, revoked_by = $4
WHERE msp_id = $2
  AND technician_id = (
    SELECT technician_id
    FROM break_glass_accounts
    WHERE id = $1 AND msp_id = $2
  )
  AND revoked_at IS NULL
`, accepted.Account.ID, accepted.MSPID, accepted.Audit.OccurredAt, accepted.Audit.ActorID)
	return err
}

func (s *BreakGlassStore) FindBreakGlassAccount(
	ctx context.Context,
	mspID string,
	username string,
) (identity.BreakGlassAccount, error) {
	var account identity.BreakGlassAccount
	var allowedCIDRs []string
	err := s.pool.QueryRow(ctx, `
SELECT id::text, msp_id::text, technician_id::text, username,
       password_hash, enabled, allowed_cidrs::text[]
FROM break_glass_accounts
WHERE msp_id = $1 AND username = $2
`, mspID, username).Scan(
		&account.ID, &account.MSPID, &account.TechnicianID,
		&account.Username, &account.PasswordHash, &account.Enabled,
		&allowedCIDRs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.BreakGlassAccount{}, identity.ErrBreakGlassIdentityNotFound
	}
	if err != nil {
		return identity.BreakGlassAccount{}, err
	}
	account.AllowedCIDRs = allowedCIDRs
	return account, nil
}

func (s *BreakGlassStore) RecordBreakGlassUse(
	ctx context.Context,
	use identity.BreakGlassUse,
) error {
	if s.newID == nil {
		return identity.ErrBreakGlassDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var subjectVersion int64
	err = tx.QueryRow(ctx, `
UPDATE break_glass_accounts
SET last_used_at = $4, last_used_ip = $5, updated_at = $4,
    version = version + 1
WHERE id = $1 AND msp_id = $2 AND technician_id = $3 AND enabled
RETURNING version
`, use.AccountID, use.MSPID, use.TechnicianID, use.OccurredAt, use.SourceIP).Scan(
		&subjectVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrBreakGlassDenied
	}
	if err != nil {
		return err
	}
	correlationID := s.newID()
	audit := mutation.AuditRecord{
		ID: s.newID(), OccurredAt: use.OccurredAt, MSPID: use.MSPID,
		ActorType: "technician", ActorID: use.TechnicianID,
		Action:      "security.local_admin.authenticated",
		SubjectType: "local_administrator", SubjectID: use.AccountID,
		SubjectVersion: subjectVersion, Source: "browser",
		Reason: "local administrator authentication", CorrelationID: correlationID,
	}
	event := mutation.EventRecord{
		EventID: s.newID(), EventType: "security.local_admin.authenticated",
		SchemaVersion: 1, OccurredAt: use.OccurredAt, MSPID: use.MSPID,
		ActorType: "technician", ActorID: use.TechnicianID,
		SubjectType: "local_administrator", SubjectID: use.AccountID,
		SubjectVersion: subjectVersion, Source: "browser", CorrelationID: correlationID,
	}
	if err := writeAuthFacts(ctx, tx, audit, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
