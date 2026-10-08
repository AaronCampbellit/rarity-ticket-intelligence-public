package mentions

import (
	"context"
	"errors"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/observability"
)

const (
	maximumInvalidationBatch = 500
	invalidationLease        = 5 * time.Minute
)

var ErrInvalidInvalidationRun = errors.New("invalid mention access invalidation run")

// AffectedItem is a leased access invalidation. It deliberately contains no
// source body, preview, token label, or token-adjacent text.
type AffectedItem struct {
	InvalidationID       string
	LeaseToken           string
	ID                   string
	MSPID                string
	ClientID             string
	RecipientID          string
	ParentType           ParentType
	ParentID             string
	ReasonCode           string
	SnapshotOccurrenceID string
	AccessLossConfirmed  bool
}

type InvalidationDecision struct {
	AccessAllowed      bool
	ItemSuppressed     bool
	DeliveriesCanceled int
}

type InvalidationResult struct {
	Claimed            int
	Completed          int
	Suppressed         int
	DeliveriesCanceled int
}

type InvalidationRepository interface {
	ClaimAccessInvalidations(context.Context, int, time.Time, time.Duration) ([]AffectedItem, error)
	ProcessAccessInvalidation(context.Context, AffectedItem, time.Time) (InvalidationDecision, error)
}

type InvalidationWorker struct {
	repository InvalidationRepository
	now        func() time.Time
	telemetry  *observability.MentionTelemetry
}

func (worker *InvalidationWorker) WithTelemetry(telemetry *observability.MentionTelemetry) *InvalidationWorker {
	if worker != nil {
		worker.telemetry = telemetry
	}
	return worker
}

func NewInvalidationWorker(repository InvalidationRepository, now func() time.Time) *InvalidationWorker {
	return &InvalidationWorker{repository: repository, now: now}
}

func (worker *InvalidationWorker) RunOnce(ctx context.Context, limit int) (InvalidationResult, error) {
	if worker == nil || worker.repository == nil || worker.now == nil || limit < 1 || limit > maximumInvalidationBatch {
		return InvalidationResult{}, ErrInvalidInvalidationRun
	}
	now := worker.now().UTC()
	if now.IsZero() {
		return InvalidationResult{}, ErrInvalidInvalidationRun
	}
	affected, err := worker.repository.ClaimAccessInvalidations(ctx, limit, now, invalidationLease)
	if err != nil {
		return InvalidationResult{}, err
	}
	if len(affected) > limit {
		return InvalidationResult{}, ErrInvalidInvalidationRun
	}
	result := InvalidationResult{Claimed: len(affected)}
	for _, item := range affected {
		decision, processErr := worker.repository.ProcessAccessInvalidation(ctx, item, now)
		if processErr != nil {
			return result, processErr
		}
		result.Completed++
		if decision.ItemSuppressed {
			result.Suppressed++
			worker.telemetry.Count(observability.MentionMetric{
				Name: "suppression", ParentType: string(item.ParentType),
				Outcome: "access_revoked", MSPID: item.MSPID,
				ClientID: item.ClientID, ObjectID: item.ParentID,
			})
		}
		result.DeliveriesCanceled += decision.DeliveriesCanceled
		worker.telemetry.CountN(observability.MentionMetric{
			Name: "notification", ParentType: string(item.ParentType),
			Outcome: "access_revoked", MSPID: item.MSPID,
			ClientID: item.ClientID, ObjectID: item.ParentID,
		}, uint64(decision.DeliveriesCanceled))
	}
	return result, nil
}
