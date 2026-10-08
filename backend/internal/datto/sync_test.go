package datto

import (
	"context"
	"testing"
	"time"
)

type syncSource struct {
	page SyncPage
	kind SyncKind
}

func (s *syncSource) Fetch(_ context.Context, _ Connection, kind SyncKind) (SyncPage, error) {
	s.kind = kind
	return s.page, nil
}

type syncRepository struct {
	started     SyncRun
	applied     SyncPage
	completed   SyncCompletion
	markMissing bool
}

func (r *syncRepository) Start(_ context.Context, run SyncRun) error {
	r.started = run
	return nil
}
func (r *syncRepository) Apply(_ context.Context, _ string, page SyncPage, markMissing bool) error {
	r.applied = page
	r.markMissing = markMissing
	return nil
}
func (r *syncRepository) Complete(_ context.Context, completion SyncCompletion) error {
	r.completed = completion
	return nil
}
func (r *syncRepository) Fail(context.Context, SyncFailure) error { return nil }

func TestRunnerAppliesFullSnapshotAndMarksMissingAssetsStale(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 30, 0, 0, time.UTC)
	source := &syncSource{page: SyncPage{
		Assets: []RemoteAsset{{ExternalID: "asset-id"}}, NextCursor: "cursor-1",
		RateLimit: RateLimitState{Remaining: 99, ResetAt: now.Add(time.Minute)},
	}}
	repository := &syncRepository{}
	runner := NewSyncRunner(source, repository, func() time.Time { return now }, func() string { return "run-id" })
	result, err := runner.Run(context.Background(), Connection{ID: "connection-id"}, false)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if source.kind != SyncFull || !repository.markMissing ||
		repository.completed.NextCursor != "cursor-1" ||
		result.AssetsSeen != 1 {
		t.Fatalf("full sync incomplete: repo=%+v result=%+v", repository, result)
	}
}

func TestRunnerIncrementalSyncNeverMarksUnseenAssetsMissing(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 30, 0, 0, time.UTC)
	source := &syncSource{page: SyncPage{NextCursor: "cursor-2"}}
	repository := &syncRepository{}
	runner := NewSyncRunner(source, repository, func() time.Time { return now }, func() string { return "run-id" })
	_, err := runner.Run(context.Background(), Connection{
		ID: "connection-id", Cursor: "cursor-1", LastCompletedAt: now.Add(-time.Hour),
	}, false)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if source.kind != SyncIncremental || repository.markMissing {
		t.Fatalf("incremental sync applied full-snapshot semantics: %+v", repository)
	}
}

func TestRunnerMarksMissingAfterProviderReturnsCompleteIncrementalInventory(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 30, 0, 0, time.UTC)
	source := &syncSource{page: SyncPage{
		NextCursor: "cursor-2", CompleteInventory: true,
	}}
	repository := &syncRepository{}
	runner := NewSyncRunner(
		source, repository, func() time.Time { return now },
		func() string { return "run-id" },
	)

	_, err := runner.Run(
		context.Background(),
		Connection{
			ID: "connection-id", Cursor: "cursor-1",
			LastCompletedAt: now.Add(-time.Hour),
		},
		false,
	)

	if err != nil || source.kind != SyncIncremental || !repository.markMissing {
		t.Fatalf(
			"complete provider inventory did not mark missing assets: repo=%+v error=%v",
			repository, err,
		)
	}
}
