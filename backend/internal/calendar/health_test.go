package calendar

import (
	"reflect"
	"testing"
	"time"
)

func TestHealthPrecedenceAndAllReasonCodes(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	result := EvaluateHealth(HealthContext{
		Now: now, TerminalState: Active, SourceBlocked: true, UnmetDependency: true,
		HardConstraint: true, DueAt: &due, CapacityShortage: true,
		DependencyDelay: true, ScheduleVariance: true, SLAAtRisk: true,
	})
	if result.State != HealthBlocked {
		t.Fatalf("state=%q", result.State)
	}
	want := []string{"dependency_blocked", "hard_constraint", "source_blocked", "overdue", "capacity_shortage", "dependency_delay", "schedule_variance", "sla_at_risk"}
	if got := healthReasonCodes(result); !reflect.DeepEqual(got, want) {
		t.Fatalf("reasons=%v want=%v", got, want)
	}
	if result.RuleVersion != CurrentHealthRuleVersion || !result.EvaluatedAt.Equal(now) {
		t.Fatalf("metadata=%+v", result)
	}
}

func TestHealthUsesEndAsDueFallbackAndDeterministicPrecedence(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	end := now.Add(-time.Minute)
	result := EvaluateHealth(HealthContext{Now: now, TerminalState: Active, EndsAt: &end, RiskReasons: []HealthReason{{Code: "z_risk"}, {Code: "a_risk"}}})
	if result.State != HealthOverdue {
		t.Fatalf("state=%q", result.State)
	}
	if got := healthReasonCodes(result); !reflect.DeepEqual(got, []string{"overdue", "a_risk", "z_risk"}) {
		t.Fatalf("reasons=%v", got)
	}
}

func TestHealthAllDayDeadlineUsesTheEvaluationCalendarDate(t *testing.T) {
	deadline := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		zone string
		now  time.Time
		want HealthState
	}{
		{name: "still deadline date west of UTC", zone: "America/Los_Angeles", now: time.Date(2026, 8, 8, 0, 30, 0, 0, time.UTC), want: HealthOnTrack},
		{name: "next date east of UTC", zone: "Pacific/Kiritimati", now: time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC), want: HealthOverdue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, err := time.LoadLocation(test.zone)
			if err != nil {
				t.Fatal(err)
			}
			result := EvaluateHealth(HealthContext{Now: test.now.In(location), TerminalState: Active, EndsOn: &deadline})
			if result.State != test.want {
				t.Fatalf("now=%s deadline=%s state=%q want=%q", test.now.In(location), deadline.Format(time.DateOnly), result.State, test.want)
			}
		})
	}
}

func TestCompletedAndCancelledEventsHaveNoActiveHealthReasons(t *testing.T) {
	due := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for _, terminal := range []TerminalState{Completed, Cancelled} {
		result := EvaluateHealth(HealthContext{Now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), TerminalState: terminal, DueAt: &due, SourceBlocked: true, CapacityShortage: true})
		if result.State != HealthTerminal || len(result.Reasons) != 0 {
			t.Fatalf("terminal=%q health=%+v", terminal, result)
		}
	}
}

func TestHealthFallsThroughAtRiskAndOnTrack(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	if result := EvaluateHealth(HealthContext{Now: now, TerminalState: Active, CapacityShortage: true}); result.State != HealthAtRisk {
		t.Fatalf("risk=%+v", result)
	}
	if result := EvaluateHealth(HealthContext{Now: now, TerminalState: Active}); result.State != HealthOnTrack || len(result.Reasons) != 0 {
		t.Fatalf("on track=%+v", result)
	}
}

func healthReasonCodes(result HealthResult) []string {
	codes := make([]string, len(result.Reasons))
	for i, reason := range result.Reasons {
		codes[i] = reason.Code
	}
	return codes
}
