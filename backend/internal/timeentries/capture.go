package timeentries

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type CaptureCommand struct {
	Principal              authorization.Principal
	WorkRecordID           string
	CaptureID              string
	ExpectedCaptureVersion int64
	LaborRoleID            string
	Billable               bool
	Note                   string
	ActorID                string
	Source                 string
	TagIDs                 []string
	ClassificationPolicy   tagging.CreationPolicy
}

type CaptureMutation struct {
	EntryID                string
	Target                 scope.Target
	WorkRecordID           string
	TechnicianID           string
	CaptureID              string
	ExpectedCaptureVersion int64
	LaborRoleID            string
	Billable               bool
	Note                   string
	CreatedAt              time.Time
	EntryAudit             mutation.AuditRecord
	EntryEvent             mutation.EventRecord
	TimerAudit             mutation.AuditRecord
	TimerEvent             mutation.EventRecord
	InitialTags            tagging.InitialAssignmentSet
}

type CaptureRepository interface {
	CreateFromCaptureAtomic(context.Context, CaptureMutation) (Entry, error)
}

type CaptureService struct {
	repository CaptureRepository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewCaptureService(
	repository CaptureRepository,
	now func() time.Time,
	newID func() string,
	creation *tagging.CreationPreparer,
) *CaptureService {
	return &CaptureService{repository: repository, now: now, newID: newID, creation: creation}
}

func (s *CaptureService) Create(
	ctx context.Context,
	command CaptureCommand,
) (Entry, error) {
	accepted, err := s.Prepare(ctx, command)
	if err != nil {
		return Entry{}, err
	}
	return s.repository.CreateFromCaptureAtomic(ctx, accepted)
}

func (s *CaptureService) Prepare(
	ctx context.Context,
	command CaptureCommand,
) (CaptureMutation, error) {
	target := scope.Target{
		MSPID:    command.Principal.Scope.MSPID,
		ClientID: command.Principal.Scope.ClientID,
	}
	workRecordID := strings.TrimSpace(command.WorkRecordID)
	captureID := strings.TrimSpace(command.CaptureID)
	laborRoleID := strings.TrimSpace(command.LaborRoleID)
	note := strings.TrimSpace(command.Note)
	actorID := strings.TrimSpace(command.ActorID)
	source := strings.TrimSpace(command.Source)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil ||
		target.MSPID == "" || target.ClientID == "" ||
		workRecordID == "" || captureID == "" || laborRoleID == "" ||
		command.ExpectedCaptureVersion < 1 || actorID == "" ||
		actorID != command.Principal.ID || source == "" {
		return CaptureMutation{}, ErrInvalidTimer
	}
	if err := authorization.Authorize(
		command.Principal,
		"time_entry.create",
		target,
	); err != nil {
		return CaptureMutation{}, err
	}
	initial, err := s.creation.Prepare(ctx, tagging.PrepareCreationCommand{
		MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTimeEntry,
		TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy,
	})
	if err != nil {
		return CaptureMutation{}, err
	}

	now := s.now().UTC()
	entryID := s.newID()
	entryAuditID := s.newID()
	entryEventID := s.newID()
	timerAuditID := s.newID()
	timerEventID := s.newID()
	correlationID := s.newID()
	accepted := CaptureMutation{
		EntryID: entryID, Target: target, WorkRecordID: workRecordID,
		TechnicianID: actorID, CaptureID: captureID,
		ExpectedCaptureVersion: command.ExpectedCaptureVersion,
		LaborRoleID:            laborRoleID, Billable: command.Billable,
		Note: note, CreatedAt: now, InitialTags: initial,
		EntryAudit: mutation.AuditRecord{
			ID: entryAuditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			Action: "time_entry.created_from_capture", SubjectType: "time_entry",
			SubjectID: entryID, SubjectVersion: 1, Source: source,
			CorrelationID: correlationID,
		},
		EntryEvent: mutation.EventRecord{
			EventID: entryEventID, EventType: "time_entry.created_from_capture",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			SubjectType: "time_entry", SubjectID: entryID,
			SubjectVersion: 1, Source: source, CorrelationID: correlationID,
		},
		TimerAudit: mutation.AuditRecord{
			ID: timerAuditID, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			Action: "ticket_timer.consumed", SubjectType: "ticket_timer",
			SubjectID:      captureID,
			SubjectVersion: command.ExpectedCaptureVersion + 1,
			Source:         source, CorrelationID: correlationID,
		},
		TimerEvent: mutation.EventRecord{
			EventID: timerEventID, EventType: "ticket_timer.consumed",
			SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID,
			ClientID: target.ClientID, ActorType: "technician", ActorID: actorID,
			SubjectType: "ticket_timer", SubjectID: captureID,
			SubjectVersion: command.ExpectedCaptureVersion + 1,
			Source:         source, CorrelationID: correlationID,
		},
	}
	accepted.InitialTags = accepted.InitialTags.WithProvenance(tagging.InitialAssignmentProvenance{
		ActorType: accepted.EntryEvent.ActorType, ActorID: accepted.EntryEvent.ActorID,
		OccurredAt: accepted.EntryEvent.OccurredAt, CorrelationID: accepted.EntryEvent.CorrelationID,
	})
	return accepted, nil
}
