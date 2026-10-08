package projects

import (
	"context"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type AIWorkspaceTaskInput struct {
	ID    string
	Title string
}

type AIWorkspaceCreateCommand struct {
	Principal     authorization.Principal
	Target        scope.Target
	ProjectID     string
	DisplayID     string
	Name          string
	Tasks         []AIWorkspaceTaskInput
	ActorID       string
	Source        string
	CorrelationID string
	TagIDs        []string
}

type AIWorkspaceCreateResult struct {
	Project Project
	Tasks   []tasks.Task
}

type AIWorkspaceCreateMutation struct {
	Project     Project
	Tasks       []tasks.Task
	Audits      []mutation.AuditRecord
	Events      []mutation.EventRecord
	InitialTags tagging.InitialAssignmentSet
}

type AIWorkspaceRepository interface {
	CreateAIWorkspaceProjectAtomic(context.Context, AIWorkspaceCreateMutation) error
}

type AIWorkspaceService struct {
	repository AIWorkspaceRepository
	now        func() time.Time
	newID      func() string
	creation   *tagging.CreationPreparer
}

func NewAIWorkspaceService(
	repository AIWorkspaceRepository,
	now func() time.Time,
	newID func() string,
	creation *tagging.CreationPreparer,
) *AIWorkspaceService {
	return &AIWorkspaceService{repository: repository, now: now, newID: newID, creation: creation}
}

func (s *AIWorkspaceService) CreateProject(
	ctx context.Context,
	command AIWorkspaceCreateCommand,
) (AIWorkspaceCreateResult, error) {
	target := projectTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil ||
		target.MSPID == "" || target.ClientID == "" ||
		strings.TrimSpace(command.ProjectID) == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		len(command.Tasks) == 0 ||
		strings.TrimSpace(command.ActorID) == "" ||
		strings.TrimSpace(command.ActorID) != strings.TrimSpace(command.Principal.ID) ||
		strings.TrimSpace(command.Source) == "" {
		return AIWorkspaceCreateResult{}, ErrInvalidProject
	}
	if err := authorization.Authorize(command.Principal, "project.create", target); err != nil {
		return AIWorkspaceCreateResult{}, err
	}
	if err := authorization.Authorize(command.Principal, "task.create", target); err != nil {
		return AIWorkspaceCreateResult{}, err
	}
	initial, err := s.creation.Prepare(ctx, tagging.PrepareCreationCommand{
		MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectProject,
		TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		return AIWorkspaceCreateResult{}, err
	}

	now := s.now().UTC()
	project := Project{
		ID:             ProjectID(strings.TrimSpace(command.ProjectID)),
		MSPID:          target.MSPID,
		ClientID:       target.ClientID,
		DisplayID:      strings.TrimSpace(command.DisplayID),
		Name:           strings.TrimSpace(command.Name),
		LifecycleState: "planned",
		Version:        1,
		CreatedAt:      now,
		CreatedBy:      command.ActorID,
	}
	createdTasks := make([]tasks.Task, 0, len(command.Tasks))
	for index, input := range command.Tasks {
		if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Title) == "" {
			return AIWorkspaceCreateResult{}, ErrInvalidProject
		}
		createdTasks = append(createdTasks, tasks.Task{
			ID: input.ID, MSPID: target.MSPID, ClientID: target.ClientID,
			Parent: tasks.Ref{
				Type: tasks.ParentProject, ID: string(project.ID),
				MSPID: target.MSPID, ClientID: target.ClientID,
			},
			Title: strings.TrimSpace(input.Title), Status: "open",
			Position: index + 1, Version: 1, CreatedBy: command.ActorID,
		})
	}

	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = s.newID()
	}
	audits := make([]mutation.AuditRecord, 0, len(createdTasks)+1)
	events := make([]mutation.EventRecord, 0, len(createdTasks)+1)
	appendFacts := func(subjectType, subjectID, action, eventType string, version int64) {
		audits = append(audits, mutation.AuditRecord{
			ID: s.newID(), OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID, Action: action,
			SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: version,
			Source: command.Source, CorrelationID: correlationID,
		})
		events = append(events, mutation.EventRecord{
			EventID: s.newID(), EventType: eventType, SchemaVersion: 1,
			OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID,
			ActorType: "technician", ActorID: command.ActorID,
			SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: version,
			Source: command.Source, CorrelationID: correlationID,
		})
	}
	appendFacts("project", string(project.ID), "project.created", "project.created", project.Version)
	for _, task := range createdTasks {
		appendFacts("task", task.ID, "task.created", "task.created", task.Version)
	}
	accepted := AIWorkspaceCreateMutation{
		Project: project, Tasks: createdTasks, Audits: audits, Events: events,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{
			ActorType: "technician", ActorID: command.ActorID, OccurredAt: now,
			CorrelationID: correlationID,
		}),
	}
	if err := s.repository.CreateAIWorkspaceProjectAtomic(ctx, accepted); err != nil {
		return AIWorkspaceCreateResult{}, err
	}
	return AIWorkspaceCreateResult{Project: project, Tasks: createdTasks}, nil
}
