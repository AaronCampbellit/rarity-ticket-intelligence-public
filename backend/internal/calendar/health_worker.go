package calendar

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrInvalidHealthRecompute = errors.New("invalid calendar health recompute event")

type HealthFactKind string

const (
	HealthFactProjection   HealthFactKind = "projection"
	HealthFactSourceStatus HealthFactKind = "source_status"
	HealthFactDependency   HealthFactKind = "dependency"
	HealthFactSchedule     HealthFactKind = "schedule"
	HealthFactAvailability HealthFactKind = "availability"
	HealthFactCapacity     HealthFactKind = "capacity"
	HealthFactSLA          HealthFactKind = "sla"
)

type HealthRecomputeEvent struct {
	SourceEventID string
	MSPID         string
	Kind          HealthFactKind
	SubjectID     string
	ProjectionIDs []string
	OccurredAt    time.Time
}

type HealthProjectionContext struct {
	ProjectionID   string
	MSPID          string
	ClientID       string
	Source         SourceRef
	SourceRevision int64
	Context        HealthContext
}

type HealthUpdate struct {
	ProjectionID   string
	MSPID          string
	ClientID       string
	Source         SourceRef
	SourceRevision int64
	Result         HealthResult
}

type HealthRepository interface {
	ResolveAffectedHealth(context.Context, HealthRecomputeEvent) ([]HealthProjectionContext, error)
	// ApplyHealthResultsAtomic claims source event ID + rule version, writes only
	// changed results, and appends their live-change rows in one transaction.
	ApplyHealthResultsAtomic(context.Context, HealthRecomputeEvent, []HealthUpdate) (bool, error)
}

type HealthWorker struct {
	repository HealthRepository
	now        func() time.Time
}

func NewHealthWorker(repository HealthRepository, now func() time.Time) *HealthWorker {
	return &HealthWorker{repository: repository, now: now}
}

func (w *HealthWorker) Handle(ctx context.Context, event HealthRecomputeEvent) error {
	if w == nil || w.repository == nil || w.now == nil || strings.TrimSpace(event.SourceEventID) == "" || strings.TrimSpace(event.MSPID) == "" || strings.TrimSpace(event.SubjectID) == "" || event.OccurredAt.IsZero() || !validHealthFactKind(event.Kind) {
		return ErrInvalidHealthRecompute
	}
	event.ProjectionIDs = append([]string(nil), event.ProjectionIDs...)
	sort.Strings(event.ProjectionIDs)
	for index, id := range event.ProjectionIDs {
		if strings.TrimSpace(id) == "" || index > 0 && id == event.ProjectionIDs[index-1] {
			return ErrInvalidHealthRecompute
		}
	}
	contexts, err := w.repository.ResolveAffectedHealth(ctx, event)
	if err != nil {
		return err
	}
	sort.Slice(contexts, func(i, j int) bool { return contexts[i].ProjectionID < contexts[j].ProjectionID })
	updates := make([]HealthUpdate, 0, len(contexts))
	for _, value := range contexts {
		if value.ProjectionID == "" || value.MSPID != event.MSPID || value.Source.MSPID != value.MSPID || value.Source.ClientID != value.ClientID || value.SourceRevision < 1 || value.Source.Validate() != nil {
			return ErrInvalidHealthRecompute
		}
		value.Context.Now = w.now()
		updates = append(updates, HealthUpdate{ProjectionID: value.ProjectionID, MSPID: value.MSPID, ClientID: value.ClientID, Source: value.Source, SourceRevision: value.SourceRevision, Result: EvaluateHealth(value.Context)})
	}
	_, err = w.repository.ApplyHealthResultsAtomic(ctx, event, updates)
	return err
}

func validHealthFactKind(kind HealthFactKind) bool {
	switch kind {
	case HealthFactProjection, HealthFactSourceStatus, HealthFactDependency, HealthFactSchedule, HealthFactAvailability, HealthFactCapacity, HealthFactSLA:
		return true
	default:
		return false
	}
}
