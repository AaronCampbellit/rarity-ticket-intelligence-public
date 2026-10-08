package workrecords

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
)

type SLAOverrideCommand struct {
	Principal          authorization.Principal
	Target             scope.Target
	WorkRecordID       string
	ExpectedVersion    int64
	SLAExpectedVersion int64
	ResponseDueAt      *time.Time
	ResolutionDueAt    *time.Time
	Reason             string
	Actor              Actor
}

type SLAOverrideEvidence struct {
	ID                        string
	SLAID                     string
	WorkRecordID              string
	MSPID                     string
	ClientID                  string
	WorkRecordVersion         int64
	SLAVersionBefore          int64
	SLAVersionAfter           int64
	ResponseWarningAtBefore   *time.Time
	ResponseWarningAtAfter    *time.Time
	ResponseDueAtBefore       *time.Time
	ResponseDueAtAfter        *time.Time
	ResolutionWarningAtBefore *time.Time
	ResolutionWarningAtAfter  *time.Time
	ResolutionDueAtBefore     *time.Time
	ResolutionDueAtAfter      *time.Time
	Reason                    string
	OverriddenAt              time.Time
	OverriddenBy              string
}

type SLAOverrideMutation struct {
	Record   Record
	SLA      AppliedSLA
	Evidence SLAOverrideEvidence
	Audit    mutation.AuditRecord
	Event    mutation.EventRecord
}

type SLAOverrideRepository interface {
	Find(context.Context, scope.Target, string) (Record, error)
	FindAppliedSLA(context.Context, scope.Target, string) (AppliedSLA, error)
	OverrideSLAAtomic(context.Context, SLAOverrideMutation) error
}

type SLAOverrideService struct {
	repository SLAOverrideRepository
	now        func() time.Time
	newID      func() string
}

func NewSLAOverrideService(
	repository SLAOverrideRepository,
	now func() time.Time,
	newID func() string,
) *SLAOverrideService {
	return &SLAOverrideService{repository: repository, now: now, newID: newID}
}

func (s *SLAOverrideService) Override(
	ctx context.Context,
	command SLAOverrideCommand,
) (Record, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	reason := strings.TrimSpace(command.Reason)
	if target.ClientID == "" || command.WorkRecordID == "" || reason == "" ||
		command.ExpectedVersion < 1 || command.SLAExpectedVersion < 1 ||
		command.ResponseDueAt == nil && command.ResolutionDueAt == nil ||
		command.Actor.Type == "" || command.Actor.ID == "" || command.Actor.Source == "" {
		return Record{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "sla.override", target); err != nil {
		return Record{}, err
	}
	record, err := s.repository.Find(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := object.RequireVersion(record.Version, command.ExpectedVersion); err != nil {
		return Record{}, err
	}
	applied, err := s.repository.FindAppliedSLA(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := object.RequireVersion(applied.Version, command.SLAExpectedVersion); err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	evidence := SLAOverrideEvidence{
		ID: s.newID(), SLAID: applied.ID, WorkRecordID: record.ID,
		MSPID: record.MSPID, ClientID: record.ClientID,
		WorkRecordVersion: record.Version + 1,
		SLAVersionBefore:  applied.Version, SLAVersionAfter: applied.Version + 1,
		Reason: reason, OverriddenAt: now, OverriddenBy: command.Actor.ID,
	}
	if command.ResponseDueAt != nil {
		due := command.ResponseDueAt.UTC()
		if !due.After(now) || applied.RespondedAt != nil {
			return Record{}, ErrInvalid
		}
		previousDue, previousWarning := applied.ResponseDueAt, applied.ResponseWarningAt
		delta := due.Sub(previousDue)
		applied.ResponseDueAt = due
		applied.ResponseWarningAt = previousWarning.Add(delta)
		evidence.ResponseDueAtBefore = &previousDue
		evidence.ResponseDueAtAfter = &applied.ResponseDueAt
		evidence.ResponseWarningAtBefore = &previousWarning
		evidence.ResponseWarningAtAfter = &applied.ResponseWarningAt
		if applied.PausedAt == nil {
			applied.ResponseState = sla.Evaluate(sla.Target{
				DueAt: applied.ResponseDueAt, WarningAt: applied.ResponseWarningAt,
			}, now)
		}
	}
	if command.ResolutionDueAt != nil {
		due := command.ResolutionDueAt.UTC()
		if !due.After(now) || applied.ResolvedAt != nil {
			return Record{}, ErrInvalid
		}
		previousDue, previousWarning := applied.ResolutionDueAt, applied.ResolutionWarningAt
		delta := due.Sub(previousDue)
		applied.ResolutionDueAt = due
		applied.ResolutionWarningAt = previousWarning.Add(delta)
		evidence.ResolutionDueAtBefore = &previousDue
		evidence.ResolutionDueAtAfter = &applied.ResolutionDueAt
		evidence.ResolutionWarningAtBefore = &previousWarning
		evidence.ResolutionWarningAtAfter = &applied.ResolutionWarningAt
		if applied.PausedAt == nil {
			applied.ResolutionState = sla.Evaluate(sla.Target{
				DueAt: applied.ResolutionDueAt, WarningAt: applied.ResolutionWarningAt,
			}, now)
		}
	}
	record.Version++
	record.UpdatedAt = now
	record.UpdatedBy = command.Actor.ID
	applied.Version++
	correlationID := s.newID()
	accepted := SLAOverrideMutation{
		Record: record, SLA: applied, Evidence: evidence,
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "sla.deadline.overridden", SubjectType: "work_record_sla",
			SubjectID: applied.ID, SubjectVersion: applied.Version,
			Reason: reason, Source: command.Actor.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: "sla.deadline.overridden", SchemaVersion: 1,
			OccurredAt: now, MSPID: record.MSPID, ClientID: record.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: "work_record_sla", SubjectID: applied.ID,
			SubjectVersion: applied.Version, Source: command.Actor.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.OverrideSLAAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return record, nil
}
