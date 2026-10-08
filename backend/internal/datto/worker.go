package datto

import (
	"context"
	"time"
)

const syncLease = 30 * time.Minute

type SyncQueue interface {
	SyncRepository
	Claim(context.Context, int, time.Time, time.Duration) ([]Connection, error)
	ReleaseClaim(context.Context, string, time.Time, string) error
}

type SyncWorker struct {
	queue   SyncQueue
	sources SourceFactory
	now     func() time.Time
	newID   func() string
}

type WorkerResult struct {
	Claimed   int
	Succeeded int
	Failed    int
}

func NewSyncWorker(
	queue SyncQueue,
	sources SourceFactory,
	now func() time.Time,
	newID func() string,
) *SyncWorker {
	return &SyncWorker{
		queue: queue, sources: sources, now: now, newID: newID,
	}
}

func (w *SyncWorker) RunOnce(
	ctx context.Context,
	limit int,
) (WorkerResult, error) {
	if w.queue == nil || w.sources == nil || w.now == nil ||
		w.newID == nil || limit <= 0 {
		return WorkerResult{}, ErrInvalidSync
	}
	now := w.now().UTC()
	connections, err := w.queue.Claim(
		ctx, limit, now, syncLease,
	)
	if err != nil {
		return WorkerResult{}, err
	}
	result := WorkerResult{Claimed: len(connections)}
	for _, connection := range connections {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		source, err := w.sources.Source(ctx, connection)
		if err != nil {
			if releaseErr := w.queue.ReleaseClaim(
				ctx, connection.ID, now, "source_unavailable",
			); releaseErr != nil {
				return result, releaseErr
			}
			result.Failed++
			continue
		}
		_, err = NewSyncRunner(
			source, w.queue, w.now, w.newID,
		).Run(ctx, connection, connection.Manual)
		if err != nil {
			if releaseErr := w.queue.ReleaseClaim(
				ctx, connection.ID, now, syncFailureCode(err),
			); releaseErr != nil {
				return result, releaseErr
			}
			result.Failed++
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func syncFailureCode(err error) string {
	switch err {
	case ErrInvalidSourceCredentials:
		return "credentials_invalid"
	case ErrInvalidProviderURL:
		return "provider_url_invalid"
	case ErrInvalidSourceResponse:
		return "provider_response_invalid"
	case ErrSyncNotDue:
		return "sync_not_due"
	default:
		return "sync_failed"
	}
}
