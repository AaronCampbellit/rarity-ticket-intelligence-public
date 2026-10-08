// Package tasks implements ordered work-record tasks and subtasks. The model
// deliberately has no dependency graph. A future terminal Task mutation must
// depend on tagging.TerminalGuard and require meaningful classification first.
package tasks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var (
	ErrInvalid       = errors.New("invalid task")
	ErrInvalidParent = errors.New("invalid parent task")
	ErrNotFound      = errors.New("task not found")
)

type Task struct {
	ID                                 string                   `json:"id"`
	MSPID                              string                   `json:"msp_id"`
	ClientID                           string                   `json:"client_id"`
	Parent                             Ref                      `json:"parent"`
	WorkRecordID                       string                   `json:"work_record_id,omitempty"`
	ParentTaskID                       string                   `json:"parent_task_id,omitempty"`
	Title                              string                   `json:"title"`
	Status                             string                   `json:"status"`
	Position                           int                      `json:"position"`
	Version                            int64                    `json:"version"`
	CreatedBy                          string                   `json:"created_by"`
	OwnerID                            string                   `json:"owner_id,omitempty"`
	EstimateMinutes                    int                      `json:"estimate_minutes"`
	ScheduledStartsAt, ScheduledEndsAt *time.Time               `json:"-"`
	ScheduleTimezone                   string                   `json:"schedule_timezone,omitempty"`
	SchedulingMode                     calendar.SchedulingMode  `json:"scheduling_mode,omitempty"`
	DueOn                              *time.Time               `json:"due_on,omitempty"`
	ScheduleRecurrence                 *calendar.RecurrenceRule `json:"-"`
}

type ScheduleMutation struct {
	TaskID, MSPID, ClientID                      string
	ExpectedVersion                              int64
	Interval                                     calendar.TypedInterval
	Mode                                         calendar.SchedulingMode
	EstimateMinutes                              int64
	Recurrence                                   *calendar.RecurrenceRule
	OccurrenceKey, OccurrenceScope, ProjectionID string
}

func ValidateScheduleMutation(principal authorization.Principal, accepted ScheduleMutation) error {
	if accepted.TaskID == "" || accepted.MSPID == "" || accepted.ClientID == "" || accepted.ExpectedVersion < 1 || accepted.Mode == calendar.Informational || accepted.EstimateMinutes <= 0 {
		return ErrInvalid
	}
	if err := authorization.Authorize(principal, "task.edit", scope.Target{MSPID: accepted.MSPID, ClientID: accepted.ClientID}); err != nil {
		return err
	}
	if err := accepted.Interval.Validate(false); err != nil || accepted.Interval.Empty() {
		return ErrInvalid
	}
	if accepted.Recurrence != nil && accepted.Recurrence.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

type CreateCommand struct {
	Principal            authorization.Principal
	Target               scope.Target
	Parent               Ref
	WorkRecordID         string
	ParentTaskID         string
	Title                string
	OwnerID              string
	EstimateMinutes      int
	ActorID              string
	Source               string
	CorrelationID        string
	TagIDs               []string
	ClassificationPolicy tagging.CreationPolicy
}

type CreateMutation struct {
	Task        Task
	Audit       mutation.AuditRecord
	Event       mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type Repository interface {
	Find(context.Context, scope.Target, string) (Task, error)
	ListForParent(context.Context, scope.Target, Ref, string) ([]Task, error)
	CreateAtomic(context.Context, CreateMutation) error
	LoadSelected(context.Context, Ref, []ID) ([]Task, error)
	MoveAtomic(context.Context, MoveMutation) error
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

func (s *Service) ListOpportunityTasks(
	ctx context.Context,
	principal authorization.Principal,
	opportunityID string,
) ([]Task, error) {
	if principal.Scope.MSPID == "" || principal.Scope.ClientID == "" ||
		strings.TrimSpace(opportunityID) == "" {
		return nil, ErrInvalid
	}
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	if err := authorization.Authorize(principal, "opportunity.read", target); err != nil {
		return nil, err
	}
	return s.repository.ListForParent(ctx, target, Ref{
		Type: ParentOpportunity, ID: strings.TrimSpace(opportunityID),
		MSPID: target.MSPID, ClientID: target.ClientID,
	}, "")
}

func (s *Service) Get(ctx context.Context, principal authorization.Principal, id string) (Task, error) {
	target := scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	if s == nil || s.repository == nil || target.MSPID == "" || target.ClientID == "" || strings.TrimSpace(id) == "" {
		return Task{}, ErrInvalid
	}
	found, err := s.repository.Find(ctx, target, strings.TrimSpace(id))
	if err != nil {
		return Task{}, err
	}
	capability := map[ParentType]string{
		ParentWorkRecord: "work_record.read", ParentOpportunity: "opportunity.read",
		ParentProject: "project.read", ParentPhase: "project.read",
	}[found.Parent.Type]
	if capability == "" {
		return Task{}, ErrInvalidParent
	}
	if err := authorization.Authorize(principal, capability, target); err != nil {
		return Task{}, err
	}
	return found, nil
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Task, error) {
	parent := command.Parent
	if parent.ID == "" && command.WorkRecordID != "" {
		parent = Ref{Type: ParentWorkRecord, ID: command.WorkRecordID}
	}
	target := command.Target
	if target.MSPID == "" && target.ClientID == "" {
		target = scope.Target{
			MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID,
		}
	}
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil ||
		command.Principal.Scope.MSPID == "" ||
		target.MSPID == "" || target.ClientID == "" ||
		(parent.Type != ParentWorkRecord && parent.Type != ParentOpportunity &&
			parent.Type != ParentProject && parent.Type != ParentPhase) ||
		parent.ID == "" ||
		strings.TrimSpace(command.Title) == "" ||
		command.EstimateMinutes < 0 ||
		command.ActorID == "" ||
		command.Source == "" {
		return Task{}, ErrInvalid
	}
	parent.MSPID, parent.ClientID = target.MSPID, target.ClientID
	if err := authorization.Authorize(command.Principal, "task.create", target); err != nil {
		return Task{}, err
	}
	initial, err := s.initialTags(ctx, target, parent, command)
	if err != nil {
		return Task{}, err
	}
	if command.ParentTaskID != "" {
		parentTask, err := s.repository.Find(ctx, target, command.ParentTaskID)
		if err != nil {
			return Task{}, err
		}
		if parentTask.Parent != parent ||
			parentTask.MSPID != target.MSPID ||
			parentTask.ClientID != target.ClientID {
			return Task{}, scope.ErrNotFound
		}
	}
	siblings, err := s.repository.ListForParent(
		ctx, target, parent, command.ParentTaskID,
	)
	if err != nil {
		return Task{}, err
	}
	position := 1
	for _, sibling := range siblings {
		if sibling.Position >= position {
			position = sibling.Position + 1
		}
	}
	now := s.now().UTC()
	taskID := s.newID()
	auditID := s.newID()
	eventID := s.newID()
	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = s.newID()
	}
	task := Task{
		ID: taskID, MSPID: target.MSPID, ClientID: target.ClientID,
		Parent: parent, ParentTaskID: command.ParentTaskID,
		Title: strings.TrimSpace(command.Title), Status: "open",
		Position: position, Version: 1, CreatedBy: command.ActorID,
		OwnerID:         strings.TrimSpace(command.OwnerID),
		EstimateMinutes: command.EstimateMinutes,
	}
	if parent.Type == ParentWorkRecord {
		task.WorkRecordID = parent.ID
	}
	accepted := CreateMutation{
		Task:        task,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: "technician", ActorID: command.ActorID, OccurredAt: now, CorrelationID: correlationID}),
		Audit: mutation.AuditRecord{
			ID: auditID, OccurredAt: now, MSPID: task.MSPID, ClientID: task.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: "task.created",
			SubjectType: "task", SubjectID: task.ID, SubjectVersion: task.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
		Event: mutation.EventRecord{
			EventID: eventID, EventType: "task.created", SchemaVersion: 1,
			OccurredAt: now, MSPID: task.MSPID, ClientID: task.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: "task", SubjectID: task.ID, SubjectVersion: task.Version,
			Source: command.Source, CorrelationID: correlationID,
		},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) initialTags(ctx context.Context, target scope.Target, parent Ref, command CreateCommand) (tagging.InitialAssignmentSet, error) {
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectTask, TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy, ParentType: string(parent.Type), ParentID: parent.ID})
}
