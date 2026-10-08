package automation

import (
	"context"
	"errors"
	"time"
)

const (
	continuationLease      = 5 * time.Minute
	continuationRetryDelay = 30 * time.Second
)

type ContinuationJob struct {
	Command ContinuationCommand
}

type ContinuationRelease struct {
	RunID      string
	ReleasedAt time.Time
	RetryAt    time.Time
	ErrorCode  string
}

type ContinuationQueue interface {
	ClaimContinuations(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]ContinuationJob, error)
	ReleaseContinuation(context.Context, ContinuationRelease) error
}

type ContinuationRunner interface {
	Resume(context.Context, ContinuationCommand) (RunResult, error)
}

type ContinuationWorker struct {
	queue  ContinuationQueue
	runner ContinuationRunner
	now    func() time.Time
}

type ContinuationWorkerResult struct {
	Claimed int
	Resumed int
	Failed  int
}

func NewContinuationWorker(
	queue ContinuationQueue,
	runner ContinuationRunner,
	now func() time.Time,
) *ContinuationWorker {
	return &ContinuationWorker{queue: queue, runner: runner, now: now}
}

func (w *ContinuationWorker) RunOnce(
	ctx context.Context,
	limit int,
) (ContinuationWorkerResult, error) {
	if w.queue == nil || w.runner == nil || w.now == nil || limit <= 0 {
		return ContinuationWorkerResult{}, ErrInvalidExecutionRuntime
	}
	now := w.now().UTC()
	jobs, err := w.queue.ClaimContinuations(
		ctx, limit, now, continuationLease,
	)
	if err != nil {
		return ContinuationWorkerResult{}, err
	}
	result := ContinuationWorkerResult{Claimed: len(jobs)}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		_, resumeErr := w.runner.Resume(ctx, job.Command)
		if resumeErr == nil || errors.Is(resumeErr, ErrActionFailed) {
			result.Resumed++
			continue
		}
		if err := w.queue.ReleaseContinuation(
			ctx,
			ContinuationRelease{
				RunID: job.Command.Run.ID, ReleasedAt: now,
				RetryAt:   now.Add(continuationRetryDelay),
				ErrorCode: "continuation_failed",
			},
		); err != nil {
			return result, err
		}
		result.Failed++
	}
	return result, nil
}
