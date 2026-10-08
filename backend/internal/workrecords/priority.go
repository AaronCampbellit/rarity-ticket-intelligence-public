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

type PriorityCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	WorkRecordID    string
	ExpectedVersion int64
	Priority        string
	Actor           Actor
	Reason          string
	CausationID     string
}

type PriorityMutation struct {
	Record           Record
	PreviousPriority string
	SLA              AppliedSLA
	Audit            mutation.AuditRecord
	Event            mutation.EventRecord
	SLAAudit         mutation.AuditRecord
	SLAEvent         mutation.EventRecord
}

type PriorityRepository interface {
	Find(context.Context, scope.Target, string) (Record, error)
	FindAppliedSLA(context.Context, scope.Target, string) (AppliedSLA, error)
	ChangePriorityAtomic(context.Context, PriorityMutation) error
}

type PriorityService struct {
	repository PriorityRepository
	now        func() time.Time
	newID      func() string
}

func NewPriorityService(
	repository PriorityRepository,
	now func() time.Time,
	newID func() string,
) *PriorityService {
	return &PriorityService{repository: repository, now: now, newID: newID}
}

func (s *PriorityService) Change(
	ctx context.Context,
	command PriorityCommand,
) (Record, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	priority := strings.TrimSpace(command.Priority)
	reason := strings.TrimSpace(command.Reason)
	if command.WorkRecordID == "" || priority == "" || reason == "" ||
		target.ClientID == "" || command.Actor.Type == "" ||
		command.Actor.ID == "" || command.Actor.Source == "" {
		return Record{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.edit", target); err != nil {
		return Record{}, err
	}
	current, err := s.repository.Find(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Record{}, err
	}
	if current.Priority == priority {
		return Record{}, ErrInvalid
	}
	appliedSLA, err := s.repository.FindAppliedSLA(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	previousPriority := current.Priority
	current.Priority = priority
	current.Version++
	current.UpdatedAt = now
	current.UpdatedBy = command.Actor.ID
	correlationID := s.newID()
	accepted := PriorityMutation{
		Record: current, PreviousPriority: previousPriority, SLA: appliedSLA,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.priority.changed", SubjectType: current.ObjectType,
			SubjectID: current.ID, SubjectVersion: current.Version,
			Reason: reason, Source: command.Actor.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: "work_record.priority.changed", SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: current.ObjectType, SubjectID: current.ID,
			SubjectVersion: current.Version, Source: command.Actor.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
			Data: map[string]any{"priority": current.Priority},
		},
		SLAAudit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "sla.policy.retained", SubjectType: "work_record_sla",
			SubjectID: appliedSLA.ID, SubjectVersion: appliedSLA.Version,
			Reason: reason, Source: command.Actor.Source, CorrelationID: correlationID,
		},
		SLAEvent: mutation.EventRecord{
			EventID: s.newID(), EventType: "sla.policy.retained", SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: "work_record_sla", SubjectID: appliedSLA.ID,
			SubjectVersion: appliedSLA.Version, Source: command.Actor.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
		},
	}
	if err := s.repository.ChangePriorityAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return current, nil
}
