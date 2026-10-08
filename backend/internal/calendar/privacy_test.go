package calendar

import (
	"context"
	"testing"
	"time"
)

func TestHiddenSourceReturnsBusyWithoutAggregateLeak(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-secret"}, rows: []QueryProjection{queryProjection("secret", "client-secret", start)}, hidden: map[string]bool{"secret": true}, busy: map[string]bool{"secret": true}}
	auth := queryAuthorizerStub{hidden: map[string]bool{"source-secret": true}, workforce: map[string]bool{"tech": true}}
	service := NewQueryService(repo, auth, nil)
	found, err := service.List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil || len(found.Events) != 1 {
		t.Fatalf("events=%+v err=%v", found.Events, err)
	}
	event := found.Events[0]
	if event.Privacy != PrivacyBusy || event.Title != "Busy" || event.Source.ID != "" || event.Source.ClientID != "" || len(event.Tags) != 0 || event.EventRole != "" || event.Health != "" || event.ProjectionID != "" || event.OccurrenceKey != "" || event.SourceRevision != 0 || event.Recurrence != nil || len(event.HealthReasons) != 0 || event.PlannedMinutes != 0 || event.CapacityBearing || event.HasConflict {
		t.Fatalf("unsafe busy event=%+v", event)
	}
	options, err := service.FilterOptions(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil || len(options.Clients) != 0 || len(options.Tags) != 0 || options.Technicians["tech"] != 1 {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	capacity, err := service.Capacity(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil || capacity["tech"].CommittedMinutes != 60 {
		t.Fatalf("capacity=%+v err=%v", capacity, err)
	}
}

func TestHiddenSourceWithoutWorkforceAuthorityIsOmitted(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &queryRepositoryStub{authorized: []string{"client-secret"}, rows: []QueryProjection{queryProjection("secret", "client-secret", start)}, hidden: map[string]bool{"secret": true}}
	found, err := NewQueryService(repo, queryAuthorizerStub{hidden: map[string]bool{"source-secret": true}}, nil).List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil || len(found.Events) != 0 {
		t.Fatalf("events=%+v err=%v", found.Events, err)
	}
}

func TestBusyEventsIgnoreFiltersForFieldsRedactedFromBusyView(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	row := queryProjection("secret", "client-secret", start)
	row.Projection.Dimensions.ProjectIDs = []string{"secret-project"}
	row.Projection.EventRole = "scheduled_work"
	row.Health.State = HealthBlocked
	row.HasConflict = true
	repo := &queryRepositoryStub{authorized: []string{"client-secret"}, rows: []QueryProjection{row}, hidden: map[string]bool{"secret": true}, busy: map[string]bool{"secret": true}}
	auth := queryAuthorizerStub{hidden: map[string]bool{"source-secret": true}, workforce: map[string]bool{"tech": true}}
	filter := Filter{TagIDs: []string{"other-tag"}, ProjectIDs: []string{"other-project"}, EventRoles: []string{"due"}, HealthStates: []HealthState{HealthOnTrack}, ConflictsOnly: false, SourceTypes: []string{"work_record"}, ClientIDs: []string{"other-client"}}
	found, err := NewQueryService(repo, auth, nil).List(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, Filter: filter})
	if err != nil || len(found.Events) != 1 || found.Events[0].Privacy != PrivacyBusy {
		t.Fatalf("events=%+v err=%v", found.Events, err)
	}
}

func TestCapacitySkipsNonCapacityBearingBusyEventsAndHidesBusySegments(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	notCapacity := queryProjection("not-capacity", "client-secret", start)
	notCapacity.Projection.CapacityBearing = false
	capacity := queryProjection("capacity", "client-secret", start.Add(time.Hour))
	capacity.Projection.CapacityBearing = true
	repo := &queryRepositoryStub{authorized: []string{"client-secret"}, rows: []QueryProjection{notCapacity, capacity}, hidden: map[string]bool{"not-capacity": true, "capacity": true}, busy: map[string]bool{"not-capacity": true, "capacity": true}}
	auth := queryAuthorizerStub{hidden: map[string]bool{"source-not-capacity": true, "source-capacity": true}, workforce: map[string]bool{"tech": true}}
	found, err := NewQueryService(repo, auth, nil).Capacity(context.Background(), QueryRequest{Principal: calendarPrincipal(), Window: QueryWindow{Start: start, End: start.Add(4 * time.Hour)}})
	if err != nil || found["tech"].CommittedMinutes != 60 || found["tech"].FixedMinutes != 0 || found["tech"].AllocatedMinutes != 0 || len(found["tech"].Segments) != 0 {
		t.Fatalf("capacity=%+v err=%v", found, err)
	}
}
