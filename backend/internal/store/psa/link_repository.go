package psa

import (
	"context"
	"fmt"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/links"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type LinkRepository struct {
	db database
}

var _ links.Repository = (*LinkRepository)(nil)

func NewLinkRepository(db database) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) CreateAtomic(
	ctx context.Context,
	accepted links.CreateMutation,
) error {
	sourceExists, ok := endpointExistsSQL(
		accepted.Link.Source.Type, "$4", accepted.Link.LinkType, true,
	)
	if !ok {
		return links.ErrInvalid
	}
	targetExists, ok := endpointExistsSQL(
		accepted.Link.Target.Type, "$5", accepted.Link.LinkType, false,
	)
	if !ok {
		return links.ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	link := accepted.Link
	query := fmt.Sprintf(`
INSERT INTO object_links (
  id, msp_id, client_id, source_type, source_id, target_type, target_id,
  link_type, created_at, created_by
)
SELECT $1, $2, $3, $6, $4, $7, $5, $8, $9, $10
WHERE EXISTS (%s)
  AND EXISTS (%s)
`, sourceExists, targetExists)
	tag, err := tx.Exec(
		ctx, query,
		link.ID, link.MSPID, link.ClientID, link.Source.ID, link.Target.ID,
		link.Source.Type, link.Target.Type, link.LinkType, link.CreatedAt, link.CreatedBy,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if tag.RowsAffected() != 1 {
		_ = tx.Rollback(ctx)
		return scope.ErrNotFound
	}
	if err := writeMutationFacts(ctx, tx, accepted.Audit, accepted.Event); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

func endpointExistsSQL(
	objectType, idPlaceholder, linkType string,
	source bool,
) (string, bool) {
	switch objectType {
	case "work_record":
		recordTypeConstraint := ""
		switch {
		case linkType == "caused_by_problem" && source:
			recordTypeConstraint = " AND record_type = 'incident'"
		case linkType == "caused_by_problem":
			recordTypeConstraint = " AND record_type = 'problem'"
		case linkType == "implemented_by_change" && !source:
			recordTypeConstraint = " AND record_type = 'change'"
		}
		return `SELECT 1 FROM work_records
WHERE id = ` + idPlaceholder + ` AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active' AND deleted_at IS NULL` + recordTypeConstraint, true
	case "asset":
		return `SELECT 1 FROM assets
WHERE id = ` + idPlaceholder + ` AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'`, true
	case "service":
		return `SELECT 1 FROM services
WHERE id = ` + idPlaceholder + ` AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'`, true
	case "contract":
		return `SELECT 1 FROM contracts
WHERE id = ` + idPlaceholder + ` AND msp_id = $2 AND client_id = $3
  AND lifecycle_state = 'active'
  AND starts_on <= CURRENT_DATE AND (ends_on IS NULL OR ends_on >= CURRENT_DATE)`, true
	default:
		return "", false
	}
}
