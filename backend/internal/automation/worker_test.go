package automation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type executionQueueStub struct {
	planned   int
	jobs      []ExecutionJob
	completed []ExecutionCompletion
	failed    []ExecutionFailure
}

func (q *executionQueueStub) Plan(
	context.Context,
	int,
	time.Time,
) (int, error) {
	return q.planned, nil
}

func (q *executionQueueStub) Claim(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]ExecutionJob, error) {
	return q.jobs, nil
}

func (q *executionQueueStub) Complete(
	_ context.Context,
	completion ExecutionCompletion,
) error {
	q.completed = append(q.completed, completion)
	return nil
}

func (q *executionQueueStub) Fail(
	_ context.Context,
	failure ExecutionFailure,
) error {
	q.failed = append(q.failed, failure)
	return nil
}

type executionRunnerStub struct {
	errors map[string]error
	seen   []RunCommand
}

func (r *executionRunnerStub) Run(
	_ context.Context,
	command RunCommand,
) (RunResult, error) {
	r.seen = append(r.seen, command)
	return RunResult{RunID: "run-" + command.Event.ID}, r.errors[command.Event.ID]
}

func TestExecutionWorkerPlansClaimsAndAcknowledgesDurableEngineOutcomes(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	queue := &executionQueueStub{
		planned: 2,
		jobs: []ExecutionJob{
			{ID: "job-1", Command: RunCommand{Event: TriggerEvent{ID: "event-1"}}},
			{ID: "job-2", Command: RunCommand{Event: TriggerEvent{ID: "event-2"}}},
		},
	}
	runner := &executionRunnerStub{errors: map[string]error{
		"event-2": ErrActionFailed,
	}}
	worker := NewExecutionWorker(queue, runner, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Planned != 2 || result.Claimed != 2 ||
		result.Processed != 2 || result.Failed != 0 ||
		len(queue.completed) != 2 || len(queue.failed) != 0 ||
		queue.completed[0].CompletedAt != now {
		t.Fatalf(
			"RunOnce() result=%+v error=%v completed=%+v failed=%+v",
			result, err, queue.completed, queue.failed,
		)
	}
}

func TestExecutionWorkerClassifiesInvalidAndTransientFailures(t *testing.T) {
	now := time.Now().UTC()
	queue := &executionQueueStub{jobs: []ExecutionJob{
		{ID: "invalid", Command: RunCommand{Event: TriggerEvent{ID: "invalid"}}},
		{ID: "transient", Command: RunCommand{Event: TriggerEvent{ID: "transient"}}},
	}}
	runner := &executionRunnerStub{errors: map[string]error{
		"invalid":   ErrExecutionDenied,
		"transient": errors.New("database unavailable"),
	}}
	worker := NewExecutionWorker(queue, runner, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Failed != 2 || len(queue.failed) != 2 ||
		queue.failed[0].Retry ||
		queue.failed[0].ErrorCode != "execution_denied" ||
		!queue.failed[1].Retry ||
		queue.failed[1].ErrorCode != "execution_failed" {
		t.Fatalf(
			"RunOnce() result=%+v error=%v failures=%+v",
			result, err, queue.failed,
		)
	}
}

func TestExecutionWorkerRecordsBoundedLoopDiagnostic(t *testing.T) {
	now := time.Now().UTC()
	queue := &executionQueueStub{jobs: []ExecutionJob{{
		ID: "loop", Command: RunCommand{Event: TriggerEvent{ID: "loop"}},
	}}}
	runner := &executionRunnerStub{errors: map[string]error{
		"loop": ErrAutomationLoop,
	}}
	worker := NewExecutionWorker(queue, runner, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Failed != 1 || len(queue.failed) != 1 ||
		queue.failed[0].Retry || queue.failed[0].ErrorCode != "automation_loop" {
		t.Fatalf("RunOnce() result=%+v error=%v failures=%+v", result, err, queue.failed)
	}
}
