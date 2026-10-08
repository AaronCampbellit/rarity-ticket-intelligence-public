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

type MergeCommand struct {
	Principal        authorization.Principal
	Target           scope.Target
	WinnerID         string
	WinnerVersion    int64
	DuplicateID      string
	DuplicateVersion int64
	Actor            Actor
	Reason           string
}

type MergeResult struct {
	Winner    Record
	Duplicate Record
}

type MergeMutation struct {
	Winner           Record
	Duplicate        Record
	ReparentChildren bool
	Audit            mutation.AuditRecord
	Event            mutation.EventRecord
}

type MergeRepository interface {
	FindForMerge(context.Context, scope.Target, string) (Record, error)
	MergeAtomic(context.Context, MergeMutation) error
}

type MergeService struct {
	repository MergeRepository
	now        func() time.Time
	newID      func() string
}

func NewMergeService(repository MergeRepository, now func() time.Time, newID func() string) *MergeService {
	return &MergeService{repository: repository, now: now, newID: newID}
}

func (s *MergeService) Merge(ctx context.Context, command MergeCommand) (MergeResult, error) {
	if command.WinnerID == "" ||
		command.DuplicateID == "" ||
		command.WinnerID == command.DuplicateID ||
		command.Actor.Type == "" ||
		command.Actor.ID == "" ||
		command.Actor.Source == "" ||
		strings.TrimSpace(command.Reason) == "" {
		return MergeResult{}, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" {
		return MergeResult{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.merge", target); err != nil {
		return MergeResult{}, err
	}
	winner, err := s.repository.FindForMerge(ctx, target, command.WinnerID)
	if err != nil {
		return MergeResult{}, err
	}
	duplicate, err := s.repository.FindForMerge(ctx, target, command.DuplicateID)
	if err != nil {
		return MergeResult{}, err
	}
	if winner.MSPID != target.MSPID ||
		winner.ClientID != target.ClientID ||
		duplicate.MSPID != target.MSPID ||
		duplicate.ClientID != target.ClientID {
		return MergeResult{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(winner.Version, command.WinnerVersion); err != nil {
		return MergeResult{}, err
	}
	if err := object.RequireVersion(duplicate.Version, command.DuplicateVersion); err != nil {
		return MergeResult{}, err
	}

	now := s.now().UTC()
	winner.Version++
	winner.UpdatedAt = now
	winner.UpdatedBy = command.Actor.ID
	duplicate.Version++
	duplicate.UpdatedAt = now
	duplicate.UpdatedBy = command.Actor.ID
	duplicate.LifecycleState = "deleted"
	duplicate.DeletedAt = &now
	duplicate.DeletedBy = command.Actor.ID
	duplicate.MergedIntoID = winner.ID
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	accepted := MergeMutation{
		Winner: winner, Duplicate: duplicate, ReparentChildren: true,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.merged", SubjectType: "work_record",
			SubjectID: duplicate.ID, SubjectVersion: duplicate.Version,
			Source: command.Actor.Source, Reason: strings.TrimSpace(command.Reason),
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "work_record.merged", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: "work_record", SubjectID: duplicate.ID,
			SubjectVersion: duplicate.Version, Source: command.Actor.Source,
			CorrelationID: correlationID,
			Data:          map[string]any{"winner_id": winner.ID},
		},
	}
	if err := s.repository.MergeAtomic(ctx, accepted); err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Winner: winner, Duplicate: duplicate}, nil
}
