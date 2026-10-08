package timeentries

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var (
	ErrInvalidTimer    = errors.New("invalid timer")
	ErrTimerState      = errors.New("invalid timer state")
	ErrCaptureConsumed = errors.New("time capture already consumed")
)

type TimerState string

const (
	TimerRunning   TimerState = "running"
	TimerStopped   TimerState = "stopped"
	TimerConsumed  TimerState = "consumed"
	TimerDiscarded TimerState = "discarded"
)

type TimerSession struct {
	ID                  string     `json:"id"`
	MSPID               string     `json:"msp_id"`
	ClientID            string     `json:"client_id"`
	WorkRecordID        string     `json:"work_record_id"`
	TechnicianID        string     `json:"technician_id"`
	State               TimerState `json:"state"`
	StartedAt           time.Time  `json:"started_at"`
	StoppedAt           *time.Time `json:"stopped_at,omitempty"`
	DurationSeconds     int64      `json:"duration_seconds,omitempty"`
	ConsumedTimeEntryID string     `json:"consumed_time_entry_id,omitempty"`
	IdempotencyKey      string     `json:"-"`
	Version             int64      `json:"version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type StartTimerCommand struct {
	Principal      authorization.Principal
	WorkRecordID   string
	IdempotencyKey string
	ActorID        string
	Source         string
}

type StopTimerCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	ActorID         string
	Source          string
}

type DiscardTimerCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	Reason          string
	ActorID         string
	Source          string
}

type ConsumeCaptureCommand struct {
	Principal       authorization.Principal
	ID              string
	ExpectedVersion int64
	TimeEntryID     string
	ActorID         string
	Source          string
}

type TimerMutation struct {
	Session TimerSession
	Audit   mutation.AuditRecord
	Event   mutation.EventRecord
}

type TimerRepository interface {
	StartTimerAtomic(context.Context, TimerMutation) (TimerSession, error)
	GetTimer(context.Context, scope.Target, string) (TimerSession, error)
	ListTicketTimers(
		context.Context,
		scope.Target,
		string,
		string,
	) ([]TimerSession, error)
	StopTimerAtomic(context.Context, TimerMutation) (TimerSession, error)
	DiscardTimerAtomic(context.Context, TimerMutation) (TimerSession, error)
	ConsumeTimerAtomic(context.Context, TimerMutation) (TimerSession, error)
}

type TimerService struct {
	repository TimerRepository
	now        func() time.Time
	newID      func() string
}

func NewTimerService(
	repository TimerRepository,
	now func() time.Time,
	newID func() string,
) *TimerService {
	return &TimerService{repository: repository, now: now, newID: newID}
}

func (s *TimerService) Start(
	ctx context.Context,
	command StartTimerCommand,
) (TimerSession, error) {
	target := timerTarget(command.Principal)
	workRecordID := strings.TrimSpace(command.WorkRecordID)
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if target.MSPID == "" || target.ClientID == "" ||
		workRecordID == "" || idempotencyKey == "" ||
		actorID == "" || actorID != command.Principal.ID || source == "" {
		return TimerSession{}, ErrInvalidTimer
	}
	if err := authorization.Authorize(
		command.Principal,
		"time_entry.create",
		target,
	); err != nil {
		return TimerSession{}, err
	}
	now := s.now().UTC()
	session := TimerSession{
		ID: s.newID(), MSPID: target.MSPID, ClientID: target.ClientID,
		WorkRecordID: workRecordID, TechnicianID: command.Principal.ID,
		State: TimerRunning, StartedAt: now, IdempotencyKey: idempotencyKey,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	accepted := s.timerMutation(session, actorID, source, "", "started")
	return s.repository.StartTimerAtomic(ctx, accepted)
}

func (s *TimerService) List(
	ctx context.Context,
	principal authorization.Principal,
	workRecordID string,
) ([]TimerSession, error) {
	target := timerTarget(principal)
	workRecordID = strings.TrimSpace(workRecordID)
	if target.MSPID == "" || target.ClientID == "" ||
		principal.ID == "" || workRecordID == "" {
		return nil, ErrInvalidTimer
	}
	if err := authorization.Authorize(
		principal,
		"time_entry.create",
		target,
	); err != nil {
		return nil, err
	}
	return s.repository.ListTicketTimers(
		ctx,
		target,
		workRecordID,
		principal.ID,
	)
}

func (s *TimerService) Stop(
	ctx context.Context,
	command StopTimerCommand,
) (TimerSession, error) {
	session, actorID, source, err := s.loadOwned(
		ctx,
		command.Principal,
		command.ID,
		command.ExpectedVersion,
		command.ActorID,
		command.Source,
	)
	if err != nil {
		return TimerSession{}, err
	}
	if session.State != TimerRunning {
		return TimerSession{}, ErrTimerState
	}
	stoppedAt := s.now().UTC()
	duration := int64(stoppedAt.Sub(session.StartedAt) / time.Second)
	if duration < 1 {
		return TimerSession{}, ErrInvalidTimer
	}
	session.State = TimerStopped
	session.StoppedAt = &stoppedAt
	session.DurationSeconds = duration
	session.Version++
	session.UpdatedAt = stoppedAt
	return s.repository.StopTimerAtomic(
		ctx,
		s.timerMutation(session, actorID, source, "", "stopped"),
	)
}

func (s *TimerService) Discard(
	ctx context.Context,
	command DiscardTimerCommand,
) (TimerSession, error) {
	session, actorID, source, err := s.loadOwned(
		ctx,
		command.Principal,
		command.ID,
		command.ExpectedVersion,
		command.ActorID,
		command.Source,
	)
	if err != nil {
		return TimerSession{}, err
	}
	reason := strings.TrimSpace(command.Reason)
	if session.State != TimerStopped || reason == "" {
		return TimerSession{}, ErrTimerState
	}
	session.State = TimerDiscarded
	session.Version++
	session.UpdatedAt = s.now().UTC()
	return s.repository.DiscardTimerAtomic(
		ctx,
		s.timerMutation(session, actorID, source, reason, "discarded"),
	)
}

func (s *TimerService) Consume(
	ctx context.Context,
	command ConsumeCaptureCommand,
) (TimerSession, error) {
	session, actorID, source, err := s.loadOwned(
		ctx,
		command.Principal,
		command.ID,
		command.ExpectedVersion,
		command.ActorID,
		command.Source,
	)
	if err != nil {
		return TimerSession{}, err
	}
	if session.State == TimerConsumed {
		return TimerSession{}, ErrCaptureConsumed
	}
	timeEntryID := strings.TrimSpace(command.TimeEntryID)
	if session.State != TimerStopped || timeEntryID == "" {
		return TimerSession{}, ErrTimerState
	}
	session.State = TimerConsumed
	session.ConsumedTimeEntryID = timeEntryID
	session.Version++
	session.UpdatedAt = s.now().UTC()
	return s.repository.ConsumeTimerAtomic(
		ctx,
		s.timerMutation(session, actorID, source, "", "consumed"),
	)
}

func (s *TimerService) loadOwned(
	ctx context.Context,
	principal authorization.Principal,
	id string,
	expectedVersion int64,
	actorID string,
	source string,
) (TimerSession, string, string, error) {
	target := timerTarget(principal)
	id = strings.TrimSpace(id)
	actorID = strings.TrimSpace(actorID)
	source = strings.TrimSpace(source)
	if target.MSPID == "" || target.ClientID == "" || id == "" ||
		expectedVersion < 1 || actorID == "" || actorID != principal.ID ||
		source == "" {
		return TimerSession{}, "", "", ErrInvalidTimer
	}
	if err := authorization.Authorize(
		principal,
		"time_entry.create",
		target,
	); err != nil {
		return TimerSession{}, "", "", err
	}
	session, err := s.repository.GetTimer(ctx, target, id)
	if err != nil {
		return TimerSession{}, "", "", err
	}
	if session.TechnicianID != principal.ID {
		return TimerSession{}, "", "", scope.ErrNotFound
	}
	if session.Version != expectedVersion {
		return TimerSession{}, "", "", object.ErrVersionConflict
	}
	return session, actorID, source, nil
}

func (s *TimerService) timerMutation(
	session TimerSession,
	actorID string,
	source string,
	reason string,
	transition string,
) TimerMutation {
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	return TimerMutation{
		Session: session,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: session.MSPID,
			ClientID: session.ClientID, ActorType: "technician", ActorID: actorID,
			Action: "ticket_timer." + transition, SubjectType: "ticket_timer",
			SubjectID: session.ID, SubjectVersion: session.Version,
			Source: source, Reason: reason, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "ticket_timer." + transition,
			SchemaVersion: 1, OccurredAt: now, MSPID: session.MSPID,
			ClientID: session.ClientID, ActorType: "technician", ActorID: actorID,
			SubjectType: "ticket_timer", SubjectID: session.ID,
			SubjectVersion: session.Version, Source: source,
			CorrelationID: correlationID,
		},
	}
}

func timerTarget(principal authorization.Principal) scope.Target {
	return scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
}
