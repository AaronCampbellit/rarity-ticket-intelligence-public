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

type AssignCommand struct {
	Principal             authorization.Principal
	Target                scope.Target
	WorkRecordID          string
	ExpectedVersion       int64
	ExpectedClientVersion int64
	ExpectedOwnerVersion  int64
	OwnerID               string
	Reason                string
	Actor                 Actor
	CausationID           string
}

type AssignmentMutation struct {
	Record                Record
	PreviousOwnerID       string
	ExpectedClientVersion int64
	ExpectedOwnerVersion  int64
	Audit                 mutation.AuditRecord
	Event                 mutation.EventRecord
}

type AssignmentRepository interface {
	Find(context.Context, scope.Target, string) (Record, error)
	AssignAtomic(context.Context, AssignmentMutation) error
}

type AssignmentService struct {
	repository AssignmentRepository
	now        func() time.Time
	newID      func() string
}

func NewAssignmentService(
	repository AssignmentRepository,
	now func() time.Time,
	newID func() string,
) *AssignmentService {
	return &AssignmentService{repository: repository, now: now, newID: newID}
}

func (s *AssignmentService) Assign(ctx context.Context, command AssignCommand) (Record, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	reason := strings.TrimSpace(command.Reason)
	if command.WorkRecordID == "" || command.OwnerID == "" || target.ClientID == "" || reason == "" {
		return Record{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.assign", target); err != nil {
		return Record{}, err
	}
	current, err := s.repository.Find(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Record{}, err
	}
	if current.PrimaryOwnerID == command.OwnerID {
		return Record{}, ErrInvalid
	}

	now := s.now().UTC()
	previousOwnerID := current.PrimaryOwnerID
	current.PrimaryOwnerID = command.OwnerID
	current.Version++
	current.UpdatedAt = now
	current.UpdatedBy = command.Actor.ID
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	accepted := AssignmentMutation{
		Record: current, PreviousOwnerID: previousOwnerID,
		ExpectedClientVersion: command.ExpectedClientVersion,
		ExpectedOwnerVersion:  command.ExpectedOwnerVersion,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.owner.changed", SubjectType: current.ObjectType,
			SubjectID: current.ID, SubjectVersion: current.Version,
			Reason: reason, Source: command.Actor.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "work_record.owner.changed", SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: current.ObjectType, SubjectID: current.ID,
			SubjectVersion: current.Version, Source: command.Actor.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
			Data: map[string]any{"owner_id": current.PrimaryOwnerID},
		},
	}
	if err := s.repository.AssignAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return current, nil
}
