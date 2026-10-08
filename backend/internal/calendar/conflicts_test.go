package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestProtectedPTOIsHardConflict(t *testing.T) {
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	found := EvaluateConflicts(ConflictInput{
		Policies:    []ConflictPolicy{{ID: "policy", Kind: ConflictApprovedPTO, Severity: ConflictHard, Version: 3}},
		Proposed:    ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
		Constraints: []ConflictConstraint{{Kind: ConflictApprovedPTO, Interval: TimeInterval{Start: start, End: start.Add(2 * time.Hour)}, Related: SafeSourceRef{Type: "pto", ID: "pto-1"}}},
	})
	if len(found) != 1 || found[0].Severity != ConflictHard || found[0].PolicyID != "policy" || found[0].PolicyVersion != 3 {
		t.Fatalf("conflicts = %+v", found)
	}
}

type conflictRepositoryStub struct{ input ConflictInput }

func (s *conflictRepositoryStub) LoadConflictInput(context.Context, authorization.Principal, ProposedSchedule) (ConflictInput, error) {
	return s.input, nil
}

func TestConflictServiceEvaluatesRepositoryPolicies(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	proposed := ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}}
	repository := &conflictRepositoryStub{input: ConflictInput{Policies: []ConflictPolicy{{ID: "policy", Kind: ConflictNonWorkingTime, Severity: ConflictWarning, Version: 2}}, Constraints: []ConflictConstraint{{Kind: ConflictNonWorkingTime, Interval: proposed.Interval}}}}
	principal := authorization.Principal{ID: "actor", Scope: scope.Principal{MSPID: "msp"}}
	found, err := NewConflictService(repository).Evaluate(context.Background(), principal, proposed)
	if err != nil || len(found) != 1 || found[0].Severity != ConflictWarning {
		t.Fatalf("conflicts=%+v err=%v", found, err)
	}
}

func TestOrdinaryOverbookingDefaultsToOverrideWithReason(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	found := EvaluateConflicts(ConflictInput{
		Proposed:    ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
		Constraints: []ConflictConstraint{{Kind: ConflictOrdinaryOverbooking, Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}, Related: SafeSourceRef{Type: "task", ID: "task-1"}}},
	})
	if len(found) != 1 || found[0].Severity != ConflictOverrideable || !found[0].ReasonRequired || found[0].ReasonCode != "ordinary_overbooking" || found[0].PolicyID == "" || found[0].PolicyVersion != 1 {
		t.Fatalf("conflicts = %+v", found)
	}
	if found[0].Related != (SafeSourceRef{Type: "task", ID: "task-1"}) {
		t.Fatalf("unsafe or missing source reference = %+v", found[0].Related)
	}
}

func TestDependencyConstraintSurfacesAsSafeHardConflict(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	found := EvaluateConflicts(ConflictInput{
		Proposed:     ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
		Dependencies: []DependencyConstraint{{ID: "dependency-1", Unmet: true, Interval: TimeInterval{Start: start, End: start.Add(30 * time.Minute)}, Related: SafeSourceRef{Type: "calendar_dependency", ID: "dependency-1"}}},
	})
	if len(found) != 1 || found[0].ReasonCode != "dependency_constraint" || found[0].Severity != ConflictHard || found[0].Related.ID != "dependency-1" {
		t.Fatalf("conflicts=%+v", found)
	}
}

func TestMaintenanceWindowPolicyIsAuthoritativeWithoutConfiguredGenericPolicy(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	windowPolicy := &ConflictPolicyEvidence{ID: "maintenance_window:window-1", Version: 7, Severity: ConflictWarning}
	found := EvaluateConflicts(ConflictInput{
		Proposed:    ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
		Constraints: []ConflictConstraint{{Kind: ConflictProtectedMaintenance, Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}, Policy: windowPolicy}},
	})
	if len(found) != 1 || found[0].Severity != ConflictWarning || found[0].PolicyID != windowPolicy.ID || found[0].PolicyVersion != 7 {
		t.Fatalf("conflicts=%+v", found)
	}
}

func TestMaintenanceConflictUsesStrongestConfiguredOrWindowPolicy(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name       string
		admin      ConflictSeverity
		window     ConflictSeverity
		want       ConflictSeverity
		wantPolicy string
	}{
		{name: "admin hard cannot be weakened", admin: ConflictHard, window: ConflictInfo, want: ConflictHard, wantPolicy: "admin"},
		{name: "window hard cannot be weakened", admin: ConflictWarning, window: ConflictHard, want: ConflictHard, wantPolicy: "maintenance_window:window-1"},
		{name: "equal favors specific evidence", admin: ConflictWarning, window: ConflictWarning, want: ConflictWarning, wantPolicy: "maintenance_window:window-1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			found := EvaluateConflicts(ConflictInput{
				Policies:    []ConflictPolicy{{ID: "admin", Kind: ConflictProtectedMaintenance, Severity: testCase.admin, Version: 3}},
				Proposed:    ProposedSchedule{Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}},
				Constraints: []ConflictConstraint{{Kind: ConflictProtectedMaintenance, Interval: TimeInterval{Start: start, End: start.Add(time.Hour)}, Policy: &ConflictPolicyEvidence{ID: "maintenance_window:window-1", Version: 7, Severity: testCase.window}}},
			})
			if len(found) != 1 || found[0].Severity != testCase.want || found[0].PolicyID != testCase.wantPolicy {
				t.Fatalf("conflicts=%+v", found)
			}
		})
	}
}
