package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type projectQueryRepositoryStub struct {
	target               scope.Target
	id                   ProjectID
	limit                int
	reference            string
	referenceLimit       int
	referenceCalls       int
	referenceProjects    []ProjectSummary
	financialInputs      FinancialInputs
	phaseFinancialInputs map[PhaseID]FinancialInputs
	capacityRows         []ResourceCapacity
	capacityCalls        int
	capacityErr          error
}

func (r *projectQueryRepositoryStub) LoadPhaseFinancialInputs(
	_ context.Context,
	target scope.Target,
	id ProjectID,
) (map[PhaseID]FinancialInputs, error) {
	r.target, r.id = target, id
	return r.phaseFinancialInputs, nil
}

func (r *projectQueryRepositoryStub) LoadCapacity(
	_ context.Context,
	target scope.Target,
	_ CapacityWindow,
	_ []string,
) ([]ResourceCapacity, error) {
	r.target = target
	r.capacityCalls++
	return r.capacityRows, r.capacityErr
}

func (r *projectQueryRepositoryStub) ListProjects(_ context.Context, target scope.Target, limit int) ([]ProjectSummary, error) {
	r.target, r.limit = target, limit
	return []ProjectSummary{{ID: "project-1", Version: 1}}, nil
}

func (r *projectQueryRepositoryStub) FindProjectsByReference(
	_ context.Context,
	target scope.Target,
	reference string,
	limit int,
) ([]ProjectSummary, error) {
	r.target, r.reference, r.referenceLimit = target, reference, limit
	r.referenceCalls++
	return append([]ProjectSummary(nil), r.referenceProjects...), nil
}

func (r *projectQueryRepositoryStub) LoadProjectWorkspace(_ context.Context, target scope.Target, id ProjectID) (ProjectWorkspace, error) {
	r.target, r.id = target, id
	return ProjectWorkspace{
		ID: id, Version: 2,
		PlannedStart: "2026-08-03", PlannedEnd: "2026-08-09",
		OriginalBaseline: BudgetBaseline{Currency: "USD", RevenueMinor: 10000},
		Phases: []ProjectPhaseView{{
			ID: "phase-id", Budget: Money{Minor: 5000, Currency: "USD"},
		}},
		ProjectTasks: []ProjectTaskView{{OwnerID: "technician-id"}},
		CostActuals:  []CostActualView{{ID: "cost", Amount: Money{Minor: 2500, Currency: "USD"}}},
	}, nil
}

func (r *projectQueryRepositoryStub) LoadFinancialInputs(
	_ context.Context,
	target scope.Target,
	id ProjectID,
) (FinancialInputs, error) {
	r.target, r.id = target, id
	return r.financialInputs, nil
}

func TestProjectQueryRedactsFinancialsWithoutDedicatedCapability(t *testing.T) {
	found, err := NewQueryService(&projectQueryRepositoryStub{}).Get(
		context.Background(), projectReader(), scope.Target{}, "project-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if found.OriginalBaseline.RevenueMinor != 0 || found.Phases[0].Budget.Minor != 0 ||
		len(found.CostActuals) != 0 {
		t.Fatalf("financial data was not redacted: %+v", found)
	}

	principal := projectReader()
	principal.Capabilities = authorization.NewCapabilitySet("project.read", "project.financial.read")
	found, err = NewQueryService(&projectQueryRepositoryStub{}).Get(
		context.Background(), principal, scope.Target{}, "project-1",
	)
	if err != nil || found.OriginalBaseline.RevenueMinor != 10000 {
		t.Fatalf("financial reader result=%+v err=%v", found, err)
	}
}

func projectReader() authorization.Principal {
	return authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet("project.read"),
	}
}

func TestProjectQueryUsesTrustedClientScopeAndBoundedLimit(t *testing.T) {
	repository := &projectQueryRepositoryStub{}
	service := NewQueryService(repository)
	found, err := service.List(context.Background(), projectReader(), scope.Target{}, 1000)
	if err != nil || len(found) != 1 || repository.target.ClientID != "client-1" ||
		repository.limit != 50 {
		t.Fatalf("found=%+v target=%+v limit=%d err=%v", found, repository.target, repository.limit, err)
	}
}

func TestProjectQueryResolvesReferenceWithAuthorizedClientScopeAndBoundedMatches(t *testing.T) {
	repository := &projectQueryRepositoryStub{referenceProjects: []ProjectSummary{{
		ID: "project-older", DisplayID: "PRJ-OLDER", Name: "Older Modernization",
	}}}
	principal := projectReader()
	principal.Scope.ClientID = ""
	service := NewQueryService(repository)

	found, err := service.ResolveReference(
		context.Background(), principal,
		scope.Target{MSPID: "msp-1", ClientID: "client-2"},
		"  Older\u2003Modernization ",
	)

	if err != nil || len(found) != 1 || found[0].ID != "project-older" {
		t.Fatalf("found=%+v error=%v", found, err)
	}
	if repository.target != (scope.Target{MSPID: "msp-1", ClientID: "client-2"}) ||
		repository.reference != "  Older\u2003Modernization " ||
		repository.referenceLimit != 2 || repository.referenceCalls != 1 {
		t.Fatalf(
			"target=%+v reference=%q limit=%d calls=%d",
			repository.target, repository.reference,
			repository.referenceLimit, repository.referenceCalls,
		)
	}
}

func TestProjectQueryDistinguishesMissingAndAmbiguousExactReferences(t *testing.T) {
	repository := &projectQueryRepositoryStub{}
	service := NewQueryService(repository)
	principal := projectReader()

	found, err := service.ResolveReference(
		context.Background(), principal, scope.Target{}, "Missing",
	)
	if err != nil || len(found) != 0 {
		t.Fatalf("missing found=%+v error=%v", found, err)
	}

	repository.referenceProjects = []ProjectSummary{
		{ID: "project-1", DisplayID: "PRJ-1", Name: "Modernization"},
		{ID: "project-2", DisplayID: "PRJ-2", Name: "Modernization"},
	}
	found, err = service.ResolveReference(
		context.Background(), principal, scope.Target{}, "Modernization",
	)
	if !errors.Is(err, ErrAmbiguousReference) || found != nil {
		t.Fatalf(
			"ambiguous found=%+v error=%v, want ErrAmbiguousReference",
			found,
			err,
		)
	}
}

func TestProjectQueryReferenceRequiresProjectRead(t *testing.T) {
	repository := &projectQueryRepositoryStub{}
	principal := projectReader()
	principal.Capabilities = nil

	_, err := NewQueryService(repository).ResolveReference(
		context.Background(), principal, scope.Target{}, "PRJ-1",
	)

	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error=%v, want ErrForbidden", err)
	}
	if repository.referenceCalls != 0 {
		t.Fatalf("unauthorized reference calls=%d", repository.referenceCalls)
	}
}

func TestProjectQueryReturnsServerCalculatedFinancials(t *testing.T) {
	repository := &projectQueryRepositoryStub{
		financialInputs: FinancialInputs{
			OriginalBudget:      Money{Minor: 20000, Currency: "USD"},
			CurrentBudget:       Money{Minor: 24000, Currency: "USD"},
			PlannedLabor:        Money{Minor: 7000, Currency: "USD"},
			ActualLabor:         Money{Minor: 6000, Currency: "USD"},
			CostActuals:         Money{Minor: 2000, Currency: "USD"},
			CommittedCost:       Money{Minor: 1000, Currency: "USD"},
			BillableWork:        Money{Minor: 16000, Currency: "USD"},
			ActualLaborComplete: true,
		},
		phaseFinancialInputs: map[PhaseID]FinancialInputs{
			"phase-id": {
				OriginalBudget:      Money{Minor: 10000, Currency: "USD"},
				CurrentBudget:       Money{Minor: 12000, Currency: "USD"},
				PlannedLabor:        Money{Minor: 4000, Currency: "USD"},
				ActualLabor:         Money{Minor: 3000, Currency: "USD"},
				CostActuals:         Money{Minor: 1000, Currency: "USD"},
				CommittedCost:       Money{Minor: 500, Currency: "USD"},
				BillableWork:        Money{Minor: 9000, Currency: "USD"},
				ActualLaborComplete: true,
			},
		},
	}
	principal := projectReader()
	principal.Capabilities = authorization.NewCapabilitySet(
		"project.read", "project.financial.read",
	)
	found, err := NewQueryService(
		repository,
		NewFinancialService(repository),
	).Get(context.Background(), principal, scope.Target{}, "project-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found.Financials == nil ||
		found.Financials.Profit.Minor != 8000 ||
		found.Financials.ProjectedProfit.Minor != 14000 {
		t.Fatalf("financials=%+v", found.Financials)
	}
	if found.Phases[0].Financials == nil ||
		found.Phases[0].Financials.Profit.Minor != 5000 {
		t.Fatalf("phase financials=%+v", found.Phases[0].Financials)
	}
}

func TestProjectQueryReturnsCapacityForAssignedTechnicians(t *testing.T) {
	repository := &projectQueryRepositoryStub{
		capacityRows: []ResourceCapacity{{
			ResourceID: "technician-id", Name: "Alex Morgan",
			Available: 40 * time.Hour, Scheduled: 48 * time.Hour,
			Actual: 12 * time.Hour,
		}},
	}
	principal := projectReader()
	principal.Capabilities = authorization.NewCapabilitySet(
		"project.read", "project.resource.plan",
	)
	service := NewQueryService(repository).WithCapacity(
		NewCapacityService(repository),
	)
	found, err := service.Get(
		context.Background(), principal, scope.Target{}, "project-1",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(found.Capacity) != 1 ||
		found.Capacity[0].Name != "Alex Morgan" ||
		found.Capacity[0].OverbookedMinutes != 1200 {
		t.Fatalf("capacity=%+v", found.Capacity)
	}
}

func TestProjectOperationalDetailSkipsCapacityForResourcePlanningReaders(t *testing.T) {
	repository := &projectQueryRepositoryStub{capacityErr: errors.New("capacity backend must not be called")}
	principal := projectReader()
	principal.Capabilities = authorization.NewCapabilitySet(
		"project.read", "project.resource.plan",
	)
	service := NewQueryService(repository).WithCapacity(NewCapacityService(repository))

	workspace, err := service.GetOperational(
		context.Background(), principal,
		scope.Target{MSPID: "msp-1", ClientID: "client-1"}, "project-1",
	)

	if err != nil || workspace.ID != "project-1" {
		t.Fatalf("GetOperational() workspace=%+v error=%v", workspace, err)
	}
	if repository.capacityCalls != 0 || len(workspace.Capacity) != 0 {
		t.Fatalf("capacity calls=%d view=%+v", repository.capacityCalls, workspace.Capacity)
	}
}

func TestProjectQueryRequiresReadCapability(t *testing.T) {
	principal := projectReader()
	principal.Capabilities = nil
	_, err := NewQueryService(&projectQueryRepositoryStub{}).Get(
		context.Background(), principal, scope.Target{}, "project-1",
	)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}

func TestProjectQueryLoadsOnlyRequestedScopedProject(t *testing.T) {
	repository := &projectQueryRepositoryStub{}
	found, err := NewQueryService(repository).Get(
		context.Background(), projectReader(), scope.Target{}, "project-1",
	)
	if err != nil || found.ID != "project-1" ||
		repository.id != "project-1" || repository.target.MSPID != "msp-1" {
		t.Fatalf("found=%+v target=%+v id=%s err=%v", found, repository.target, repository.id, err)
	}
}
