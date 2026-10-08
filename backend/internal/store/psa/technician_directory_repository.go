package psa

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/objectidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type TechnicianDirectoryRepository struct {
	db database
}

var _ workrecords.AssignmentDirectory = (*TechnicianDirectoryRepository)(nil)

func NewTechnicianDirectoryRepository(db database) *TechnicianDirectoryRepository {
	return &TechnicianDirectoryRepository{db: db}
}

func (r *TechnicianDirectoryRepository) ResolveAssignmentCandidates(
	ctx context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]workrecords.AssignmentCandidate, error) {
	if limit < 1 || limit > 2 {
		limit = 2
	}
	normalized := objectidentity.Normalize(reference)
	if target.MSPID == "" || target.ClientID == "" || normalized == "" {
		return nil, workrecords.ErrInvalid
	}
	rows, err := r.db.Query(ctx, `
SELECT t.id::text, t.msp_id::text, t.display_name, t.email, t.version
FROM technicians t
WHERE t.msp_id = $1
  AND t.lifecycle_state = 'active'
  AND (
    lower(regexp_replace(btrim(t.display_name), '\s+', ' ', 'g')) = $3
    OR lower(regexp_replace(btrim(t.email), '\s+', ' ', 'g')) = $3
  )
  AND EXISTS (
    SELECT 1 FROM role_assignments ra
    WHERE ra.technician_id = t.id AND ra.msp_id = t.msp_id
      AND (ra.client_id IS NULL OR ra.client_id = $2)
      AND (ra.expires_at IS NULL OR ra.expires_at > $4)
  )
ORDER BY t.id
LIMIT $5
`, target.MSPID, target.ClientID, normalized, time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]workrecords.AssignmentCandidate, 0)
	for rows.Next() {
		var candidate workrecords.AssignmentCandidate
		if err := rows.Scan(
			&candidate.ID, &candidate.MSPID, &candidate.DisplayName,
			&candidate.Email, &candidate.Version,
		); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

// FindAssignmentIdentity resolves a known current owner to a safe display
// identity. It deliberately permits inactive or no-longer-scoped technicians:
// their historical assignment remains meaningful in an assignment preview.
func (r *TechnicianDirectoryRepository) FindAssignmentIdentity(
	ctx context.Context,
	target scope.Target,
	id string,
) (workrecords.AssignmentCandidate, error) {
	if target.MSPID == "" || target.ClientID == "" || strings.TrimSpace(id) == "" {
		return workrecords.AssignmentCandidate{}, workrecords.ErrInvalid
	}
	var candidate workrecords.AssignmentCandidate
	err := r.db.QueryRow(ctx, `
SELECT id::text, msp_id::text, display_name, email, version
FROM technicians
WHERE id = $1::uuid AND msp_id = $2
`, id, target.MSPID).Scan(
		&candidate.ID, &candidate.MSPID, &candidate.DisplayName,
		&candidate.Email, &candidate.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workrecords.AssignmentCandidate{}, scope.ErrNotFound
	}
	return candidate, err
}
