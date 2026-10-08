package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCalendarPreferenceChannelEnabledHonorsExplicitDisablement(t *testing.T) {
	preference := CalendarPreference{Rules: []CalendarPreferenceRule{{
		EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: false,
	}}}
	if preference.ChannelEnabled(CalendarSchedule, CalendarRoutine, InApp) {
		t.Fatal("ChannelEnabled() = true, want false for explicitly disabled in-app rule")
	}
}

func TestCalendarPreferenceChannelEnabledDefaultsSafelyWhenRuleIsAbsent(t *testing.T) {
	preference := CalendarPreference{}
	if !preference.ChannelEnabled(CalendarSchedule, CalendarRoutine, InApp) {
		t.Fatal("ChannelEnabled() = false, want true for the safe in-app default")
	}
	if !preference.ChannelEnabled(CalendarSchedule, CalendarRoutine, Email) {
		t.Fatal("ChannelEnabled() = false, want true when email has not been explicitly disabled")
	}
}

type calendarPermissionStub struct{ allowed bool }

func (s calendarPermissionStub) CanViewCalendarSource(context.Context, string, calendar.SourceRef) (bool, error) {
	return s.allowed, nil
}

type calendarPreferenceStub struct{ preference CalendarPreference }

func (s calendarPreferenceStub) CalendarPreference(context.Context, string, string) (CalendarPreference, error) {
	return s.preference, nil
}

func TestCalendarCascadeProducesOneDigestPerRecipientAndChannel(t *testing.T) {
	planner := NewCalendarPlanner(calendarPermissionStub{allowed: true}, calendarPreferenceStub{})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{
		MSPID: "msp", CorrelationID: "cascade-1",
		Changes: []ScheduleChange{
			{AssigneeID: "tech-1", ChangeClass: CalendarSchedule, SourceTitle: "Task A", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "a"}},
			{AssigneeID: "tech-1", ChangeClass: CalendarSchedule, SourceTitle: "Task B", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "b"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].RecipientID != "tech-1" || found[1].RecipientID != "tech-1" || !strings.Contains(found[0].Body, "2 calendar changes") || !strings.Contains(found[1].Body, "2 calendar changes") {
		t.Fatalf("plans = %+v", found)
	}
}

func TestCalendarNotificationOmitsUnauthorizedSourceDetails(t *testing.T) {
	planner := NewCalendarPlanner(calendarPermissionStub{allowed: false}, calendarPreferenceStub{})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{
		MSPID: "msp", CorrelationID: "change-1",
		Changes: []ScheduleChange{{AssigneeID: "tech-1", ChangeClass: CalendarSchedule, ClientName: "Client A", SourceTitle: "VPN", Source: calendar.SourceRef{MSPID: "msp", ClientID: "secret", Type: "task", ID: "a"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("plans=%+v, want one per default channel", found)
	}
	for _, plan := range found {
		if strings.Contains(plan.Body, "Client A") || strings.Contains(plan.Body, "VPN") {
			t.Fatalf("unsafe notification: %+v", found)
		}
	}
}

func TestCalendarNotificationDisablesOnlyTheExplicitChannel(t *testing.T) {
	planner := NewCalendarPlanner(calendarPermissionStub{allowed: true}, calendarPreferenceStub{preference: CalendarPreference{Rules: []CalendarPreferenceRule{{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: false}}}})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{MSPID: "msp", CorrelationID: "change-1", Changes: []ScheduleChange{{AssigneeID: "tech-1", ChangeClass: CalendarSchedule, SourceTitle: "Task", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Channel != Email {
		t.Fatalf("plans = %+v, want default email only", found)
	}
}

func TestCalendarNotificationPlansEveryExplicitlyEnabledChannel(t *testing.T) {
	preference := CalendarPreference{Rules: []CalendarPreferenceRule{
		{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: true},
		{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: Email, Enabled: true},
		{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: Teams, Enabled: false},
	}}
	planner := NewCalendarPlanner(calendarPermissionStub{allowed: true}, calendarPreferenceStub{preference: preference})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{MSPID: "msp", CorrelationID: "change-1", Changes: []ScheduleChange{{AssigneeID: "tech-1", ChangeClass: CalendarSchedule, SourceTitle: "Task", Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].Channel != InApp || found[1].Channel != Email {
		t.Fatalf("plans = %+v, want in-app and email", found)
	}
}

func TestCalendarLegacyChangeClassIsNotPlannable(t *testing.T) {
	planner := NewCalendarPlanner(calendarPermissionStub{allowed: true}, calendarPreferenceStub{})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{
		MSPID: "msp", CorrelationID: "change-1",
		Changes: []ScheduleChange{{AssigneeID: "tech-1", ChangeClass: CalendarRescheduled, Urgency: CalendarRoutine, Source: calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "a"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("legacy change class planned deliveries: %+v", found)
	}
}

type calendarPreferenceRepositoryStub struct {
	mutation CalendarPreferenceMutation
}

func (s *calendarPreferenceRepositoryStub) GetCalendarPreference(context.Context, string, string) (CalendarPreference, error) {
	return CalendarPreference{TechnicianID: "tech-1", Version: 2}, nil
}

func (s *calendarPreferenceRepositoryStub) ReplaceCalendarPreference(_ context.Context, mutation CalendarPreferenceMutation) (CalendarPreference, error) {
	s.mutation = mutation
	return mutation.Preference, nil
}

func TestCalendarPreferenceReplaceDerivesRecipientFromPrincipal(t *testing.T) {
	repository := &calendarPreferenceRepositoryStub{}
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	ids := []string{"audit-1", "event-1", "correlation-1"}
	service := NewCalendarPreferenceService(repository, func() time.Time { return now }, func() string { id := ids[0]; ids = ids[1:]; return id })
	principal := authorization.Principal{ID: "tech-1", Scope: scope.Principal{MSPID: "msp"}}
	preference, err := service.Replace(context.Background(), ReplaceCalendarPreferenceCommand{
		Principal: principal, ExpectedVersion: 2,
		Rules: []CalendarPreferenceRule{{EventClass: CalendarScheduleChanged, ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.mutation.Preference.TechnicianID != "tech-1" || repository.mutation.ExpectedVersion != 2 || preference.Version != 3 || repository.mutation.Audit.ActorID != "tech-1" || repository.mutation.Event.EventType != "calendar.notification_preferences.replaced" {
		t.Fatalf("mutation=%+v result=%+v", repository.mutation, preference)
	}
}

func TestCalendarPreferenceRejectsUnknownValues(t *testing.T) {
	service := NewCalendarPreferenceService(&calendarPreferenceRepositoryStub{}, time.Now, func() string { return "id" })
	_, err := service.Replace(context.Background(), ReplaceCalendarPreferenceCommand{
		Principal: authorization.Principal{ID: "tech-1", Scope: scope.Principal{MSPID: "msp"}},
		Rules:     []CalendarPreferenceRule{{EventClass: "surprise", ChangeClass: CalendarSchedule, Urgency: CalendarRoutine, Channel: InApp, Enabled: true}},
	})
	if err == nil {
		t.Fatal("expected invalid preference error")
	}
}
