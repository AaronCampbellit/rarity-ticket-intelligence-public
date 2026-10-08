package adapters

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type WorkforceSource interface {
	LoadSchedule(context.Context, calendar.SourceRef) (workforce.Schedule, error)
	LoadPTO(context.Context, calendar.SourceRef) (workforce.PTORequest, error)
}

func (a *ScheduleAdapter) Prepare(context.Context, authorization.Principal, calendar.RequestedChange) (calendar.PreparedChange, error) {
	return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
}
func (a *ScheduleAdapter) Apply(context.Context, calendar.ScheduleTx, calendar.PreparedChange, mutation.Evidence) error {
	return calendar.ErrReadOnlyEventRole
}
func (a *PTOAdapter) Prepare(context.Context, authorization.Principal, calendar.RequestedChange) (calendar.PreparedChange, error) {
	return calendar.PreparedChange{}, calendar.ErrReadOnlyEventRole
}
func (a *PTOAdapter) Apply(context.Context, calendar.ScheduleTx, calendar.PreparedChange, mutation.Evidence) error {
	return calendar.ErrReadOnlyEventRole
}

type ScheduleAdapter struct{ source WorkforceSource }

func NewScheduleAdapter(s WorkforceSource) *ScheduleAdapter { return &ScheduleAdapter{s} }
func (*ScheduleAdapter) SourceType() string                 { return "technician_schedule" }
func (a *ScheduleAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "technician_schedule" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadSchedule(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	location, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, calendar.ErrInvalidAdapter
	}
	result := make([]calendar.Projection, 0, len(s.Windows))
	for _, window := range s.Windows {
		anchor := firstWeekday(s.EffectiveFrom, window.Weekday)
		startLocal := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), window.StartsMinute/60, window.StartsMinute%60, 0, 0, location)
		endDate, endMinute := anchor, window.EndsMinute
		if endMinute == 1440 {
			endDate = endDate.AddDate(0, 0, 1)
			endMinute = 0
		}
		endLocal := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), endMinute/60, endMinute%60, 0, 0, location)
		start, end := startLocal.UTC(), endLocal.UTC()
		rule := &calendar.RecurrenceRule{Frequency: calendar.Weekly, Interval: 1, Weekdays: []time.Weekday{window.Weekday}}
		if s.EffectiveThrough != nil {
			through := dateOnly(*s.EffectiveThrough)
			until := time.Date(through.Year(), through.Month(), through.Day(), 23, 59, 59, 0, location).UTC()
			rule.Until = &until
		}
		p := timedProjection(ref, s.Version, "Availability", "availability", &start, &end, s.Timezone, calendar.Informational, rule)
		p.ID = projectionID(ref, "availability", window.ID)
		p.SourceRoleKey = window.ID
		p.OwnerID = s.TechnicianID
		p.Dimensions = calendar.FilterDimensions{TechnicianIDs: compact(s.TechnicianID)}
		result = append(result, p)
	}
	sortProjections(result)
	return result, nil
}

func firstWeekday(from time.Time, weekday time.Weekday) time.Time {
	value := dateOnly(from)
	return value.AddDate(0, 0, (int(weekday)-int(value.Weekday())+7)%7)
}

type PTOAdapter struct{ source WorkforceSource }

func NewPTOAdapter(s WorkforceSource) *PTOAdapter { return &PTOAdapter{s} }
func (*PTOAdapter) SourceType() string            { return "pto" }
func (a *PTOAdapter) Project(ctx context.Context, ref calendar.SourceRef) ([]calendar.Projection, error) {
	if a == nil || a.source == nil || ref.Type != "pto" {
		return nil, calendar.ErrInvalidAdapter
	}
	s, e := a.source.LoadPTO(ctx, ref)
	if e != nil {
		return nil, e
	}
	if s.MSPID != ref.MSPID || s.ID != ref.ID || s.Version < 1 {
		return nil, calendar.ErrInvalidAdapter
	}
	var p calendar.Projection
	if s.AllDay {
		p = allDayProjection(ref, s.Version, "Unavailable", "unavailability", *s.StartsOn)
		p.EndsOn = s.EndsOn
	} else {
		p = timedProjection(ref, s.Version, "Unavailable", "unavailability", s.StartsAt, s.EndsAt, s.Timezone, calendar.Informational, nil)
	}
	p.OwnerID = s.TechnicianID
	p.Dimensions = calendar.FilterDimensions{TechnicianIDs: compact(s.TechnicianID)}
	p.TerminalState = terminalState(string(s.State))
	return []calendar.Projection{p}, nil
}
