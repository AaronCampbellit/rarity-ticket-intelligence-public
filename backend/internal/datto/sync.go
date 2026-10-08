package datto

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidSync = errors.New("invalid Datto sync")
	ErrSyncNotDue  = errors.New("Datto sync not due")
)

type RateLimitState struct {
	Remaining int
	ResetAt   time.Time
}

type SyncPage struct {
	Assets            []RemoteAsset
	Alerts            []AlertObservation
	NextCursor        string
	Changed           int
	RateLimit         RateLimitState
	ObservedAt        time.Time
	CompleteInventory bool
}

type SyncRun struct {
	ID           string
	ConnectionID string
	Kind         SyncKind
	Manual       bool
	StartedAt    time.Time
}

type SyncCompletion struct {
	RunID         string
	ConnectionID  string
	CompletedAt   time.Time
	NextCursor    string
	AssetsSeen    int
	AssetsChanged int
	RateLimit     RateLimitState
}

type SyncFailure struct {
	RunID     string
	FailedAt  time.Time
	ErrorCode string
	RateLimit RateLimitState
}

type SyncSource interface {
	Fetch(context.Context, Connection, SyncKind) (SyncPage, error)
}

type SyncRepository interface {
	Start(context.Context, SyncRun) error
	Apply(context.Context, string, SyncPage, bool) error
	Complete(context.Context, SyncCompletion) error
	Fail(context.Context, SyncFailure) error
}

type SyncRunner struct {
	source     SyncSource
	repository SyncRepository
	now        func() time.Time
	newID      func() string
}

func NewSyncRunner(
	source SyncSource,
	repository SyncRepository,
	now func() time.Time,
	newID func() string,
) *SyncRunner {
	return &SyncRunner{source: source, repository: repository, now: now, newID: newID}
}

type SyncResult struct {
	RunID         string
	Kind          SyncKind
	AssetsSeen    int
	AssetsChanged int
	RateLimit     RateLimitState
}

func (r *SyncRunner) Run(
	ctx context.Context,
	connection Connection,
	manual bool,
) (SyncResult, error) {
	if r.source == nil || r.repository == nil || r.now == nil || r.newID == nil ||
		strings.TrimSpace(connection.ID) == "" {
		return SyncResult{}, ErrInvalidSync
	}
	now := r.now().UTC()
	plan := PlanSync(connection, now)
	if !manual && !plan.Due {
		return SyncResult{}, ErrSyncNotDue
	}
	runID := r.newID()
	run := SyncRun{
		ID: runID, ConnectionID: connection.ID, Kind: plan.Kind,
		Manual: manual, StartedAt: now,
	}
	if err := r.repository.Start(ctx, run); err != nil {
		return SyncResult{}, err
	}
	page, err := r.source.Fetch(ctx, connection, plan.Kind)
	if err != nil {
		_ = r.repository.Fail(ctx, SyncFailure{
			RunID: runID, FailedAt: now, ErrorCode: "datto_fetch_failed",
		})
		return SyncResult{}, err
	}
	if strings.TrimSpace(page.NextCursor) == "" {
		_ = r.repository.Fail(ctx, SyncFailure{
			RunID: runID, FailedAt: now, ErrorCode: "datto_cursor_missing",
			RateLimit: page.RateLimit,
		})
		return SyncResult{}, ErrInvalidSync
	}
	markMissing := plan.Kind == SyncFull || page.CompleteInventory
	if err := r.repository.Apply(ctx, connection.ID, page, markMissing); err != nil {
		_ = r.repository.Fail(ctx, SyncFailure{
			RunID: runID, FailedAt: now, ErrorCode: "datto_apply_failed",
			RateLimit: page.RateLimit,
		})
		return SyncResult{}, err
	}
	completion := SyncCompletion{
		RunID: runID, ConnectionID: connection.ID, CompletedAt: now,
		NextCursor: page.NextCursor, AssetsSeen: len(page.Assets),
		AssetsChanged: page.Changed, RateLimit: page.RateLimit,
	}
	if err := r.repository.Complete(ctx, completion); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{
		RunID: runID, Kind: plan.Kind, AssetsSeen: len(page.Assets),
		AssetsChanged: page.Changed, RateLimit: page.RateLimit,
	}, nil
}
