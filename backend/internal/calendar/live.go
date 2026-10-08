package calendar

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type LiveChangeType string

const (
	LiveUpsert LiveChangeType = "upsert"
	LiveRemove LiveChangeType = "remove"
)

type LiveRepositoryChange struct {
	Cursor     uint64
	Type       LiveChangeType
	Projection *QueryProjection
	Source     SourceRef
	EventRole  string
	AssigneeID string
}
type LiveRepositoryPage struct {
	Changes                    []LiveRepositoryChange
	OldestCursor, LatestCursor uint64
	Expired                    bool
}
type CalendarLiveRepository interface {
	AuthorizedCalendarClientIDs(context.Context, authorization.Principal) ([]string, error)
	ListCalendarChangesAfter(context.Context, string, []string, uint64, int) (LiveRepositoryPage, error)
}
type LiveRequest struct {
	Principal authorization.Principal
	Cursor    string
	Limit     int
}
type LiveChange struct {
	Type    LiveChangeType `json:"type"`
	Event   *EventView     `json:"event,omitempty"`
	EventID string         `json:"event_id,omitempty"`
	Source  SourceRef      `json:"source,omitempty"`
}
type LivePage struct {
	Changes         []LiveChange `json:"changes"`
	Cursor          string       `json:"cursor"`
	RefetchRequired bool         `json:"refetch_required"`
}
type LiveService struct {
	repository CalendarLiveRepository
	authorizer SourceVisibilityAuthorizer
	recurrence CalendarRecurrenceExpander
}

func NewLiveService(repository CalendarLiveRepository, authorizer SourceVisibilityAuthorizer, recurrence CalendarRecurrenceExpander) *LiveService {
	return &LiveService{repository: repository, authorizer: authorizer, recurrence: recurrence}
}

func (s *LiveService) ListAfter(ctx context.Context, request LiveRequest) (LivePage, error) {
	if s == nil || s.repository == nil || s.authorizer == nil || strings.TrimSpace(request.Principal.Scope.MSPID) == "" {
		return LivePage{}, ErrInvalidCalendarQuery
	}
	principal := request.Principal
	principal.Scope.ClientID = ""
	if err := authorization.Authorize(principal, "calendar.read", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		return LivePage{}, err
	}
	cursor, ok := decodeLiveCursor(request.Cursor)
	limit := request.Limit
	if limit == 0 {
		limit = 250
	}
	if limit < 1 || limit > 1000 {
		return LivePage{}, ErrResultTooLarge
	}
	clients, err := s.repository.AuthorizedCalendarClientIDs(ctx, principal)
	if err != nil {
		return LivePage{}, err
	}
	page, err := s.repository.ListCalendarChangesAfter(ctx, principal.Scope.MSPID, clients, cursor, limit)
	if err != nil {
		return LivePage{}, err
	}
	if !ok {
		return LivePage{Changes: []LiveChange{}, Cursor: encodeLiveCursor(page.LatestCursor), RefetchRequired: true}, nil
	}
	if cursor > page.LatestCursor {
		return LivePage{Changes: []LiveChange{}, Cursor: encodeLiveCursor(page.LatestCursor), RefetchRequired: true}, nil
	}
	result := LivePage{Changes: []LiveChange{}, Cursor: encodeLiveCursor(page.LatestCursor), RefetchRequired: page.Expired}
	if page.Expired {
		return result, nil
	}
	query := NewQueryService(nil, s.authorizer, s.recurrence)
	deliveredCursor := cursor
	for _, change := range page.Changes {
		if change.Cursor > deliveredCursor {
			deliveredCursor = change.Cursor
		}
		switch change.Type {
		case LiveRemove:
			read, authorizeErr := s.authorizer.CanReadCalendarSource(ctx, principal, change.Source)
			if authorizeErr != nil {
				return LivePage{}, authorizeErr
			}
			schedule := false
			if change.AssigneeID != "" {
				schedule, authorizeErr = s.authorizer.CanScheduleCalendarTechnician(ctx, principal, change.AssigneeID)
				if authorizeErr != nil {
					return LivePage{}, authorizeErr
				}
			}
			if !read && !schedule {
				result.RefetchRequired = true
				continue
			}
			result.Changes = append(result.Changes, LiveChange{Type: LiveRemove, EventID: stableEventID(change.Source, change.EventRole)})
		case LiveUpsert:
			if change.Projection == nil {
				result.RefetchRequired = true
				continue
			}
			if change.Projection.Projection.Recurrence != nil {
				// The live subscription has no visible-window contract. Expanding a
				// recurring series around its origin would silently miss changes in
				// the subscriber's current window, so force the authoritative query.
				result.RefetchRequired = true
				continue
			}
			occurrences, expandErr := query.expand(ctx, *change.Projection, liveProjectionWindow(change.Projection.Projection))
			if expandErr != nil {
				result.RefetchRequired = true
				continue
			}
			for _, occ := range occurrences {
				item, visible, viewErr := query.eventItem(ctx, principal, *change.Projection, occ)
				if viewErr != nil {
					return LivePage{}, viewErr
				}
				if visible {
					view := item.view
					result.Changes = append(result.Changes, LiveChange{Type: LiveUpsert, Event: &view})
				}
			}
		}
	}
	result.Cursor = encodeLiveCursor(deliveredCursor)
	return result, nil
}
func liveProjectionWindow(p Projection) QueryWindow {
	if p.AllDay && p.StartsOn != nil {
		end := p.StartsOn.AddDate(0, 0, 2)
		if p.EndsOn != nil {
			end = p.EndsOn.AddDate(0, 0, 1)
		}
		return QueryWindow{Start: p.StartsOn.AddDate(0, 0, -1), End: end}
	}
	if p.StartsAt != nil {
		end := p.StartsAt.Add(48 * time.Hour)
		if p.EndsAt != nil {
			end = p.EndsAt.Add(time.Second)
		}
		return QueryWindow{Start: p.StartsAt.Add(-time.Second), End: end}
	}
	return QueryWindow{}
}
func encodeLiveCursor(v uint64) string {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, v)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeLiveCursor(v string) (uint64, bool) {
	if v == "" {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(raw) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(raw), true
}
