package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCapacityCountsOnlyAssignedScheduledEffort(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	found := CalculateCapacity(CapacityInput{
		Window:    QueryWindow{Start: start, End: start.Add(8 * time.Hour)},
		Available: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(8 * time.Hour)}, CapacityPercent: 100}},
		Events: []CapacityEvent{
			{Assigned: true, Mode: FixedBlock, PlannedMinutes: 120, Interval: TimeInterval{Start: start, End: start.Add(2 * time.Hour)}},
			{Assigned: true, Mode: Informational, PlannedMinutes: 60},
			{Assigned: false, Mode: EffortAllocation, PlannedMinutes: 180, Interval: TimeInterval{Start: start, End: start.Add(8 * time.Hour)}},
		},
	})
	if found.FixedMinutes != 120 || found.CommittedMinutes != 120 || found.RemainingMinutes != 360 || found.UnscheduledMinutes != 180 {
		t.Fatalf("capacity = %+v", found)
	}
}

func TestCapacityAllocatesEffortChronologicallyAfterFixedBlocks(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	found := CalculateCapacity(CapacityInput{
		Window:    QueryWindow{Start: start, End: start.Add(4 * time.Hour)},
		Available: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(4 * time.Hour)}, CapacityPercent: 100}},
		Events: []CapacityEvent{
			{ID: "fixed", Assigned: true, Mode: FixedBlock, PlannedMinutes: 60, Interval: TimeInterval{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)}},
			{ID: "effort", Assigned: true, Mode: EffortAllocation, PlannedMinutes: 240, Interval: TimeInterval{Start: start, End: start.Add(4 * time.Hour)}},
		},
	})
	if found.FixedMinutes != 60 || found.AllocatedMinutes != 180 || found.UnscheduledMinutes != 60 || found.RemainingMinutes != 0 {
		t.Fatalf("capacity = %+v", found)
	}
	if len(found.Segments) != 3 || found.Segments[0].EventID != "effort" || !found.Segments[0].Interval.Start.Equal(start) {
		t.Fatalf("segments = %+v", found.Segments)
	}
}

type capacityRepositoryStub struct {
	inputs map[string]CapacityInput
	got    authorization.Principal
}

func (s *capacityRepositoryStub) LoadCapacityInputs(_ context.Context, principal authorization.Principal, _ QueryWindow, _ []string) (map[string]CapacityInput, error) {
	s.got = principal
	return s.inputs, nil
}

func TestCapacityServiceAggregatesAllAuthorizedClientWork(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &capacityRepositoryStub{inputs: map[string]CapacityInput{"tech": {
		Window:    QueryWindow{Start: start, End: start.Add(8 * time.Hour)},
		Available: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(8 * time.Hour)}, CapacityPercent: 100}},
		Events: []CapacityEvent{
			{ID: "client-a", ClientID: "a", Assigned: true, Mode: FixedBlock, PlannedMinutes: 60, Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
			{ID: "client-b", ClientID: "b", Assigned: true, Mode: FixedBlock, PlannedMinutes: 60, Interval: TimeInterval{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)}},
		},
	}}}
	principal := authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: ""}}
	found, err := NewCapacityService(repo).Calculate(context.Background(), principal, QueryWindow{Start: start, End: start.Add(8 * time.Hour)}, []string{"tech"})
	if err != nil {
		t.Fatal(err)
	}
	if found["tech"].CommittedMinutes != 120 || repo.got.Scope.ClientID != "" {
		t.Fatalf("capacity = %+v, principal = %+v", found, repo.got)
	}
}

func TestCapacitySurfacesUnmetDependencyConstraints(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	found := CalculateCapacity(CapacityInput{Window: QueryWindow{Start: start, End: start.Add(2 * time.Hour)}, Available: []AvailabilitySegment{{Interval: TimeInterval{Start: start, End: start.Add(2 * time.Hour)}, CapacityPercent: 100}}, Dependencies: []DependencyConstraint{{ID: "dependency-1", Unmet: true, Interval: TimeInterval{Start: start, End: start.Add(45 * time.Minute)}, Related: SafeSourceRef{Type: "calendar_dependency", ID: "dependency-1"}}}})
	if found.DependencyBlockedMinutes != 45 {
		t.Fatalf("capacity=%+v", found)
	}
}
