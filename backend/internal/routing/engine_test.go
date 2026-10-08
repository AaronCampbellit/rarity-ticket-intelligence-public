package routing

import (
	"errors"
	"testing"
)

func TestEngineSelectsFirstMatchingRuleDeterministically(t *testing.T) {
	engine, err := NewEngine([]Rule{
		{ID: "critical", Position: 1, Priority: "critical", QueueID: "noc"},
		{ID: "client-default", Position: 2, ClientID: "client-a", QueueID: "service-desk"},
		{ID: "fallback", Position: 3, QueueID: "triage"},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	decision, err := engine.Route(Input{
		ClientID: "client-a", RecordType: "incident", Priority: "critical",
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if decision.QueueID != "noc" || decision.RuleID != "critical" ||
		decision.Explanation == "" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestEngineRejectsAmbiguousPositionsAndMissingFallback(t *testing.T) {
	tests := [][]Rule{
		{
			{ID: "one", Position: 1, QueueID: "q1"},
			{ID: "two", Position: 1, QueueID: "q2"},
		},
		{{ID: "conditional", Position: 1, Priority: "high", QueueID: "q1"}},
		{
			{ID: "duplicate", Position: 1, Priority: "high", QueueID: "q1"},
			{ID: "duplicate", Position: 2, QueueID: "q2"},
		},
		{
			{ID: "invalid-type", Position: 1, RecordType: "project", QueueID: "q1"},
			{ID: "fallback", Position: 2, QueueID: "q2"},
		},
	}
	for _, rules := range tests {
		if _, err := NewEngine(rules); !errors.Is(err, ErrInvalidRules) {
			t.Fatalf("NewEngine(%+v) error = %v", rules, err)
		}
	}
}
