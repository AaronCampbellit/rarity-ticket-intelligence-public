package automation

import (
	"context"
	"errors"
	"time"
)

const (
	executionLease      = 5 * time.Minute
	executionRetryDelay = 30 * time.Second
)

var ErrInvalidExecutionRuntime = errors.New("invalid automation execution runtime")

type ExecutionJob struct {
	ID      string
	Command RunCommand
}

type ExecutionCompletion struct {
	JobID       string
	RunID       string
	CompletedAt time.Time
}

type ExecutionFailure struct {
	JobID         string
	FailedAt      time.Time
	NextAttemptAt time.Time
	ErrorCode     string
	Retry         bool
}

type ExecutionQueue interface {
	Plan(context.Context, int, time.Time) (int, error)
	Claim(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]ExecutionJob, error)
	Complete(context.Context, ExecutionCompletion) error
	Fail(context.Context, ExecutionFailure) error
}

type ExecutionRunner interface {
	Run(context.Context, RunCommand) (RunResult, error)
}

type ExecutionWorker struct {
	queue  ExecutionQueue
	runner ExecutionRunner
	now    func() time.Time
}

type ExecutionWorkerResult struct {
	Planned   int
	Claimed   int
	Processed int
	Failed    int
}

func NewExecutionWorker(
	queue ExecutionQueue,
	runner ExecutionRunner,
	now func() time.Time,
) *ExecutionWorker {
	return &ExecutionWorker{queue: queue, runner: runner, now: now}
}

func (w *ExecutionWorker) RunOnce(
	ctx context.Context,
	limit int,
) (ExecutionWorkerResult, error) {
	if w.queue == nil || w.runner == nil || w.now == nil || limit <= 0 {
		return ExecutionWorkerResult{}, ErrInvalidExecutionRuntime
	}
	now := w.now().UTC()
	planned, err := w.queue.Plan(ctx, limit, now)
	if err != nil {
		return ExecutionWorkerResult{}, err
	}
	jobs, err := w.queue.Claim(ctx, limit, now, executionLease)
	if err != nil {
		return ExecutionWorkerResult{Planned: planned}, err
	}
	result := ExecutionWorkerResult{
		Planned: planned, Claimed: len(jobs),
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		runResult, runErr := w.runner.Run(ctx, job.Command)
		if runErr == nil || errors.Is(runErr, ErrActionFailed) {
			if err := w.queue.Complete(ctx, ExecutionCompletion{
				JobID: job.ID, RunID: runResult.RunID,
				CompletedAt: now,
			}); err != nil {
				return result, err
			}
			result.Processed++
			continue
		}
		failure := ExecutionFailure{
			JobID: job.ID, FailedAt: now,
			ErrorCode: "execution_failed", Retry: true,
			NextAttemptAt: now.Add(executionRetryDelay),
		}
		if errors.Is(runErr, ErrAutomationLoop) {
			failure.ErrorCode = "automation_loop"
			failure.Retry = false
			failure.NextAttemptAt = time.Time{}
		} else if errors.Is(runErr, ErrExecutionDenied) {
			failure.ErrorCode = "execution_denied"
			failure.Retry = false
			failure.NextAttemptAt = time.Time{}
		}
		if err := w.queue.Fail(ctx, failure); err != nil {
			return result, err
		}
		result.Failed++
	}
	return result, nil
}
