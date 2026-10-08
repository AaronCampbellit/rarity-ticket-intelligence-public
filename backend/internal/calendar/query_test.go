package calendar

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type queryRepositoryStub struct {
	authorized []string
	rows       []QueryProjection
	gotClients []string
	hidden     map[string]bool
	busy       map[string]bool
}

type queryAvailabilityRepositoryStub struct {
	*queryRepositoryStub
	available map[string]ResolvedAvailability
	gotIDs    []string
}

func (r *queryAvailabilityRepositoryStub) LoadCalendarQueryAvailability(_ context.Context, _ string, ids []string, _ QueryWindow) (map[string]ResolvedAvailability, error) {
	r.gotIDs = append([]string(nil), ids...)
	return r.available, nil
}

func (r *queryRepositoryStub) AuthorizedCalendarClientIDs(context.Context, authorization.Principal) ([]string, error) {
	return append([]string(nil), r.authorized...), nil
}
func (r *queryRepositoryStub) ListCalendarProjections(_ context.Context, _ authorization.Principal, clients []string, _ QueryWindow, filter Filter, visibility QueryVisibility, limit int) ([]QueryProjection, error) {
	r.gotClients = append([]string(nil), clients...)
	allowed := map[string]bool{"": true}
	for _, id := range clients {
		allowed[id] = true
	}
	var found []QueryProjection
	for _, row := range r.rows {
		if visibility == QueryVisibilityFull && r.hidden[row.Projection.ID] {
			continue
		}
		if visibility == QueryVisibilityBusy && !r.busy[row.Projection.ID] {
			continue
		}
		if allowed[row.Projection.Source.ClientID] && matchesCalendarFilter(row, filter) {
			found = append(found, row)
			if len(found) == limit {
				break
			}
		}
	}
	return found, nil
}

func TestCalendarNarrowFilterIsAppliedBeforeRepositoryResultCap(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	rows := make([]QueryProjection, maximumCalendarResults+1)
	for i := range rows {
		rows[i] = queryProjection(fmt.Sprintf("bulk-%05d", i), "client-a", start)
		rows[i].Projection.Dimensions.TagIDs = []string{"other"}
	}
	rows[len(rows)-1].Projection.Dimensions.TagIDs = []string{"needle"}
	repo := &queryRepositoryStub{authorized: []string{"client-a"}, rows: rows}
	result, err := NewQueryService(repo, queryAuthorizerStub{}, nil).List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: Filter{TagIDs: []string{"needle"}}})
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("events=%d err=%v", len(result.Events), err)
	}
}

func TestCalendarPrivateFilterMatchesDoNotConsumeVisibleResultCap(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	rows := make([]QueryProjection, maximumCalendarResults+1)
	hidden := make(map[string]bool, len(rows))
	for i := range rows {
		rows[i] = queryProjection(fmt.Sprintf("private-%05d", i), "client-a", start)
		rows[i].Projection.Dimensions.TagIDs = []string{"private-match"}
		hidden[rows[i].Projection.ID] = true
	}
	repo := &queryRepositoryStub{authorized: []string{"client-a"}, rows: rows, hidden: hidden}
	result, err := NewQueryService(repo, queryAuthorizerStub{}, nil).List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: Filter{TagIDs: []string{"private-match"}}})
	if err != nil || len(result.Events) != 0 {
		t.Fatalf("private volume affected visible query: events=%d err=%v", len(result.Events), err)
	}
}

type queryAuthorizerStub struct {
	hidden    map[string]bool
	workforce map[string]bool
}

func (a queryAuthorizerStub) CanReadCalendarSource(_ context.Context, _ authorization.Principal, source SourceRef) (bool, error) {
	return !a.hidden[source.ID], nil
}
func (a queryAuthorizerStub) CanScheduleCalendarTechnician(_ context.Context, _ authorization.Principal, id string) (bool, error) {
	return a.workforce[id], nil
}

func queryProjection(id, client string, start time.Time) QueryProjection {
	end := start.Add(time.Hour)
	return QueryProjection{Projection: Projection{ID: id, EventRole: "scheduled_work", Source: SourceRef{MSPID: "msp", ClientID: client, Type: "task", ID: "source-" + id}, SourceRevision: 1, Title: "Event " + id, StartsAt: &start, EndsAt: &end, Timezone: "UTC", SchedulingMode: FixedBlock, CapacityBearing: true, AssigneeID: "tech", PlannedMinutes: 60, Dimensions: FilterDimensions{ClientIDs: []string{client}, TagIDs: []string{"tag"}}, TerminalState: Active}, Health: HealthResult{State: HealthOnTrack}}
}
func calendarPrincipal() authorization.Principal {
	return authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.read")}
}

func TestCalendarQueryDefaultsToAllAuthorizedClientsAndIgnoresActiveClient(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-a", "client-c"}, rows: []QueryProjection{queryProjection("a", "client-a", start), queryProjection("b", "client-b", start), queryProjection("c", "client-c", start)}}
	principal := calendarPrincipal()
	principal.Scope.ClientID = "client-a"
	service := NewQueryService(repo, queryAuthorizerStub{}, nil)
	found, err := service.List(context.Background(), QueryRequest{Principal: principal, Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]bool{}
	for _, event := range found.Events {
		clients[event.Source.ClientID] = true
	}
	if len(found.Events) != 2 || !clients["client-a"] || !clients["client-c"] {
		t.Fatalf("events=%+v", found.Events)
	}
}

func TestCalendarQueryIntersectsExplicitClientFilterAndRejectsOversizedWindow(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-a", "client-c"}, rows: []QueryProjection{queryProjection("a", "client-a", start), queryProjection("c", "client-c", start)}}
	service := NewQueryService(repo, queryAuthorizerStub{}, nil)
	found, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: Filter{ClientIDs: []string{"client-c", "client-x"}}})
	if err != nil || len(found.Events) != 1 || found.Events[0].Source.ClientID != "client-c" {
		t.Fatalf("events=%+v err=%v", found.Events, err)
	}
	_, err = service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(367 * 24 * time.Hour)}})
	if !errors.Is(err, ErrWindowTooLarge) {
		t.Fatalf("error=%v", err)
	}
}

func TestCalendarQueryExplicitEmptyClientFilterReturnsNoFullEvents(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-a"}, rows: []QueryProjection{queryProjection("a", "client-a", start)}, hidden: map[string]bool{"a": true}, busy: map[string]bool{"a": true}}
	service := NewQueryService(repo, queryAuthorizerStub{hidden: map[string]bool{"source-a": true}, workforce: map[string]bool{"tech": true}}, nil)
	all, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: Filter{ClientIDs: nil}})
	if err != nil || len(all.Events) != 1 {
		t.Fatalf("nil client filter events=%+v err=%v", all.Events, err)
	}
	none, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: Filter{ClientIDs: []string{}}})
	if err != nil || len(none.Events) != 0 {
		t.Fatalf("explicit-empty client filter broadened: events=%+v err=%v", none.Events, err)
	}
}

func TestCalendarAgendaCursorIsStableAcrossEqualStartTimes(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-a"}, rows: []QueryProjection{queryProjection("c", "client-a", start), queryProjection("a", "client-a", start), queryProjection("b", "client-a", start)}}
	service := NewQueryService(repo, queryAuthorizerStub{}, nil)
	first, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Limit: 2})
	if err != nil || len(first.Events) != 2 || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Events) != 1 || second.Events[0].OccurrenceID == first.Events[0].OccurrenceID || second.Events[0].OccurrenceID == first.Events[1].OccurrenceID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestCalendarQueryExpandsRecurrenceOnlyInsideWindow(t *testing.T) {
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	row := queryProjection("repeat", "client-a", start)
	row.Projection.Recurrence = &RecurrenceRule{Frequency: Daily, Interval: 1}
	repo := &queryRepositoryStub{authorized: []string{"client-a"}, rows: []QueryProjection{row}}
	found, err := NewQueryService(repo, queryAuthorizerStub{}, nil).List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start.Add(9 * 24 * time.Hour), End: start.Add(11 * 24 * time.Hour)}})
	if err != nil || len(found.Events) != 2 {
		t.Fatalf("events=%+v err=%v", found.Events, err)
	}
}

func TestCalendarCapacityUsesAvailabilityAfterPrivacyFiltering(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	row := queryProjection("effort", "client-a", start)
	row.Projection.SchedulingMode = EffortAllocation
	row.Projection.PlannedMinutes = 120
	end := start.Add(4 * time.Hour)
	row.Projection.EndsAt = &end
	base := &queryRepositoryStub{authorized: []string{"client-a"}, rows: []QueryProjection{row}}
	repo := &queryAvailabilityRepositoryStub{queryRepositoryStub: base, available: map[string]ResolvedAvailability{"tech": {Segments: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: end}, CapacityPercent: 100}}, AvailableMinutes: 240}}}
	capacity, err := NewQueryService(repo, queryAuthorizerStub{workforce: map[string]bool{"tech": true}}, nil).Capacity(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: end}})
	if err != nil || capacity["tech"].AllocatedMinutes != 120 || capacity["tech"].RemainingMinutes != 120 {
		t.Fatalf("capacity=%+v err=%v", capacity, err)
	}
}

func TestCalendarCapacityLoadsAndAggregatesOnlyWorkforceAuthorizedTechnicians(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	authorized := queryProjection("authorized", "client-a", start)
	denied := queryProjection("denied", "client-a", start)
	authorized.Projection.AssigneeID = "tech-authorized"
	denied.Projection.AssigneeID = "tech-denied"
	base := &queryRepositoryStub{authorized: []string{"client-a"}, rows: []QueryProjection{authorized, denied}}
	repo := &queryAvailabilityRepositoryStub{queryRepositoryStub: base, available: map[string]ResolvedAvailability{
		"tech-authorized": {Segments: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(8 * time.Hour)}, CapacityPercent: 100}}},
		"tech-denied":     {Segments: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(8 * time.Hour)}, CapacityPercent: 100}}, TentativeMinutes: 120},
	}}
	result, err := NewQueryService(repo, queryAuthorizerStub{workforce: map[string]bool{"tech-authorized": true}}, nil).Capacity(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(8 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.gotIDs) != 1 || repo.gotIDs[0] != "tech-authorized" {
		t.Fatalf("availability loaded for unauthorized technicians: %v", repo.gotIDs)
	}
	if _, exists := result["tech-denied"]; exists {
		t.Fatalf("capacity exposed unauthorized technician: %+v", result)
	}
}
