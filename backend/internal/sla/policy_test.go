package sla

import (
	"errors"
	"testing"
)

func publishedPolicies() []PublishedPolicy {
	calendar := PublishedCalendar{
		ID: "calendar", Version: 2, Definition: businessCalendarDefinition(),
	}
	return []PublishedPolicy{
		{
			ID: "contract-policy", Version: 4, Priority: 100, StableOrder: 1,
			Enabled: true, Conditions: PolicyConditions{ContractID: "contract"},
			Calendar: calendar, ResponseTargetSeconds: 900,
			ResolutionTargetSeconds: 7200, WarningPercent: 80,
		},
		{
			ID: "fallback-policy", Version: 6, StableOrder: 2,
			Enabled: true, Fallback: true, Calendar: calendar,
			ResponseTargetSeconds: 3600, ResolutionTargetSeconds: 14400,
			WarningPercent: 80,
		},
	}
}

func TestPolicyEngineSelectsSpecificPolicyWithFullTrace(t *testing.T) {
	engine, err := NewPolicyEngine(publishedPolicies())
	if err != nil {
		t.Fatalf("NewPolicyEngine() error = %v", err)
	}
	selection, err := engine.Select(PolicyInput{
		ClientID: "client", RecordType: "incident", Priority: "critical",
		QueueID: "noc", ContractID: "contract",
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.Policy.ID != "contract-policy" || len(selection.Trace) != 2 ||
		!selection.Trace[0].Matched {
		t.Fatalf("unexpected policy selection: %+v", selection)
	}
}

func TestPolicyEngineRejectsMissingFallbackAndAmbiguousOrder(t *testing.T) {
	tests := [][]PublishedPolicy{
		{publishedPolicies()[0]},
		{
			publishedPolicies()[0],
			{
				ID: "fallback", Version: 1, Enabled: true, Fallback: true,
				Priority: 100, StableOrder: 1,
				Calendar:              publishedPolicies()[0].Calendar,
				ResponseTargetSeconds: 60, ResolutionTargetSeconds: 120,
				WarningPercent: 80,
			},
		},
	}
	for _, policies := range tests {
		if _, err := NewPolicyEngine(policies); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("NewPolicyEngine(%+v) error = %v", policies, err)
		}
	}
}
