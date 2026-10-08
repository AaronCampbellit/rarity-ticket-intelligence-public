package sla

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type calendarRepository struct {
	current  PublishedCalendar
	findErr  error
	accepted CalendarPublishMutation
	calls    int
}

func (r *calendarRepository) FindCalendar(
	context.Context,
	scope.Target,
	string,
) (PublishedCalendar, error) {
	return r.current, r.findErr
}

func (r *calendarRepository) ListCalendars(
	_ context.Context,
	_ scope.Target,
) ([]PublishedCalendar, error) {
	return []PublishedCalendar{r.current}, nil
}

func (r *calendarRepository) PublishCalendarAtomic(
	_ context.Context,
	accepted CalendarPublishMutation,
) error {
	r.calls++
	r.accepted = accepted
	return nil
}

func businessCalendarDefinition() CalendarDefinition {
	return CalendarDefinition{
		Timezone: "America/Chicago",
		Weekly: map[string][]Window{
			"monday":  {{StartMinute: 9 * 60, EndMinute: 17 * 60}},
			"tuesday": {{StartMinute: 9 * 60, EndMinute: 17 * 60}},
		},
		Holidays: []string{"2026-12-25"},
	}
}

func TestPublishCalendarCreatesImmutableValidatedVersion(t *testing.T) {
	repository := &calendarRepository{}
	at := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	ids := []string{"calendar", "audit", "event", "correlation"}
	service := NewCalendarManagementService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	published, err := service.PublishCalendar(context.Background(), PublishCalendarCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("sla.manage"),
		},
		Key: "support-hours", Name: "Support Hours",
		Definition: businessCalendarDefinition(),
		ActorID:    "actor", Source: "api",
	})
	if err != nil {
		t.Fatalf("PublishCalendar() error = %v", err)
	}
	if published.ID != "calendar" || published.Version != 1 ||
		repository.calls != 1 ||
		repository.accepted.Audit.Action != "business_calendar.published" ||
		repository.accepted.Event.EventType != "business_calendar.published" {
		t.Fatalf("unexpected calendar publication: published=%+v repository=%+v", published, repository)
	}
	if _, err := published.Definition.Calendar(); err != nil {
		t.Fatalf("published definition does not produce a calendar: %v", err)
	}
}

func TestPublishCalendarRejectsStaleAndOverlappingWindows(t *testing.T) {
	repository := &calendarRepository{current: PublishedCalendar{
		ID: "calendar", MSPID: "msp", ClientID: "client", Version: 3,
	}}
	service := NewCalendarManagementService(repository, time.Now, func() string { return "unused" })
	command := PublishCalendarCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("sla.manage"),
		},
		CalendarID: "calendar", ExpectedVersion: 2,
		Key: "support-hours", Name: "Support Hours",
		Definition: businessCalendarDefinition(),
		ActorID:    "actor", Source: "api",
	}
	if _, err := service.PublishCalendar(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale PublishCalendar() error = %v", err)
	}
	command.ExpectedVersion = 3
	command.Definition.Weekly["monday"] = []Window{
		{StartMinute: 9 * 60, EndMinute: 12 * 60},
		{StartMinute: 11 * 60, EndMinute: 17 * 60},
	}
	if _, err := service.PublishCalendar(context.Background(), command); !errors.Is(err, ErrInvalidCalendar) {
		t.Fatalf("overlapping PublishCalendar() error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("rejected calendar reached repository")
	}
}
