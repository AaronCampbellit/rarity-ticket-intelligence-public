package graphintake

import (
	"context"
	"testing"
	"time"
)

type graphNotificationQueue struct {
	jobs       []NotificationJob
	repository *fakeIntakeRepository
	processed  []string
	failed     []string
}

func (q *graphNotificationQueue) ClaimNotifications(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]NotificationJob, error) {
	return append([]NotificationJob(nil), q.jobs...), nil
}

func (q *graphNotificationQueue) Bind(
	string,
) (IntakeRepository, ThreadIndex) {
	return q.repository, fakeThreadIndex{}
}

func (q *graphNotificationQueue) MarkNotificationProcessed(
	_ context.Context,
	hintID string,
	_ time.Time,
) error {
	q.processed = append(q.processed, hintID)
	return nil
}

func (q *graphNotificationQueue) MarkNotificationFailed(
	_ context.Context,
	hintID string,
	_ time.Time,
	errorCode string,
) error {
	q.failed = append(q.failed, hintID+"|"+errorCode)
	return nil
}

func TestNotificationWorkerRetrievesHintedMessageAndMarksItProcessed(t *testing.T) {
	now := time.Date(2026, time.July, 29, 21, 5, 0, 0, time.UTC)
	queue := &graphNotificationQueue{
		jobs: []NotificationJob{{
			HintID: "hint-id",
			ReconciliationJob: ReconciliationJob{
				ConnectionID: "connection-id", Mailbox: "support@example.com",
				CredentialSecretRef: "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
			},
			MessageID: "message-id",
		}},
		repository: &fakeIntakeRepository{},
	}
	factory := &graphSourceFactory{source: &fakeGraphSource{message: Message{
		ID: "message-id", Sender: "sender@example.net",
		RawMIMERef: "graph/connection-id/message-id.eml",
	}}}
	worker := NewNotificationWorker(queue, factory, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 100)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Claimed != 1 || result.Processed != 1 || result.Failed != 0 ||
		len(queue.processed) != 1 || queue.processed[0] != "hint-id" ||
		queue.repository.message.ExternalID != "message-id" {
		t.Fatalf("unexpected result=%+v queue=%+v", result, queue)
	}
}
