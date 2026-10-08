package adapters

import (
	"context"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
)

type CommitmentSource interface {
	LoadMaintenance(context.Context, calendar.SourceRef) (commitments.MaintenanceWindow, error)
	LoadCommercial(context.Context, calendar.SourceRef) (commitments.CommercialCommitment, error)
}

func (a *MaintenanceAdapter) Prepare(context.Context, authorization.Principal, calendar.RequestedChange) (calendar.PreparedChange, error) {
	return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
}
func (a *MaintenanceAdapter) Apply(context.Context, calendar.ScheduleTx, calendar.PreparedChange, mutation.Evidence) error {
	return calendar.ErrReadOnlyEventRole
}
func (a *CommercialAdapter) Prepare(context.Context, authorization.Principal, calendar.RequestedChange) (calendar.PreparedChange, error) {
	return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
}
func (a *CommercialAdapter) Apply(context.Context, calendar.ScheduleTx, calendar.PreparedChange, mutation.Evidence) error {
	return calendar.ErrReadOnlyEventRole
}

type MaintenanceAdapter struct{ source CommitmentSource }

func NewMaintenanceAdapter(s CommitmentSource) *MaintenanceAdapter { return &MaintenanceAdapter{s} }
func (*MaintenanceAdapter) SourceType() string                     { return "maintenance_window" }
func (a *MaintenanceAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "maintenance_window" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadMaintenance(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	var p calendar.Projection
	if s.AllDay {
		p = allDayProjection(ref, s.Version, s.Title, "maintenance", *s.StartsOn)
		p.EndsOn = s.EndsOn
	} else {
		p = timedProjection(ref, s.Version, s.Title, "maintenance", &s.StartsAt, &s.EndsAt, s.Timezone, calendar.Informational, s.Recurrence)
	}
	p.OwnerID = s.OwnerID
	p.Recurrence = s.Recurrence
	p.TerminalState = terminalState(s.Status)
	for _, v := range s.Scopes {
		p.Dimensions.ClientIDs = append(p.Dimensions.ClientIDs, v.ClientID)
		if v.Type == commitments.ScopeService {
			p.Dimensions.TechnologyIDs = append(p.Dimensions.TechnologyIDs, v.ResourceID)
		}
	}
	return []calendar.Projection{p}, nil
}

type CommercialAdapter struct{ source CommitmentSource }

func NewCommercialAdapter(s CommitmentSource) *CommercialAdapter { return &CommercialAdapter{s} }
func (*CommercialAdapter) SourceType() string                    { return "commercial_commitment" }
func (a *CommercialAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "commercial_commitment" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadCommercial(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	r := []calendar.Projection{allDayProjection(ref, s.Version, s.Title, "effective", s.EffectiveOn), allDayProjection(ref, s.Version, s.Title, "expiration", s.ExpirationOn)}
	if !s.NoticeOn.IsZero() {
		r = append(r, allDayProjection(ref, s.Version, s.Title, "notice", s.NoticeOn))
	}
	if !s.RenewalOn.IsZero() {
		r = append(r, allDayProjection(ref, s.Version, s.Title, "renewal", s.RenewalOn))
	}
	for i := range r {
		r[i].OwnerID = s.OwnerID
		r[i].Recurrence = s.Recurrence
		r[i].Dimensions = calendar.FilterDimensions{ClientIDs: compact(s.ClientID), TechnologyIDs: compact(s.ServiceID)}
		r[i].TerminalState = terminalState(s.Status)
	}
	sortProjections(r)
	return r, nil
}
