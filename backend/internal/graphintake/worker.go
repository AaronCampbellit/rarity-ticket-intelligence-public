package graphintake

import (
	"context"
	"errors"
	"strings"
	"time"
)

const reconciliationLease = 10 * time.Minute

type ReconciliationJob struct {
	ConnectionID        string
	MSPID               string
	Mailbox             string
	Folder              string
	CredentialSecretRef string
}

type ReconciliationQueue interface {
	Plan(context.Context, int, time.Time) (int, error)
	Claim(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]ReconciliationJob, error)
	Bind(string) (IntakeRepository, ThreadIndex)
	MarkFailed(context.Context, string, time.Time, string) error
}

type SourceFactory interface {
	Source(context.Context, ReconciliationJob) (GraphSource, error)
}

type ReconciliationWorker struct {
	queue   ReconciliationQueue
	sources SourceFactory
	now     func() time.Time
}

type ReconciliationRun struct {
	Planned   int
	Claimed   int
	Processed int
	Failed    int
}

func NewReconciliationWorker(
	queue ReconciliationQueue,
	sources SourceFactory,
	now func() time.Time,
) *ReconciliationWorker {
	return &ReconciliationWorker{queue: queue, sources: sources, now: now}
}

func (w *ReconciliationWorker) RunOnce(
	ctx context.Context,
	limit int,
) (ReconciliationRun, error) {
	if w.queue == nil || w.sources == nil || w.now == nil || limit <= 0 {
		return ReconciliationRun{}, ErrInvalidGraphConfiguration
	}
	now := w.now().UTC()
	planned, err := w.queue.Plan(ctx, limit, now)
	if err != nil {
		return ReconciliationRun{}, err
	}
	jobs, err := w.queue.Claim(ctx, limit, now, reconciliationLease)
	if err != nil {
		return ReconciliationRun{Planned: planned}, err
	}
	result := ReconciliationRun{Planned: planned, Claimed: len(jobs)}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		source, err := w.sources.Source(ctx, job)
		if err != nil {
			if markErr := w.queue.MarkFailed(
				ctx, job.ConnectionID, now, "source_unavailable",
			); markErr != nil {
				return result, markErr
			}
			result.Failed++
			continue
		}
		repository, threads := w.queue.Bind(job.ConnectionID)
		processor := NewProcessor(source, repository, threads, w.now)
		reconciled, err := processor.Reconcile(ctx, job.Mailbox, job.Folder)
		if err != nil {
			if markErr := w.queue.MarkFailed(
				ctx, job.ConnectionID, now, graphFailureCode(err),
			); markErr != nil {
				return result, markErr
			}
			result.Failed++
			continue
		}
		result.Processed += reconciled.Processed
	}
	return result, nil
}

func graphFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidGraphCredentials):
		return "credentials_invalid"
	case errors.Is(err, ErrInvalidGraphCursor):
		return "cursor_invalid"
	case errors.Is(err, ErrInvalidGraphResponse),
		errors.Is(err, ErrInvalidDeltaPage):
		return "provider_response_invalid"
	default:
		value := strings.ToLower(err.Error())
		if strings.Contains(value, "context canceled") ||
			strings.Contains(value, "deadline exceeded") {
			return "provider_timeout"
		}
		return "reconciliation_failed"
	}
}
