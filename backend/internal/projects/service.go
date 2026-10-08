package projects

// Future Project terminal lifecycle mutations must depend on
// tagging.TerminalGuard; this package intentionally has no general terminal API.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

var (
	ErrInvalidProject     = errors.New("invalid project")
	ErrAmbiguousReference = errors.New("ambiguous project reference")
	ErrVersionConflict    = object.ErrVersionConflict
)

type PhaseInput struct {
	Name               string
	OwnerID            string
	ParticipatingTeams []string
	PlannedStart       time.Time
	PlannedEnd         time.Time
	PlannedMinutes     int64
	Budget             Money
	Deliverables       []string
	CompletionCriteria []string
}

type CreateCommand struct {
	Principal                 authorization.Principal
	Target                    scope.Target
	DisplayID                 string
	Name                      string
	OriginalProposalVersionID string
	PlannedStart              time.Time
	PlannedEnd                time.Time
	Phases                    []PhaseInput
	ActorID                   string
	Source                    string
	TagIDs                    []string
	ClassificationPolicy      tagging.CreationPolicy
}

type UpdatePhaseCommand struct {
	Principal          authorization.Principal
	Target             scope.Target
	ID                 PhaseID
	ExpectedVersion    int64
	Name               string
	OwnerID            string
	ParticipatingTeams []string
	PlannedStart       time.Time
	PlannedEnd         time.Time
	PlannedMinutes     int64
	Budget             Money
	Deliverables       []string
	CompletionCriteria []string
	ActorID            string
	Source             string
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

func (s *Service) Create(ctx context.Context, command CreateCommand) (Project, error) {
	target := projectTarget(command.Principal, command.Target)
	if s == nil || s.repository == nil || s.now == nil || s.newID == nil || s.creation == nil || target.ClientID == "" ||
		strings.TrimSpace(command.DisplayID) == "" ||
		strings.TrimSpace(command.Name) == "" ||
		command.OriginalProposalVersionID == "" ||
		len(command.Phases) == 0 ||
		command.ActorID == "" || command.Source == "" ||
		(!command.PlannedEnd.IsZero() && command.PlannedEnd.Before(command.PlannedStart)) {
		return Project{}, ErrInvalidProject
	}
	if err := authorization.Authorize(command.Principal, "project.create", target); err != nil {
		return Project{}, err
	}
	initial, err := s.initialTags(ctx, target, command)
	if err != nil {
		return Project{}, err
	}
	now := s.now().UTC()
	projectID := ProjectID(s.newID())
	phases := make([]Phase, 0, len(command.Phases))
	for index, input := range command.Phases {
		if strings.TrimSpace(input.Name) == "" ||
			input.PlannedMinutes < 0 || input.Budget.Minor < 0 ||
			(!input.PlannedEnd.IsZero() && input.PlannedEnd.Before(input.PlannedStart)) {
			return Project{}, ErrInvalidProject
		}
		phases = append(phases, Phase{
			ID: PhaseID(s.newID()), ProjectID: projectID,
			MSPID: target.MSPID, ClientID: target.ClientID,
			Position: index + 1, Name: strings.TrimSpace(input.Name), State: "planned",
			OwnerID: input.OwnerID, ParticipatingTeams: append([]string(nil), input.ParticipatingTeams...),
			PlannedStart: input.PlannedStart, PlannedEnd: input.PlannedEnd,
			PlannedMinutes: input.PlannedMinutes, Budget: input.Budget,
			Deliverables:       append([]string(nil), input.Deliverables...),
			CompletionCriteria: append([]string(nil), input.CompletionCriteria...),
			Version:            1,
		})
	}
	project := Project{
		ID: projectID, MSPID: target.MSPID, ClientID: target.ClientID,
		DisplayID: strings.TrimSpace(command.DisplayID), Name: strings.TrimSpace(command.Name),
		OriginalProposalVersionID: command.OriginalProposalVersionID,
		LifecycleState:            "planned", PlannedStart: command.PlannedStart, PlannedEnd: command.PlannedEnd,
		Version: 1, Phases: phases, SupportsMilestones: false,
		SupportsTaskDependencies: false, CreatedAt: now, CreatedBy: command.ActorID,
	}
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := CreateMutation{
		Project:     project,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: "technician", ActorID: command.ActorID, OccurredAt: now, CorrelationID: correlationID}),
		Audit:       mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, Action: "project.created", SubjectType: "project", SubjectID: string(project.ID), SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
		Event:       mutation.EventRecord{EventID: eventID, EventType: "project.created", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, SubjectType: "project", SubjectID: string(project.ID), SubjectVersion: 1, Source: command.Source, CorrelationID: correlationID},
	}
	if err := s.repository.CreateAtomic(ctx, accepted); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Service) initialTags(ctx context.Context, target scope.Target, command CreateCommand) (tagging.InitialAssignmentSet, error) {
	return s.creation.Prepare(ctx, tagging.PrepareCreationCommand{MSPID: target.MSPID, ClientID: target.ClientID, ObjectType: tagging.ObjectProject, TagIDs: command.TagIDs, Source: tagging.SourceHuman, Policy: command.ClassificationPolicy})
}

func (s *Service) UpdatePhase(ctx context.Context, command UpdatePhaseCommand) (Phase, error) {
	target := projectTarget(command.Principal, command.Target)
	if target.ClientID == "" || command.ID == "" || command.ExpectedVersion < 1 ||
		strings.TrimSpace(command.Name) == "" || command.PlannedMinutes < 0 ||
		command.Budget.Minor < 0 || command.ActorID == "" || command.Source == "" ||
		(!command.PlannedEnd.IsZero() && command.PlannedEnd.Before(command.PlannedStart)) {
		return Phase{}, ErrInvalidProject
	}
	if err := authorization.Authorize(command.Principal, "project.edit", target); err != nil {
		return Phase{}, err
	}
	phase, err := s.repository.FindPhase(ctx, target, command.ID)
	if err != nil {
		return Phase{}, err
	}
	if phase.MSPID != target.MSPID || phase.ClientID != target.ClientID {
		return Phase{}, scope.ErrNotFound
	}
	if err := object.RequireVersion(phase.Version, command.ExpectedVersion); err != nil {
		return Phase{}, err
	}
	phase.Name = strings.TrimSpace(command.Name)
	phase.OwnerID = command.OwnerID
	phase.ParticipatingTeams = append([]string(nil), command.ParticipatingTeams...)
	phase.PlannedStart, phase.PlannedEnd = command.PlannedStart, command.PlannedEnd
	phase.PlannedMinutes, phase.Budget = command.PlannedMinutes, command.Budget
	phase.Deliverables = append([]string(nil), command.Deliverables...)
	phase.CompletionCriteria = append([]string(nil), command.CompletionCriteria...)
	phase.Version++
	now := s.now().UTC()
	auditID, eventID, correlationID := s.newID(), s.newID(), s.newID()
	accepted := PhaseMutation{
		Phase: phase,
		Audit: mutation.AuditRecord{ID: auditID, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, Action: "phase.updated", SubjectType: "phase", SubjectID: string(phase.ID), SubjectVersion: phase.Version, Source: command.Source, CorrelationID: correlationID},
		Event: mutation.EventRecord{EventID: eventID, EventType: "phase.updated", SchemaVersion: 1, OccurredAt: now, MSPID: target.MSPID, ClientID: target.ClientID, ActorType: "technician", ActorID: command.ActorID, SubjectType: "phase", SubjectID: string(phase.ID), SubjectVersion: phase.Version, Source: command.Source, CorrelationID: correlationID},
	}
	if err := s.repository.UpdatePhaseAtomic(ctx, accepted); err != nil {
		return Phase{}, err
	}
	return phase, nil
}

func projectTarget(principal authorization.Principal, target scope.Target) scope.Target {
	if target.MSPID == "" {
		return scope.Target{MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID}
	}
	return target
}
