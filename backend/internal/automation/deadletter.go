package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidDeadLetterAction = errors.New("invalid dead-letter action")

type DeadLetterAction string

const (
	DeadLetterRetry   DeadLetterAction = "retry"
	DeadLetterReplay  DeadLetterAction = "replay"
	DeadLetterDismiss DeadLetterAction = "dismiss"
)

type DeadLetterCommand struct {
	Principal authorization.Principal
	ID        string
	Action    DeadLetterAction
	Reason    string
}

type DeadLetterMutation struct {
	Letter   DeadLetter
	ActionID string
	Action   DeadLetterAction
	Reason   string
	ActedAt  time.Time
	ActedBy  string
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type DeadLetterRepository interface {
	List(context.Context, scope.Target) ([]DeadLetter, error)
	Get(context.Context, scope.Target, string) (DeadLetter, error)
	Apply(context.Context, DeadLetterMutation) error
}

func (s *DeadLetterService) List(
	ctx context.Context,
	principal authorization.Principal,
) ([]DeadLetter, error) {
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if err := authorization.Authorize(
		principal, "automation.dead_letter.manage", target,
	); err != nil {
		return nil, err
	}
	if s.repository == nil {
		return nil, ErrInvalidDeadLetterAction
	}
	return s.repository.List(ctx, target)
}

type DeadLetterService struct {
	repository DeadLetterRepository
	now        func() time.Time
	newID      func() string
}

func NewDeadLetterService(
	repository DeadLetterRepository,
	now func() time.Time,
	newID func() string,
) *DeadLetterService {
	return &DeadLetterService{repository: repository, now: now, newID: newID}
}

func (s *DeadLetterService) Act(
	ctx context.Context,
	command DeadLetterCommand,
) (DeadLetter, error) {
	if s.repository == nil || s.now == nil || s.newID == nil ||
		strings.TrimSpace(command.ID) == "" || strings.TrimSpace(command.Reason) == "" {
		return DeadLetter{}, ErrInvalidDeadLetterAction
	}
	target := scope.Target{
		MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
	}
	letter, err := s.repository.Get(ctx, target, command.ID)
	if err != nil {
		return DeadLetter{}, err
	}
	if err := authorization.Authorize(
		command.Principal,
		"automation.dead_letter.manage",
		target,
	); err != nil {
		return DeadLetter{}, err
	}
	if letter.State != "open" {
		return DeadLetter{}, ErrInvalidDeadLetterAction
	}
	switch command.Action {
	case DeadLetterRetry:
		letter.State = "retrying"
	case DeadLetterReplay:
		letter.State = "replayed"
	case DeadLetterDismiss:
		letter.State = "dismissed"
	default:
		return DeadLetter{}, ErrInvalidDeadLetterAction
	}
	now := s.now().UTC()
	actionID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	actionName := "automation.dead_letter." + string(command.Action)
	accepted := DeadLetterMutation{
		Letter: letter, ActionID: actionID, Action: command.Action,
		Reason: command.Reason, ActedAt: now, ActedBy: command.Principal.ID,
		Audit: mutation.AuditRecord{
			ID: actionID, OccurredAt: now, MSPID: letter.MSPID, ClientID: letter.ClientID,
			ActorType: "technician", ActorID: command.Principal.ID, Action: actionName,
			SubjectType: "automation_dead_letter", SubjectID: letter.ID,
			SubjectVersion: 1, Source: "api", Reason: command.Reason,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: actionName, SchemaVersion: 1,
			OccurredAt: now, MSPID: letter.MSPID, ClientID: letter.ClientID,
			ActorType: "technician", ActorID: command.Principal.ID,
			SubjectType: "automation_dead_letter", SubjectID: letter.ID,
			SubjectVersion: 1, Source: "api", CorrelationID: correlationID,
		},
	}
	if err := s.repository.Apply(ctx, accepted); err != nil {
		return DeadLetter{}, err
	}
	return letter, nil
}
