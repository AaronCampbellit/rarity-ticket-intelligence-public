package calendar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidProjectionBatch = errors.New("invalid calendar projection batch")

type ProjectionCursor struct {
	ConsumerKey string
	OccurredAt  time.Time
	EventID     string
}

type ResolvedProjectionSource struct {
	Source   SourceRef
	Relevant bool
}
type ProjectionSourceResolver interface {
	ResolveProjectionSource(context.Context, mutation.EventRecord) (ResolvedProjectionSource, error)
}
type ProjectionEventSource interface {
	ListCalendarProjectionEvents(context.Context, string, string, int) ([]mutation.EventRecord, error)
}
type projectionRevisionResolver interface {
	ResolveCalendarProjectionRevision(context.Context, SourceRef, int64) (int64, error)
}
type ProjectionWorker struct {
	resolver    ProjectionSourceResolver
	adapters    *AdapterRegistry
	service     *ProjectionService
	consumerKey string
}

func NewProjectionWorker(resolver ProjectionSourceResolver, adapters *AdapterRegistry, service *ProjectionService, consumerKey string) *ProjectionWorker {
	return &ProjectionWorker{resolver: resolver, adapters: adapters, service: service, consumerKey: consumerKey}
}
func (w *ProjectionWorker) Handle(ctx context.Context, event mutation.EventRecord) error {
	if w == nil || w.resolver == nil || w.adapters == nil || w.service == nil {
		return ErrInvalidProjectionBatch
	}
	resolved, err := w.resolver.ResolveProjectionSource(ctx, event)
	if err != nil {
		return err
	}
	if !resolved.Relevant {
		return w.service.AdvanceCursor(ctx, event.MSPID, ProjectionCursor{ConsumerKey: w.consumerKey, OccurredAt: event.OccurredAt, EventID: event.EventID})
	}
	adapter, ok := w.adapters.ForSource(resolved.Source.Type)
	if !ok {
		return fmt.Errorf("%w: no adapter for %s", ErrInvalidAdapter, resolved.Source.Type)
	}
	projections, err := adapter.Project(ctx, resolved.Source)
	missing := false
	if err != nil {
		if errors.Is(err, scope.ErrNotFound) {
			projections = nil
			missing = true
		} else {
			return err
		}
	}
	revision := event.SubjectVersion
	if missing {
		if resolver, ok := w.resolver.(projectionRevisionResolver); ok {
			resolvedRevision, resolveErr := resolver.ResolveCalendarProjectionRevision(ctx, resolved.Source, revision)
			if resolveErr == nil {
				revision = resolvedRevision
			} else if !errors.Is(resolveErr, scope.ErrNotFound) {
				return resolveErr
			}
		}
	}
	if len(projections) > 0 {
		revision = projections[0].SourceRevision
	}
	if revision < 1 {
		return ErrInvalidProjectionBatch
	}
	_, err = w.service.Apply(ctx, ProjectionBatch{Source: resolved.Source, SourceRevision: revision, Projections: projections, Cursor: ProjectionCursor{ConsumerKey: w.consumerKey, OccurredAt: event.OccurredAt, EventID: event.EventID}, TrustedSourceReload: true})
	return err
}

type ProjectionBatch struct {
	Source         SourceRef
	SourceRevision int64
	Projections    []Projection
	Cursor         ProjectionCursor
	// TrustedSourceReload means the source was reloaded through its typed
	// repository and may authoritatively move between client scopes.
	TrustedSourceReload bool
}

type CustomRoleDefinitionSource interface {
	LoadCustomRoleDefinitions(context.Context, string) (map[string]EventRoleDefinition, error)
}

type ProjectionRepository interface {
	// ApplyProjectionBatchAtomic returns false for a stale source revision.
	ApplyProjectionBatchAtomic(context.Context, ProjectionBatch) (bool, error)
}
type ProjectionCursorRepository interface {
	AdvanceProjectionCursorAtomic(context.Context, string, ProjectionCursor) error
}

type ProjectionService struct {
	repository ProjectionRepository
	roles      *RoleRegistry
}

func NewProjectionService(repository ProjectionRepository, roles *RoleRegistry) *ProjectionService {
	return &ProjectionService{repository: repository, roles: roles}
}
func (s *ProjectionService) Apply(ctx context.Context, batch ProjectionBatch) (bool, error) {
	if s == nil || s.repository == nil || s.roles == nil || batch.SourceRevision < 1 || batch.Source.Validate() != nil {
		return false, ErrInvalidProjectionBatch
	}
	roles := s.roles
	if batch.Source.Type == "custom_date" {
		if source, ok := s.repository.(CustomRoleDefinitionSource); ok {
			custom, err := source.LoadCustomRoleDefinitions(ctx, batch.Source.MSPID)
			if err != nil {
				return false, err
			}
			definitions := make([]EventRoleDefinition, 0, len(custom))
			for _, definition := range custom {
				definitions = append(definitions, definition)
			}
			roles, err = NewProductionRoleRegistry(definitions)
			if err != nil {
				return false, err
			}
		}
	}
	seen := map[string]struct{}{}
	for _, projection := range batch.Projections {
		if projection.Source != batch.Source || projection.SourceRevision != batch.SourceRevision {
			return false, fmt.Errorf("%w: mixed source or revision", ErrInvalidProjectionBatch)
		}
		if err := roles.ValidateProjection(projection); err != nil {
			return false, err
		}
		key := projection.EventRole + "\x00" + projection.SourceRoleKey
		if _, duplicate := seen[key]; duplicate {
			return false, fmt.Errorf("%w: duplicate role", ErrInvalidProjectionBatch)
		}
		seen[key] = struct{}{}
	}
	return s.repository.ApplyProjectionBatchAtomic(ctx, batch)
}

func (s *ProjectionService) AdvanceCursor(ctx context.Context, mspID string, cursor ProjectionCursor) error {
	if s == nil || s.repository == nil || mspID == "" || cursor.ConsumerKey == "" || cursor.OccurredAt.IsZero() || cursor.EventID == "" {
		return ErrInvalidProjectionBatch
	}
	repository, ok := s.repository.(ProjectionCursorRepository)
	if !ok {
		return ErrInvalidProjectionBatch
	}
	return repository.AdvanceProjectionCursorAtomic(ctx, mspID, cursor)
}
