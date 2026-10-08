package datto

import (
	"context"
	"errors"
	"testing"
	"time"
)

type alertQueueStub struct {
	items      []AlertQueueItem
	resolution AlertResolution
	completed  []AlertQueueItem
	released   []AlertQueueItem
}

func (q *alertQueueStub) ClaimAlerts(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]AlertQueueItem, error) {
	return q.items, nil
}

func (q *alertQueueStub) ResolveQueuedAlert(
	context.Context,
	AlertQueueItem,
	time.Duration,
) (AlertResolution, error) {
	return q.resolution, nil
}

func (q *alertQueueStub) CompleteAlert(
	_ context.Context,
	item AlertQueueItem,
	_ AlertResolution,
	_ time.Time,
) error {
	q.completed = append(q.completed, item)
	return nil
}

func (q *alertQueueStub) ReleaseAlert(
	_ context.Context,
	item AlertQueueItem,
	_ time.Time,
	_ string,
) error {
	q.released = append(q.released, item)
	return nil
}

type alertIncidentWriterStub struct {
	incidents []AlertIncident
	err       error
}

func (w *alertIncidentWriterStub) EnsureIncident(
	_ context.Context,
	incident AlertIncident,
) error {
	w.incidents = append(w.incidents, incident)
	return w.err
}

func TestAlertWorkerCreatesOneIdempotentIncidentForUnmatchedActiveAlert(t *testing.T) {
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	item := AlertQueueItem{
		ID:           "11111111-1111-4111-8111-111111111111",
		ConnectionID: "connection-id", MSPID: "msp-id",
		ClientID: "client-id", ActorID: "actor-id",
		Observation: AlertObservation{
			ExternalID: "alert-id", State: AlertActive,
			Title: "Gateway unreachable", Priority: "Critical",
			ObservedAt: now,
		},
	}
	queue := &alertQueueStub{
		items: []AlertQueueItem{item},
		resolution: AlertResolution{
			Action:     AlertCreateIncident,
			IncidentID: item.ID,
		},
	}
	writer := &alertIncidentWriterStub{}
	worker := NewAlertWorker(queue, writer, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Created != 1 || len(writer.incidents) != 1 ||
		writer.incidents[0].ID != item.ID ||
		writer.incidents[0].Priority != "critical" ||
		len(queue.completed) != 1 || len(queue.released) != 0 {
		t.Fatalf(
			"RunOnce() result=%+v error=%v incidents=%+v completed=%+v released=%+v",
			result, err, writer.incidents, queue.completed, queue.released,
		)
	}
}

func TestAlertWorkerReleasesClaimWhenIncidentCreationFails(t *testing.T) {
	now := time.Now().UTC()
	item := AlertQueueItem{
		ID:           "11111111-1111-4111-8111-111111111111",
		ConnectionID: "connection-id", MSPID: "msp-id",
		ClientID: "client-id", ActorID: "actor-id",
		Observation: AlertObservation{
			ExternalID: "alert-id", State: AlertActive,
			Title: "Gateway unreachable", ObservedAt: now,
		},
	}
	queue := &alertQueueStub{
		items: []AlertQueueItem{item},
		resolution: AlertResolution{
			Action: AlertCreateIncident, IncidentID: item.ID,
		},
	}
	writer := &alertIncidentWriterStub{err: errors.New("workflow unavailable")}
	worker := NewAlertWorker(queue, writer, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)

	if err != nil || result.Failed != 1 || len(queue.released) != 1 ||
		len(queue.completed) != 0 {
		t.Fatalf(
			"RunOnce() result=%+v error=%v completed=%+v released=%+v",
			result, err, queue.completed, queue.released,
		)
	}
}
