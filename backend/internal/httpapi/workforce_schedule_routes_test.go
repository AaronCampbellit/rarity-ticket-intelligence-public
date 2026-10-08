package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type scheduleActionsStub struct {
	command workforce.PublishScheduleCommand
}

type scheduleExceptionActionsStub struct {
	command workforce.AddScheduleExceptionCommand
	err     error
}

func (s *scheduleExceptionActionsStub) AddException(_ context.Context, command workforce.AddScheduleExceptionCommand) (workforce.Schedule, error) {
	s.command = command
	return workforce.Schedule{ID: command.ScheduleID, Version: command.ExpectedVersion, Exceptions: []workforce.ScheduleException{command.Exception}}, s.err
}

func (s *scheduleActionsStub) Publish(_ context.Context, command workforce.PublishScheduleCommand) (workforce.Schedule, error) {
	s.command = command
	return workforce.Schedule{ID: "schedule", Version: command.ExpectedVersion + 1}, nil
}

type ptoActionsStub struct {
	decide  workforce.DecidePTOCommand
	cancel  workforce.CancelPTOCommand
	request workforce.RequestPTOCommand
}

func (s *ptoActionsStub) Request(_ context.Context, command workforce.RequestPTOCommand) (workforce.PTORequest, error) {
	s.request = command
	return workforce.PTORequest{ID: "pto", Version: 1}, nil
}
func (s *ptoActionsStub) Decide(_ context.Context, command workforce.DecidePTOCommand) (workforce.PTORequest, error) {
	s.decide = command
	return workforce.PTORequest{ID: command.RequestID, Version: command.ExpectedVersion + 1}, nil
}
func (s *ptoActionsStub) Cancel(_ context.Context, command workforce.CancelPTOCommand) (workforce.PTORequest, error) {
	s.cancel = command
	return workforce.PTORequest{ID: command.RequestID, Version: command.ExpectedVersion + 1}, nil
}

func workforceRouter(schedules WorkforceScheduleActions, pto WorkforcePTOActions, principal authorization.Principal) http.Handler {
	return NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, WorkforceSchedules: schedules, WorkforcePTO: pto})
}

func TestPTODecisionRejectsUnknownFields(t *testing.T) {
	principal := authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	response := performCalendarJSON(workforceRouter(nil, &ptoActionsStub{}, principal), http.MethodPost, "/api/v1/workforce/pto/pto-1/decision", `{"decision":"approved","expected_version":1,"idempotency_key":"decide","surprise":true}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_pto_decision"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPTODecisionDerivesActorAndRequiresVersion(t *testing.T) {
	actions := &ptoActionsStub{}
	principal := authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	response := performCalendarJSON(workforceRouter(nil, actions, principal), http.MethodPost, "/api/v1/workforce/pto/pto-1/decision", `{"decision":"approved","expected_version":3,"reason":"approved","idempotency_key":"decide"}`)
	if response.Code != http.StatusOK || actions.decide.ActorID != "manager" || actions.decide.ExpectedVersion != 3 || actions.decide.Source != "http" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.decide, response.Body.String())
	}
}

func TestSchedulePublicationAcceptsISODateAndRejectsTimestampDate(t *testing.T) {
	actions := &scheduleActionsStub{}
	principal := authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	body := `{"technician_id":"tech","timezone":"America/New_York","effective_from":"2026-08-10","expected_version":0,"idempotency_key":"publish","windows":[{"weekday":1,"starts_minute":540,"ends_minute":1020,"capacity_percent":100}]}`
	response := performCalendarJSON(workforceRouter(actions, nil, principal), http.MethodPost, "/api/v1/workforce/schedules", body)
	if response.Code != http.StatusCreated || actions.command.ActorID != "manager" || !actions.command.EffectiveFrom.Equal(timeDate(2026, 8, 10)) {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, actions.command, response.Body.String())
	}
	body = strings.Replace(body, `"2026-08-10"`, `"2026-08-10T00:00:00Z"`, 1)
	response = performCalendarJSON(workforceRouter(actions, nil, principal), http.MethodPost, "/api/v1/workforce/schedules", body)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_workforce_schedule"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestScheduleExceptionUsesTrustedPrincipalAndExpectedVersion(t *testing.T) {
	actions := &scheduleExceptionActionsStub{}
	principal := authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, WorkforceScheduleExceptions: actions})
	response := performCalendarJSON(router, http.MethodPost, "/api/v1/workforce/schedules/schedule-1/exceptions", `{"exception_on":"2026-08-12","state":"unavailable","all_day":true,"reason":"training","expected_version":3,"idempotency_key":"add-exception"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	command := actions.command
	if command.Principal.ID != "manager" || command.ActorID != "manager" || command.Source != "http" || command.ScheduleID != "schedule-1" || command.ExpectedVersion != 3 || !command.Exception.ExceptionOn.Equal(timeDate(2026, 8, 12)) {
		t.Fatalf("command=%+v", command)
	}
}

func TestScheduleExceptionRejectsUnknownFields(t *testing.T) {
	actions := &scheduleExceptionActionsStub{}
	principal := authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp"}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	router := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, WorkforceScheduleExceptions: actions})
	response := performCalendarJSON(router, http.MethodPost, "/api/v1/workforce/schedules/schedule-1/exceptions", `{"exception_on":"2026-08-12","state":"unavailable","all_day":true,"expected_version":3,"idempotency_key":"add-exception","surprise":true}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_workforce_schedule_exception"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func timeDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
