package calendar

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"strings"
)

// List exposes only edges whose two source records remain readable. Busy-only
// workforce authority never grants dependency identity or membership.
func (s *DependencyService) List(ctx context.Context, principal authorization.Principal, projectionID string) ([]Dependency, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(projectionID) == "" {
		return nil, ErrInvalidDependency
	}
	principal.Scope.ClientID = ""
	if err := authorization.Authorize(principal, "calendar.read", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		return nil, err
	}
	visibility, ok := s.repository.(SourceVisibilityAuthorizer)
	if !ok {
		return nil, ErrInvalidDependency
	}
	source, err := s.repository.LoadDependencyProjection(ctx, projectionID)
	if err != nil {
		return nil, err
	}
	if source.Source.MSPID != principal.Scope.MSPID {
		return nil, scope.ErrNotFound
	}
	allowed, err := visibility.CanReadCalendarSource(ctx, principal, source.Source)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scope.ErrNotFound
	}
	if source.Source.ClientID == "" {
		return []Dependency{}, nil
	}
	edges, err := s.repository.ListDependencies(ctx, principal.Scope.MSPID, source.Source.ClientID)
	if err != nil {
		return nil, err
	}
	if len(edges) > maximumCalendarResults {
		return nil, ErrResultTooLarge
	}
	result := []Dependency{}
	for _, edge := range edges {
		if edge.MSPID != principal.Scope.MSPID || edge.ClientID != source.Source.ClientID || (edge.PredecessorID != projectionID && edge.SuccessorID != projectionID) {
			continue
		}
		visible := true
		for _, id := range []string{edge.PredecessorID, edge.SuccessorID} {
			p, loadErr := s.repository.LoadDependencyProjection(ctx, id)
			if loadErr != nil {
				return nil, loadErr
			}
			if p.Source.MSPID != principal.Scope.MSPID || p.Source.ClientID != source.Source.ClientID {
				visible = false
				break
			}
			allowed, readErr := visibility.CanReadCalendarSource(ctx, principal, p.Source)
			if readErr != nil {
				return nil, readErr
			}
			if !allowed {
				visible = false
				break
			}
		}
		if visible {
			result = append(result, edge)
		}
	}
	return result, nil
}
