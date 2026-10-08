package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

type outboundQueue struct {
	planned     int
	jobs        []OutboundJob
	delivered   []OutboundCompletion
	retrying    []OutboundCompletion
	failed      []OutboundCompletion
	planErr     error
	claimErr    error
	completeErr error
}

func (q *outboundQueue) Plan(
	context.Context,
	int,
	time.Time,
) (int, error) {
	return q.planned, q.planErr
}

func (q *outboundQueue) Claim(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]OutboundJob, error) {
	return q.jobs, q.claimErr
}

func (q *outboundQueue) MarkDelivered(
	_ context.Context,
	completion OutboundCompletion,
) error {
	q.delivered = append(q.delivered, completion)
	return q.completeErr
}

func (q *outboundQueue) MarkRetry(
	_ context.Context,
	completion OutboundCompletion,
) error {
	q.retrying = append(q.retrying, completion)
	return q.completeErr
}

func (q *outboundQueue) MarkFailed(
	_ context.Context,
	completion OutboundCompletion,
) error {
	q.failed = append(q.failed, completion)
	return q.completeErr
}

type outboundPublisher struct {
	errors map[string]error
	seen   []OutboundJob
}

func (p *outboundPublisher) PublishOutbound(
	_ context.Context,
	job OutboundJob,
) error {
	p.seen = append(p.seen, job)
	return p.errors[job.Event.ID]
}

func TestOutboundWorkerPlansClaimsAndCompletesDeliveries(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	queue := &outboundQueue{
		planned: 2,
		jobs: []OutboundJob{
			{
				Connection: WebhookConnection{ID: "connection-1"},
				Event:      OutboundEvent{ID: "event-1"}, Attempt: 1,
				FirstAttemptAt: now, RetryWindow: 24 * time.Hour,
			},
			{
				Connection: WebhookConnection{ID: "connection-2"},
				Event:      OutboundEvent{ID: "event-2"}, Attempt: 3,
				FirstAttemptAt: now.Add(-time.Hour), RetryWindow: 24 * time.Hour,
			},
		},
	}
	publisher := &outboundPublisher{
		errors: map[string]error{"event-2": ErrDeliveryFailed},
	}
	worker := NewOutboundWorker(queue, publisher, func() time.Time { return now })
	result, err := worker.RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if result.Planned != 2 || result.Claimed != 2 ||
		result.Delivered != 1 || result.Retrying != 1 || result.Failed != 0 ||
		len(queue.delivered) != 1 || len(queue.retrying) != 1 ||
		!queue.retrying[0].NextAttemptAt.After(now) {
		t.Fatalf("worker result=%+v queue=%+v", result, queue)
	}
}

func TestOutboundWorkerPermanentlyFailsUnsafeOrExpiredDelivery(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	queue := &outboundQueue{jobs: []OutboundJob{
		{
			Connection: WebhookConnection{ID: "unsafe"},
			Event:      OutboundEvent{ID: "event-unsafe"}, Attempt: 1,
			FirstAttemptAt: now, RetryWindow: 24 * time.Hour,
		},
		{
			Connection: WebhookConnection{ID: "expired"},
			Event:      OutboundEvent{ID: "event-expired"}, Attempt: 10,
			FirstAttemptAt: now.Add(-24 * time.Hour), RetryWindow: 24 * time.Hour,
		},
	}}
	publisher := &outboundPublisher{errors: map[string]error{
		"event-unsafe":  ErrUnsafeDestination,
		"event-expired": ErrDeliveryFailed,
	}}
	result, err := NewOutboundWorker(
		queue, publisher, func() time.Time { return now },
	).RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if result.Failed != 2 || len(queue.failed) != 2 ||
		queue.failed[0].ErrorCode != "unsafe_destination" ||
		queue.failed[1].ErrorCode != "retry_window_expired" {
		t.Fatalf("worker result=%+v failures=%+v", result, queue.failed)
	}
}

func TestOutboundWorkerStopsOnQueueStateFailure(t *testing.T) {
	queue := &outboundQueue{completeErr: errors.New("database unavailable")}
	queue.jobs = []OutboundJob{{
		Connection: WebhookConnection{ID: "connection"},
		Event:      OutboundEvent{ID: "event"}, Attempt: 1,
		FirstAttemptAt: time.Now(), RetryWindow: time.Hour,
	}}
	_, err := NewOutboundWorker(
		queue, &outboundPublisher{errors: map[string]error{}}, time.Now,
	).RunOnce(context.Background(), 1)
	if !errors.Is(err, queue.completeErr) {
		t.Fatalf("RunOnce() error=%v", err)
	}
}
