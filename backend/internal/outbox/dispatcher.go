// Package outbox dispatches durable domain events with at-least-once delivery.
package outbox

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type Event struct {
	ID               string
	Type             string
	SchemaVersion    int
	Payload          []byte
	DeliveryAttempts int
	Record           mutation.EventRecord
}

type Failure struct {
	EventID       string
	ErrorCode     string
	NextAttemptAt time.Time
}

type Result struct {
	Published int
	Failed    int
}

type Store interface {
	Claim(context.Context, int, time.Time) ([]Event, error)
	MarkPublished(context.Context, string, time.Time) error
	MarkFailed(context.Context, Failure) error
}

type Publisher interface {
	Publish(context.Context, Event) error
}

type DomainEventConsumer interface {
	Handle(context.Context, mutation.EventRecord) error
}

type Dispatcher struct {
	store     Store
	publisher Publisher
	now       func() time.Time
	consumer  DomainEventConsumer
}

func NewDispatcherWithConsumer(store Store, publisher Publisher, consumer DomainEventConsumer, now func() time.Time) *Dispatcher {
	return &Dispatcher{store: store, publisher: publisher, consumer: consumer, now: now}
}

func NewDispatcher(store Store, publisher Publisher, now func() time.Time) *Dispatcher {
	return &Dispatcher{store: store, publisher: publisher, now: now}
}

func (d *Dispatcher) Dispatch(ctx context.Context, limit int) (Result, error) {
	now := d.now().UTC()
	events, err := d.store.Claim(ctx, limit, now)
	if err != nil {
		return Result{}, err
	}
	result := Result{}
	for _, event := range events {
		if d.consumer != nil {
			if err := d.consumer.Handle(ctx, event.Record); err != nil {
				failure := Failure{EventID: event.ID, ErrorCode: "projection_failed", NextAttemptAt: now.Add(RetryDelay(event.DeliveryAttempts))}
				if err := d.store.MarkFailed(ctx, failure); err != nil {
					return result, err
				}
				result.Failed++
				continue
			}
		}
		if err := d.publisher.Publish(ctx, event); err != nil {
			failure := Failure{
				EventID:       event.ID,
				ErrorCode:     "publish_failed",
				NextAttemptAt: now.Add(RetryDelay(event.DeliveryAttempts)),
			}
			if err := d.store.MarkFailed(ctx, failure); err != nil {
				return result, err
			}
			result.Failed++
			continue
		}
		if err := d.store.MarkPublished(ctx, event.ID, now); err != nil {
			return result, err
		}
		result.Published++
	}
	return result, nil
}

func RetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 9 {
		return 15 * time.Minute
	}
	delay := time.Second * time.Duration(1<<attempt)
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}
