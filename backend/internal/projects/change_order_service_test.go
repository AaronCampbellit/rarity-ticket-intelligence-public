package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type changeOrderRepository struct {
	order            ChangeOrder
	version          ChangeOrderVersion
	project          Project
	createMutation   CreateChangeOrderMutation
	issueMutation    IssueChangeOrderMutation
	decisionMutation DecideChangeOrderMutation
	applyMutation    ApplyChangeOrderMutation
}

func (r *changeOrderRepository) FindChangeOrder(context.Context, scope.Target, string) (ChangeOrder, error) {
	return r.order, nil
}

func (r *changeOrderRepository) FindChangeOrderVersion(context.Context, scope.Target, string) (ChangeOrderVersion, error) {
	return r.version, nil
}

func (r *changeOrderRepository) FindProjectForChange(context.Context, scope.Target, ProjectID) (Project, error) {
	return r.project, nil
}

func (r *changeOrderRepository) CreateChangeOrderAtomic(_ context.Context, mutation CreateChangeOrderMutation) error {
	r.createMutation = mutation
	r.order = mutation.Order
	return nil
}

func (r *changeOrderRepository) IssueChangeOrderAtomic(_ context.Context, mutation IssueChangeOrderMutation) error {
	r.issueMutation = mutation
	r.order = mutation.Order
	r.version = mutation.Version
	return nil
}

func (r *changeOrderRepository) DecideChangeOrderAtomic(_ context.Context, mutation DecideChangeOrderMutation) error {
	r.decisionMutation = mutation
	r.order = mutation.Order
	return nil
}

func (r *changeOrderRepository) ApplyChangeOrderAtomic(_ context.Context, mutation ApplyChangeOrderMutation) error {
	r.applyMutation = mutation
	r.order = mutation.Order
	r.project = mutation.Project
	return nil
}

func TestCreateChangeOrderCreatesScopedAuditedDraft(t *testing.T) {
	repository := validChangeOrderRepository()
	service := changeOrderTestService(repository)
	created, err := service.Create(context.Background(), CreateChangeOrderCommand{
		Principal: projectPrincipal("change_order.update"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		ProjectID: "project-id", DisplayID: " CO-1042 ",
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.DisplayID != "CO-1042" || created.State != ChangeOrderDraft ||
		created.Version != 1 || created.ProjectID != "project-id" ||
		repository.createMutation.Audit.Action != "change_order.created" ||
		repository.createMutation.Event.EventType != "change_order.created" {
		t.Fatalf("created Change Order = %+v mutation = %+v", created, repository.createMutation)
	}
}

func TestAppliedChangeOrderPreservesOriginalBaseline(t *testing.T) {
	repository := validChangeOrderRepository()
	service := changeOrderTestService(repository)
	approved, err := service.OverrideApproval(context.Background(), OverrideCommand{
		Principal: projectPrincipal("change_order.update"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		VersionID: "change-version-id", ExpectedOrderVersion: 2,
		Reason:  "Customer approved during recorded steering call",
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("OverrideApproval() error = %v", err)
	}
	decision := repository.decisionMutation.Decision
	if !decision.Override || decision.Reason == "" ||
		decision.PreviousState != ChangeOrderIssued ||
		decision.ChangeOrderVersionID != "change-version-id" ||
		repository.decisionMutation.Audit.Reason != decision.Reason ||
		repository.decisionMutation.Event.EventType != "change_order.approval.decided" {
		t.Fatalf("override evidence incomplete: %+v", repository.decisionMutation)
	}
	repository.version = approved
	applied, err := service.Apply(context.Background(), ApplyChangeOrderCommand{
		Principal: projectPrincipal("change_order.update"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		VersionID: "change-version-id", ExpectedOrderVersion: 3,
		ActorID: "actor-id", Source: "web",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if applied.OriginalBaseline.RevenueMinor != 10000 ||
		applied.CurrentBaseline.RevenueMinor != 12000 ||
		applied.CurrentBaseline.CostMinor != 4500 ||
		applied.CurrentBaseline.PlannedMinutes != 660 {
		t.Fatalf("unexpected applied Project baselines: %+v", applied)
	}
	if repository.applyMutation.Application.ChangeOrderVersionID != "change-version-id" {
		t.Fatal("application did not retain exact Change Order version")
	}
}

func TestChangeOrderDecisionEnforcesOptimisticConcurrency(t *testing.T) {
	repository := validChangeOrderRepository()
	service := changeOrderTestService(repository)
	_, err := service.OverrideApproval(context.Background(), OverrideCommand{
		Principal: projectPrincipal("change_order.update"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		VersionID: "change-version-id", ExpectedOrderVersion: 1,
		Reason: "Recorded customer approval", ActorID: "actor-id", Source: "web",
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("OverrideApproval() error = %v, want ErrVersionConflict", err)
	}
	if repository.decisionMutation.Decision.ID != "" {
		t.Fatal("stale Change Order decision reached the atomic repository")
	}
}

func TestOverrideUsesNormalUpdateAccessAndRequiresReason(t *testing.T) {
	repository := validChangeOrderRepository()
	service := changeOrderTestService(repository)
	command := OverrideCommand{
		Principal: projectPrincipal("change_order.update"),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		VersionID: "change-version-id", ExpectedOrderVersion: 2,
		ActorID: "actor-id", Source: "web",
	}
	if _, err := service.OverrideApproval(context.Background(), command); !errors.Is(err, ErrOverrideReasonRequired) {
		t.Fatalf("OverrideApproval() error = %v, want ErrOverrideReasonRequired", err)
	}
	command.Reason = "Approved by customer"
	command.Principal = projectPrincipal("change_order.override")
	if _, err := service.OverrideApproval(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("OverrideApproval() dedicated capability error = %v", err)
	}
}

func validChangeOrderRepository() *changeOrderRepository {
	return &changeOrderRepository{
		order: ChangeOrder{
			ID: "change-order-id", ProjectID: "project-id",
			MSPID: "msp-id", ClientID: "client-id", State: ChangeOrderIssued,
			CurrentVersion: 1, Version: 2,
		},
		version: ChangeOrderVersion{
			ID: "change-version-id", ChangeOrderID: "change-order-id",
			MSPID: "msp-id", ClientID: "client-id", Version: 1,
			Currency: "USD", RevenueDeltaMinor: 2000,
			CostDeltaMinor: 500, LaborDeltaMinutes: 60,
		},
		project: Project{
			ID: "project-id", MSPID: "msp-id", ClientID: "client-id", Version: 4,
			OriginalBaseline: BudgetBaseline{
				Currency: "USD", RevenueMinor: 10000, CostMinor: 4000, PlannedMinutes: 600,
			},
			CurrentBaseline: BudgetBaseline{
				Currency: "USD", RevenueMinor: 10000, CostMinor: 4000, PlannedMinutes: 600,
			},
		},
	}
}

func changeOrderTestService(repository ChangeOrderRepository) *ChangeOrderService {
	ids := []string{"decision-id", "audit-id", "event-id", "correlation-id", "application-id", "audit-2", "event-2", "correlation-2"}
	return NewChangeOrderService(repository, func() time.Time {
		return time.Date(2026, time.July, 29, 13, 0, 0, 0, time.UTC)
	}, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
}
