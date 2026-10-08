package graphintake

import (
	"context"
	"errors"
	"testing"
	"time"
)

type graphWorkerQueue struct {
	planned    int
	jobs       []ReconciliationJob
	repository *fakeIntakeRepository
	failures   []string
}

func (q *graphWorkerQueue) Plan(
	context.Context,
	int,
	time.Time,
) (int, error) {
	return q.planned, nil
}

func (q *graphWorkerQueue) Claim(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]ReconciliationJob, error) {
	return append([]ReconciliationJob(nil), q.jobs...), nil
}

func (q *graphWorkerQueue) Bind(
	string,
) (IntakeRepository, ThreadIndex) {
	return q.repository, fakeThreadIndex{}
}

func (q *graphWorkerQueue) MarkFailed(
	_ context.Context,
	connectionID string,
	_ time.Time,
	errorCode string,
) error {
	q.failures = append(q.failures, connectionID+"|"+errorCode)
	return nil
}

type graphSourceFactory struct {
	source GraphSource
	err    error
	jobs   []ReconciliationJob
}

func (f *graphSourceFactory) Source(
	_ context.Context,
	job ReconciliationJob,
) (GraphSource, error) {
	f.jobs = append(f.jobs, job)
	return f.source, f.err
}

func TestReconciliationWorkerRunsEveryClaimedMailboxAndAdvancesCursor(t *testing.T) {
	now := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	repository := &fakeIntakeRepository{cursor: "cursor-1"}
	queue := &graphWorkerQueue{
		planned: 1,
		jobs: []ReconciliationJob{{
			ConnectionID: "connection-id", Mailbox: "support@example.com",
			Folder: "inbox", CredentialSecretRef: "env://RARITY_GRAPH_CREDENTIAL_SUPPORT",
		}},
		repository: repository,
	}
	factory := &graphSourceFactory{source: &fakeGraphSource{page: DeltaPage{
		Messages:   []Message{{ID: "message-id"}},
		NextCursor: "cursor-2",
	}}}
	worker := NewReconciliationWorker(queue, factory, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Planned != 1 || result.Claimed != 1 ||
		result.Processed != 1 || result.Failed != 0 ||
		repository.committed.NextCursor != "cursor-2" ||
		len(factory.jobs) != 1 {
		t.Fatalf("unexpected worker result=%+v batch=%+v jobs=%+v", result, repository.committed, factory.jobs)
	}
}

func TestReconciliationWorkerRecordsBoundedFailureWithoutBlockingOtherMailbox(t *testing.T) {
	now := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	queue := &graphWorkerQueue{
		jobs: []ReconciliationJob{
			{ConnectionID: "connection-1", Mailbox: "first@example.com", Folder: "inbox"},
			{ConnectionID: "connection-2", Mailbox: "second@example.com", Folder: "inbox"},
		},
		repository: &fakeIntakeRepository{},
	}
	factory := &graphSourceFactory{err: errors.New("credential contents must never escape")}
	worker := NewReconciliationWorker(queue, factory, func() time.Time { return now })

	result, err := worker.RunOnce(context.Background(), 25)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Claimed != 2 || result.Failed != 2 ||
		len(queue.failures) != 2 ||
		queue.failures[0] != "connection-1|source_unavailable" ||
		queue.failures[1] != "connection-2|source_unavailable" {
		t.Fatalf("unexpected failure result=%+v failures=%v", result, queue.failures)
	}
}
