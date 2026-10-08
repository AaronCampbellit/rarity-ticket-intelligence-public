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

type fakeRepository struct {
	project Project
	phase   Phase
	created CreateMutation
	updated PhaseMutation
	calls   int
}

type projectClassificationRepository struct{}

func (projectClassificationRepository) ResolveTags(_ context.Context, mspID string, ids []string) ([]tagging.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []tagging.Tag{{ID: ids[0], MSPID: mspID, InternalKey: "taxonomy.support", State: tagging.StateActive}}, nil
}
func (projectClassificationRepository) FindUnclassified(_ context.Context, msp string) (tagging.Tag, error) {
	return tagging.Tag{ID: "unclassified", MSPID: msp, InternalKey: "unclassified", State: tagging.StateActive}, nil
}
func TestInitialTagsClassificationPolicyForProject(t *testing.T) {
	s := &Service{creation: tagging.NewCreationPreparer(projectClassificationRepository{})}
	target := scope.Target{MSPID: "msp", ClientID: "client"}
	if _, err := s.initialTags(context.Background(), target, CreateCommand{Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful}); !errors.Is(err, tagging.ErrMeaningfulTagRequired) {
		t.Fatalf("interactive error=%v", err)
	}
	_, err := s.initialTags(context.Background(), target, CreateCommand{Source: "automation", ClassificationPolicy: tagging.CreationAllowFallback})
	if !errors.Is(err, tagging.ErrInvalidAssociation) {
		t.Fatalf("untrusted fallback error=%v", err)
	}
}

func (r *fakeRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.created = mutation
	r.project = mutation.Project
	return nil
}

func TestCreateRejectsInteractiveMissingTagsBeforePersistence(t *testing.T) {
	repository := &fakeRepository{}
	service := testProjectService(repository)
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: projectPrincipal("project.create"), DisplayID: "PROJECT-1", Name: "Email Migration", OriginalProposalVersionID: "proposal-version-id", Phases: []PhaseInput{{Name: "Deliver"}}, ActorID: "actor-id", Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if !errors.Is(err, tagging.ErrMeaningfulTagRequired) || repository.calls != 0 {
		t.Fatalf("Create() error=%v calls=%d", err, repository.calls)
	}
}
func (r *fakeRepository) FindProject(_ context.Context, _ scope.Target, _ ProjectID) (Project, error) {
	return r.project, nil
}
func (r *fakeRepository) FindPhase(_ context.Context, _ scope.Target, _ PhaseID) (Phase, error) {
	return r.phase, nil
}
func (r *fakeRepository) UpdatePhaseAtomic(_ context.Context, mutation PhaseMutation) error {
	r.updated = mutation
	return nil
}

func TestProjectUsesOrderedPhasesWithoutMilestonesOrDependencies(t *testing.T) {
	repository := &fakeRepository{}
	service := testProjectService(repository)
	project, err := service.Create(context.Background(), CreateCommand{
		Principal: projectPrincipal("project.create"),
		DisplayID: "PROJECT-1", Name: "Email Migration",
		OriginalProposalVersionID: "proposal-version-id",
		Phases:                    []PhaseInput{{Name: "Discover"}, {Name: "Deliver"}},
		ActorID:                   "actor-id", Source: "conversion",
		TagIDs: []string{"tag-id"}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(project.Phases) != 2 ||
		project.Phases[0].Position != 1 || project.Phases[1].Position != 2 {
		t.Fatalf("phases are not ordered: %+v", project.Phases)
	}
	if project.SupportsMilestones || project.SupportsTaskDependencies {
		t.Fatalf("unsupported concepts enabled: %+v", project)
	}
	if repository.created.Event.EventType != "project.created" {
		t.Fatal("Project creation event missing")
	}
}

func TestUpdatePhaseRequiresVersionAndValidWindow(t *testing.T) {
	start := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		project: Project{ID: "project-id", MSPID: "msp-id", ClientID: "client-id"},
		phase: Phase{
			ID: "phase-id", ProjectID: "project-id", MSPID: "msp-id",
			ClientID: "client-id", Position: 1, Version: 2,
		},
	}
	service := testProjectService(repository)
	updated, err := service.UpdatePhase(context.Background(), UpdatePhaseCommand{
		Principal: projectPrincipal("project.edit"),
		ID:        "phase-id", ExpectedVersion: 2, Name: "Delivery",
		OwnerID: "owner-id", PlannedStart: start, PlannedEnd: start.AddDate(0, 0, 5),
		PlannedMinutes: 2400, ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("UpdatePhase() error = %v", err)
	}
	if updated.Version != 3 || updated.OwnerID != "owner-id" {
		t.Fatalf("unexpected Phase update: %+v", updated)
	}

	_, err = service.UpdatePhase(context.Background(), UpdatePhaseCommand{
		Principal: projectPrincipal("project.edit"),
		ID:        "phase-id", ExpectedVersion: 1, Name: "Delivery",
		PlannedStart: start, PlannedEnd: start.AddDate(0, 0, 5),
		ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale Phase error = %v", err)
	}
}

func TestCapacityShowsOverbookingWithoutChangingPlan(t *testing.T) {
	got := CalculateCapacity(40*time.Hour, 48*time.Hour, 12*time.Hour)
	if got.Overbooked != 20*time.Hour ||
		got.Available != 40*time.Hour || got.Scheduled != 48*time.Hour {
		t.Fatalf("unexpected capacity: %+v", got)
	}
}

func TestProjectCreationDeniesCrossClientTarget(t *testing.T) {
	repository := &fakeRepository{}
	service := testProjectService(repository)
	principal := projectPrincipal("project.create")
	principal.Scope.ClientID = "client-alpha"
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-bravo"},
		DisplayID: "PROJECT-2", Name: "Wrong Client",
		OriginalProposalVersionID: "proposal-version-id",
		Phases:                    []PhaseInput{{Name: "Deliver"}},
		ActorID:                   "actor-id", Source: "api",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client Create() error = %v", err)
	}
}

func projectPrincipal(capabilities ...string) authorization.Principal {
	return authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(capabilities...),
	}
}

func testProjectService(repository Repository) *Service {
	counter := 0
	return NewService(repository, time.Now, func() string {
		counter++
		return "id-" + string(rune('0'+counter))
	}, tagging.NewCreationPreparer(projectClassificationRepository{}))
}
