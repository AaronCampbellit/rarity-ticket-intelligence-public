package workforce

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	workforceActorID     = "00000000-0000-4000-8000-000000000201"
	workforceMSPID       = "00000000-0000-4000-8000-000000000202"
	workforceTechID      = "00000000-0000-4000-8000-000000000203"
	workforceManagerID   = "00000000-0000-4000-8000-000000000204"
	workforceRequesterID = "00000000-0000-4000-8000-000000000205"
	workforcePTOID       = "00000000-0000-4000-8000-000000000206"
)

type scheduleRepositoryStub struct {
	current           int64
	currentEffective  time.Time
	mutation          ScheduleMutation
	exceptionMutation ScheduleExceptionMutation
	schedule          Schedule
}

func (r *scheduleRepositoryStub) TechnicianInMSP(context.Context, string, string) (bool, error) {
	return true, nil
}
func (r *scheduleRepositoryStub) CurrentScheduleVersion(context.Context, string, string) (int64, error) {
	return r.current, nil
}
func (r *scheduleRepositoryStub) CurrentSchedule(context.Context, string, string) (Schedule, error) {
	return Schedule{Version: r.current, EffectiveFrom: r.currentEffective}, nil
}
func (r *scheduleRepositoryStub) PublishScheduleAtomic(_ context.Context, m ScheduleMutation) error {
	r.mutation = m
	if r.current != m.ExpectedVersion {
		return ErrWorkforceVersionConflict
	}
	return nil
}

func (r *scheduleRepositoryStub) FindSchedule(context.Context, string, string) (Schedule, error) {
	if r.schedule.ID != "" {
		return r.schedule, nil
	}
	return Schedule{ID: workforceTechID, MSPID: workforceMSPID, TechnicianID: workforceTechID, Timezone: "UTC", EffectiveFrom: timeDate(2026, 8, 1), Version: r.current}, nil
}

func (r *scheduleRepositoryStub) AddScheduleExceptionAtomic(_ context.Context, mutation ScheduleExceptionMutation) error {
	r.exceptionMutation = mutation
	if mutation.ExpectedVersion != r.current {
		return ErrWorkforceVersionConflict
	}
	r.current = mutation.Schedule.Version
	r.schedule = mutation.Schedule
	return nil
}

func workforcePrincipal(c string) authorization.Principal {
	return authorization.Principal{ID: workforceActorID, Scope: scope.Principal{MSPID: workforceMSPID}, Capabilities: authorization.NewCapabilitySet(c)}
}
func wfIDs() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("00000000-0000-4000-8003-%012d", n) }
}

func TestScheduleRejectsOverlappingWeeklyWindows(t *testing.T) {
	service := NewScheduleService(&scheduleRepositoryStub{}, time.Now, wfIDs())
	_, err := service.Publish(context.Background(), PublishScheduleCommand{Principal: workforcePrincipal("calendar.workforce.manage"), TechnicianID: workforceTechID, Timezone: "America/Chicago", EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Windows: []WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020}, {Weekday: time.Monday, StartsMinute: 600, EndsMinute: 660}}, ActorID: workforceActorID, Source: "api", IdempotencyKey: "overlap"})
	if !errors.Is(err, ErrScheduleOverlap) {
		t.Fatalf("error = %v", err)
	}
}

func TestScheduleRejectsUnknownTimezoneAndStaleVersion(t *testing.T) {
	r := &scheduleRepositoryStub{current: 2}
	service := NewScheduleService(r, time.Now, wfIDs())
	base := PublishScheduleCommand{Principal: workforcePrincipal("calendar.workforce.manage"), TechnicianID: workforceTechID, EffectiveFrom: time.Now(), Windows: []WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020}}, ActorID: workforceActorID, Source: "api", IdempotencyKey: "base"}
	base.Timezone = "Mars/Olympus"
	if _, err := service.Publish(context.Background(), base); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("timezone error = %v", err)
	}
	base.Timezone = "UTC"
	base.ExpectedVersion = 1
	if _, err := service.Publish(context.Background(), base); !errors.Is(err, ErrWorkforceVersionConflict) {
		t.Fatalf("version error = %v", err)
	}
}

func TestAddScheduleExceptionDerivesActorAndWritesVersionBoundMutation(t *testing.T) {
	repository := &scheduleRepositoryStub{current: 3, schedule: Schedule{ID: workforceTechID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, Timezone: "UTC", EffectiveFrom: timeDate(2026, 8, 1), Version: 3}}
	service := NewScheduleService(repository, func() time.Time { return timeDate(2026, 8, 9) }, wfIDs())

	result, err := service.AddException(context.Background(), AddScheduleExceptionCommand{
		Principal:       workforcePrincipal("calendar.workforce.manage"),
		ScheduleID:      workforceTechID,
		ExpectedVersion: 3,
		Exception:       ScheduleException{ExceptionOn: timeDate(2026, 8, 12), State: Unavailable, AllDay: true, Reason: "training"},
		ActorID:         workforceActorID,
		Source:          "http",
		IdempotencyKey:  "exception-add",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 4 || len(result.Exceptions) != 1 {
		t.Fatalf("result=%+v", result)
	}
	accepted := repository.exceptionMutation
	if accepted.ExpectedVersion != 3 || accepted.Schedule.Version != 4 || accepted.Audit.SubjectVersion != 4 || accepted.Event.SubjectVersion != 4 || accepted.Audit.ActorID != workforceActorID || accepted.Event.ActorID != workforceActorID || accepted.Event.EventType != "technician.schedule.exception.added" {
		t.Fatalf("mutation=%+v", accepted)
	}
}

func TestAddScheduleExceptionRejectsReusedExpectedVersionAfterSuccessfulWrite(t *testing.T) {
	repository := &scheduleRepositoryStub{current: 3, schedule: Schedule{ID: workforceTechID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, Timezone: "UTC", EffectiveFrom: timeDate(2026, 8, 1), Version: 3}}
	service := NewScheduleService(repository, time.Now, wfIDs())
	command := AddScheduleExceptionCommand{Principal: workforcePrincipal("calendar.workforce.manage"), ScheduleID: workforceTechID, ExpectedVersion: 3, Exception: ScheduleException{ExceptionOn: timeDate(2026, 8, 12), State: Unavailable, AllDay: true}, Source: "http", IdempotencyKey: "exception-first"}
	if _, err := service.AddException(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	command.Exception.ExceptionOn = timeDate(2026, 8, 13)
	command.IdempotencyKey = "exception-second"
	if _, err := service.AddException(context.Background(), command); !errors.Is(err, ErrWorkforceVersionConflict) {
		t.Fatalf("reused version error=%v", err)
	}
}

func TestAddScheduleExceptionRequiresWorkforceCapability(t *testing.T) {
	repository := &scheduleRepositoryStub{current: 1, schedule: Schedule{ID: workforceTechID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, Timezone: "UTC", EffectiveFrom: timeDate(2026, 8, 1), Version: 1}}
	service := NewScheduleService(repository, time.Now, wfIDs())
	_, err := service.AddException(context.Background(), AddScheduleExceptionCommand{Principal: workforcePrincipal("calendar.read"), ScheduleID: workforceTechID, ExpectedVersion: 1, Exception: ScheduleException{ExceptionOn: timeDate(2026, 8, 12), State: Unavailable, AllDay: true}, Source: "http", IdempotencyKey: "exception-auth"})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
}

func TestAddScheduleExceptionRejectsStaleScheduleVersion(t *testing.T) {
	repository := &scheduleRepositoryStub{current: 4, schedule: Schedule{ID: workforceTechID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, Timezone: "UTC", EffectiveFrom: timeDate(2026, 8, 1), Version: 4}}
	service := NewScheduleService(repository, time.Now, wfIDs())
	_, err := service.AddException(context.Background(), AddScheduleExceptionCommand{Principal: workforcePrincipal("calendar.workforce.manage"), ScheduleID: workforceTechID, ExpectedVersion: 3, Exception: ScheduleException{ExceptionOn: timeDate(2026, 8, 12), State: Unavailable, AllDay: true}, Source: "http", IdempotencyKey: "exception-stale"})
	if !errors.Is(err, ErrWorkforceVersionConflict) {
		t.Fatalf("error=%v", err)
	}
}

func timeDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
