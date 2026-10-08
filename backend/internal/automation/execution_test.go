package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type runStore struct {
	existing               *Run
	begun                  Run
	steps                  []StepRecord
	completed              RunCompletion
	failure                RunFailure
	deadLetter             DeadLetter
	deadLetteredAtomically bool
	suspension             Suspension
}

func (s *runStore) Begin(_ context.Context, run Run) (*Run, error) {
	s.begun = run
	return s.existing, nil
}
func (s *runStore) RecordStep(_ context.Context, step StepRecord) error {
	s.steps = append(s.steps, step)
	return nil
}
func (s *runStore) Complete(_ context.Context, completion RunCompletion) error {
	s.completed = completion
	return nil
}
func (s *runStore) Fail(_ context.Context, failure RunFailure) error {
	s.failure = failure
	return nil
}
func (s *runStore) PutDeadLetter(_ context.Context, deadLetter DeadLetter) error {
	s.deadLetter = deadLetter
	return nil
}
func (s *runStore) DeadLetter(
	_ context.Context,
	failure RunFailure,
	deadLetter DeadLetter,
) error {
	s.failure = failure
	s.deadLetter = deadLetter
	s.deadLetteredAtomically = true
	return nil
}
func (s *runStore) Suspend(_ context.Context, suspension Suspension) error {
	s.suspension = suspension
	return nil
}

type actionExecutor struct {
	principal authorization.Principal
	action    Action
	err       error
	snapshots []map[string]string
	outputs   []map[string]string
}

func (e *actionExecutor) Execute(
	_ context.Context,
	principal authorization.Principal,
	action Action,
	snapshot map[string]string,
) (ActionResult, error) {
	e.principal = principal
	e.action = action
	e.snapshots = append(e.snapshots, snapshot)
	result := ActionResult{ChangedObjectIDs: []string{"work-id"}}
	if len(e.outputs) >= len(e.snapshots) {
		result.OutputSnapshot = e.outputs[len(e.snapshots)-1]
	}
	return result, e.err
}

type connectionGate struct {
	ref      string
	clientID string
	err      error
}

func (g *connectionGate) Authorize(
	_ context.Context,
	ref, _, clientID string,
	_ ActionKind,
) error {
	g.ref, g.clientID = ref, clientID
	return g.err
}

func TestEngineRunsWithPublishedAllowlistedAutomationPrincipal(t *testing.T) {
	now := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	store := &runStore{}
	executor := &actionExecutor{}
	gate := &connectionGate{}
	engine := NewEngine(store, executor, gate, func() time.Time { return now }, sequenceAutomationIDs("run-id"))
	definition := publishedDefinition(Action{
		Kind: ActionCallHTTP, ConnectionRef: "connection-id",
		Parameters: map[string]string{"request_template_id": "template-id"},
	})

	result, err := engine.Run(context.Background(), RunCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event-id", Type: "work_record.created", MSPID: "msp-id",
			ClientID: "client-alpha", CausationID: "cause-id",
			InputSnapshot: map[string]string{"priority": "critical"},
		},
		IdempotencyKey: "event-id|automation-id|1", Attempt: 1, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !executor.principal.Capabilities.Has("work_record.edit") ||
		executor.principal.Capabilities.Has("administrator") ||
		executor.principal.ID != definition.ID ||
		executor.principal.Scope.ClientID != "client-alpha" {
		t.Fatalf("automation principal widened authority: %+v", executor.principal)
	}
	if gate.ref != "connection-id" || gate.clientID != "client-alpha" ||
		store.completed.RunID != "run-id" ||
		store.begun.MaxAttempts != 3 ||
		len(result.ChangedObjectIDs) != 1 {
		t.Fatalf("execution evidence incomplete: gate=%+v store=%+v result=%+v", gate, store, result)
	}
}

func TestEngineRejectsScopeTriggerAndDepthEscalation(t *testing.T) {
	engine := NewEngine(&runStore{}, &actionExecutor{}, &connectionGate{}, time.Now, sequenceAutomationIDs("run-id"))
	definition := publishedDefinition(Action{
		Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
	})
	cases := []TriggerEvent{
		{ID: "event", Type: "wrong.event", MSPID: "msp-id", ClientID: "client-alpha"},
		{ID: "event", Type: "work_record.created", MSPID: "other-msp", ClientID: "client-alpha"},
		{ID: "event", Type: "work_record.created", MSPID: "msp-id", ClientID: "client-bravo"},
		{ID: "event", Type: "work_record.created", MSPID: "msp-id", ClientID: "client-alpha", Depth: MaxRunDepth},
	}
	for _, event := range cases {
		_, err := engine.Run(context.Background(), RunCommand{
			Definition: definition, Event: event,
			IdempotencyKey: "key", Attempt: 1, MaxAttempts: 3,
		})
		if !errors.Is(err, ErrExecutionDenied) {
			t.Fatalf("unsafe event accepted: %+v error=%v", event, err)
		}
	}
}

func TestEngineRejectsCorrelationDepthAsAutomationLoop(t *testing.T) {
	engine := NewEngine(
		&runStore{}, &actionExecutor{}, &connectionGate{},
		time.Now, sequenceAutomationIDs("run-id"),
	)
	command := RunCommand{
		Definition: publishedDefinition(Action{
			Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
		}),
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created", MSPID: "msp-id",
			ClientID: "client-alpha", Depth: MaxRunDepth,
		},
		IdempotencyKey: "key", Attempt: 1, MaxAttempts: 3,
	}

	_, err := engine.Run(context.Background(), command)
	if !errors.Is(err, ErrAutomationLoop) || !errors.Is(err, ErrExecutionDenied) {
		t.Fatalf("Run() error=%v, want loop and execution denied", err)
	}
}

func TestEngineIdempotencyReturnsCompletedRunWithoutRepeatingActions(t *testing.T) {
	store := &runStore{existing: &Run{
		ID: "existing-run", State: RunSucceeded,
		ChangedObjectIDs: []string{"work-id"},
	}}
	executor := &actionExecutor{}
	engine := NewEngine(store, executor, &connectionGate{}, time.Now, sequenceAutomationIDs("new-run"))
	result, err := engine.Run(context.Background(), RunCommand{
		Definition: publishedDefinition(Action{
			Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
		}),
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created", MSPID: "msp-id", ClientID: "client-alpha",
		},
		IdempotencyKey: "same-key", Attempt: 1, MaxAttempts: 3,
	})
	if err != nil || result.RunID != "existing-run" || executor.action.Kind != "" {
		t.Fatalf("idempotent replay executed again: result=%+v executor=%+v err=%v", result, executor, err)
	}
}

func TestEngineRecordsRetryThenDeadLettersSanitizedFailure(t *testing.T) {
	now := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	executor := &actionExecutor{err: errors.New("credential super-secret rejected")}
	store := &runStore{}
	engine := NewEngine(store, executor, &connectionGate{}, func() time.Time { return now }, sequenceAutomationIDs("run-id"))
	command := RunCommand{
		Definition: publishedDefinition(Action{
			Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
		}),
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created", MSPID: "msp-id", ClientID: "client-alpha",
		},
		IdempotencyKey: "key", Attempt: 1, MaxAttempts: 2,
	}
	retryResult, err := engine.Run(context.Background(), command)
	if !errors.Is(err, ErrActionFailed) || retryResult.RunID != "run-id" {
		t.Fatalf("retryable execution error = %v", err)
	}
	if store.failure.ErrorCode != "action_failed" || !store.failure.RetryAt.After(now) ||
		store.deadLetter.ID != "" {
		t.Fatalf("retry evidence invalid: failure=%+v dead=%+v", store.failure, store.deadLetter)
	}

	store = &runStore{}
	engine = NewEngine(store, executor, &connectionGate{}, func() time.Time { return now }, sequenceAutomationIDs("run-id-2", "dead-id"))
	command.Attempt = 2
	terminalResult, err := engine.Run(context.Background(), command)
	if !errors.Is(err, ErrActionFailed) || terminalResult.RunID != "run-id-2" {
		t.Fatalf("terminal execution error = %v", err)
	}
	if store.deadLetter.ID != "dead-id" || store.deadLetter.ErrorCode != "action_failed" ||
		store.deadLetter.SafeMessage != "automation action failed" ||
		!store.deadLetteredAtomically {
		t.Fatalf("dead letter missing or leaked error: %+v", store.deadLetter)
	}
}

func TestEngineHonorsConditionsAndPersistsWaitWithoutExecutingPastIt(t *testing.T) {
	now := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	store := &runStore{}
	executor := &actionExecutor{}
	engine := NewEngine(store, executor, &connectionGate{}, func() time.Time { return now }, sequenceAutomationIDs("run-id"))
	definition := publishedDefinition(Action{
		Kind: ActionAssign, Parameters: map[string]string{"team_id": "team-id"},
	})
	definition.Steps = []Step{
		{
			ID: "condition", Kind: StepCondition,
			Condition: &Condition{Field: "priority", Operator: OperatorEquals, Value: "critical"},
			Children: []Step{{ID: "skipped", Kind: StepAction, Action: &Action{
				Kind: ActionAssign, Parameters: map[string]string{"team_id": "critical-team"},
			}}},
		},
		{ID: "wait", Kind: StepWait, WaitSeconds: 60},
		{ID: "after-wait", Kind: StepAction, Action: &Action{
			Kind: ActionAssign, Parameters: map[string]string{"team_id": "later-team"},
		}},
	}
	result, err := engine.Run(context.Background(), RunCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created", MSPID: "msp-id",
			ClientID: "client-alpha", InputSnapshot: map[string]string{"priority": "low"},
		},
		IdempotencyKey: "key", Attempt: 1, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Suspended || store.suspension.ResumeAt != now.Add(time.Minute) ||
		store.suspension.StartedAt != now ||
		store.suspension.CompletedAt != now ||
		len(store.steps) != 0 ||
		executor.action.Kind != "" || store.completed.RunID != "" {
		t.Fatalf("condition/wait semantics invalid: result=%+v store=%+v executor=%+v", result, store, executor)
	}
}

func TestEngineCarriesGovernedActionOutputsToFollowingSteps(t *testing.T) {
	now := time.Now().UTC()
	store := &runStore{}
	executor := &actionExecutor{outputs: []map[string]string{
		{"_subject_version": "2"},
		nil,
	}}
	engine := NewEngine(
		store, executor, &connectionGate{},
		func() time.Time { return now },
		sequenceAutomationIDs("run-id"),
	)
	definition := publishedDefinition(Action{
		Kind:       ActionAssign,
		Parameters: map[string]string{"owner_id": "owner-id"},
	})
	definition.Steps = append(definition.Steps, Step{
		ID: "transition", Kind: StepAction,
		Action: &Action{
			Kind:       ActionTransition,
			Parameters: map[string]string{"to_status": "in_progress"},
		},
	})

	_, err := engine.Run(context.Background(), RunCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created",
			MSPID: "msp-id", ClientID: "client-alpha",
			InputSnapshot: map[string]string{"_subject_version": "1"},
		},
		IdempotencyKey: "key", Attempt: 1, MaxAttempts: 3,
	})

	if err != nil || len(executor.snapshots) != 2 ||
		executor.snapshots[0]["_automation_run_id"] != "run-id" ||
		executor.snapshots[1]["_subject_version"] != "2" {
		t.Fatalf("Run() error=%v snapshots=%+v", err, executor.snapshots)
	}
}

func TestEngineResumesAfterDurableWaitWithoutRepeatingEarlierSteps(t *testing.T) {
	now := time.Now().UTC()
	store := &runStore{}
	executor := &actionExecutor{}
	engine := NewEngine(
		store, executor, &connectionGate{},
		func() time.Time { return now },
		sequenceAutomationIDs("unused"),
	)
	definition := publishedDefinition(Action{
		Kind:       ActionAssign,
		Parameters: map[string]string{"owner_id": "owner-id"},
	})
	definition.Steps = []Step{
		{ID: "before", Kind: StepAction, Action: &Action{
			Kind:       ActionAssign,
			Parameters: map[string]string{"owner_id": "owner-id"},
		}},
		{ID: "wait", Kind: StepWait, WaitSeconds: 60},
		{ID: "after", Kind: StepAction, Action: &Action{
			Kind:       ActionTransition,
			Parameters: map[string]string{"to_status": "in_progress"},
		}},
	}

	result, err := engine.Resume(context.Background(), ContinuationCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created",
			MSPID: "msp-id", ClientID: "client-alpha",
		},
		Run: Run{
			ID: "run-id", Attempt: 1, MaxAttempts: 3,
			ChangedObjectIDs: []string{"work-id"},
			InputSnapshot: map[string]string{
				"_subject_version":   "2",
				"_automation_run_id": "run-id",
			},
		},
		Mode: ContinuationAfterWait, StepID: "wait",
	})

	if err != nil || result.RunID != "run-id" ||
		len(executor.snapshots) != 1 ||
		executor.action.Kind != ActionTransition ||
		store.completed.RunID != "run-id" {
		t.Fatalf(
			"Resume() result=%+v error=%v actions=%+v store=%+v",
			result, err, executor.snapshots, store,
		)
	}
}

func TestEngineRetriesFromFailedStepWithIncrementedAttempt(t *testing.T) {
	now := time.Now().UTC()
	store := &runStore{}
	executor := &actionExecutor{}
	engine := NewEngine(
		store, executor, &connectionGate{},
		func() time.Time { return now },
		sequenceAutomationIDs("unused"),
	)
	definition := publishedDefinition(Action{
		Kind:       ActionAssign,
		Parameters: map[string]string{"owner_id": "owner-id"},
	})

	_, err := engine.Resume(context.Background(), ContinuationCommand{
		Definition: definition,
		Event: TriggerEvent{
			ID: "event", Type: "work_record.created",
			MSPID: "msp-id", ClientID: "client-alpha",
		},
		Run: Run{
			ID: "run-id", Attempt: 2, MaxAttempts: 3,
			InputSnapshot: map[string]string{"_subject_version": "1"},
		},
		Mode: ContinuationRetryStep, StepID: "step-id",
	})

	if err != nil || len(store.steps) != 1 ||
		store.steps[0].Attempt != 2 {
		t.Fatalf("Resume() error=%v steps=%+v", err, store.steps)
	}
}

func publishedDefinition(action Action) Definition {
	return Definition{
		ID: "automation-id", MSPID: "msp-id", Version: 1, State: Published,
		ClientScopes: []string{"client-alpha"}, Capabilities: []string{"work_record.edit"},
		Trigger: Trigger{EventType: "work_record.created"},
		Steps:   []Step{{ID: "step-id", Kind: StepAction, Action: &action}},
	}
}

func sequenceAutomationIDs(values ...string) func() string {
	index := 0
	return func() string {
		value := values[index]
		index++
		return value
	}
}
