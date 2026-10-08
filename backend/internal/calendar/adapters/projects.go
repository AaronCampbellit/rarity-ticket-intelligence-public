package adapters

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
)

type ProjectSource interface {
	LoadProject(context.Context, calendar.SourceRef) (projects.Project, error)
	LoadPhase(context.Context, calendar.SourceRef) (projects.Phase, error)
	LoadMilestone(context.Context, calendar.SourceRef) (projects.Milestone, error)
	LoadResourcePlan(context.Context, calendar.SourceRef) (projects.ResourcePlan, error)
}

type ResourcePlanAdapter struct{ source ProjectSource }

func NewResourcePlanAdapter(s ProjectSource) *ResourcePlanAdapter { return &ResourcePlanAdapter{s} }
func (*ResourcePlanAdapter) SourceType() string                   { return "resource_plan" }
func (a *ResourcePlanAdapter) Prepare(ctx context.Context, principal authorization.Principal, r calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || r.Source.Type != "resource_plan" || r.EventRole != "allocation" || !r.AllDay {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	s, err := a.source.LoadResourcePlan(ctx, r.Source)
	if err != nil {
		return calendar.PreparedChange{}, err
	}
	if s.ID != r.Source.ID || s.MSPID != r.Source.MSPID || s.ClientID != r.Source.ClientID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	mutation := projects.ScheduleMutation{SourceType: "resource_plan", SourceID: s.ID, MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, EventRole: r.EventRole, Interval: r.Interval(), ProjectionID: r.ProjectionID}
	if err = projects.ValidateScheduleMutation(principal, mutation); err != nil {
		return calendar.PreparedChange{}, err
	}
	return calendar.PreparedChange{Request: r, Source: r.Source, EventRole: r.EventRole, ExpectedSourceRevision: r.SourceRevision, Mutation: mutation}, nil
}
func (a *ResourcePlanAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, p calendar.PreparedChange, e mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: e.ActorID}, p, e)
}
func (a *ResourcePlanAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "resource_plan" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, err := a.source.LoadResourcePlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	if s.ID != ref.ID || s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || s.Version < 1 || s.StartsOn.IsZero() || s.EndsOn.Before(s.StartsOn) {
		return nil, calendar.ErrInvalidAdapter
	}
	p := allDayProjection(ref, s.Version, "Resource plan", "allocation", s.StartsOn)
	endsOn := s.EndsOn
	p.EndsOn = &endsOn
	p.SchedulingMode = calendar.EffortAllocation
	p.PlannedMinutes = s.PlannedMinutes
	p.Dimensions = calendar.FilterDimensions{ClientIDs: compact(s.ClientID), ProjectIDs: compact(string(s.ProjectID)), TeamIDs: compact(s.TeamID)}
	return []calendar.Projection{p}, nil
}

func (a *ProjectAdapter) Prepare(ctx context.Context, principal authorization.Principal, r calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || r.Source.Type != "project" || (r.EventRole != "planned_start" && r.EventRole != "planned_end") {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	s, e := a.source.LoadProject(ctx, r.Source)
	if e != nil {
		return calendar.PreparedChange{}, e
	}
	if string(s.ID) != r.Source.ID || s.MSPID != r.Source.MSPID || s.ClientID != r.Source.ClientID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	m := projects.ScheduleMutation{SourceType: "project", SourceID: string(s.ID), MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, EventRole: r.EventRole, Interval: calendar.TypedInterval{AllDay: r.AllDay, StartsOn: r.StartsOn, EndsOn: r.EndsOn, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Timezone: r.Timezone}, ProjectionID: r.ProjectionID}
	if e = projects.ValidateScheduleMutation(principal, m); e != nil {
		return calendar.PreparedChange{}, e
	}
	return calendar.PreparedChange{Request: r, Source: r.Source, EventRole: r.EventRole, ExpectedSourceRevision: r.SourceRevision, Mutation: m}, nil
}
func (a *ProjectAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, p calendar.PreparedChange, e mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: e.ActorID}, p, e)
}
func (a *PhaseAdapter) Prepare(ctx context.Context, principal authorization.Principal, r calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || r.Source.Type != "phase" || (r.EventRole != "planned_start" && r.EventRole != "planned_end") {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	s, e := a.source.LoadPhase(ctx, r.Source)
	if e != nil {
		return calendar.PreparedChange{}, e
	}
	if string(s.ID) != r.Source.ID || s.MSPID != r.Source.MSPID || s.ClientID != r.Source.ClientID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	m := projects.ScheduleMutation{SourceType: "phase", SourceID: string(s.ID), MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, EventRole: r.EventRole, Interval: calendar.TypedInterval{AllDay: r.AllDay, StartsOn: r.StartsOn, EndsOn: r.EndsOn, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Timezone: r.Timezone}, ProjectionID: r.ProjectionID}
	if e = projects.ValidateScheduleMutation(principal, m); e != nil {
		return calendar.PreparedChange{}, e
	}
	return calendar.PreparedChange{Request: r, Source: r.Source, EventRole: r.EventRole, ExpectedSourceRevision: r.SourceRevision, Mutation: m}, nil
}
func (a *PhaseAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, p calendar.PreparedChange, e mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: e.ActorID}, p, e)
}
func (a *MilestoneAdapter) Prepare(ctx context.Context, principal authorization.Principal, r calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || r.Source.Type != "milestone" || r.EventRole != "milestone" {
		return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
	}
	s, e := a.source.LoadMilestone(ctx, r.Source)
	if e != nil {
		return calendar.PreparedChange{}, e
	}
	if s.ID != r.Source.ID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	m := projects.ScheduleMutation{SourceType: "milestone", SourceID: s.ID, MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, EventRole: r.EventRole, Interval: calendar.TypedInterval{AllDay: r.AllDay, StartsOn: r.StartsOn, EndsOn: r.EndsOn, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Timezone: r.Timezone}, Recurrence: r.Recurrence, OccurrenceKey: r.OccurrenceKey, OccurrenceScope: string(r.OccurrenceScope), ProjectionID: r.ProjectionID}
	if e = projects.ValidateScheduleMutation(principal, m); e != nil {
		return calendar.PreparedChange{}, e
	}
	return calendar.PreparedChange{Request: r, Source: r.Source, EventRole: r.EventRole, ExpectedSourceRevision: r.SourceRevision, Mutation: m}, nil
}
func (a *MilestoneAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, p calendar.PreparedChange, e mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: e.ActorID}, p, e)
}

type ProjectAdapter struct{ source ProjectSource }

func NewProjectAdapter(s ProjectSource) *ProjectAdapter { return &ProjectAdapter{s} }
func (*ProjectAdapter) SourceType() string              { return "project" }
func (a *ProjectAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "project" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadProject(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || string(s.ID) != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	tags, e := resolveTags(ctx, a.source, ref)
	if e != nil {
		return nil, e
	}
	revision, e := resolveProjectionRevision(ctx, a.source, ref, s.Version)
	if e != nil {
		return nil, e
	}
	if revision < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	r := []calendar.Projection{}
	if !s.PlannedStart.IsZero() {
		r = append(r, allDayProjection(ref, revision, s.Name, "planned_start", s.PlannedStart))
	}
	if !s.PlannedEnd.IsZero() {
		r = append(r, allDayProjection(ref, revision, s.Name, "planned_end", s.PlannedEnd))
	}
	for i := range r {
		r[i].Dimensions = calendar.FilterDimensions{ClientIDs: compact(s.ClientID), ProjectIDs: compact(ref.ID)}
		r[i].Dimensions.TagIDs = tags
		r[i].TerminalState = terminalState(s.LifecycleState)
	}
	sortProjections(r)
	return r, nil
}

type PhaseAdapter struct{ source ProjectSource }

func NewPhaseAdapter(s ProjectSource) *PhaseAdapter { return &PhaseAdapter{s} }
func (*PhaseAdapter) SourceType() string            { return "phase" }
func (a *PhaseAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "phase" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadPhase(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || string(s.ID) != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	r := []calendar.Projection{}
	if !s.PlannedStart.IsZero() {
		r = append(r, allDayProjection(ref, s.Version, s.Name, "planned_start", s.PlannedStart))
	}
	if !s.PlannedEnd.IsZero() {
		r = append(r, allDayProjection(ref, s.Version, s.Name, "planned_end", s.PlannedEnd))
	}
	for i := range r {
		r[i].OwnerID = s.OwnerID
		r[i].Dimensions = calendar.FilterDimensions{ClientIDs: compact(s.ClientID), ProjectIDs: compact(string(s.ProjectID)), TeamIDs: append([]string(nil), s.ParticipatingTeams...)}
		r[i].TerminalState = terminalState(s.State)
	}
	sortProjections(r)
	return r, nil
}

type MilestoneAdapter struct{ source ProjectSource }

func NewMilestoneAdapter(s ProjectSource) *MilestoneAdapter { return &MilestoneAdapter{s} }
func (*MilestoneAdapter) SourceType() string                { return "milestone" }
func (a *MilestoneAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "milestone" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadMilestone(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	var p calendar.Projection
	if s.StartsOn != nil || s.StartsAt != nil {
		if s.AllDay {
			p = allDayProjection(ref, s.Version, s.Name, "milestone", *s.StartsOn)
			p.EndsOn = s.EndsOn
		} else {
			p = timedProjection(ref, s.Version, s.Name, "milestone", s.StartsAt, s.EndsAt, s.Timezone, calendar.Informational, s.Recurrence)
		}
	} else {
		p = allDayProjection(ref, s.Version, s.Name, "milestone", s.DueOn)
	}
	p.OwnerID = s.OwnerID
	p.Recurrence = s.Recurrence
	p.Dimensions = calendar.FilterDimensions{ClientIDs: compact(s.ClientID), ProjectIDs: compact(string(s.ProjectID)), Priorities: compact(s.Priority)}
	p.TerminalState = terminalState(s.Status)
	return []calendar.Projection{p}, nil
}
