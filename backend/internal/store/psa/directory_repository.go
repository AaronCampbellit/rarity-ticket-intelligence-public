package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type DirectoryRepository struct {
	db database
}

var _ organizations.DirectoryRepository = (*DirectoryRepository)(nil)

func NewDirectoryRepository(db database) *DirectoryRepository {
	return &DirectoryRepository{db: db}
}

func (r *DirectoryRepository) CreateDepartmentAtomic(
	ctx context.Context,
	accepted organizations.DirectoryMutation,
) error {
	return r.createAtomic(ctx, accepted, func(tx transaction) error {
		department := accepted.Department
		_, err := tx.Exec(ctx, `
INSERT INTO departments (id, msp_id, key, name, version)
VALUES ($1, $2, $3, $4, $5)
`, department.ID, department.MSPID, department.Key, department.Name,
			department.Version)
		return err
	})
}

func (r *DirectoryRepository) CreateTeamAtomic(
	ctx context.Context,
	accepted organizations.DirectoryMutation,
) error {
	return r.createAtomic(ctx, accepted, func(tx transaction) error {
		team := accepted.Team
		tag, err := tx.Exec(ctx, `
INSERT INTO teams (id, msp_id, department_id, key, name, version)
SELECT $1, $2, $3, $4, $5, $6
WHERE EXISTS (
  SELECT 1 FROM departments WHERE id = $3 AND msp_id = $2
)
`, team.ID, team.MSPID, team.DepartmentID, team.Key, team.Name, team.Version)
		if err == nil && tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return err
	})
}

func (r *DirectoryRepository) CreateQueueAtomic(
	ctx context.Context,
	accepted organizations.DirectoryMutation,
) error {
	return r.createAtomic(ctx, accepted, func(tx transaction) error {
		queue := accepted.Queue
		tag, err := tx.Exec(ctx, `
INSERT INTO queues (
  id, msp_id, client_id, department_id, team_id, key, name, version
)
SELECT $1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid,
       NULLIF($5, '')::uuid, $6, $7, $8
WHERE (
  $3 = '' OR EXISTS (
    SELECT 1 FROM client_organizations
    WHERE id = $3::uuid AND msp_id = $2 AND lifecycle_state = 'active'
  )
) AND (
  $4 = '' OR EXISTS (
    SELECT 1 FROM departments WHERE id = $4::uuid AND msp_id = $2
  )
) AND (
  $5 = '' OR EXISTS (
    SELECT 1 FROM teams
    WHERE id = $5::uuid AND msp_id = $2
      AND ($4 = '' OR department_id = $4::uuid)
  )
)
`, queue.ID, queue.MSPID, queue.ClientID, queue.DepartmentID, queue.TeamID,
			queue.Key, queue.Name, queue.Version)
		if err == nil && tag.RowsAffected() != 1 {
			return scope.ErrNotFound
		}
		return err
	})
}

func (r *DirectoryRepository) createAtomic(
	ctx context.Context,
	accepted organizations.DirectoryMutation,
	insert func(transaction) error,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insert(tx); err != nil {
		return err
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *DirectoryRepository) ReplaceTeamMembersAtomic(
	ctx context.Context,
	accepted organizations.TeamMembershipMutation,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	expectedVersion := accepted.Team.Version - 1
	var currentVersion int64
	err = tx.QueryRow(ctx, `
SELECT version FROM teams
WHERE id=$1::uuid AND msp_id=$2::uuid
FOR UPDATE
`, accepted.Team.ID, accepted.Team.MSPID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope.ErrNotFound
	}
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		return object.ErrVersionConflict
	}

	if len(accepted.TechnicianIDs) > 0 {
		rows, queryErr := tx.Query(ctx, `
SELECT id::text FROM technicians
WHERE msp_id=$1::uuid AND lifecycle_state='active'
  AND id=ANY($2::uuid[])
ORDER BY id
FOR SHARE
`, accepted.Team.MSPID, accepted.TechnicianIDs)
		if queryErr != nil {
			return queryErr
		}
		validated := make([]string, 0, len(accepted.TechnicianIDs))
		for rows.Next() {
			var technicianID string
			if scanErr := rows.Scan(&technicianID); scanErr != nil {
				rows.Close()
				return scanErr
			}
			validated = append(validated, technicianID)
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return rowsErr
		}
		if len(validated) != len(accepted.TechnicianIDs) {
			return scope.ErrNotFound
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE team_memberships
SET lifecycle_state='inactive', version=version+1,
    updated_at=$4, updated_by=$5::uuid
WHERE team_id=$1::uuid AND msp_id=$2::uuid
  AND lifecycle_state='active'
  AND NOT (technician_id=ANY($3::uuid[]))
`, accepted.Team.ID, accepted.Team.MSPID, accepted.TechnicianIDs,
		accepted.ChangedAt, accepted.ChangedBy); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO team_memberships(
  team_id,technician_id,msp_id,lifecycle_state,version,
  created_at,created_by,updated_at,updated_by
)
SELECT $1::uuid,member.id,$2::uuid,'active',1,$4,$5::uuid,$4,$5::uuid
FROM unnest($3::uuid[]) AS member(id)
ON CONFLICT(team_id,technician_id) DO UPDATE
SET lifecycle_state='active',
    version=team_memberships.version+1,
    updated_at=EXCLUDED.updated_at,
    updated_by=EXCLUDED.updated_by
WHERE team_memberships.msp_id=EXCLUDED.msp_id
  AND team_memberships.lifecycle_state='inactive'
`, accepted.Team.ID, accepted.Team.MSPID, accepted.TechnicianIDs,
		accepted.ChangedAt, accepted.ChangedBy); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
UPDATE teams SET version=$3
WHERE id=$1::uuid AND msp_id=$2::uuid AND version=$4
`, accepted.Team.ID, accepted.Team.MSPID, accepted.Team.Version, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return object.ErrVersionConflict
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *DirectoryRepository) ListDirectory(
	ctx context.Context,
	target scope.Target,
) (organizations.Directory, error) {
	rows, err := r.db.Query(ctx, `
SELECT kind, id::text, msp_id::text, client_id, department_id, team_id,
       key, name, version, member_ids, email, display_name
FROM (
  SELECT 'client' AS kind, id, msp_id, id::text AS client_id,
         '' AS department_id, '' AS team_id, display_id AS key, name, version,
         '{}'::text[] AS member_ids, '' AS email, '' AS display_name
  FROM client_organizations
  WHERE msp_id = $1 AND lifecycle_state = 'active'
    AND ($2 = '' OR id = NULLIF($2, '')::uuid)
  UNION ALL
  SELECT 'department' AS kind, id, msp_id, '' AS client_id,
         '' AS department_id, '' AS team_id, key, name, version,
         '{}'::text[] AS member_ids, '' AS email, '' AS display_name
  FROM departments
  WHERE msp_id = $1
  UNION ALL
  SELECT 'team' AS kind, id, msp_id, '' AS client_id,
         COALESCE(department_id::text, '') AS department_id,
         '' AS team_id, key, name, version,
         CASE WHEN $2 = '' THEN ARRAY(
           SELECT membership.technician_id::text
           FROM team_memberships membership
           WHERE membership.team_id=teams.id AND membership.msp_id=teams.msp_id
             AND membership.lifecycle_state = 'active'
           ORDER BY membership.technician_id::text
         ) ELSE '{}'::text[] END AS member_ids,
         '' AS email, '' AS display_name
  FROM teams WHERE msp_id = $1
  UNION ALL
  SELECT 'queue' AS kind, id, msp_id,
         COALESCE(client_id::text, '') AS client_id,
         COALESCE(department_id::text, '') AS department_id,
         COALESCE(team_id::text, '') AS team_id, key, name, version,
         '{}'::text[] AS member_ids, '' AS email, '' AS display_name
  FROM queues
  WHERE msp_id = $1 AND (
    ($2 = '' AND client_id IS NULL)
    OR ($2 <> '' AND (
      client_id IS NULL OR client_id = NULLIF($2, '')::uuid
    ))
  )
  UNION ALL
  SELECT 'technician' AS kind, id, msp_id, '' AS client_id,
         '' AS department_id, '' AS team_id, email AS key,
         display_name AS name, version, '{}'::text[] AS member_ids,
         email, display_name
  FROM technicians
  WHERE msp_id=$1 AND lifecycle_state='active' AND $2 = ''
) AS directory
ORDER BY kind, key, id
`, target.MSPID, target.ClientID)
	if err != nil {
		return organizations.Directory{}, err
	}
	defer rows.Close()
	result := organizations.Directory{
		Clients:     []organizations.Client{},
		Departments: []organizations.Department{},
		Teams:       []organizations.Team{},
		Queues:      []organizations.Queue{},
		Technicians: []organizations.Technician{},
	}
	for rows.Next() {
		var kind, id, mspID, clientID, departmentID, teamID, key, name string
		var email, displayName string
		var memberIDs []string
		var version int64
		if err := rows.Scan(
			&kind, &id, &mspID, &clientID, &departmentID, &teamID,
			&key, &name, &version, &memberIDs, &email, &displayName,
		); err != nil {
			return organizations.Directory{}, err
		}
		switch kind {
		case "client":
			result.Clients = append(result.Clients, organizations.Client{
				Envelope: object.Envelope{
					ID: id, ObjectType: "client_organization",
					MSPID: mspID, ClientID: clientID, DisplayID: key,
					LifecycleState: "active", Version: version,
				},
				Name: name,
			})
		case "department":
			result.Departments = append(result.Departments, organizations.Department{
				ID: id, MSPID: mspID, Key: key, Name: name, Version: version,
			})
		case "team":
			result.Teams = append(result.Teams, organizations.Team{
				ID: id, MSPID: mspID, DepartmentID: departmentID,
				Key: key, Name: name, Version: version, MemberIDs: memberIDs,
			})
		case "queue":
			result.Queues = append(result.Queues, organizations.Queue{
				ID: id, MSPID: mspID, ClientID: clientID,
				DepartmentID: departmentID, TeamID: teamID,
				Key: key, Name: name, Version: version,
			})
		case "technician":
			result.Technicians = append(result.Technicians, organizations.Technician{
				ID: id, MSPID: mspID, Email: email,
				DisplayName: displayName, Version: version,
			})
		default:
			return organizations.Directory{}, pgx.ErrNoRows
		}
	}
	if err := rows.Err(); err != nil {
		return organizations.Directory{}, err
	}
	return result, nil
}
