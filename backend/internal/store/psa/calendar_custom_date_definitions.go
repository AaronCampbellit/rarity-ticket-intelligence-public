package psa

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

// ListObjectCustomDateFields exposes active definitions only after checking the
// current source and persisted read grants. Calendar access alone is insufficient.
func (r *CalendarRepository) ListObjectCustomDateFields(ctx context.Context, principal authorization.Principal, objectType customfields.ObjectType, objectID string) ([]calendar.CustomDateField, error) {
	if r == nil || r.db == nil || !internalid.ValidCanonical(principal.ID) {
		return nil, authorization.ErrForbidden
	}
	source, err := NewCustomDateRepository(r.db).FindDateSource(ctx, scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}, objectType, objectID)
	if err != nil {
		return nil, err
	}
	var allowed bool
	if objectType == "task" {
		allowed, err = r.CanReadCalendarSource(ctx, principal, calendar.SourceRef{MSPID: source.MSPID, ClientID: source.ClientID, Type: "task", ID: source.ID})
	} else {
		capability := map[customfields.ObjectType]string{"work_record": "work_record.read", "project": "project.read", "asset": "search.read", "knowledge_article": "knowledge.read", "time_entry": "time_entry.read_scoped"}[objectType]
		if capability == "" {
			return nil, customfields.ErrInvalidCustomDate
		}
		err = r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_assignments assignment JOIN role_capabilities capability ON capability.role_id=assignment.role_id AND capability.msp_id=assignment.msp_id WHERE assignment.msp_id=$1::uuid AND assignment.technician_id=$2::uuid AND (assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid) AND (assignment.expires_at IS NULL OR assignment.expires_at>now()) AND capability.capability=$4)`, source.MSPID, principal.ID, source.ClientID, capability).Scan(&allowed)
	}
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scope.ErrNotFound
	}
	return r.listCustomDateDefinitions(ctx, source.MSPID, string(objectType))
}
