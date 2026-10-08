package projects

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type financialInputRepositoryStub struct {
	project     Project
	phase       Phase
	rate        LaborCostRateMutation
	recognition BillableWorkMutation
}

func (r *financialInputRepositoryStub) FindProject(
	context.Context, scope.Target, ProjectID,
) (Project, error) {
	return r.project, nil
}

func (r *financialInputRepositoryStub) FindPhase(
	context.Context, scope.Target, PhaseID,
) (Phase, error) {
	return r.phase, nil
}

func (r *financialInputRepositoryStub) CreateLaborCostRateAtomic(
	_ context.Context,
	mutation LaborCostRateMutation,
) error {
	r.rate = mutation
	return nil
}

func (r *financialInputRepositoryStub) RecognizeBillableWorkAtomic(
	_ context.Context,
	mutation BillableWorkMutation,
) error {
	r.recognition = mutation
	return nil
}

func TestCreateLaborCostRateUsesMSPAdministration(t *testing.T) {
	repository := &financialInputRepositoryStub{}
	ids := []string{"rate-id", "audit-id", "event-id", "correlation-id"}
	service := NewFinancialInputService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	found, err := service.CreateLaborCostRate(
		context.Background(),
		CreateLaborCostRateCommand{
			Principal: authorization.Principal{
				ID:           "manager-id",
				Scope:        scope.Principal{MSPID: "msp-id"},
				Capabilities: authorization.NewCapabilitySet("organization.manage"),
			},
			TechnicianID: "technician-id",
			HourlyRate:   Money{Minor: 8500, Currency: "usd"},
			EffectiveAt:  time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
			ActorID:      "manager-id", Source: "web",
		},
	)
	if err != nil {
		t.Fatalf("CreateLaborCostRate() error = %v", err)
	}
	if found.HourlyRate.Currency != "USD" ||
		found.HourlyRate.Minor != 8500 ||
		repository.rate.Event.EventType != "technician.labor_cost_rate.created" ||
		repository.rate.Audit.ClientID != "" {
		t.Fatalf("found=%+v mutation=%+v", found, repository.rate)
	}
}

func TestRecognizeBillableWorkPersistsScopedPhaseRevenue(t *testing.T) {
	repository := &financialInputRepositoryStub{
		project: Project{ID: "project-id", MSPID: "msp-id", ClientID: "client-id"},
		phase: Phase{
			ID: "phase-id", ProjectID: "project-id",
			MSPID: "msp-id", ClientID: "client-id",
		},
	}
	ids := []string{"recognition-id", "audit-id", "event-id", "correlation-id"}
	service := NewFinancialInputService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	found, err := service.RecognizeBillableWork(
		context.Background(),
		RecognizeBillableWorkCommand{
			Principal: projectPrincipal("project.edit"),
			Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
			ProjectID: "project-id", PhaseID: "phase-id",
			Description: "Accepted delivery milestone",
			Amount:      Money{Minor: 1200000, Currency: "usd"},
			RecognizedAt: time.Date(
				2026, time.August, 15, 18, 0, 0, 0, time.UTC,
			),
			ActorID: "actor-id", Source: "web",
		},
	)
	if err != nil {
		t.Fatalf("RecognizeBillableWork() error = %v", err)
	}
	if found.PhaseID != "phase-id" || found.Amount.Currency != "USD" ||
		repository.recognition.Event.EventType != "project.billable_work.recognized" {
		t.Fatalf("found=%+v mutation=%+v", found, repository.recognition)
	}
}
