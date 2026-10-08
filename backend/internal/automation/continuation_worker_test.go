package automation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type continuationQueueStub struct {
	jobs     []ContinuationJob
	released []ContinuationRelease
}

func (q *continuationQueueStub) ClaimContinuations(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]ContinuationJob, error) {
	return q.jobs, nil
}

func (q *continuationQueueStub) ReleaseContinuation(
	_ context.Context,
	release ContinuationRelease,
) error {
	q.released = append(q.released, release)
	return nil
}

type continuationRunnerStub struct {
	seen   []ContinuationCommand
	errors map[string]error
}

func (r *continuationRunnerStub) Resume(
	_ context.Context,
	command ContinuationCommand,
) (RunResult, error) {
	r.seen = append(r.seen, command)
	return RunResult{RunID: command.Run.ID}, r.errors[command.Run.ID]
}

func TestContinuationWorkerResumesDueWaitsAndRetries(t *testing.T) {
	now := time.Now().UTC()
	queue := &continuationQueueStub{jobs: []ContinuationJob{
		{Command: ContinuationCommand{
			Run: Run{ID: "wait-run"}, Mode: ContinuationAfterWait,
		}},
		{Command: ContinuationCommand{
			Run: Run{ID: "retry-run"}, Mode: ContinuationRetryStep,
		}},
	}}
	runner := &continuationRunnerStub{}
	worker := NewContinuationWorker(
		queue, runner, func() time.Time { return now },
	)

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Claimed != 2 || result.Resumed != 2 ||
		result.Failed != 0 || len(runner.seen) != 2 ||
		len(queue.released) != 0 {
		t.Fatalf(
			"RunOnce() result=%+v error=%v seen=%+v released=%+v",
			result, err, runner.seen, queue.released,
		)
	}
}

func TestContinuationWorkerReleasesInfrastructureFailureWithBackoff(t *testing.T) {
	now := time.Now().UTC()
	queue := &continuationQueueStub{jobs: []ContinuationJob{{
		Command: ContinuationCommand{Run: Run{ID: "run-id"}},
	}}}
	runner := &continuationRunnerStub{errors: map[string]error{
		"run-id": errors.New("database unavailable"),
	}}
	worker := NewContinuationWorker(
		queue, runner, func() time.Time { return now },
	)

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Failed != 1 || len(queue.released) != 1 ||
		queue.released[0].RunID != "run-id" ||
		!queue.released[0].RetryAt.After(now) ||
		queue.released[0].ErrorCode != "continuation_failed" {
		t.Fatalf(
			"RunOnce() result=%+v error=%v released=%+v",
			result, err, queue.released,
		)
	}
}
