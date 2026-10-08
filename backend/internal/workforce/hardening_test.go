package workforce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

func TestPTODecisionRejectsActorImpersonation(t *testing.T) {
	r := &ptoRepositoryStub{managerID: workforceManagerID, request: PTORequest{ID: workforcePTOID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, PTOType: "vacation", State: Requested, Version: 1}}
	s := NewPTOService(r, time.Now, wfIDs())
	_, err := s.Decide(context.Background(), DecidePTOCommand{Principal: authorization.Principal{ID: workforceManagerID, Scope: workforcePrincipal("calendar.schedule").Scope}, RequestID: workforcePTOID, Decision: Approved, ActorID: workforceActorID, ExpectedVersion: 1, Source: "api", IdempotencyKey: "decide-1"})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
}

func TestScheduleAllowsEndOfDayAndDatedException(t *testing.T) {
	r := &scheduleRepositoryStub{}
	s := NewScheduleService(r, time.Now, wfIDs())
	found, err := s.Publish(context.Background(), PublishScheduleCommand{Principal: workforcePrincipal("calendar.workforce.manage"), TechnicianID: workforceTechID, Timezone: "UTC", EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Windows: []WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1440}}, Exceptions: []ScheduleException{{ExceptionOn: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), State: Unavailable, AllDay: true, Reason: "holiday"}}, ActorID: workforceActorID, Source: "api", IdempotencyKey: "schedule-1"})
	if err != nil || len(found.Exceptions) != 1 {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}

func TestScheduleRejectsNonIncreasingEffectiveDate(t *testing.T) {
	r := &scheduleRepositoryStub{current: 2, currentEffective: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)}
	s := NewScheduleService(r, time.Now, wfIDs())
	_, err := s.Publish(context.Background(), PublishScheduleCommand{Principal: workforcePrincipal("calendar.workforce.manage"), TechnicianID: workforceTechID, Timezone: "UTC", EffectiveFrom: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), ExpectedVersion: 2, Windows: []WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 600}}, ActorID: workforceActorID, Source: "api", IdempotencyKey: "schedule-2"})
	if !errors.Is(err, ErrScheduleEffectiveOverlap) {
		t.Fatalf("error=%v", err)
	}
}

func TestScheduleRejectsExceptionOutsideEffectiveRange(t *testing.T) {
	r := &scheduleRepositoryStub{}
	s := NewScheduleService(r, time.Now, wfIDs())
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 5)
	principal := workforcePrincipal("calendar.workforce.manage")
	for _, exceptionOn := range []time.Time{start.AddDate(0, 0, -1), end.AddDate(0, 0, 1)} {
		_, err := s.Publish(context.Background(), PublishScheduleCommand{Principal: principal, TechnicianID: "00000000-0000-4000-8000-000000000021", Timezone: "UTC", EffectiveFrom: start, EffectiveThrough: &end, Windows: []WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 600}}, Exceptions: []ScheduleException{{ExceptionOn: exceptionOn, State: Unavailable, AllDay: true}}, ActorID: principal.ID, Source: "api", IdempotencyKey: "exception-bounds-" + exceptionOn.Format("02")})
		if !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("exception=%s error=%v", exceptionOn, err)
		}
	}
}
