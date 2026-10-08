package timeentries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type timerRepositoryStub struct {
	sessions map[string]TimerSession
	started  []TimerMutation
	stopped  []TimerMutation
	consumed []TimerMutation
}

func newTimerRepositoryStub() *timerRepositoryStub {
	return &timerRepositoryStub{sessions: make(map[string]TimerSession)}
}

func (r *timerRepositoryStub) StartTimerAtomic(
	_ context.Context,
	mutation TimerMutation,
) (TimerSession, error) {
	for _, session := range r.sessions {
		if session.MSPID == mutation.Session.MSPID &&
			session.TechnicianID == mutation.Session.TechnicianID &&
			session.IdempotencyKey == mutation.Session.IdempotencyKey {
			return session, nil
		}
	}
	r.started = append(r.started, mutation)
	r.sessions[mutation.Session.ID] = mutation.Session
	return mutation.Session, nil
}

func (r *timerRepositoryStub) GetTimer(
	_ context.Context,
	target scope.Target,
	id string,
) (TimerSession, error) {
	session, exists := r.sessions[id]
	if !exists || session.MSPID != target.MSPID || session.ClientID != target.ClientID {
		return TimerSession{}, scope.ErrNotFound
	}
	return session, nil
}

func (r *timerRepositoryStub) ListTicketTimers(
	_ context.Context,
	target scope.Target,
	workRecordID string,
	technicianID string,
) ([]TimerSession, error) {
	found := make([]TimerSession, 0)
	for _, session := range r.sessions {
		if session.MSPID == target.MSPID &&
			session.ClientID == target.ClientID &&
			session.WorkRecordID == workRecordID &&
			session.TechnicianID == technicianID {
			found = append(found, session)
		}
	}
	return found, nil
}

func (r *timerRepositoryStub) StopTimerAtomic(
	_ context.Context,
	mutation TimerMutation,
) (TimerSession, error) {
	current := r.sessions[mutation.Session.ID]
	if current.State != TimerRunning || current.Version != mutation.Session.Version-1 {
		return TimerSession{}, ErrTimerState
	}
	r.stopped = append(r.stopped, mutation)
	r.sessions[mutation.Session.ID] = mutation.Session
	return mutation.Session, nil
}

func (r *timerRepositoryStub) DiscardTimerAtomic(
	_ context.Context,
	mutation TimerMutation,
) (TimerSession, error) {
	current := r.sessions[mutation.Session.ID]
	if current.State != TimerStopped || current.Version != mutation.Session.Version-1 {
		return TimerSession{}, ErrTimerState
	}
	r.sessions[mutation.Session.ID] = mutation.Session
	return mutation.Session, nil
}

func (r *timerRepositoryStub) ConsumeTimerAtomic(
	_ context.Context,
	mutation TimerMutation,
) (TimerSession, error) {
	current := r.sessions[mutation.Session.ID]
	if current.State == TimerConsumed {
		return TimerSession{}, ErrCaptureConsumed
	}
	if current.State != TimerStopped || current.Version != mutation.Session.Version-1 {
		return TimerSession{}, ErrTimerState
	}
	r.consumed = append(r.consumed, mutation)
	r.sessions[mutation.Session.ID] = mutation.Session
	return mutation.Session, nil
}

func TestTechnicianCanRunTimersOnDifferentTickets(t *testing.T) {
	now := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	repository := newTimerRepositoryStub()
	next := 0
	service := NewTimerService(repository, func() time.Time { return now }, func() string {
		next++
		return []string{
			"timer-1", "audit-1", "event-1", "correlation-1",
			"timer-2", "audit-2", "event-2", "correlation-2",
		}[next-1]
	})
	principal := timerPrincipal()
	first, err := service.Start(context.Background(), StartTimerCommand{
		Principal: principal, WorkRecordID: "ticket-1",
		IdempotencyKey: "start-1", ActorID: "tech", Source: "api",
	})
	if err != nil {
		t.Fatalf("Start(ticket-1) error = %v", err)
	}
	second, err := service.Start(context.Background(), StartTimerCommand{
		Principal: principal, WorkRecordID: "ticket-2",
		IdempotencyKey: "start-2", ActorID: "tech", Source: "api",
	})
	if err != nil {
		t.Fatalf("Start(ticket-2) error = %v", err)
	}
	if first.ID == second.ID || len(repository.started) != 2 {
		t.Fatalf("ticket timers are not independent: first=%+v second=%+v", first, second)
	}
}

func TestListTicketTimersReturnsOnlyCurrentTechnicianTicketCaptures(t *testing.T) {
	repository := newTimerRepositoryStub()
	repository.sessions["selected"] = TimerSession{
		ID: "selected", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket-1", TechnicianID: "tech",
		State: TimerRunning, Version: 1,
	}
	repository.sessions["other-ticket"] = TimerSession{
		ID: "other-ticket", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket-2", TechnicianID: "tech",
		State: TimerRunning, Version: 1,
	}
	repository.sessions["other-tech"] = TimerSession{
		ID: "other-tech", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket-1", TechnicianID: "other",
		State: TimerRunning, Version: 1,
	}
	found, err := NewTimerService(repository, time.Now, idSequence()).
		List(context.Background(), timerPrincipal(), "ticket-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(found) != 1 || found[0].ID != "selected" {
		t.Fatalf("List() = %+v", found)
	}
}

func TestStopTimerUsesServerTimeAndProducesStoppedCapture(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	stoppedAt := startedAt.Add(15 * time.Minute)
	repository := newTimerRepositoryStub()
	repository.sessions["timer"] = TimerSession{
		ID: "timer", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket", TechnicianID: "tech",
		State: TimerRunning, StartedAt: startedAt, Version: 1,
	}
	service := NewTimerService(repository, func() time.Time { return stoppedAt }, idSequence(
		"audit", "event", "correlation",
	))
	found, err := service.Stop(context.Background(), StopTimerCommand{
		Principal: timerPrincipal(), ID: "timer", ExpectedVersion: 1,
		ActorID: "tech", Source: "api",
	})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if found.State != TimerStopped ||
		found.DurationSeconds != 900 ||
		found.StoppedAt == nil ||
		!found.StoppedAt.Equal(stoppedAt) ||
		found.Version != 2 {
		t.Fatalf("Stop() = %+v", found)
	}
}

func TestStoppedCaptureCanBeConsumedExactlyOnce(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	stoppedAt := startedAt.Add(15 * time.Minute)
	repository := newTimerRepositoryStub()
	repository.sessions["timer"] = TimerSession{
		ID: "timer", MSPID: "msp", ClientID: "client",
		WorkRecordID: "ticket", TechnicianID: "tech",
		State: TimerStopped, StartedAt: startedAt, StoppedAt: &stoppedAt,
		DurationSeconds: 900, Version: 2,
	}
	service := NewTimerService(repository, time.Now, idSequence(
		"audit", "event", "correlation",
	))
	found, err := service.Consume(context.Background(), ConsumeCaptureCommand{
		Principal: timerPrincipal(), ID: "timer", ExpectedVersion: 2,
		TimeEntryID: "entry", ActorID: "tech", Source: "api",
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if found.State != TimerConsumed ||
		found.ConsumedTimeEntryID != "entry" ||
		found.Version != 3 {
		t.Fatalf("Consume() = %+v", found)
	}
	_, err = service.Consume(context.Background(), ConsumeCaptureCommand{
		Principal: timerPrincipal(), ID: "timer", ExpectedVersion: 3,
		TimeEntryID: "other-entry", ActorID: "tech", Source: "api",
	})
	if !errors.Is(err, ErrCaptureConsumed) {
		t.Fatalf("second Consume() error = %v", err)
	}
}

func timerPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "tech",
		Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet("time_entry.create"),
	}
}

func idSequence(values ...string) func() string {
	next := 0
	return func() string {
		value := values[next]
		next++
		return value
	}
}
