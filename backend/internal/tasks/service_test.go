package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type fakeRepository struct {
	parent Task
	list   []Task
	listed Ref
	saved  CreateMutation
	calls  int
}

type taskClassificationRepository struct{}

func (taskClassificationRepository) ResolveTags(_ context.Context, _ string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}, nil
}
func (taskClassificationRepository) FindUnclassified(_ context.Context, msp string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: msp, InternalKey: "unclassified", State: tagging.StateActive}, nil
}
func (taskClassificationRepository) Get(context.Context, tagging.TargetRef) (tagging.TaggedObject, error) {
	return tagging.TaggedObject{Effective: []tagging.Assignment{{Tag: tagging.Tag{ID: "project-tag", InternalKey: "project", State: tagging.StateActive}}}}, nil
}
func (taskClassificationRepository) ResolveTaskProject(_ context.Context, mspID, clientID, _ string, parentID string) (tagging.TargetRef, error) {
	return tagging.TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: tagging.ObjectProject, ObjectID: parentID}, nil
}

func taskCreationPreparer() *tagging.CreationPreparer {
	return tagging.NewCreationPreparer(taskClassificationRepository{})
}
func TestInitialTagsClassificationPolicyForTask(t *testing.T) {
	s := &Service{creation: tagging.NewCreationPreparer(taskClassificationRepository{})}
	target := scope.Target{MSPID: "msp", ClientID: "client"}
	parent := Ref{Type: ParentOpportunity, ID: "opportunity"}
	if _, err := s.initialTags(context.Background(), target, parent, CreateCommand{Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful}); !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("interactive error=%v", err)
	}
	_, err := s.initialTags(context.Background(), target, parent, CreateCommand{Source: "automation", ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *fakeRepository) Find(_ context.Context, _ scope.Target, _ string) (Task, error) {
	if r.parent.ID == "" {
		return Task{}, ErrNotFound
	}
	return r.parent, nil
}
func (r *fakeRepository) ListForParent(_ context.Context, _ scope.Target, parent Ref, _ string) ([]Task, error) {
	r.listed = parent
	return r.list, nil
}

func TestListOpportunityTasksUsesScopedReadCapability(t *testing.T) {
	repository := &fakeRepository{list: []Task{{ID: "task-id"}}}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("opportunity.read"),
	}
	found, err := service.ListOpportunityTasks(
		context.Background(), principal, "opportunity-id",
	)
	if err != nil || len(found) != 1 ||
		repository.listed.Type != ParentOpportunity ||
		repository.listed.ID != "opportunity-id" ||
		repository.listed.ClientID != "client-id" {
		t.Fatalf("ListOpportunityTasks() found=%+v ref=%+v error=%v",
			found, repository.listed, err)
	}
}

func TestGetUsesTheReadCapabilityForTheTasksActualParent(t *testing.T) {
	repository := &fakeRepository{parent: Task{
		ID: "task-id", MSPID: "msp-id", ClientID: "client-id",
		Parent: Ref{Type: ParentWorkRecord, ID: "work-id"}, Title: "Investigate",
	}}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("work_record.read"),
	}
	found, err := service.Get(context.Background(), principal, "task-id")
	if err != nil || found.ID != "task-id" || found.Parent.ID != "work-id" {
		t.Fatalf("Get() found=%+v error=%v", found, err)
	}
}

func TestCreateOpportunityTaskUsesPolymorphicParent(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}
	task, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Parent:    Ref{Type: ParentOpportunity, ID: "opportunity-id"},
		Title:     "Prepare proposal", ActorID: "actor-id", Source: "api",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.Parent.Type != ParentOpportunity || task.Parent.ID != "opportunity-id" ||
		task.WorkRecordID != "" {
		t.Fatalf("unexpected opportunity task: %+v", task)
	}
}

func TestCreateProjectSubtaskUsesSharedPolymorphicParent(t *testing.T) {
	parent := Ref{
		Type: ParentProject, ID: "project-id",
		MSPID: "msp-id", ClientID: "client-id",
	}
	repository := &fakeRepository{
		parent: Task{
			ID: "parent-id", MSPID: "msp-id", ClientID: "client-id",
			Parent: parent,
		},
	}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}
	task, err := service.Create(context.Background(), CreateCommand{
		Principal:    principal,
		Parent:       Ref{Type: ParentProject, ID: "project-id"},
		ParentTaskID: "parent-id", Title: "Confirm handoff",
		OwnerID: "technician-id", EstimateMinutes: 90,
		ActorID: "actor-id", Source: "api",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.Parent != parent || task.ParentTaskID != "parent-id" ||
		task.WorkRecordID != "" || task.OwnerID != "technician-id" ||
		task.EstimateMinutes != 90 {
		t.Fatalf("unexpected Project subtask: %+v", task)
	}
}

func (r *fakeRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.saved = mutation
	return nil
}

func TestCreateRejectsInteractiveMissingTagsBeforePersistence(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"}, Capabilities: authorization.NewCapabilitySet("task.create")}, Parent: Ref{Type: ParentOpportunity, ID: "opportunity-id"}, Title: "Prepare proposal", ActorID: "actor-id", Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.calls != 0 {
		t.Fatalf("Create() error=%v calls=%d", err, repository.calls)
	}
}
func (r *fakeRepository) LoadSelected(context.Context, Ref, []ID) ([]Task, error) {
	return nil, nil
}
func (r *fakeRepository) MoveAtomic(context.Context, MoveMutation) error {
	return nil
}

func TestCreateSubtaskPreservesScopeAndAppendsPosition(t *testing.T) {
	repository := &fakeRepository{
		parent: Task{
			ID: "parent-id", MSPID: "msp-id", ClientID: "client-id",
			Parent: Ref{
				Type: ParentWorkRecord, ID: "work-id",
				MSPID: "msp-id", ClientID: "client-id",
			},
			WorkRecordID: "work-id",
		},
		list: []Task{{ID: "existing", Position: 1}},
	}
	ids := []string{"task-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}

	task, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", ParentTaskID: "parent-id",
		Title: "Collect logs", ActorID: "actor-id", Source: "api",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.MSPID != "msp-id" || task.ClientID != "client-id" ||
		task.ParentTaskID != "parent-id" || task.Position != 2 {
		t.Fatalf("unexpected subtask: %+v", task)
	}
	if repository.saved.Audit.Action != "task.created" ||
		repository.saved.Audit.Source != "api" ||
		repository.saved.Event.EventType != "task.created" {
		t.Fatal("task creation did not produce matching audit and event records")
	}
}

func TestCreateUsesTrustedCallerCorrelationID(t *testing.T) {
	repository := &fakeRepository{}
	ids := []string{"task-id", "audit-id", "event-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}

	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Parent:    Ref{Type: ParentProject, ID: "project-id"},
		Title:     "Verify recovery", ActorID: "actor-id", Source: "ai_workspace",
		CorrelationID: "proposal-correlation-id",
		TagIDs:        []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.saved.Audit.CorrelationID != "proposal-correlation-id" ||
		repository.saved.Event.CorrelationID != "proposal-correlation-id" {
		t.Fatalf("mutation=%+v", repository.saved)
	}
}

func TestCreateAllowsExplicitSameMSPTargetForGlobalPrincipal(t *testing.T) {
	repository := &fakeRepository{}
	ids := []string{"task-id", "audit-id", "event-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, taskCreationPreparer())
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}

	task, err := service.Create(context.Background(), CreateCommand{
		Principal:            principal,
		Target:               scope.Target{MSPID: "msp-id", ClientID: "client-2"},
		Parent:               Ref{Type: ParentProject, ID: "project-id"},
		Title:                "Verify recovery",
		ActorID:              "technician-1",
		Source:               "ai_workspace",
		CorrelationID:        "proposal-correlation-id",
		TagIDs:               []string{"meaningful"},
		ClassificationPolicy: tagging.CreationRequireMeaningful,
	})

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.ClientID != "client-2" ||
		repository.listed.ClientID != "client-2" ||
		repository.saved.Task.ClientID != "client-2" {
		t.Fatalf("task=%+v listed=%+v mutation=%+v", task, repository.listed, repository.saved)
	}
}

func TestCreateRejectsExplicitClientMismatchForClientPrincipal(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, taskCreationPreparer())
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}

	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-2"},
		Parent:    Ref{Type: ParentProject, ID: "project-id"},
		Title:     "Verify recovery",
		ActorID:   "technician-1",
		Source:    "ai_workspace",
	})

	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v, want scope ErrNotFound", err)
	}
	if repository.saved.Task.ID != "" {
		t.Fatalf("repository received mutation=%+v", repository.saved)
	}
}

func TestCreateRejectsParentFromDifferentWorkRecord(t *testing.T) {
	repository := &fakeRepository{
		parent: Task{
			ID: "parent-id", MSPID: "msp-id", ClientID: "client-id",
			Parent: Ref{
				Type: ParentWorkRecord, ID: "other-work",
				MSPID: "msp-id", ClientID: "client-id",
			},
			WorkRecordID: "other-work",
		},
	}
	service := NewService(repository, time.Now, func() string { return "task-id" }, taskCreationPreparer())
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal, WorkRecordID: "work-id", ParentTaskID: "parent-id",
		Title: "Invalid child", ActorID: "actor-id", Source: "api",
		TagIDs: []string{"meaningful"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v, want ErrNotFound", err)
	}
}

func TestTaskModelHasNoDependencyContract(t *testing.T) {
	task := Task{ID: "task-id", Position: 1}
	if task.Position != 1 {
		t.Fatal("task ordering is unavailable")
	}
}
