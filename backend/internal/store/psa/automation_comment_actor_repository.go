package psa

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// AutomationCommentActorRepository resolves an automation definition's
// persisted creator without carrying definition capabilities into the human
// principal. Collaboration performs the creator's current role checks.
type AutomationCommentActorRepository struct {
	db database
}

var _ automation.AutomationCommentActorResolver = (*AutomationCommentActorRepository)(nil)

func NewAutomationCommentActorRepository(db database) *AutomationCommentActorRepository {
	return &AutomationCommentActorRepository{db: db}
}

func (r *AutomationCommentActorRepository) ResolveAutomationCommentActor(
	ctx context.Context,
	principal authorization.Principal,
) (authorization.Principal, error) {
	if r == nil || r.db == nil || principal.ID == "" ||
		principal.Scope.MSPID == "" || principal.Scope.ClientID == "" {
		return authorization.Principal{}, scope.ErrNotFound
	}

	var creatorID string
	err := r.db.QueryRow(ctx, `
SELECT definition.created_by::text
FROM automation_definitions definition
JOIN automation_versions version
  ON version.automation_id = definition.id
 AND version.msp_id = definition.msp_id
 AND version.version = definition.current_version
 AND version.state = 'published'
JOIN technicians creator
  ON creator.id = definition.created_by
 AND creator.msp_id = definition.msp_id
 AND creator.lifecycle_state = 'active'
WHERE definition.id = $1::uuid
  AND definition.msp_id = $2::uuid
  AND definition.enabled
  AND $3::uuid = ANY(version.client_scopes)
`, principal.ID, principal.Scope.MSPID, principal.Scope.ClientID).Scan(&creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return authorization.Principal{}, scope.ErrNotFound
	}
	if err != nil {
		return authorization.Principal{}, err
	}
	if creatorID == "" {
		return authorization.Principal{}, scope.ErrNotFound
	}
	return authorization.Principal{ID: creatorID, Scope: principal.Scope}, nil
}
