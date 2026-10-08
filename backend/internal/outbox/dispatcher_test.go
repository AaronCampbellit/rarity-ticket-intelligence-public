package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type eventConsumerStub struct {
	seen []string
	err  error
}

func (c *eventConsumerStub) Handle(_ context.Context, event mutation.EventRecord) error {
	c.seen = append(c.seen, event.EventID)
	return c.err
}

type fakeStore struct {
	events         []Event
	published      []string
	failed         []Failure
	claimErr       error
	markPublishErr error
}

func (s *fakeStore) Claim(context.Context, int, time.Time) ([]Event, error) {
	return s.events, s.claimErr
}
func (s *fakeStore) MarkPublished(_ context.Context, eventID string, _ time.Time) error {
	s.published = append(s.published, eventID)
	return s.markPublishErr
}
func (s *fakeStore) MarkFailed(_ context.Context, failure Failure) error {
	s.failed = append(s.failed, failure)
	return nil
}

type fakePublisher struct {
	fail map[string]error
	seen []string
}

func (p *fakePublisher) Publish(_ context.Context, event Event) error {
	p.seen = append(p.seen, event.ID)
	return p.fail[event.ID]
}

func TestDispatchMarksPublishedOnlyAfterSuccessfulDelivery(t *testing.T) {
	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	store := &fakeStore{events: []Event{{ID: "event-1"}, {ID: "event-2"}}}
	publisher := &fakePublisher{fail: map[string]error{"event-2": errors.New("offline")}}
	dispatcher := NewDispatcher(store, publisher, func() time.Time { return now })

	result, err := dispatcher.Dispatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Published != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.published) != 1 || store.published[0] != "event-1" {
		t.Fatalf("published marks = %v", store.published)
	}
	if len(store.failed) != 1 || store.failed[0].EventID != "event-2" {
		t.Fatalf("failure marks = %+v", store.failed)
	}
	if !store.failed[0].NextAttemptAt.After(now) {
		t.Fatal("failed delivery was not scheduled for retry")
	}
	if store.failed[0].ErrorCode != "publish_failed" {
		t.Fatalf("unsafe or unstable error code: %q", store.failed[0].ErrorCode)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := RetryDelay(0); got != time.Second {
		t.Fatalf("RetryDelay(0) = %v", got)
	}
	if got := RetryDelay(100); got != 15*time.Minute {
		t.Fatalf("RetryDelay(100) = %v, want 15m", got)
	}
}

func TestDispatchProjectsDomainEventBeforePublishing(t *testing.T) {
	store := &fakeStore{events: []Event{{ID: "event-1", Record: mutation.EventRecord{EventID: "event-1"}}}}
	publisher := &fakePublisher{fail: map[string]error{}}
	consumer := &eventConsumerStub{}
	dispatcher := NewDispatcherWithConsumer(store, publisher, consumer, func() time.Time { return time.Now() })
	result, err := dispatcher.Dispatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published != 1 || len(consumer.seen) != 1 || consumer.seen[0] != "event-1" {
		t.Fatalf("result=%+v consumer=%v", result, consumer.seen)
	}
}
func TestDispatchRetriesWithoutPublishingWhenProjectionFails(t *testing.T) {
	store := &fakeStore{events: []Event{{ID: "event-1", Record: mutation.EventRecord{EventID: "event-1"}}}}
	publisher := &fakePublisher{fail: map[string]error{}}
	consumer := &eventConsumerStub{err: errors.New("projection failed")}
	dispatcher := NewDispatcherWithConsumer(store, publisher, consumer, func() time.Time { return time.Now() })
	result, err := dispatcher.Dispatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || len(publisher.seen) != 0 {
		t.Fatalf("result=%+v published=%v", result, publisher.seen)
	}
}
