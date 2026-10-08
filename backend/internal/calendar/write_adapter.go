package calendar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

var (
	ErrInvalidScheduleChange = errors.New("invalid calendar schedule change")
	ErrReadOnlyEventRole     = errors.New("calendar event role is read only")
	ErrWriteAdapterNotFound  = errors.New("calendar write adapter not found")
)

type OccurrenceScope string

const (
	ThisOccurrence OccurrenceScope = "this_occurrence"
	ThisAndFuture  OccurrenceScope = "this_and_future"
	EntireSeries   OccurrenceScope = "entire_series"
)

type RequestedChange struct {
	ID, ProjectionID, OccurrenceKey string
	OccurrenceScope                 OccurrenceScope
	Source                          SourceRef
	EventRole, SourceRoleKey        string
	SourceRevision                  int64
	AllDay                          bool
	StartsOn, EndsOn                *time.Time
	StartsAt, EndsAt                *time.Time
	Timezone                        string
	Recurrence                      *RecurrenceRule
	Required                        bool
}

func (r RequestedChange) interval() TypedInterval {
	return TypedInterval{AllDay: r.AllDay, StartsOn: r.StartsOn, EndsOn: r.EndsOn, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Timezone: r.Timezone}
}

func (r RequestedChange) Interval() TypedInterval { return r.interval() }

type PreparedChange struct {
	ID                       string
	Request                  RequestedChange
	Source                   SourceRef
	EventRole, SourceRoleKey string
	ExpectedSourceRevision   int64
	Mutation                 any
}

func (p PreparedChange) RequestInterval() TypedInterval { return p.Request.interval() }

type SchedulingEvidence = mutation.Evidence

// ScheduleTx is deliberately source-oriented. Implementations mutate typed
// source tables and facts; calendar projections remain outbox-maintained read models.
type ScheduleTx interface {
	RevalidateSchedulingChange(context.Context, authorization.Principal, PreparedChange) error
	ApplyTypedScheduleMutation(context.Context, PreparedChange, mutation.Evidence) error
}

type WriteAdapter interface {
	SourceType() string
	Prepare(context.Context, authorization.Principal, RequestedChange) (PreparedChange, error)
	Apply(context.Context, ScheduleTx, PreparedChange, mutation.Evidence) error
}

type WriteAdapterRegistry struct{ adapters map[string]WriteAdapter }

func NewWriteAdapterRegistry() *WriteAdapterRegistry {
	return &WriteAdapterRegistry{adapters: map[string]WriteAdapter{}}
}
func (r *WriteAdapterRegistry) Register(adapter WriteAdapter) error {
	if r == nil || adapter == nil {
		return ErrInvalidScheduleChange
	}
	typ := strings.TrimSpace(adapter.SourceType())
	if _, ok := sourceTypes[typ]; !ok || typ == "generic_event" {
		return ErrInvalidScheduleChange
	}
	if r.adapters == nil {
		r.adapters = map[string]WriteAdapter{}
	}
	if _, exists := r.adapters[typ]; exists {
		return fmt.Errorf("%w: duplicate %s", ErrInvalidScheduleChange, typ)
	}
	r.adapters[typ] = adapter
	return nil
}
func (r *WriteAdapterRegistry) ForSource(typ string) (WriteAdapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.adapters[typ]
	return a, ok
}
func (r *WriteAdapterRegistry) SourceTypes() []string {
	if r == nil {
		return nil
	}
	result := make([]string, 0, len(r.adapters))
	for typ := range r.adapters {
		result = append(result, typ)
	}
	sort.Strings(result)
	return result
}

func ApplyPreparedWithTx(ctx context.Context, tx ScheduleTx, principal authorization.Principal, prepared PreparedChange, evidence mutation.Evidence) error {
	if tx == nil {
		return ErrInvalidScheduleChange
	}
	if err := tx.RevalidateSchedulingChange(ctx, principal, prepared); err != nil {
		return err
	}
	return tx.ApplyTypedScheduleMutation(ctx, prepared, evidence)
}
