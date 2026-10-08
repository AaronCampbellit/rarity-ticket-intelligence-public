package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type aiWorkspaceRepositoryStub struct {
	accepted AIWorkspaceCreateMutation
}

type aiWorkspaceClassificationRepository struct{}

func (aiWorkspaceClassificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (aiWorkspaceClassificationRepository) FindUnclassified(context.Context, string) (tagging.Tag, error) {
	return tagging.Tag{}, scope.ErrNotFound
}

func aiWorkspaceCreationPreparer() *tagging.CreationPreparer {
	return tagging.NewCreationPreparer(aiWorkspaceClassificationRepository{})
}

func (s *aiWorkspaceRepositoryStub) CreateAIWorkspaceProjectAtomic(
	_ context.Context,
	accepted AIWorkspaceCreateMutation,
) error {
	s.accepted = accepted
	return nil
}

func TestAIWorkspaceProjectCreationUsesOnlySuppliedBusinessValues(t *testing.T) {
	repository := &aiWorkspaceRepositoryStub{}
	now := time.Date(2026, time.August, 4, 18, 30, 0, 0, time.UTC)
	nextID := 0
	service := NewAIWorkspaceService(
		repository,
		func() time.Time { return now },
		func() string {
			nextID++
			return "fact-" + string(rune('0'+nextID))
		},
		aiWorkspaceCreationPreparer(),
	)
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet(
			"project.create",
			"task.create",
		),
	}

	result, err := service.CreateProject(context.Background(), AIWorkspaceCreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		ProjectID: "project-1",
		DisplayID: "PRJ-0001",
		Name:      "Onboarding",
		Tasks: []AIWorkspaceTaskInput{
			{ID: "task-1", Title: "A"},
			{ID: "task-2", Title: "B"},
			{ID: "task-3", Title: "C"},
		},
		ActorID:       "technician-1",
		Source:        "ai_workspace",
		CorrelationID: "correlation-1",
		TagIDs:        []string{"tag-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Project.Name != "Onboarding" ||
		result.Project.OriginalProposalVersionID != "" ||
		result.Project.LifecycleState != "planned" ||
		!result.Project.PlannedStart.IsZero() ||
		!result.Project.PlannedEnd.IsZero() {
		t.Fatalf("project=%+v", result.Project)
	}
	if len(result.Tasks) != 3 {
		t.Fatalf("tasks=%+v", result.Tasks)
	}
	for index, task := range result.Tasks {
		if task.Title != string(rune('A'+index)) ||
			task.OwnerID != "" ||
			task.EstimateMinutes != 0 ||
			task.Status != "open" ||
			task.Position != index+1 {
			t.Fatalf("task[%d]=%+v", index, task)
		}
	}
	if repository.accepted.Project.ID != "project-1" ||
		len(repository.accepted.Audits) != 4 ||
		len(repository.accepted.Events) != 4 {
		t.Fatalf("accepted=%+v", repository.accepted)
	}
}

func TestAIWorkspaceProjectCreationAllowsExplicitSameMSPTargetForGlobalPrincipal(t *testing.T) {
	repository := &aiWorkspaceRepositoryStub{}
	service := NewAIWorkspaceService(repository, time.Now, func() string { return "fact" }, aiWorkspaceCreationPreparer())
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(
			"project.create",
			"task.create",
		),
	}

	result, err := service.CreateProject(context.Background(), AIWorkspaceCreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-2"},
		ProjectID: "project-1",
		DisplayID: "PRJ-0001",
		Name:      "Onboarding",
		Tasks:     []AIWorkspaceTaskInput{{ID: "task-1", Title: "A"}},
		ActorID:   "technician-1",
		Source:    "ai_workspace",
		TagIDs:    []string{"tag-1"},
	})

	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if result.Project.ClientID != "client-2" ||
		repository.accepted.Project.ClientID != "client-2" ||
		repository.accepted.Tasks[0].ClientID != "client-2" {
		t.Fatalf("result=%+v accepted=%+v", result, repository.accepted)
	}
}

func TestAIWorkspaceProjectCreationRejectsExplicitClientMismatchForClientPrincipal(t *testing.T) {
	repository := &aiWorkspaceRepositoryStub{}
	service := NewAIWorkspaceService(repository, time.Now, func() string { return "fact" }, aiWorkspaceCreationPreparer())
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet(
			"project.create",
			"task.create",
		),
	}

	_, err := service.CreateProject(context.Background(), AIWorkspaceCreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-2"},
		ProjectID: "project-1",
		DisplayID: "PRJ-0001",
		Name:      "Onboarding",
		Tasks:     []AIWorkspaceTaskInput{{ID: "task-1", Title: "A"}},
		ActorID:   "technician-1",
		Source:    "ai_workspace",
	})

	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("CreateProject() error = %v, want scope ErrNotFound", err)
	}
	if repository.accepted.Project.ID != "" {
		t.Fatalf("repository received mutation=%+v", repository.accepted)
	}
}

func TestAIWorkspaceProjectCreationRequiresBothCapabilitiesBeforeWriting(t *testing.T) {
	repository := &aiWorkspaceRepositoryStub{}
	service := NewAIWorkspaceService(repository, time.Now, func() string { return "fact" }, aiWorkspaceCreationPreparer())
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet("project.create"),
	}

	_, err := service.CreateProject(context.Background(), AIWorkspaceCreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		ProjectID: "project-1",
		DisplayID: "PRJ-0001",
		Name:      "Onboarding",
		Tasks:     []AIWorkspaceTaskInput{{ID: "task-1", Title: "A"}},
		ActorID:   "technician-1",
		Source:    "ai_workspace",
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
	if repository.accepted.Project.ID != "" {
		t.Fatalf("repository received mutation=%+v", repository.accepted)
	}
}

func TestAIWorkspaceProjectCreationRejectsSpoofedAuditActor(t *testing.T) {
	repository := &aiWorkspaceRepositoryStub{}
	service := NewAIWorkspaceService(repository, time.Now, func() string { return "fact" }, aiWorkspaceCreationPreparer())
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet(
			"project.create",
			"task.create",
		),
	}

	_, err := service.CreateProject(context.Background(), AIWorkspaceCreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		ProjectID: "project-1",
		DisplayID: "PRJ-0001",
		Name:      "Onboarding",
		Tasks:     []AIWorkspaceTaskInput{{ID: "task-1", Title: "A"}},
		ActorID:   "another-technician",
		Source:    "ai_workspace",
	})
	if !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("error=%v", err)
	}
	if repository.accepted.Project.ID != "" {
		t.Fatalf("repository received mutation=%+v", repository.accepted)
	}
}
