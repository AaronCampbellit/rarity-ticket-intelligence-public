package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type costActualRepository struct {
	project  Project
	phase    Phase
	mutation CostActualMutation
}

func (r *costActualRepository) FindProject(context.Context, scope.Target, ProjectID) (Project, error) {
	return r.project, nil
}

func (r *costActualRepository) FindPhase(context.Context, scope.Target, PhaseID) (Phase, error) {
	return r.phase, nil
}

func (r *costActualRepository) CreateCostActualAtomic(_ context.Context, mutation CostActualMutation) error {
	r.mutation = mutation
	return nil
}

func TestCreateCostActualPersistsScopedAuditedCommittedCost(t *testing.T) {
	repository := &costActualRepository{
		project: Project{ID: "project-id", MSPID: "msp-id", ClientID: "client-id"},
		phase: Phase{
			ID: "phase-id", ProjectID: "project-id",
			MSPID: "msp-id", ClientID: "client-id",
		},
	}
	ids := []string{"cost-id", "audit-id", "event-id", "correlation-id"}
	service := NewCostActualService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	incurred := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	cost, err := service.Create(context.Background(), CreateCostActualCommand{
		Principal: projectPrincipal("project.edit"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		ProjectID: "project-id", PhaseID: "phase-id",
		CostType: "license", Description: "Annual security license",
		Amount:    Money{Minor: 250000, Currency: "usd"},
		Committed: true, IncurredAt: incurred,
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if cost.Amount.Currency != "USD" || cost.Amount.Minor != 250000 ||
		!cost.Committed || cost.PhaseID != "phase-id" ||
		repository.mutation.Audit.Action != "project.cost_actual.created" ||
		repository.mutation.Event.EventType != "project.cost_actual.created" {
		t.Fatalf("cost=%+v mutation=%+v", cost, repository.mutation)
	}
}

func TestCreateCostActualRejectsPhaseFromAnotherProject(t *testing.T) {
	repository := &costActualRepository{
		project: Project{ID: "project-id", MSPID: "msp-id", ClientID: "client-id"},
		phase: Phase{
			ID: "phase-id", ProjectID: "other-project",
			MSPID: "msp-id", ClientID: "client-id",
		},
	}
	service := NewCostActualService(repository, time.Now, func() string { return "id" })
	_, err := service.Create(context.Background(), CreateCostActualCommand{
		Principal: projectPrincipal("project.edit"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		ProjectID: "project-id", PhaseID: "phase-id",
		CostType: "expense", Description: "Travel",
		Amount:     Money{Minor: 1000, Currency: "USD"},
		IncurredAt: time.Now(), ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, scope.ErrNotFound) || repository.mutation.Cost.ID != "" {
		t.Fatalf("error=%v mutation=%+v", err, repository.mutation)
	}
}
