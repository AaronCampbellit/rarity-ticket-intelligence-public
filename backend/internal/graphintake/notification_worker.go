package graphintake

import (
	"context"
	"time"
)

type NotificationJob struct {
	ReconciliationJob
	HintID    string
	MessageID string
}

type NotificationQueue interface {
	ClaimNotifications(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]NotificationJob, error)
	Bind(string) (IntakeRepository, ThreadIndex)
	MarkNotificationProcessed(context.Context, string, time.Time) error
	MarkNotificationFailed(context.Context, string, time.Time, string) error
}

type NotificationWorker struct {
	queue   NotificationQueue
	sources SourceFactory
	now     func() time.Time
}

type NotificationRun struct {
	Claimed   int
	Processed int
	Failed    int
}

func NewNotificationWorker(
	queue NotificationQueue,
	sources SourceFactory,
	now func() time.Time,
) *NotificationWorker {
	return &NotificationWorker{queue: queue, sources: sources, now: now}
}

func (w *NotificationWorker) RunOnce(
	ctx context.Context,
	limit int,
) (NotificationRun, error) {
	if w.queue == nil || w.sources == nil || w.now == nil || limit <= 0 {
		return NotificationRun{}, ErrInvalidGraphConfiguration
	}
	now := w.now().UTC()
	jobs, err := w.queue.ClaimNotifications(
		ctx, limit, now, reconciliationLease,
	)
	if err != nil {
		return NotificationRun{}, err
	}
	result := NotificationRun{Claimed: len(jobs)}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		source, err := w.sources.Source(ctx, job.ReconciliationJob)
		if err == nil {
			repository, threads := w.queue.Bind(job.ConnectionID)
			_, err = NewProcessor(
				source, repository, threads, w.now,
			).Retrieve(ctx, job.Mailbox, job.MessageID)
		}
		if err != nil {
			if markErr := w.queue.MarkNotificationFailed(
				ctx, job.HintID, now, graphFailureCode(err),
			); markErr != nil {
				return result, markErr
			}
			result.Failed++
			continue
		}
		if err := w.queue.MarkNotificationProcessed(
			ctx, job.HintID, now,
		); err != nil {
			return result, err
		}
		result.Processed++
	}
	return result, nil
}
