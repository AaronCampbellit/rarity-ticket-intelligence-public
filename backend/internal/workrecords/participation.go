package workrecords

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ParticipantRole string

const (
	Collaborator ParticipantRole = "collaborator"
	Reviewer     ParticipantRole = "reviewer"
	Escalation   ParticipantRole = "escalation"
	Watcher      ParticipantRole = "watcher"
)

type Participant struct {
	ID           string
	MSPID        string
	ClientID     string
	WorkRecordID string
	TechnicianID string
	Role         ParticipantRole
	Version      int64
	AddedAt      time.Time
	AddedBy      string
	RemovedAt    *time.Time
	RemovedBy    string
}

type AddParticipantCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	WorkRecordID    string
	ExpectedVersion int64
	TechnicianID    string
	Role            ParticipantRole
	Actor           Actor
}

type RemoveParticipantCommand struct {
	Principal          authorization.Principal
	Target             scope.Target
	WorkRecordID       string
	ExpectedVersion    int64
	ParticipantID      string
	ParticipantVersion int64
	Actor              Actor
	Reason             string
}

type ParticipationResult struct {
	Record      Record
	Participant Participant
}

type ParticipantMutation struct {
	Record      Record
	Participant Participant
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
}

type ParticipantRepository interface {
	FindForParticipation(context.Context, scope.Target, string) (Record, error)
	FindParticipant(context.Context, scope.Target, string, string) (Participant, error)
	AddParticipantAtomic(context.Context, ParticipantMutation) error
	RemoveParticipantAtomic(context.Context, ParticipantMutation) error
}

type ParticipationService struct {
	repository ParticipantRepository
	now        func() time.Time
	newID      func() string
}

func NewParticipationService(
	repository ParticipantRepository,
	now func() time.Time,
	newID func() string,
) *ParticipationService {
	return &ParticipationService{repository: repository, now: now, newID: newID}
}

func (s *ParticipationService) Add(
	ctx context.Context,
	command AddParticipantCommand,
) (ParticipationResult, error) {
	target := participationTarget(command.Principal, command.Target)
	if target.ClientID == "" ||
		command.WorkRecordID == "" ||
		command.ExpectedVersion < 1 ||
		command.TechnicianID == "" ||
		!validParticipantRole(command.Role) ||
		!validActor(command.Actor) {
		return ParticipationResult{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.assign", target); err != nil {
		return ParticipationResult{}, err
	}
	record, err := s.repository.FindForParticipation(ctx, target, command.WorkRecordID)
	if err != nil {
		return ParticipationResult{}, err
	}
	if err := object.RequireVersion(record.Version, command.ExpectedVersion); err != nil {
		return ParticipationResult{}, err
	}
	now := s.now().UTC()
	record.Version++
	record.UpdatedAt = now
	record.UpdatedBy = command.Actor.ID
	participant := Participant{
		ID: s.newID(), MSPID: target.MSPID, ClientID: target.ClientID,
		WorkRecordID: record.ID, TechnicianID: command.TechnicianID,
		Role: command.Role, Version: 1, AddedAt: now, AddedBy: command.Actor.ID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ParticipantMutation{
		Record: record, Participant: participant,
		Audit: participationAudit(
			auditID, now, record, command.Actor,
			"work_record.participant.added", "", correlationID,
		),
		Event: participationEvent(
			eventID, now, record, command.Actor,
			"work_record.participant.added", correlationID,
		),
	}
	if err := s.repository.AddParticipantAtomic(ctx, accepted); err != nil {
		return ParticipationResult{}, err
	}
	return ParticipationResult{Record: record, Participant: participant}, nil
}

func (s *ParticipationService) Remove(
	ctx context.Context,
	command RemoveParticipantCommand,
) (ParticipationResult, error) {
	target := participationTarget(command.Principal, command.Target)
	reason := strings.TrimSpace(command.Reason)
	if target.ClientID == "" ||
		command.WorkRecordID == "" ||
		command.ExpectedVersion < 1 ||
		command.ParticipantID == "" ||
		command.ParticipantVersion < 1 ||
		reason == "" ||
		!validActor(command.Actor) {
		return ParticipationResult{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.assign", target); err != nil {
		return ParticipationResult{}, err
	}
	record, err := s.repository.FindForParticipation(ctx, target, command.WorkRecordID)
	if err != nil {
		return ParticipationResult{}, err
	}
	if err := object.RequireVersion(record.Version, command.ExpectedVersion); err != nil {
		return ParticipationResult{}, err
	}
	participant, err := s.repository.FindParticipant(
		ctx, target, command.WorkRecordID, command.ParticipantID,
	)
	if err != nil {
		return ParticipationResult{}, err
	}
	if participant.RemovedAt != nil {
		return ParticipationResult{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(
		participant.Version, command.ParticipantVersion,
	); err != nil {
		return ParticipationResult{}, err
	}
	now := s.now().UTC()
	record.Version++
	record.UpdatedAt = now
	record.UpdatedBy = command.Actor.ID
	participant.Version++
	participant.RemovedAt = &now
	participant.RemovedBy = command.Actor.ID
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := ParticipantMutation{
		Record: record, Participant: participant,
		Audit: participationAudit(
			auditID, now, record, command.Actor,
			"work_record.participant.removed", reason, correlationID,
		),
		Event: participationEvent(
			eventID, now, record, command.Actor,
			"work_record.participant.removed", correlationID,
		),
	}
	if err := s.repository.RemoveParticipantAtomic(ctx, accepted); err != nil {
		return ParticipationResult{}, err
	}
	return ParticipationResult{Record: record, Participant: participant}, nil
}

func participationTarget(
	principal authorization.Principal,
	target scope.Target,
) scope.Target {
	if target.MSPID == "" {
		return scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	return target
}

func validParticipantRole(role ParticipantRole) bool {
	switch role {
	case Collaborator, Reviewer, Escalation, Watcher:
		return true
	default:
		return false
	}
}

func validActor(actor Actor) bool {
	return actor.Type != "" && actor.ID != "" && actor.Source != ""
}

func participationAudit(
	id string,
	now time.Time,
	record Record,
	actor Actor,
	action string,
	reason string,
	correlationID string,
) mutation.AuditRecord {
	return mutation.AuditRecord{
		ID: id, OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
		ActorType: actor.Type, ActorID: actor.ID, Action: action,
		SubjectType: "work_record", SubjectID: record.ID,
		SubjectVersion: record.Version, Source: actor.Source,
		Reason: reason, CorrelationID: correlationID,
	}
}

func participationEvent(
	id string,
	now time.Time,
	record Record,
	actor Actor,
	eventType string,
	correlationID string,
) mutation.EventRecord {
	return mutation.EventRecord{
		EventID: id, EventType: eventType, SchemaVersion: 1,
		OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
		ActorType: actor.Type, ActorID: actor.ID,
		SubjectType: "work_record", SubjectID: record.ID,
		SubjectVersion: record.Version, Source: actor.Source,
		CorrelationID: correlationID,
	}
}
