package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type WorkRecordSource struct {
	MSPID, ClientID, ID, Title, RecordType, Status, Priority, OwnerID, QueueID, TeamID, ServiceID, AssetID, SLAID, Timezone string
	Version, PlannedMinutes                                                                                                 int64
	SchedulingMode                                                                                                          calendar.SchedulingMode
	ScheduledStartsAt, ScheduledEndsAt                                                                                      *time.Time
	DueOn, FollowUpOn                                                                                                       *time.Time
	SLAResponseDueAt, SLAResolutionDueAt                                                                                    *time.Time
	Recurrence                                                                                                              *calendar.RecurrenceRule
}
type TaskSource struct {
	MSPID, ClientID, ID, Title, Status, OwnerID, ProjectID, Timezone string
	Version, PlannedMinutes                                          int64
	SchedulingMode                                                   calendar.SchedulingMode
	ScheduledStartsAt, ScheduledEndsAt, DueOn                        *time.Time
	Recurrence                                                       *calendar.RecurrenceRule
}
type WorkSource interface {
	LoadWorkRecord(context.Context, calendar.SourceRef) (WorkRecordSource, error)
	LoadTask(context.Context, calendar.SourceRef) (TaskSource, error)
}

type WorkRecordAdapter struct{ source WorkSource }

func NewWorkRecordAdapter(source WorkSource) *WorkRecordAdapter { return &WorkRecordAdapter{source} }
func (*WorkRecordAdapter) SourceType() string                   { return "work_record" }
func (a *WorkRecordAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "work_record" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, err := a.source.LoadWorkRecord(ctx, ref)
	if err != nil {
		return nil, err
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || s.ID != ref.ID || s.Version < 1 {
		return nil, fmt.Errorf("%w: mismatched work source", calendar.ErrInvalidAdapter)
	}
	tagIDs, err := resolveTags(ctx, a.source, ref)
	if err != nil {
		return nil, err
	}
	revision, err := resolveProjectionRevision(ctx, a.source, ref, s.Version)
	if err != nil {
		return nil, err
	}
	if revision < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	result := []calendar.Projection{}
	if s.ScheduledStartsAt != nil {
		p := timedProjection(ref, revision, s.Title, "scheduled_work", s.ScheduledStartsAt, s.ScheduledEndsAt, s.Timezone, s.SchedulingMode, s.Recurrence)
		p.AssigneeID = s.OwnerID
		p.PlannedMinutes = s.PlannedMinutes
		p.CapacityBearing = s.OwnerID != "" && s.PlannedMinutes > 0
		p.Dimensions = calendar.FilterDimensions{TechnicianIDs: compact(s.OwnerID), ClientIDs: compact(s.ClientID), TeamIDs: compact(s.TeamID), TechnologyIDs: compact(s.ServiceID), SLAIDs: compact(s.SLAID), TicketTypes: compact(s.RecordType), Priorities: compact(s.Priority)}
		p.Dimensions.AssetIDs = compact(s.AssetID)
		p.Dimensions.TagIDs = tagIDs
		p.TerminalState = terminalState(s.Status)
		result = append(result, p)
	}
	if s.DueOn != nil {
		result = append(result, allDayProjection(ref, revision, s.Title, "due", *s.DueOn))
	}
	if s.FollowUpOn != nil {
		result = append(result, allDayProjection(ref, revision, s.Title, "follow_up", *s.FollowUpOn))
	}
	if s.SLAResponseDueAt != nil {
		result = append(result, timedProjection(ref, revision, s.Title, "sla_response_deadline", s.SLAResponseDueAt, nil, "UTC", calendar.Informational, nil))
	}
	if s.SLAResolutionDueAt != nil {
		result = append(result, timedProjection(ref, revision, s.Title, "sla_resolution_deadline", s.SLAResolutionDueAt, nil, "UTC", calendar.Informational, nil))
	}
	for i := range result {
		result[i].TerminalState = terminalState(s.Status)
		if len(result[i].Dimensions.TechnicianIDs) == 0 {
			result[i].Dimensions.TechnicianIDs = compact(s.OwnerID)
		}
		if len(result[i].Dimensions.ClientIDs) == 0 {
			result[i].Dimensions.ClientIDs = compact(s.ClientID)
		}
		if len(result[i].Dimensions.TagIDs) == 0 {
			result[i].Dimensions.TagIDs = tagIDs
		}
		if len(result[i].Dimensions.TeamIDs) == 0 {
			result[i].Dimensions.TeamIDs = compact(s.TeamID)
		}
		if len(result[i].Dimensions.TechnologyIDs) == 0 {
			result[i].Dimensions.TechnologyIDs = compact(s.ServiceID)
		}
		if len(result[i].Dimensions.AssetIDs) == 0 {
			result[i].Dimensions.AssetIDs = compact(s.AssetID)
		}
		if len(result[i].Dimensions.TicketTypes) == 0 {
			result[i].Dimensions.TicketTypes = compact(s.RecordType)
		}
		if len(result[i].Dimensions.SLAIDs) == 0 {
			result[i].Dimensions.SLAIDs = compact(s.SLAID)
		}
		if len(result[i].Dimensions.Priorities) == 0 {
			result[i].Dimensions.Priorities = compact(s.Priority)
		}
	}
	sortProjections(result)
	return result, nil
}

type TaskAdapter struct{ source WorkSource }

func NewTaskAdapter(source WorkSource) *TaskAdapter { return &TaskAdapter{source} }
func (*TaskAdapter) SourceType() string             { return "task" }
func (a *TaskAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "task" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, err := a.source.LoadTask(ctx, ref)
	if err != nil {
		return nil, err
	}
	if s.MSPID != ref.MSPID || s.ClientID != ref.ClientID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	tagIDs, err := resolveTags(ctx, a.source, ref)
	if err != nil {
		return nil, err
	}
	revision, err := resolveProjectionRevision(ctx, a.source, ref, s.Version)
	if err != nil {
		return nil, err
	}
	if revision < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	result := []calendar.Projection{}
	if s.ScheduledStartsAt != nil {
		p := timedProjection(ref, revision, s.Title, "scheduled_work", s.ScheduledStartsAt, s.ScheduledEndsAt, s.Timezone, s.SchedulingMode, s.Recurrence)
		p.AssigneeID = s.OwnerID
		p.PlannedMinutes = s.PlannedMinutes
		p.CapacityBearing = s.OwnerID != "" && s.PlannedMinutes > 0
		p.Dimensions = calendar.FilterDimensions{TechnicianIDs: compact(s.OwnerID), ClientIDs: compact(s.ClientID), ProjectIDs: compact(s.ProjectID)}
		p.Dimensions.TagIDs = tagIDs
		p.TerminalState = terminalState(s.Status)
		result = append(result, p)
	}
	if s.DueOn != nil {
		p := allDayProjection(ref, revision, s.Title, "due", *s.DueOn)
		p.Dimensions = calendar.FilterDimensions{TechnicianIDs: compact(s.OwnerID), ClientIDs: compact(s.ClientID), ProjectIDs: compact(s.ProjectID)}
		p.Dimensions.TagIDs = tagIDs
		p.TerminalState = terminalState(s.Status)
		result = append(result, p)
	}
	sortProjections(result)
	return result, nil
}
func compact(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func (a *WorkRecordAdapter) Prepare(ctx context.Context, principal authorization.Principal, requested calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || requested.Source.Type != "work_record" || requested.EventRole != "scheduled_work" {
		return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
	}
	s, err := a.source.LoadWorkRecord(ctx, requested.Source)
	if err != nil {
		return calendar.PreparedChange{}, err
	}
	if s.ID != requested.Source.ID || s.MSPID != requested.Source.MSPID || s.ClientID != requested.Source.ClientID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	accepted := workrecords.ScheduleMutation{RecordID: s.ID, MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, Interval: calendar.TypedInterval{AllDay: requested.AllDay, StartsOn: requested.StartsOn, EndsOn: requested.EndsOn, StartsAt: requested.StartsAt, EndsAt: requested.EndsAt, Timezone: requested.Timezone}, Mode: s.SchedulingMode, PlannedMinutes: s.PlannedMinutes, Recurrence: requested.Recurrence, OccurrenceKey: requested.OccurrenceKey, OccurrenceScope: string(requested.OccurrenceScope), ProjectionID: requested.ProjectionID}
	if accepted.Mode == calendar.Informational {
		accepted.Mode = calendar.FixedBlock
	}
	if err = workrecords.ValidateScheduleMutation(principal, accepted); err != nil {
		return calendar.PreparedChange{}, err
	}
	return calendar.PreparedChange{Request: requested, Source: requested.Source, EventRole: requested.EventRole, SourceRoleKey: requested.SourceRoleKey, ExpectedSourceRevision: requested.SourceRevision, Mutation: accepted}, nil
}
func (a *WorkRecordAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, prepared calendar.PreparedChange, evidence mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: evidence.ActorID}, prepared, evidence)
}

func (a *TaskAdapter) Prepare(ctx context.Context, principal authorization.Principal, requested calendar.RequestedChange) (calendar.PreparedChange, error) {
	if a == nil || a.source == nil || requested.Source.Type != "task" {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	if requested.EventRole != "scheduled_work" {
		return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
	}
	s, err := a.source.LoadTask(ctx, requested.Source)
	if err != nil {
		return calendar.PreparedChange{}, err
	}
	if s.ID != requested.Source.ID || s.MSPID != requested.Source.MSPID || s.ClientID != requested.Source.ClientID || s.Version < 1 {
		return calendar.PreparedChange{}, calendar.ErrInvalidScheduleChange
	}
	accepted := tasks.ScheduleMutation{TaskID: s.ID, MSPID: s.MSPID, ClientID: s.ClientID, ExpectedVersion: s.Version, Interval: calendar.TypedInterval{AllDay: requested.AllDay, StartsOn: requested.StartsOn, EndsOn: requested.EndsOn, StartsAt: requested.StartsAt, EndsAt: requested.EndsAt, Timezone: requested.Timezone}, Mode: s.SchedulingMode, EstimateMinutes: s.PlannedMinutes, Recurrence: requested.Recurrence, OccurrenceKey: requested.OccurrenceKey, OccurrenceScope: string(requested.OccurrenceScope), ProjectionID: requested.ProjectionID}
	if accepted.Mode == calendar.Informational {
		accepted.Mode = calendar.FixedBlock
	}
	if err = tasks.ValidateScheduleMutation(principal, accepted); err != nil {
		return calendar.PreparedChange{}, err
	}
	return calendar.PreparedChange{Request: requested, Source: requested.Source, EventRole: requested.EventRole, SourceRoleKey: requested.SourceRoleKey, ExpectedSourceRevision: requested.SourceRevision, Mutation: accepted}, nil
}
func (a *TaskAdapter) Apply(ctx context.Context, tx calendar.ScheduleTx, prepared calendar.PreparedChange, evidence mutation.Evidence) error {
	return calendar.ApplyPreparedWithTx(ctx, tx, authorization.Principal{ID: evidence.ActorID}, prepared, evidence)
}
