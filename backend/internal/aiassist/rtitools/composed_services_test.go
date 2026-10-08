package rtitools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
)

type composedProposalStore struct {
	proposal aiassist.ActionProposal
}

type composedClassificationRepository struct{}

func (composedClassificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (composedClassificationRepository) FindUnclassified(context.Context, string) (tagging.Tag, error) {
	return tagging.Tag{}, scope.ErrNotFound
}
func (composedClassificationRepository) ResolveTaskProject(_ context.Context, mspID, clientID, _, parentID string) (tagging.TargetRef, error) {
	return tagging.TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: tagging.ObjectProject, ObjectID: parentID}, nil
}
func (composedClassificationRepository) Get(context.Context, tagging.TargetRef) (tagging.TaggedObject, error) {
	return tagging.TaggedObject{}, nil
}

type composedTargetAuthorizer struct {
	err error
}

func (a *composedTargetAuthorizer) AuthorizeExecutionTarget(
	context.Context,
	authorization.Principal,
	scope.Target,
) error {
	return a.err
}

func (s *composedProposalStore) CreateProposal(
	_ context.Context,
	proposal aiassist.ActionProposal,
) error {
	s.proposal = proposal
	return nil
}

func (s *composedProposalStore) GetProposal(
	_ context.Context,
	_ scope.Target,
	_, _ string,
) (aiassist.ActionProposal, error) {
	return s.proposal, nil
}

func (s *composedProposalStore) ConfirmProposal(
	_ context.Context,
	_ scope.Target,
	_, _ string,
	_ int64,
	at time.Time,
) (aiassist.ActionProposal, error) {
	s.proposal.State = aiassist.ProposalConfirmed
	s.proposal.ConfirmedAt = &at
	s.proposal.Version++
	return s.proposal, nil
}

func (s *composedProposalStore) CompleteProposal(
	_ context.Context,
	_ scope.Target,
	_ string,
	_ int64,
	result aiassist.ToolResult,
	_ time.Time,
) error {
	s.proposal.Result = &result
	return nil
}

func (*composedProposalStore) FailProposal(
	context.Context,
	scope.Target,
	string,
	int64,
	string,
	time.Time,
) error {
	return nil
}

func (s *composedProposalStore) RejectProposal(
	_ context.Context,
	_ scope.Target,
	_, _ string,
	_ int64,
	at time.Time,
) error {
	s.proposal.State = aiassist.ProposalRejected
	s.proposal.RejectedAt = &at
	s.proposal.Version++
	return nil
}

func (*composedProposalStore) ExpireProposal(
	context.Context,
	scope.Target,
	string,
	string,
	int64,
	time.Time,
) error {
	return nil
}

type composedProjectRepository struct {
	mutation projects.AIWorkspaceCreateMutation
}

type composedClientResourceRepository struct {
	mutation clientresources.CreateMutation
}

func (r *composedClientResourceRepository) CreateAtomic(
	_ context.Context,
	mutation clientresources.CreateMutation,
) error {
	r.mutation = mutation
	return nil
}

func (r *composedProjectRepository) CreateAIWorkspaceProjectAtomic(
	_ context.Context,
	mutation projects.AIWorkspaceCreateMutation,
) error {
	r.mutation = mutation
	return nil
}

func TestMSPGlobalRegistryConfirmationExecutesComposedProjectService(t *testing.T) {
	now := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	repository := &composedProjectRepository{}
	factID := 0
	service := projects.NewAIWorkspaceService(
		repository,
		func() time.Time { return now },
		func() string {
			factID++
			return "fact-id"
		},
		tagging.NewCreationPreparer(composedClassificationRepository{}),
	)
	preparedIDs := []string{"project01", "task-1"}
	tool := NewProjectCreateTool(service, func() string {
		value := preparedIDs[0]
		preparedIDs = preparedIDs[1:]
		return value
	})
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(
			"project.create",
			"task.create",
		),
	}
	store := &composedProposalStore{}
	registryIDs := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool},
		store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		func() time.Time { return now },
		func() string {
			value := registryIDs[0]
			registryIDs = registryIDs[1:]
			return value
		},
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(
		context.Background(),
		principal,
		aiassist.ToolRequest{
			Name:           "project.create",
			ConversationID: "conversation-1",
			Input: json.RawMessage(
				`{"client_id":"client-2","name":"Onboarding","tasks":["Verify handoff"],"tag_ids":["tag-1"]}`,
			),
		},
	)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if proposal.ClientID != "" || proposal.TargetClientID != "client-2" {
		t.Fatalf("proposal access client=%q target client=%q", proposal.ClientID, proposal.TargetClientID)
	}

	if _, err := registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if repository.mutation.Project.ClientID != "client-2" ||
		len(repository.mutation.Tasks) != 1 ||
		repository.mutation.Tasks[0].ClientID != "client-2" {
		t.Fatalf("composed mutation = %+v", repository.mutation)
	}
}

type composedProjectGetter struct {
	target    scope.Target
	workspace projects.ProjectWorkspace
}

func (g *composedProjectGetter) Get(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	_ projects.ProjectID,
) (projects.ProjectWorkspace, error) {
	g.target = target
	return g.workspace, nil
}

type composedTaskRepository struct {
	listTarget scope.Target
	listParent tasks.Ref
	mutation   tasks.CreateMutation
}

func (*composedTaskRepository) Find(
	context.Context,
	scope.Target,
	string,
) (tasks.Task, error) {
	return tasks.Task{}, tasks.ErrNotFound
}

func (r *composedTaskRepository) ListForParent(
	_ context.Context,
	target scope.Target,
	parent tasks.Ref,
	_ string,
) ([]tasks.Task, error) {
	r.listTarget = target
	r.listParent = parent
	return nil, nil
}

func (r *composedTaskRepository) CreateAtomic(
	_ context.Context,
	mutation tasks.CreateMutation,
) error {
	r.mutation = mutation
	return nil
}

func (*composedTaskRepository) LoadSelected(
	context.Context,
	tasks.Ref,
	[]tasks.ID,
) ([]tasks.Task, error) {
	return nil, nil
}

func (*composedTaskRepository) MoveAtomic(
	context.Context,
	tasks.MoveMutation,
) error {
	return nil
}

func TestMSPGlobalRegistryConfirmationExecutesComposedTaskService(t *testing.T) {
	now := time.Date(2026, time.August, 5, 8, 15, 0, 0, time.UTC)
	repository := &composedTaskRepository{}
	serviceIDs := []string{"task-1", "audit-1", "event-1"}
	service := tasks.NewService(
		repository,
		func() time.Time { return now },
		func() string {
			value := serviceIDs[0]
			serviceIDs = serviceIDs[1:]
			return value
		},
		tagging.NewCreationPreparer(composedClassificationRepository{}),
	)
	projectGetter := &composedProjectGetter{workspace: projects.ProjectWorkspace{
		ID: "project-1", DisplayID: "PRJ-0001", Name: "Onboarding", Version: 4,
	}}
	tool := NewProjectTaskCreateTool(projectGetter, service)
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("task.create", "project.read"),
	}
	store := &composedProposalStore{}
	registryIDs := []string{"proposal-2", "correlation-2"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool},
		store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		func() time.Time { return now },
		func() string {
			value := registryIDs[0]
			registryIDs = registryIDs[1:]
			return value
		},
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(
		context.Background(),
		principal,
		aiassist.ToolRequest{
			Name:           "task.create",
			ConversationID: "conversation-2",
			Input: json.RawMessage(
				`{"client_id":"client-2","project_id":"project-1","project_ref":"PRJ-0001","title":"Verify recovery","tag_ids":["tag-1"]}`,
			),
		},
	)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if proposal.ClientID != "" || proposal.TargetClientID != "client-2" {
		t.Fatalf("proposal access client=%q target client=%q", proposal.ClientID, proposal.TargetClientID)
	}

	if _, err := registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if projectGetter.target != (scope.Target{MSPID: "msp-1", ClientID: "client-2"}) ||
		repository.listTarget != projectGetter.target ||
		repository.listParent.ClientID != "client-2" ||
		repository.mutation.Task.ClientID != "client-2" {
		t.Fatalf(
			"project target=%+v list target=%+v parent=%+v mutation=%+v",
			projectGetter.target,
			repository.listTarget,
			repository.listParent,
			repository.mutation,
		)
	}
}

func TestMSPGlobalRegistryConfirmationExecutesComposedLocationService(t *testing.T) {
	now := time.Date(2026, time.August, 5, 21, 0, 0, 0, time.UTC)
	repository := &composedClientResourceRepository{}
	service := clientresources.NewService(repository, func() time.Time { return now }, func() string { return "audit-or-event" })
	directory := activeResourceDirectory()
	tool := NewLocationCreateTool(directory, service, func() string { return resourceID() })
	principal := authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("location.create", "organization.read"),
	}
	store := &composedProposalStore{}
	registryIDs := []string{"proposal-location", correlationID()}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now },
		func() string { value := registryIDs[0]; registryIDs = registryIDs[1:]; return value },
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "location.create", ConversationID: "conversation-location",
		Input: json.RawMessage(`{"client":"Northwind Legal","display_id":"LOC-2","name":"Branch Office"}`),
	})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if repository.mutation.Object.ID != resourceID() || repository.mutation.Object.ClientID != "client-1" ||
		repository.mutation.Audit.Source != "ai_workspace" || repository.mutation.Audit.CorrelationID != correlationID() ||
		repository.mutation.Event.CorrelationID != correlationID() {
		t.Fatalf("mutation=%+v", repository.mutation)
	}
}
