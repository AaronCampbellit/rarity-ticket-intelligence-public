package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestSelectReturnsHighestPriorityMatchWithExplainableTrace(t *testing.T) {
	now := time.Date(2026, time.July, 30, 2, 0, 0, 0, time.UTC)
	engine, err := NewEngine([]Published{
		{ID: "fallback", Version: 1, Priority: 0, Fallback: true, Enabled: true},
		{ID: "client-incidents", Version: 3, Priority: 100, Enabled: true, Conditions: Conditions{
			ClientID: "client-id", RecordType: "incident",
		}},
		{ID: "future", Version: 1, Priority: 200, Enabled: true, EffectiveFrom: now.Add(time.Hour)},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	selection, err := engine.Select(Input{
		ClientID: "client-id", RecordType: "incident", EvaluatedAt: now,
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selection.WorkflowID != "client-incidents" || selection.Version != 3 {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if len(selection.Trace) != 3 || selection.Trace[0].WorkflowID != "future" ||
		selection.Trace[0].Matched {
		t.Fatalf("selection trace does not explain rejected higher priority: %+v", selection.Trace)
	}
}

func TestNewEngineRejectsAmbiguousPriorityAndMutablePublishedVersion(t *testing.T) {
	tests := [][]Published{
		{
			{ID: "one", Version: 1, Priority: 10, Enabled: true, Conditions: Conditions{RecordType: "incident"}},
			{ID: "two", Version: 1, Priority: 10, Enabled: true, Conditions: Conditions{RecordType: "incident"}},
			{ID: "fallback", Version: 1, Fallback: true, Enabled: true},
		},
		{{ID: "draft", Version: 0, Enabled: true, Fallback: true}},
	}
	for _, workflows := range tests {
		if _, err := NewEngine(workflows); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewEngine(%+v) error = %v", workflows, err)
		}
	}
}

func TestTransitionRequiresOwnerWhenWorkBecomesActive(t *testing.T) {
	definition := Definition{
		States: []State{
			{Key: "triage"},
			{Key: "in_progress", RequiresOwner: true},
		},
		Transitions: []Transition{{From: "triage", To: "in_progress"}},
	}
	if err := definition.ValidateTransition(TransitionInput{
		From: "triage", To: "in_progress",
	}); !errors.Is(err, ErrTransitionRequirements) {
		t.Fatalf("unowned transition error = %v", err)
	}
	if err := definition.ValidateTransition(TransitionInput{
		From: "triage", To: "in_progress", OwnerID: "tech-id",
	}); err != nil {
		t.Fatalf("owned transition rejected: %v", err)
	}
}

func TestDefinitionRejectsUnknownSLABehavior(t *testing.T) {
	definition := Definition{
		States:      []State{{Key: "new"}, {Key: "done", SLABehavior: SLABehavior("stop")}},
		Transitions: []Transition{{From: "new", To: "done"}},
	}
	if err := definition.Validate(); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Validate() error = %v", err)
	}
}
