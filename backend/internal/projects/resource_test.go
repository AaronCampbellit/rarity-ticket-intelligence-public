package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type resourceRepository struct {
	accepted ResourcePlanMutation
}

func (r *resourceRepository) CreateResourcePlanAtomic(_ context.Context, mutation ResourcePlanMutation) error {
	r.accepted = mutation
	return nil
}

func TestPlanResourceSupportsPhaseRoleAllocation(t *testing.T) {
	repository := &resourceRepository{}
	service := NewResourceService(repository, time.Now, func() string {
		return "resource-plan-id"
	})
	start := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
	plan, err := service.Plan(context.Background(), PlanResourceCommand{
		Principal: projectPrincipal("project.resource.plan"),
		ProjectID: "project-id", PhaseID: "phase-id", RoleID: "engineer-role-id",
		StartsOn: start, EndsOn: start.AddDate(0, 0, 4), PlannedMinutes: 1200,
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.RoleID != "engineer-role-id" || plan.PlannedMinutes != 1200 {
		t.Fatalf("unexpected resource plan: %+v", plan)
	}
	if repository.accepted.Event.EventType != "resource_plan.created" {
		t.Fatal("resource plan event missing")
	}
}

func TestPlanResourceRejectsAmbiguousResourceAndInvalidWindow(t *testing.T) {
	service := NewResourceService(&resourceRepository{}, time.Now, func() string { return "unused" })
	start := time.Now()
	_, err := service.Plan(context.Background(), PlanResourceCommand{
		Principal: projectPrincipal("project.resource.plan"),
		ProjectID: "project-id", PhaseID: "phase-id",
		RoleID: "role-id", TeamID: "team-id",
		StartsOn: start, EndsOn: start.Add(-time.Hour), PlannedMinutes: 60,
		ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, ErrInvalidResourcePlan) {
		t.Fatalf("Plan() error = %v, want ErrInvalidResourcePlan", err)
	}
}

func TestCapacityServiceReportsEachResourceWithoutMutatingAllocations(t *testing.T) {
	repository := capacityRepository{rows: []ResourceCapacity{
		{ResourceID: "tech-1", Available: 40 * time.Hour, Scheduled: 48 * time.Hour, Actual: 12 * time.Hour},
	}}
	service := NewCapacityService(repository)
	view, err := service.Calculate(
		context.Background(),
		scope.Target{MSPID: "msp-id"},
		CapacityWindow{Start: time.Now(), End: time.Now().AddDate(0, 0, 7)},
		[]string{"tech-1"},
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if view.Resources["tech-1"].Overbooked != 20*time.Hour {
		t.Fatalf("unexpected capacity view: %+v", view)
	}
}

type capacityRepository struct {
	rows []ResourceCapacity
}

func (r capacityRepository) LoadCapacity(
	context.Context,
	scope.Target,
	CapacityWindow,
	[]string,
) ([]ResourceCapacity, error) {
	return r.rows, nil
}
