// Package timeentries owns timer and manual labor records without applying
// billing-rounding policy at capture time.
package timeentries

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var ErrInvalid = errors.New("invalid time entry")

type Entry struct {
	ID                 string
	MSPID              string
	ClientID           string
	WorkRecordID       string
	TaskID             string
	TechnicianID       string
	StartedAt          time.Time
	EndedAt            time.Time
	DurationSeconds    int64
	Billable           bool
	Note               string
	LaborRoleVersionID string
	InternalCostMinor  int64
	BillRateMinor      int64
	RateCurrency       string
	Version            int64
	CreatedAt          time.Time
	CreatedBy          string
}

type CreateCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	WorkRecordID         string
	TaskID               string
	TechnicianID         string
	StartedAt            time.Time
	EndedAt              time.Time
	Billable             bool
	Note                 string
	ActorID              string
	Source               string
	TagIDs               []string
	ClassificationPolicy tagging.CreationPolicy
}

type CreateMutation struct {
	Entry       Entry
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type Repository interface {
	CreateAtomic(context.Context, CreateMutation) error
}

type Service struct {
	repository Repository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewService(repository Repository, now func() time.Time, newID func() string, creation *tagging.CreationPreparer) *Service {
	return &Service{repository: repository, now: now, newID: newID, creation: creation}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Entry, error) {
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil ||
		(command.WorkRecordID == "" && command.TaskID == "") ||
		command.TechnicianID == "" ||
		command.ActorID == "" ||
		command.Source == "" ||
		command.StartedAt.IsZero() ||
		!command.EndedAt.After(command.StartedAt) {
		return Entry{}, ErrInvalid
	}
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if target.ClientID == "" {
		return Entry{}, ErrInvalid
	}
	if err := authorization.Authorize(command.Principal, "time_entry.create", target); err != nil {
		return Entry{}, err
	}
	initial, err := s.initialTags(ctx, target, command)
	if err != nil {
		return Entry{}, err
	}
	now := s.now().UTC()
	entryID := s.newID()
	auditID := s.newID()
	eventID := s.newID()
	correlationID := s.newID()
	entry := Entry{
		ID: entryID, MSPID: target.MSPID, ClientID: target.ClientID,
		WorkRecordID: command.WorkRecordID, TaskID: command.TaskID,
		TechnicianID: command.TechnicianID,
		StartedAt:    command.StartedAt.UTC(), EndedAt: command.EndedAt.UTC(),
		DurationSeconds: int64(command.EndedAt.Sub(command.StartedAt) / time.Second),
		Billable:        command.Billable, Note: strings.TrimSpace(command.Note),
		Version: 1, CreatedAt: now, CreatedBy: command.ActorID,
	}
	if entry.DurationSeconds < 1 {
		return Entry{}, ErrInvalid
	}
	accepted := CreateMutation{
		Entry:       entry,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: "technician", ActorID: command.ActorID, OccurredAt: now, CorrelationID: correlationID}),
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			Action: "time_entry.created", SubjectType: "time_entry",
			SubjectID: entry.ID, SubjectVersion: entry.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "time_entry.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "time_entry", SubjectID: entry.ID,
			SubjectVersion: entry.Version, Source: command.Source,
			CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Service) initialTags(ctx context.Context, target scope.Target, command CreateCommand) (tagging.InitialAssignmentSet, error) {
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTimeEntry, TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy})
}
