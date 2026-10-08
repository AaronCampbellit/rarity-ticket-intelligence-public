package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrExecutionDenied = errors.New("automation execution denied")
	ErrAutomationLoop  = errors.New("automation correlation depth exceeded")
	ErrActionFailed    = errors.New("automation action failed")
)

const MaxRunDepth = 8

type RunState string

const (
	RunRunning   RunState = "running"
	RunWaiting   RunState = "waiting"
	RunSucceeded RunState = "succeeded"
	RunRetrying  RunState = "retrying"
	RunFailed    RunState = "failed"
)

type TriggerEvent struct {
	ID            string
	Type          string
	MSPID         string
	ClientID      string
	CausationID   string
	Depth         int
	InputSnapshot map[string]string
}

type RunCommand struct {
	Definition     Definition
	Event          TriggerEvent
	IdempotencyKey string
	Attempt        int
	MaxAttempts    int
}

type Run struct {
	ID                string
	AutomationID      string
	AutomationVersion int64
	EventID           string
	MSPID             string
	ClientID          string
	IdempotencyKey    string
	CausationID       string
	Depth             int
	Attempt           int
	MaxAttempts       int
	State             RunState
	StartedAt         time.Time
	InputSnapshot     map[string]string
	ChangedObjectIDs  []string
}

type StepRecord struct {
	RunID            string
	StepID           string
	Attempt          int
	ActionKind       ActionKind
	StartedAt        time.Time
	CompletedAt      time.Time
	State            RunState
	ErrorCode        string
	ChangedObjectIDs []string
	InputSnapshot    map[string]string
}

type RunCompletion struct {
	RunID            string
	CompletedAt      time.Time
	ChangedObjectIDs []string
}

type RunFailure struct {
	RunID       string
	StepID      string
	FailedAt    time.Time
	ErrorCode   string
	RetryAt     time.Time
	Attempt     int
	MaxAttempts int
}

type DeadLetter struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	AutomationID      string    `json:"automation_id"`
	AutomationVersion int64     `json:"automation_version"`
	EventID           string    `json:"event_id"`
	MSPID             string    `json:"msp_id"`
	ClientID          string    `json:"client_id"`
	CreatedAt         time.Time `json:"created_at"`
	ErrorCode         string    `json:"error_code"`
	SafeMessage       string    `json:"safe_message"`
	State             string    `json:"state"`
}

type Suspension struct {
	RunID         string
	StepID        string
	Attempt       int
	ResumeAt      time.Time
	StartedAt     time.Time
	CompletedAt   time.Time
	InputSnapshot map[string]string
}

type RunStore interface {
	Begin(context.Context, Run) (*Run, error)
	RecordStep(context.Context, StepRecord) error
	Complete(context.Context, RunCompletion) error
	Fail(context.Context, RunFailure) error
	DeadLetter(context.Context, RunFailure, DeadLetter) error
	Suspend(context.Context, Suspension) error
}

type ActionResult struct {
	ChangedObjectIDs []string
	OutputSnapshot   map[string]string
}

type ActionExecutor interface {
	Execute(
		context.Context,
		authorization.Principal,
		Action,
		map[string]string,
	) (ActionResult, error)
}

type ConnectionGate interface {
	Authorize(context.Context, string, string, string, ActionKind) error
}

type Engine struct {
	store       RunStore
	executor    ActionExecutor
	connections ConnectionGate
	now         func() time.Time
	newID       func() string
}

func NewEngine(
	store RunStore,
	executor ActionExecutor,
	connections ConnectionGate,
	now func() time.Time,
	newID func() string,
) *Engine {
	return &Engine{
		store: store, executor: executor, connections: connections,
		now: now, newID: newID,
	}
}

type RunResult struct {
	RunID            string
	Replayed         bool
	Suspended        bool
	ChangedObjectIDs []string
}

type ContinuationMode string

const (
	ContinuationAfterWait ContinuationMode = "after_wait"
	ContinuationRetryStep ContinuationMode = "retry_step"
)

type ContinuationCommand struct {
	Definition Definition
	Event      TriggerEvent
	Run        Run
	Mode       ContinuationMode
	StepID     string
}

func (e *Engine) Run(ctx context.Context, command RunCommand) (RunResult, error) {
	if err := e.validateCommand(command); err != nil {
		return RunResult{}, err
	}
	now := e.now().UTC()
	run := Run{
		ID: e.newID(), AutomationID: command.Definition.ID,
		AutomationVersion: command.Definition.Version, EventID: command.Event.ID,
		MSPID: command.Event.MSPID, ClientID: command.Event.ClientID,
		IdempotencyKey: command.IdempotencyKey, CausationID: command.Event.CausationID,
		Depth: command.Event.Depth + 1, Attempt: command.Attempt,
		MaxAttempts: command.MaxAttempts,
		State:       RunRunning, StartedAt: now,
		InputSnapshot: sanitizeSnapshot(command.Event.InputSnapshot),
	}
	existing, err := e.store.Begin(ctx, run)
	if err != nil {
		return RunResult{}, err
	}
	if existing != nil {
		if existing.State != RunSucceeded {
			return RunResult{}, ErrExecutionDenied
		}
		return RunResult{
			RunID: existing.ID, Replayed: true,
			ChangedObjectIDs: append([]string(nil), existing.ChangedObjectIDs...),
		}, nil
	}
	principal := automationPrincipal(command.Definition, command.Event)
	executionSnapshot := sanitizeSnapshot(command.Event.InputSnapshot)
	executionSnapshot["_automation_run_id"] = run.ID
	execution, err := e.executeSteps(
		ctx, run, command, principal, command.Definition.Steps,
		executionSnapshot, nil,
	)
	if err != nil {
		if !errors.Is(err, ErrActionFailed) {
			return RunResult{}, err
		}
		return e.fail(
			ctx, run, command, execution.failedStep, execution.failedStartedAt,
			execution.inputSnapshot,
		)
	}
	if execution.suspended {
		return RunResult{
			RunID: run.ID, Suspended: true,
			ChangedObjectIDs: execution.changedObjectIDs,
		}, nil
	}
	if err := e.store.Complete(ctx, RunCompletion{
		RunID: run.ID, CompletedAt: e.now().UTC(),
		ChangedObjectIDs: append([]string(nil), execution.changedObjectIDs...),
	}); err != nil {
		return RunResult{}, err
	}
	return RunResult{RunID: run.ID, ChangedObjectIDs: execution.changedObjectIDs}, nil
}

func (e *Engine) Resume(
	ctx context.Context,
	continuation ContinuationCommand,
) (RunResult, error) {
	if err := e.validateContinuation(continuation); err != nil {
		return RunResult{}, err
	}
	run := continuation.Run
	command := RunCommand{
		Definition:  continuation.Definition,
		Event:       continuation.Event,
		Attempt:     run.Attempt,
		MaxAttempts: run.MaxAttempts,
	}
	principal := automationPrincipal(
		continuation.Definition, continuation.Event,
	)
	snapshot := sanitizeSnapshot(run.InputSnapshot)
	snapshot["_automation_run_id"] = run.ID
	cursor := &continuationCursor{
		mode: continuation.Mode, stepID: continuation.StepID,
	}
	execution, err := e.executeSteps(
		ctx, run, command, principal, continuation.Definition.Steps,
		snapshot, cursor,
	)
	execution.changedObjectIDs = append(
		append([]string(nil), run.ChangedObjectIDs...),
		execution.changedObjectIDs...,
	)
	if !cursor.reached {
		return RunResult{}, ErrExecutionDenied
	}
	if err != nil {
		if !errors.Is(err, ErrActionFailed) {
			return RunResult{}, err
		}
		return e.fail(
			ctx, run, command, execution.failedStep,
			execution.failedStartedAt,
			execution.inputSnapshot,
		)
	}
	if execution.suspended {
		return RunResult{
			RunID: run.ID, Suspended: true,
			ChangedObjectIDs: execution.changedObjectIDs,
		}, nil
	}
	if err := e.store.Complete(ctx, RunCompletion{
		RunID: run.ID, CompletedAt: e.now().UTC(),
		ChangedObjectIDs: execution.changedObjectIDs,
	}); err != nil {
		return RunResult{}, err
	}
	return RunResult{
		RunID: run.ID, ChangedObjectIDs: execution.changedObjectIDs,
	}, nil
}

func (e *Engine) validateCommand(command RunCommand) error {
	if command.Event.Depth >= MaxRunDepth {
		return errors.Join(ErrExecutionDenied, ErrAutomationLoop)
	}
	if e.store == nil || e.executor == nil || e.connections == nil ||
		e.now == nil || e.newID == nil ||
		command.Definition.State != Published ||
		Validate(command.Definition) != nil ||
		command.Event.ID == "" ||
		command.Event.Type != command.Definition.Trigger.EventType ||
		command.Event.MSPID != command.Definition.MSPID ||
		!containsExact(command.Definition.ClientScopes, command.Event.ClientID) ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		command.Attempt < 1 || command.MaxAttempts < command.Attempt {
		return ErrExecutionDenied
	}
	return nil
}

func automationPrincipal(
	definition Definition,
	event TriggerEvent,
) authorization.Principal {
	return authorization.Principal{
		ID: definition.ID,
		Scope: scope.Principal{
			MSPID: event.MSPID, ClientID: event.ClientID,
		},
		Capabilities: authorization.NewCapabilitySet(
			definition.Capabilities...,
		),
	}
}

func (e *Engine) validateContinuation(
	command ContinuationCommand,
) error {
	if e.store == nil || e.executor == nil || e.connections == nil ||
		e.now == nil || strings.TrimSpace(command.Run.ID) == "" ||
		command.Run.Attempt < 1 ||
		command.Run.MaxAttempts < command.Run.Attempt ||
		strings.TrimSpace(command.StepID) == "" ||
		(command.Mode != ContinuationAfterWait &&
			command.Mode != ContinuationRetryStep) ||
		command.Definition.State != Published ||
		Validate(command.Definition) != nil ||
		command.Event.ID == "" ||
		command.Event.Type != command.Definition.Trigger.EventType ||
		command.Event.MSPID != command.Definition.MSPID ||
		!containsExact(
			command.Definition.ClientScopes, command.Event.ClientID,
		) {
		return ErrExecutionDenied
	}
	return nil
}

func (e *Engine) fail(
	ctx context.Context,
	run Run,
	command RunCommand,
	step Step,
	startedAt time.Time,
	inputSnapshot map[string]string,
) (RunResult, error) {
	now := e.now().UTC()
	if err := e.store.RecordStep(ctx, StepRecord{
		RunID: run.ID, StepID: step.ID, Attempt: command.Attempt,
		ActionKind: step.Action.Kind,
		StartedAt:  startedAt, CompletedAt: now, State: RunFailed,
		ErrorCode:     "action_failed",
		InputSnapshot: sanitizeSnapshot(inputSnapshot),
	}); err != nil {
		return RunResult{}, err
	}
	failure := RunFailure{
		RunID: run.ID, StepID: step.ID,
		FailedAt: now, ErrorCode: "action_failed",
		Attempt: command.Attempt, MaxAttempts: command.MaxAttempts,
	}
	if command.Attempt < command.MaxAttempts {
		failure.RetryAt = now.Add(automationRetryDelay(command.Attempt))
		if err := e.store.Fail(ctx, failure); err != nil {
			return RunResult{}, err
		}
		return RunResult{RunID: run.ID}, ErrActionFailed
	}
	if err := e.store.DeadLetter(ctx, failure, DeadLetter{
		ID: e.newID(), RunID: run.ID, AutomationID: command.Definition.ID,
		AutomationVersion: command.Definition.Version, EventID: command.Event.ID,
		MSPID: command.Event.MSPID, ClientID: command.Event.ClientID,
		CreatedAt: now, ErrorCode: "action_failed",
		SafeMessage: "automation action failed", State: "open",
	}); err != nil {
		return RunResult{}, err
	}
	return RunResult{RunID: run.ID}, ErrActionFailed
}

type stepExecution struct {
	changedObjectIDs []string
	suspended        bool
	failedStep       Step
	failedStartedAt  time.Time
	inputSnapshot    map[string]string
}

type continuationCursor struct {
	mode    ContinuationMode
	stepID  string
	reached bool
}

func (e *Engine) executeSteps(
	ctx context.Context,
	run Run,
	command RunCommand,
	principal authorization.Principal,
	steps []Step,
	snapshot map[string]string,
	cursor *continuationCursor,
) (stepExecution, error) {
	execution := stepExecution{changedObjectIDs: make([]string, 0)}
	for _, step := range steps {
		switch step.Kind {
		case StepCondition:
			if !conditionMatches(*step.Condition, snapshot) {
				continue
			}
			child, err := e.executeSteps(
				ctx, run, command, principal, step.Children, snapshot,
				cursor,
			)
			execution.changedObjectIDs = append(execution.changedObjectIDs, child.changedObjectIDs...)
			if err != nil || child.suspended {
				child.changedObjectIDs = execution.changedObjectIDs
				return child, err
			}
		case StepBranch:
			child, err := e.executeSteps(
				ctx, run, command, principal, step.Children, snapshot,
				cursor,
			)
			execution.changedObjectIDs = append(execution.changedObjectIDs, child.changedObjectIDs...)
			if err != nil || child.suspended {
				child.changedObjectIDs = execution.changedObjectIDs
				return child, err
			}
		case StepWait:
			if cursor != nil && !cursor.reached {
				if step.ID == cursor.stepID &&
					cursor.mode == ContinuationAfterWait {
					cursor.reached = true
				}
				continue
			}
			startedAt := e.now().UTC()
			completedAt := e.now().UTC()
			resumeAt := completedAt.Add(
				time.Duration(step.WaitSeconds) * time.Second,
			)
			if err := e.store.Suspend(ctx, Suspension{
				RunID: run.ID, StepID: step.ID,
				Attempt: command.Attempt, ResumeAt: resumeAt,
				StartedAt: startedAt, CompletedAt: completedAt,
				InputSnapshot: sanitizeSnapshot(snapshot),
			}); err != nil {
				return execution, err
			}
			execution.suspended = true
			return execution, nil
		case StepAction:
			if cursor != nil && !cursor.reached {
				if step.ID != cursor.stepID ||
					cursor.mode != ContinuationRetryStep {
					continue
				}
				cursor.reached = true
			}
			startedAt := e.now().UTC()
			if step.Action.Kind == ActionCallHTTP {
				if err := e.connections.Authorize(
					ctx, step.Action.ConnectionRef, command.Event.MSPID,
					command.Event.ClientID, step.Action.Kind,
				); err != nil {
					execution.failedStep, execution.failedStartedAt = step, startedAt
					execution.inputSnapshot = sanitizeSnapshot(snapshot)
					return execution, ErrActionFailed
				}
			}
			snapshot["_automation_step_id"] = step.ID
			result, err := e.executor.Execute(
				ctx, principal, *step.Action, sanitizeSnapshot(snapshot),
			)
			if err != nil {
				execution.failedStep, execution.failedStartedAt = step, startedAt
				execution.inputSnapshot = sanitizeSnapshot(snapshot)
				return execution, ErrActionFailed
			}
			execution.changedObjectIDs = append(
				execution.changedObjectIDs,
				result.ChangedObjectIDs...,
			)
			for key, value := range sanitizeSnapshot(result.OutputSnapshot) {
				snapshot[key] = value
			}
			if err := e.store.RecordStep(ctx, StepRecord{
				RunID: run.ID, StepID: step.ID, Attempt: command.Attempt,
				ActionKind: step.Action.Kind,
				StartedAt:  startedAt, CompletedAt: e.now().UTC(), State: RunSucceeded,
				ChangedObjectIDs: append([]string(nil), result.ChangedObjectIDs...),
				InputSnapshot:    sanitizeSnapshot(snapshot),
			}); err != nil {
				return execution, err
			}
		}
	}
	return execution, nil
}

func conditionMatches(condition Condition, snapshot map[string]string) bool {
	actual := snapshot[condition.Field]
	switch condition.Operator {
	case OperatorEquals:
		return actual == condition.Value
	case OperatorNotEquals:
		return actual != condition.Value
	case OperatorContains:
		return strings.Contains(actual, condition.Value)
	case OperatorHasAnyTag, OperatorHasAllTags, OperatorHasNoTags:
		actualIDs, wantedIDs := jsonStringSet(actual), jsonStringSet(condition.Value)
		if actualIDs == nil || wantedIDs == nil {
			return false
		}
		matched := 0
		for id := range wantedIDs {
			if _, ok := actualIDs[id]; ok {
				matched++
			}
		}
		if condition.Operator == OperatorHasAnyTag {
			return matched > 0
		}
		if condition.Operator == OperatorHasAllTags {
			return matched == len(wantedIDs)
		}
		return matched == 0
	case OperatorHasTagInGroup:
		groups := jsonStringSet(actual)
		_, ok := groups[strings.TrimSpace(condition.Value)]
		return ok
	default:
		return false
	}
}

func jsonStringSet(raw string) map[string]struct{} {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sanitizeSnapshot(snapshot map[string]string) map[string]string {
	result := make(map[string]string, len(snapshot))
	for key, value := range snapshot {
		normalized := strings.ToLower(key)
		sensitive := false
		for _, fragment := range []string{"secret", "password", "token", "credential", "attachment"} {
			if strings.Contains(normalized, fragment) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			result[key] = value
		}
	}
	return result
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func automationRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		return 15 * time.Minute
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}
