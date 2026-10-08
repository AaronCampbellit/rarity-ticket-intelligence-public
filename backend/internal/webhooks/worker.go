package webhooks

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/outbox"
)

const outboundLeaseDuration = 30 * time.Second

var ErrDeliveryStateConflict = errors.New("webhook delivery state conflict")

type OutboundJob struct {
	Connection     WebhookConnection
	Event          OutboundEvent
	Attempt        int
	FirstAttemptAt time.Time
	RetryWindow    time.Duration
}

type OutboundCompletion struct {
	ConnectionID  string
	EventID       string
	Attempt       int
	CompletedAt   time.Time
	NextAttemptAt time.Time
	ErrorCode     string
}

type OutboundQueue interface {
	Plan(context.Context, int, time.Time) (int, error)
	Claim(context.Context, int, time.Time, time.Duration) ([]OutboundJob, error)
	MarkDelivered(context.Context, OutboundCompletion) error
	MarkRetry(context.Context, OutboundCompletion) error
	MarkFailed(context.Context, OutboundCompletion) error
}

type OutboundPublisher interface {
	PublishOutbound(context.Context, OutboundJob) error
}

type OutboundResult struct {
	Planned   int
	Claimed   int
	Delivered int
	Retrying  int
	Failed    int
}

type OutboundWorker struct {
	queue     OutboundQueue
	publisher OutboundPublisher
	now       func() time.Time
}

func NewOutboundWorker(
	queue OutboundQueue,
	publisher OutboundPublisher,
	now func() time.Time,
) *OutboundWorker {
	return &OutboundWorker{queue: queue, publisher: publisher, now: now}
}

func (w *OutboundWorker) RunOnce(
	ctx context.Context,
	limit int,
) (OutboundResult, error) {
	if w.queue == nil || w.publisher == nil || w.now == nil || limit < 1 {
		return OutboundResult{}, ErrDeliveryFailed
	}
	now := w.now().UTC()
	planned, err := w.queue.Plan(ctx, limit, now)
	if err != nil {
		return OutboundResult{}, err
	}
	jobs, err := w.queue.Claim(ctx, limit, now, outboundLeaseDuration)
	if err != nil {
		return OutboundResult{Planned: planned}, err
	}
	result := OutboundResult{Planned: planned, Claimed: len(jobs)}
	for _, job := range jobs {
		completion := OutboundCompletion{
			ConnectionID: job.Connection.ID, EventID: job.Event.ID,
			Attempt: job.Attempt, CompletedAt: now,
		}
		deadline := job.FirstAttemptAt.Add(job.RetryWindow)
		if job.FirstAttemptAt.IsZero() || job.RetryWindow <= 0 ||
			!deadline.After(now) {
			completion.ErrorCode = "retry_window_expired"
			if err := w.queue.MarkFailed(ctx, completion); err != nil {
				return result, err
			}
			result.Failed++
			continue
		}
		publishErr := w.publisher.PublishOutbound(ctx, job)
		if publishErr == nil {
			if err := w.queue.MarkDelivered(ctx, completion); err != nil {
				return result, err
			}
			result.Delivered++
			continue
		}
		if errors.Is(publishErr, ErrUnsafeDestination) {
			completion.ErrorCode = "unsafe_destination"
			if err := w.queue.MarkFailed(ctx, completion); err != nil {
				return result, err
			}
			result.Failed++
			continue
		}
		completion.NextAttemptAt = now.Add(outbox.RetryDelay(job.Attempt))
		if !deadline.After(completion.NextAttemptAt) {
			completion.NextAttemptAt = time.Time{}
			completion.ErrorCode = "retry_window_expired"
			if err := w.queue.MarkFailed(ctx, completion); err != nil {
				return result, err
			}
			result.Failed++
			continue
		}
		completion.ErrorCode = "delivery_failed"
		if err := w.queue.MarkRetry(ctx, completion); err != nil {
			return result, err
		}
		result.Retrying++
	}
	return result, nil
}
