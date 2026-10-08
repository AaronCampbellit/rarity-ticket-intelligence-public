package authn

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type RoleRepository struct {
	pool *pgxpool.Pool
}

var _ authorization.RoleRepository = (*RoleRepository)(nil)

func NewRoleRepository(pool *pgxpool.Pool) *RoleRepository {
	return &RoleRepository{pool: pool}
}

func (r *RoleRepository) ListRoles(
	ctx context.Context,
	mspID string,
) ([]authorization.Role, error) {
	rows, err := r.pool.Query(ctx, `
SELECT r.id::text, r.key, r.name, r.system_role, r.version,
       COALESCE(array_agg(rc.capability ORDER BY rc.capability)
         FILTER (WHERE rc.capability IS NOT NULL), '{}'::text[])
FROM roles r
LEFT JOIN role_capabilities rc
  ON rc.role_id = r.id AND rc.msp_id = r.msp_id
WHERE r.msp_id = $1
GROUP BY r.id
ORDER BY r.system_role DESC, r.name, r.id
`, mspID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []authorization.Role
	for rows.Next() {
		var role authorization.Role
		if err := rows.Scan(
			&role.ID, &role.Key, &role.Name, &role.SystemRole,
			&role.Version, &role.Capabilities,
		); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *RoleRepository) CreateRoleAtomic(
	ctx context.Context,
	mspID string,
	role authorization.Role,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
INSERT INTO roles (
  id, msp_id, key, name, system_role, version, created_at, updated_at
) VALUES ($1, $2, $3, $4, false, $5, $6, $6)
`, role.ID, mspID, role.Key, role.Name, role.Version,
		audit.OccurredAt,
	); err != nil {
		return err
	}
	for _, capability := range role.Capabilities {
		if _, err := tx.Exec(ctx, `
INSERT INTO role_capabilities (role_id, msp_id, capability)
VALUES ($1, $2, $3)
`, role.ID, mspID, capability); err != nil {
			return err
		}
	}
	if err := writeAuthFacts(ctx, tx, audit, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *RoleRepository) AssignRoleAtomic(
	ctx context.Context,
	assignment authorization.RoleAssignment,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT begin_mention_authorization_revision($1::uuid)`, assignment.MSPID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO role_assignments (
  id, msp_id, client_id, technician_id, role_id, granted_at, granted_by
)
SELECT $1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7
WHERE EXISTS (
  SELECT 1
  FROM technicians
  WHERE id = $4 AND msp_id = $2 AND lifecycle_state = 'active'
) AND (
  NULLIF($3, '')::uuid IS NULL OR EXISTS (
    SELECT 1
    FROM client_organizations
    WHERE id = NULLIF($3, '')::uuid AND msp_id = $2
      AND lifecycle_state = 'active'
  )
) AND EXISTS (
  SELECT 1
  FROM roles
  WHERE id = $5 AND msp_id = $2
)
`, assignment.ID, assignment.MSPID, assignment.ClientID,
		assignment.TechnicianID, assignment.RoleID,
		assignment.GrantedAt, assignment.GrantedBy,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return scope.ErrNotFound
	}
	if err := writeAuthFacts(ctx, tx, audit, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *RoleRepository) ReplaceRoleCapabilities(
	ctx context.Context,
	mspID string,
	role authorization.Role,
	audit mutation.AuditRecord,
	event mutation.EventRecord,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT begin_mention_authorization_revision($1::uuid)`, mspID); err != nil {
		return err
	}
	var currentVersion int64
	if err := tx.QueryRow(ctx, `
SELECT version
FROM roles
WHERE id = $1 AND msp_id = $2
FOR UPDATE
`, role.ID, mspID).Scan(&currentVersion); errors.Is(err, pgx.ErrNoRows) {
		return object.ErrVersionConflict
	} else if err != nil {
		return err
	}
	if currentVersion+1 != role.Version {
		return object.ErrVersionConflict
	}
	if !slices.Contains(role.Capabilities, "role.manage") {
		var currentGrantsRoleManagement bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM role_capabilities
  WHERE role_id = $1 AND msp_id = $2 AND capability = 'role.manage'
)
`, role.ID, mspID).Scan(&currentGrantsRoleManagement); err != nil {
			return err
		}
		if currentGrantsRoleManagement {
			var otherManagers int
			if err := tx.QueryRow(ctx, `
SELECT COUNT(DISTINCT ra.technician_id)
FROM role_assignments ra
JOIN role_capabilities rc
  ON rc.role_id = ra.role_id AND rc.msp_id = ra.msp_id
JOIN technicians t
  ON t.id = ra.technician_id AND t.msp_id = ra.msp_id
WHERE ra.msp_id = $1 AND ra.role_id <> $2
  AND rc.capability = 'role.manage'
  AND (ra.expires_at IS NULL OR ra.expires_at > $3)
  AND t.lifecycle_state = 'active'
`, mspID, role.ID, audit.OccurredAt).Scan(&otherManagers); err != nil {
				return err
			}
			if otherManagers == 0 {
				return authorization.ErrLastRoleManager
			}
		}
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM role_capabilities
WHERE role_id = $1 AND msp_id = $2
`, role.ID, mspID); err != nil {
		return err
	}
	for _, capability := range role.Capabilities {
		if _, err := tx.Exec(ctx, `
INSERT INTO role_capabilities (role_id, msp_id, capability)
VALUES ($1, $2, $3)
`, role.ID, mspID, capability); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `
UPDATE roles
SET version = version + 1, updated_at = $4
WHERE id = $1 AND msp_id = $2 AND version = $3
`, role.ID, mspID, currentVersion, audit.OccurredAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	if err := writeAuthFacts(ctx, tx, audit, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
