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
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type TransitionCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	WorkRecordID    string
	ExpectedVersion int64
	ToStatus        string
	Actor           Actor
	Reason          string
	CausationID     string
}

type SelectedWorkflow struct {
	WorkflowID string
	Version    int64
	Definition workflow.Definition
}

type TransitionMutation struct {
	Record          Record
	PreviousStatus  string
	WorkflowID      string
	WorkflowVersion int64
	Reason          string
	SLA             AppliedSLA
	SLAChanged      bool
	SLAAudit        mutation.AuditRecord
	SLAEvent        mutation.EventRecord
	Audit           mutation.AuditRecord
	Event           mutation.EventRecord
}

type TransitionRepository interface {
	Find(context.Context, scope.Target, string) (Record, error)
	FindSelectedWorkflow(context.Context, scope.Target, string) (SelectedWorkflow, error)
	FindAppliedSLA(context.Context, scope.Target, string) (AppliedSLA, error)
	TransitionAtomic(context.Context, TransitionMutation) error
}

type TransitionService struct {
	repository TransitionRepository
	now        func() time.Time
	newID      func() string
	guard      *tagging.TerminalGuard
}

func NewTransitionService(
	repository TransitionRepository,
	now func() time.Time,
	newID func() string,
	guard *tagging.TerminalGuard,
) *TransitionService {
	return &TransitionService{repository: repository, now: now, newID: newID, guard: guard}
}

func (s *TransitionService) Transition(
	ctx context.Context,
	command TransitionCommand,
) (Record, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	toStatus := strings.TrimSpace(command.ToStatus)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.guard == nil ||
		command.WorkRecordID == "" || toStatus == "" || target.ClientID == "" ||
		command.Actor.Type == "" || command.Actor.ID == "" || command.Actor.Source == "" {
		return Record{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "work_record.transition", target); err != nil {
		return Record{}, err
	}
	current, err := s.repository.Find(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := object.RequireVersion(current.Version, command.ExpectedVersion); err != nil {
		return Record{}, err
	}
	selected, err := s.repository.FindSelectedWorkflow(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}
	if err := selected.Definition.Validate(); err != nil {
		return Record{}, err
	}
	if err := selected.Definition.ValidateTransition(workflow.TransitionInput{
		From: current.Status, To: toStatus, OwnerID: current.PrimaryOwnerID,
	}); err != nil {
		return Record{}, err
	}
	destination, ok := selected.Definition.State(toStatus)
	if !ok {
		return Record{}, workflow.ErrInvalidConfiguration
	}
	if destination.SLABehavior == workflow.SLAResolved || destination.SLABehavior == workflow.SLACancelled {
		if err := s.guard.RequireMeaningful(ctx, tagging.GuardCommand{Principal: command.Principal, Target: tagging.TargetRef{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectWorkRecord, ObjectID: command.WorkRecordID}}); err != nil {
			return Record{}, err
		}
	}
	appliedSLA, err := s.repository.FindAppliedSLA(ctx, target, command.WorkRecordID)
	if err != nil {
		return Record{}, err
	}

	now := s.now().UTC()
	slaAction, err := transitionSLA(&appliedSLA, destination, toStatus, now)
	if err != nil {
		return Record{}, err
	}
	previousStatus := current.Status
	current.Status = toStatus
	current.Version++
	current.UpdatedAt = now
	current.UpdatedBy = command.Actor.ID
	correlationID := s.newID()
	accepted := TransitionMutation{
		Record: current, PreviousStatus: previousStatus,
		WorkflowID: selected.WorkflowID, WorkflowVersion: selected.Version,
		Reason: strings.TrimSpace(command.Reason),
		Audit: mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: "work_record.transitioned", SubjectType: current.ObjectType,
			SubjectID: current.ID, SubjectVersion: current.Version,
			Reason: strings.TrimSpace(command.Reason), Source: command.Actor.Source,
			CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: s.newID(), EventType: "work_record.transitioned", SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: current.ObjectType, SubjectID: current.ID,
			SubjectVersion: current.Version, Source: command.Actor.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
			Data: map[string]any{"status": current.Status},
		},
	}
	if slaAction != "" {
		appliedSLA.Version++
		accepted.SLA = appliedSLA
		accepted.SLAChanged = true
		accepted.SLAAudit = mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			Action: slaAction, SubjectType: "work_record_sla",
			SubjectID: appliedSLA.ID, SubjectVersion: appliedSLA.Version,
			Reason: strings.TrimSpace(command.Reason), Source: command.Actor.Source,
			CorrelationID: correlationID,
		}
		accepted.SLAEvent = mutation.EventRecord{
			EventID: s.newID(), EventType: slaAction, SchemaVersion: 1,
			OccurredAt: now, MSPID: current.MSPID, ClientID: current.ClientID,
			ActorType: command.Actor.Type, ActorID: command.Actor.ID,
			SubjectType: "work_record_sla", SubjectID: appliedSLA.ID,
			SubjectVersion: appliedSLA.Version, Source: command.Actor.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
		}
	}
	if err := s.repository.TransitionAtomic(ctx, accepted); err != nil {
		return Record{}, err
	}
	return current, nil
}

func transitionSLA(
	applied *AppliedSLA,
	destination workflow.State,
	toStatus string,
	now time.Time,
) (string, error) {
	calendar, err := applied.CalendarDefinition.Calendar()
	if err != nil {
		return "", err
	}
	shouldPause := contains(applied.PauseStates, toStatus)
	action := ""
	if applied.PausedAt != nil && (!shouldPause || destination.SLABehavior != "") {
		pausedBusiness, err := sla.BusinessTimeBetween(calendar, *applied.PausedAt, now)
		if err != nil {
			return "", err
		}
		pausedWall := now.Sub(*applied.PausedAt)
		if pausedWall < 0 {
			return "", sla.ErrInvalidCalendar
		}
		applied.PausedSeconds += int64(pausedWall / time.Second)
		if applied.ResponseState == sla.Paused {
			applied.ResponseWarningAt, err = extendBusinessDeadline(
				calendar, applied.ResponseWarningAt, pausedBusiness,
			)
			if err != nil {
				return "", err
			}
			applied.ResponseDueAt, err = extendBusinessDeadline(
				calendar, applied.ResponseDueAt, pausedBusiness,
			)
			if err != nil {
				return "", err
			}
			applied.ResponseState = sla.Evaluate(sla.Target{
				DueAt: applied.ResponseDueAt, WarningAt: applied.ResponseWarningAt,
				MetAt: applied.RespondedAt,
			}, now)
		}
		if applied.ResolutionState == sla.Paused {
			applied.ResolutionWarningAt, err = extendBusinessDeadline(
				calendar, applied.ResolutionWarningAt, pausedBusiness,
			)
			if err != nil {
				return "", err
			}
			applied.ResolutionDueAt, err = extendBusinessDeadline(
				calendar, applied.ResolutionDueAt, pausedBusiness,
			)
			if err != nil {
				return "", err
			}
			applied.ResolutionState = sla.Evaluate(sla.Target{
				DueAt: applied.ResolutionDueAt, WarningAt: applied.ResolutionWarningAt,
				MetAt: applied.ResolvedAt,
			}, now)
		}
		applied.PausedAt = nil
		action = "sla.resumed"
	}

	switch destination.SLABehavior {
	case workflow.SLAResolved:
		applied.ResolvedAt = &now
		applied.ResolutionState = sla.Evaluate(sla.Target{
			DueAt: applied.ResolutionDueAt, WarningAt: applied.ResolutionWarningAt,
			MetAt: applied.ResolvedAt,
		}, now)
		action = "sla.resolved"
	case workflow.SLACancelled:
		applied.ResolutionState = sla.Cancelled
		if applied.ResponseState != sla.Met && applied.ResponseState != sla.Breached {
			applied.ResponseState = sla.Cancelled
		}
		action = "sla.cancelled"
	default:
		if applied.ResolvedAt != nil || applied.ResolutionState == sla.Cancelled {
			applied.ResolvedAt = nil
			applied.ResolutionState = sla.Evaluate(sla.Target{
				DueAt: applied.ResolutionDueAt, WarningAt: applied.ResolutionWarningAt,
			}, now)
			action = "sla.reopened"
		}
		if shouldPause && applied.PausedAt == nil {
			applied.PausedAt = &now
			if applied.ResponseState == sla.Running || applied.ResponseState == sla.Warning {
				applied.ResponseState = sla.Paused
			}
			if applied.ResolutionState == sla.Running || applied.ResolutionState == sla.Warning {
				applied.ResolutionState = sla.Paused
			}
			action = "sla.paused"
		}
	}
	return action, nil
}

func extendBusinessDeadline(
	calendar sla.Calendar,
	deadline time.Time,
	duration time.Duration,
) (time.Time, error) {
	if duration == 0 {
		return deadline, nil
	}
	return sla.AddBusinessTime(calendar, deadline, duration)
}
