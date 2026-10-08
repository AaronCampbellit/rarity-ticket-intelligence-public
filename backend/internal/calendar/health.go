package calendar

import (
	"sort"
	"strings"
	"time"
)

type HealthState string

const (
	HealthBlocked  HealthState = "blocked"
	HealthOverdue  HealthState = "overdue"
	HealthAtRisk   HealthState = "at_risk"
	HealthOnTrack  HealthState = "on_track"
	HealthTerminal HealthState = "terminal"
)

const CurrentHealthRuleVersion int64 = 2

type HealthReason struct {
	Code string `json:"code"`
}

type HealthContext struct {
	Now              time.Time
	TerminalState    TerminalState
	SourceBlocked    bool
	UnmetDependency  bool
	HardConstraint   bool
	DueAt            *time.Time
	EndsAt           *time.Time
	DueOn            *time.Time
	EndsOn           *time.Time
	CapacityShortage bool
	DependencyDelay  bool
	ScheduleVariance bool
	SLAAtRisk        bool
	SLAAtRiskAt      *time.Time
	BlockedReasons   []HealthReason
	RiskReasons      []HealthReason
}

type HealthResult struct {
	State       HealthState    `json:"state"`
	Reasons     []HealthReason `json:"reasons"`
	RuleVersion int64          `json:"rule_version"`
	EvaluatedAt time.Time      `json:"evaluated_at"`
}

// EvaluateHealth records every applicable reason and selects one visible state
// using the fixed blocked > overdue > at-risk > on-track precedence.
func EvaluateHealth(context HealthContext) HealthResult {
	result := HealthResult{State: HealthOnTrack, Reasons: []HealthReason{}, RuleVersion: CurrentHealthRuleVersion, EvaluatedAt: context.Now}
	if context.TerminalState == Completed || context.TerminalState == Cancelled {
		result.State = HealthTerminal
		return result
	}
	blocked := append([]HealthReason(nil), context.BlockedReasons...)
	if context.SourceBlocked {
		blocked = append(blocked, HealthReason{Code: "source_blocked"})
	}
	if context.UnmetDependency {
		blocked = append(blocked, HealthReason{Code: "dependency_blocked"})
	}
	if context.HardConstraint {
		blocked = append(blocked, HealthReason{Code: "hard_constraint"})
	}
	blocked = normalizedHealthReasons(blocked)

	overdue := false
	due := context.DueAt
	if due == nil {
		due = context.EndsAt
	}
	if due != nil && !due.After(context.Now) {
		overdue = true
	} else if due == nil {
		deadline := context.DueOn
		if deadline == nil {
			deadline = context.EndsOn
		}
		if deadline != nil {
			year, month, day := context.Now.Date()
			today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			overdue = deadline.Before(today)
		}
	}

	risk := append([]HealthReason(nil), context.RiskReasons...)
	if context.CapacityShortage {
		risk = append(risk, HealthReason{Code: "capacity_shortage"})
	}
	if context.DependencyDelay {
		risk = append(risk, HealthReason{Code: "dependency_delay"})
	}
	if context.ScheduleVariance {
		risk = append(risk, HealthReason{Code: "schedule_variance"})
	}
	if context.SLAAtRisk || (context.SLAAtRiskAt != nil && !context.SLAAtRiskAt.After(context.Now)) {
		risk = append(risk, HealthReason{Code: "sla_at_risk"})
	}
	risk = normalizedHealthReasons(risk)

	result.Reasons = append(result.Reasons, blocked...)
	if overdue {
		result.Reasons = append(result.Reasons, HealthReason{Code: "overdue"})
	}
	result.Reasons = append(result.Reasons, risk...)
	switch {
	case len(blocked) > 0:
		result.State = HealthBlocked
	case overdue:
		result.State = HealthOverdue
	case len(risk) > 0:
		result.State = HealthAtRisk
	default:
		result.State = HealthOnTrack
	}
	return result
}

func normalizedHealthReasons(values []HealthReason) []HealthReason {
	seen := map[string]struct{}{}
	result := make([]HealthReason, 0, len(values))
	for _, reason := range values {
		reason.Code = strings.TrimSpace(reason.Code)
		if reason.Code == "" {
			continue
		}
		if _, ok := seen[reason.Code]; ok {
			continue
		}
		seen[reason.Code] = struct{}{}
		result = append(result, reason)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}
