package sla

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type CalendarDefinition struct {
	Timezone string              `json:"timezone"`
	Weekly   map[string][]Window `json:"weekly"`
	Holidays []string            `json:"holidays,omitempty"`
}

func (d CalendarDefinition) Calendar() (Calendar, error) {
	location, err := time.LoadLocation(strings.TrimSpace(d.Timezone))
	if err != nil || len(d.Weekly) == 0 {
		return Calendar{}, ErrInvalidCalendar
	}
	weekly := make(map[time.Weekday][]Window, len(d.Weekly))
	windowCount := 0
	for dayName, configured := range d.Weekly {
		day, ok := parseWeekday(dayName)
		if !ok || len(configured) == 0 {
			return Calendar{}, ErrInvalidCalendar
		}
		windows := append([]Window(nil), configured...)
		sort.Slice(windows, func(i, j int) bool {
			return windows[i].StartMinute < windows[j].StartMinute
		})
		for index, window := range windows {
			if window.StartMinute < 0 || window.EndMinute > 24*60 ||
				window.EndMinute <= window.StartMinute ||
				index > 0 && windows[index-1].EndMinute > window.StartMinute {
				return Calendar{}, ErrInvalidCalendar
			}
			windowCount++
		}
		weekly[day] = windows
	}
	if windowCount == 0 {
		return Calendar{}, ErrInvalidCalendar
	}
	holidays := make(map[string]struct{}, len(d.Holidays))
	for _, date := range d.Holidays {
		parsed, err := time.ParseInLocation("2006-01-02", date, location)
		if err != nil || parsed.Format("2006-01-02") != date {
			return Calendar{}, ErrInvalidCalendar
		}
		if _, duplicate := holidays[date]; duplicate {
			return Calendar{}, ErrInvalidCalendar
		}
		holidays[date] = struct{}{}
	}
	return Calendar{Location: location, Weekly: weekly, Holidays: holidays}, nil
}

func parseWeekday(value string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sunday":
		return time.Sunday, true
	case "monday":
		return time.Monday, true
	case "tuesday":
		return time.Tuesday, true
	case "wednesday":
		return time.Wednesday, true
	case "thursday":
		return time.Thursday, true
	case "friday":
		return time.Friday, true
	case "saturday":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

type PublishedCalendar struct {
	ID          string             `json:"id"`
	MSPID       string             `json:"msp_id"`
	ClientID    string             `json:"client_id,omitempty"`
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Version     int64              `json:"version"`
	Definition  CalendarDefinition `json:"definition"`
	PublishedAt time.Time          `json:"published_at"`
	PublishedBy string             `json:"published_by"`
}

type PublishCalendarCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	CalendarID      string
	ExpectedVersion int64
	Key             string
	Name            string
	Definition      CalendarDefinition
	ActorID         string
	Source          string
}

type CalendarPublishMutation struct {
	Calendar        PublishedCalendar
	ExpectedVersion int64
	Created         bool
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type CalendarManagementRepository interface {
	FindCalendar(context.Context, scope.Target, string) (PublishedCalendar, error)
	ListCalendars(context.Context, scope.Target) ([]PublishedCalendar, error)
	PublishCalendarAtomic(context.Context, CalendarPublishMutation) error
}

type CalendarManagementService struct {
	repository CalendarManagementRepository
	now        func() time.Time
	newID      func() string
}

func NewCalendarManagementService(
	repository CalendarManagementRepository,
	now func() time.Time,
	newID func() string,
) *CalendarManagementService {
	return &CalendarManagementService{repository: repository, now: now, newID: newID}
}

func (s *CalendarManagementService) ListCalendars(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
) ([]PublishedCalendar, error) {
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if err := authorization.Authorize(principal, "sla.manage", target); err != nil {
		return nil, err
	}
	return s.repository.ListCalendars(ctx, target)
}

func (s *CalendarManagementService) PublishCalendar(
	ctx context.Context,
	command PublishCalendarCommand,
) (PublishedCalendar, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.MSPID == "" || command.ExpectedVersion < 0 ||
		strings.TrimSpace(command.Key) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		command.ActorID == "" || command.Source == "" {
		return PublishedCalendar{}, ErrInvalidCalendar
	}
	if _, err := command.Definition.Calendar(); err != nil {
		return PublishedCalendar{}, err
	}
	if err := authorization.Authorize(command.Principal, "sla.manage", target); err != nil {
		return PublishedCalendar{}, err
	}
	created := command.ExpectedVersion == 0
	calendarID := command.CalendarID
	version := int64(1)
	if created {
		if calendarID != "" {
			return PublishedCalendar{}, ErrInvalidCalendar
		}
		calendarID = s.newID()
	} else {
		current, err := s.repository.FindCalendar(ctx, target, calendarID)
		if err != nil {
			return PublishedCalendar{}, err
		}
		if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
			return PublishedCalendar{}, err
		}
		version = current.Version + 1
	}
	now := s.now().UTC()
	published := PublishedCalendar{
		ID: calendarID, MSPID: target.MSPID, ClientID: target.ClientID,
		Key: strings.TrimSpace(command.Key), Name: strings.TrimSpace(command.Name),
		Version: version, Definition: command.Definition,
		PublishedAt: now, PublishedBy: command.ActorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := CalendarPublishMutation{
		Calendar: published, ExpectedVersion: command.ExpectedVersion, Created: created,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "business_calendar.published", SubjectType: "business_calendar",
			SubjectID: published.ID, SubjectVersion: published.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "business_calendar.published", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "business_calendar", SubjectID: published.ID,
			SubjectVersion: published.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.PublishCalendarAtomic(ctx, accepted); err != nil {
		return PublishedCalendar{}, err
	}
	return published, nil
}
