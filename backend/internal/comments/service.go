// Package comments owns internal notes and client-visible work-record replies.
package comments

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

var ErrInvalid = errors.New("invalid comment")

type Visibility string

const (
	Internal      Visibility = "internal"
	ClientVisible Visibility = "client"
)

type Comment struct {
	ID           string
	MSPID        string
	ClientID     string
	WorkRecordID string
	AuthorID     string
	Visibility   Visibility
	Body         string
	Version      int64
	CreatedAt    time.Time
}

type CreateCommand struct {
	Principal    authorization.Principal
	Target       scope.Target
	WorkRecordID string
	Visibility   Visibility
	Body         string
	ActorID      string
	ActorType    string
	Source       string
	CausationID  string
	TimeCapture  *TimeCapture
}

type TimeCapture struct {
	ID              string
	ExpectedVersion int64
	LaborRoleID     string
	Billable        bool
	TagIDs          []string
}

type CreateMutation struct {
	Comment    Comment
	SLA        workrecords.AppliedSLA
	SLAChanged bool
	SLAAudit   mutation.AuditRecord
	SLAEvent   mutation.EventRecord
	Audit      mutation.AuditRecord
	Event      mutation.EventRecord
}

type Repository interface {
	FindAppliedSLA(context.Context, scope.Target, string) (workrecords.AppliedSLA, error)
	CreateAtomic(context.Context, CreateMutation) error
}

type CapturePreparer interface {
	Prepare(context.Context, timeentries.CaptureCommand) (timeentries.CaptureMutation, error)
}

type CaptureRepository interface {
	CreateWithCaptureAtomic(
		context.Context,
		CreateMutation,
		timeentries.CaptureMutation,
	) error
}

type Service struct {
	repository Repository
	capture    CapturePreparer
	now        func() time.Time
	newID      func() string
}

func NewService(repository Repository, now func() time.Time, newID func() string) *Service {
	return &Service{repository: repository, now: now, newID: newID}
}

func NewServiceWithCapture(
	repository Repository,
	capture CapturePreparer,
	now func() time.Time,
	newID func() string,
) *Service {
	return &Service{
		repository: repository,
		capture:    capture,
		now:        now,
		newID:      newID,
	}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Comment, error) {
	if command.WorkRecordID == "" ||
		command.ActorID == "" ||
		command.Source == "" ||
		strings.TrimSpace(command.Body) == "" ||
		(command.Visibility != Internal && command.Visibility != ClientVisible) {
		return Comment{}, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" {
		return Comment{}, ErrInvalid
	}
	capability := "comment.internal.create"
	if command.Visibility == ClientVisible {
		capability = "comment.public.create"
	}
	if err := authorization.Authorize(command.Principal, capability, target); err != nil {
		return Comment{}, err
	}

	now := s.now().UTC()
	actorType := strings.TrimSpace(command.ActorType)
	if actorType == "" {
		actorType = "technician"
	}
	var appliedSLA workrecords.AppliedSLA
	slaChanged := false
	if command.Visibility == ClientVisible {
		loadedSLA, err := s.repository.FindAppliedSLA(ctx, target, command.WorkRecordID)
		if err != nil {
			return Comment{}, err
		}
		appliedSLA = loadedSLA
		if appliedSLA.RespondedAt == nil {
			if appliedSLA.PausedAt != nil {
				calendar, err := appliedSLA.CalendarDefinition.Calendar()
				if err != nil {
					return Comment{}, err
				}
				pausedBusiness, err := sla.BusinessTimeBetween(
					calendar, *appliedSLA.PausedAt, now,
				)
				if err != nil {
					return Comment{}, err
				}
				if pausedBusiness > 0 {
					appliedSLA.ResponseWarningAt, err = sla.AddBusinessTime(
						calendar, appliedSLA.ResponseWarningAt, pausedBusiness,
					)
					if err != nil {
						return Comment{}, err
					}
					appliedSLA.ResponseDueAt, err = sla.AddBusinessTime(
						calendar, appliedSLA.ResponseDueAt, pausedBusiness,
					)
					if err != nil {
						return Comment{}, err
					}
				}
			}
			appliedSLA.RespondedAt = &now
			appliedSLA.ResponseState = sla.Evaluate(sla.Target{
				DueAt:     appliedSLA.ResponseDueAt,
				WarningAt: appliedSLA.ResponseWarningAt,
				MetAt:     appliedSLA.RespondedAt,
			}, now)
			appliedSLA.Version++
			slaChanged = true
		}
	}
	commentID := s.newID()
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	comment := Comment{
		ID: commentID, MSPID: target.MSPID, ClientID: target.ClientID,
		WorkRecordID: command.WorkRecordID, AuthorID: command.ActorID,
		Visibility: command.Visibility, Body: strings.TrimSpace(command.Body),
		Version: 1, CreatedAt: now,
	}
	accepted := CreateMutation{
		Comment: comment, SLA: appliedSLA, SLAChanged: slaChanged,
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: comment.MSPID, ClientID: comment.ClientID,
			ActorType: actorType, ActorID: command.ActorID, Action: "comment.added",
			SubjectType: "comment", SubjectID: comment.ID, SubjectVersion: comment.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "comment.added", SchemaVersion: 1,
			OccurredAt: now, MSPID: comment.MSPID, ClientID: comment.ClientID,
			ActorType: actorType, ActorID: command.ActorID,
			SubjectType: "comment", SubjectID: comment.ID, SubjectVersion: comment.Version,
			Source: command.Source, CorrelationID: correlationID,
			CausationID: command.CausationID,
			Data:        map[string]any{"visibility": string(comment.Visibility)},
		},
	}
	if slaChanged {
		accepted.SLAAudit = mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: comment.MSPID, ClientID: comment.ClientID,
			ActorType: actorType, ActorID: command.ActorID,
			Action: "sla.response.recorded", SubjectType: "work_record_sla",
			SubjectID: appliedSLA.ID, SubjectVersion: appliedSLA.Version,
			Source: command.Source, CorrelationID: correlationID,
		}
		accepted.SLAEvent = mutation.EventRecord{
			EventID: s.newID(), EventType: "sla.response.recorded", SchemaVersion: 1,
			OccurredAt: now, MSPID: comment.MSPID, ClientID: comment.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "work_record_sla", SubjectID: appliedSLA.ID,
			SubjectVersion: appliedSLA.Version, Source: command.Source,
			CorrelationID: correlationID, CausationID: command.CausationID,
		}
	}
	if command.TimeCapture == nil {
		if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
			return Comment{}, err
		}
		return comment, nil
	}
	if s.capture == nil {
		return Comment{}, ErrInvalid
	}
	captureRepository, ok := s.repository.(CaptureRepository)
	if !ok {
		return Comment{}, ErrInvalid
	}
	capture := command.TimeCapture
	acceptedCapture, err := s.capture.Prepare(ctx, timeentries.CaptureCommand{
		Principal: command.Principal, WorkRecordID: command.WorkRecordID,
		CaptureID: capture.ID, ExpectedCaptureVersion: capture.ExpectedVersion,
		LaborRoleID: capture.LaborRoleID, Billable: capture.Billable,
		Note: comment.Body, ActorID: command.ActorID, Source: command.Source,
		TagIDs: capture.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		return Comment{}, err
	}
	if err := captureRepository.CreateWithCaptureAtomic(
		ctx,
		accepted,
		acceptedCapture,
	); err != nil {
		return Comment{}, err
	}
	return comment, nil
}
