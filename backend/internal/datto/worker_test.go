package datto

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dattoSyncQueue struct {
	connections []Connection
	repository  *syncRepository
	failures    []string
}

func (q *dattoSyncQueue) Claim(
	context.Context,
	int,
	time.Time,
	time.Duration,
) ([]Connection, error) {
	return append([]Connection(nil), q.connections...), nil
}

func (q *dattoSyncQueue) Start(ctx context.Context, run SyncRun) error {
	return q.repository.Start(ctx, run)
}
func (q *dattoSyncQueue) Apply(
	ctx context.Context,
	connectionID string,
	page SyncPage,
	markMissing bool,
) error {
	return q.repository.Apply(ctx, connectionID, page, markMissing)
}
func (q *dattoSyncQueue) Complete(
	ctx context.Context,
	completion SyncCompletion,
) error {
	return q.repository.Complete(ctx, completion)
}
func (q *dattoSyncQueue) Fail(
	ctx context.Context,
	failure SyncFailure,
) error {
	q.failures = append(q.failures, failure.RunID+"|"+failure.ErrorCode)
	return q.repository.Fail(ctx, failure)
}
func (q *dattoSyncQueue) ReleaseClaim(
	_ context.Context,
	connectionID string,
	_ time.Time,
	errorCode string,
) error {
	q.failures = append(q.failures, connectionID+"|"+errorCode)
	return nil
}

type dattoSourceFactory struct {
	source SyncSource
	err    error
}

func (f dattoSourceFactory) Source(
	context.Context,
	Connection,
) (SyncSource, error) {
	return f.source, f.err
}

func TestSyncWorkerRunsEveryDueConnectionWithoutCrossConnectionBlocking(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 30, 0, 0, time.UTC)
	repository := &syncRepository{}
	queue := &dattoSyncQueue{
		connections: []Connection{{
			ID:                  "connection-id",
			CredentialSecretRef: "env://RARITY_DATTO_CREDENTIAL_PRIMARY",
		}},
		repository: repository,
	}
	factory := dattoSourceFactory{source: &syncSource{page: SyncPage{
		Assets:     []RemoteAsset{{ExternalID: "asset-id"}},
		NextCursor: "cursor-1",
	}}}
	worker := NewSyncWorker(
		queue, factory, func() time.Time { return now },
		func() string { return "run-id" },
	)
	result, err := worker.RunOnce(context.Background(), 25)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Claimed != 1 || result.Succeeded != 1 || result.Failed != 0 ||
		repository.completed.NextCursor != "cursor-1" {
		t.Fatalf("unexpected result=%+v repository=%+v", result, repository)
	}
}

func TestSyncWorkerReleasesClaimWhenCredentialSourceCannotBeBuilt(t *testing.T) {
	queue := &dattoSyncQueue{
		connections: []Connection{{ID: "connection-id"}},
		repository:  &syncRepository{},
	}
	worker := NewSyncWorker(
		queue, dattoSourceFactory{err: errors.New("secret value")},
		time.Now, func() string { return "run-id" },
	)
	result, err := worker.RunOnce(context.Background(), 25)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Failed != 1 ||
		len(queue.failures) != 1 ||
		queue.failures[0] != "connection-id|source_unavailable" {
		t.Fatalf("unexpected result=%+v failures=%v", result, queue.failures)
	}
}
