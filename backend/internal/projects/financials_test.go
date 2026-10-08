package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type financialRepository struct {
	inputs FinancialInputs
	err    error
}

type phaseFinancialRepository struct {
	financialRepository
	phases map[PhaseID]FinancialInputs
}

func (r phaseFinancialRepository) LoadPhaseFinancialInputs(
	context.Context,
	scope.Target,
	ProjectID,
) (map[PhaseID]FinancialInputs, error) {
	return r.phases, nil
}

func (r financialRepository) LoadFinancialInputs(
	context.Context,
	scope.Target,
	ProjectID,
) (FinancialInputs, error) {
	return r.inputs, r.err
}

func TestFinancialSummaryCalculatesRecognizedProfitAndMargin(t *testing.T) {
	service := NewFinancialService(financialRepository{inputs: FinancialInputs{
		OriginalBudget:      Money{Minor: 20000, Currency: "USD"},
		CurrentBudget:       Money{Minor: 24000, Currency: "USD"},
		PlannedLabor:        Money{Minor: 7000, Currency: "USD"},
		ActualLabor:         Money{Minor: 6000, Currency: "USD"},
		CostActuals:         Money{Minor: 2000, Currency: "USD"},
		CommittedCost:       Money{Minor: 1000, Currency: "USD"},
		BillableWork:        Money{Minor: 16000, Currency: "USD"},
		ActualLaborComplete: true,
	}})
	summary, err := service.Calculate(
		context.Background(),
		projectPrincipal("project.financial.read"),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"project-id",
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if summary.Profit.Minor != 8000 || summary.MarginBasisPoints != 5000 {
		t.Fatalf("unexpected financial summary: %+v", summary)
	}
	if summary.ProjectedProfit.Minor != 14000 {
		t.Fatalf("unexpected projected profit: %+v", summary.ProjectedProfit)
	}
}

func TestFinancialSummaryMarksRecognizedProfitUnavailableWithoutAllLaborRates(t *testing.T) {
	service := NewFinancialService(financialRepository{inputs: FinancialInputs{
		OriginalBudget: Money{Minor: 20000, Currency: "USD"},
		CurrentBudget:  Money{Minor: 20000, Currency: "USD"},
		PlannedLabor:   Money{Minor: 7000, Currency: "USD"},
		ActualLabor:    Money{Minor: 1000, Currency: "USD"},
		CostActuals:    Money{Minor: 2000, Currency: "USD"},
		CommittedCost:  Money{Minor: 1000, Currency: "USD"},
		BillableWork:   Money{Minor: 16000, Currency: "USD"},
	}})
	summary, err := service.Calculate(
		context.Background(),
		projectPrincipal("project.financial.read"),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"project-id",
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if summary.ActualLaborComplete || summary.ProfitAvailable ||
		summary.Profit.Minor != 0 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestFinancialServiceCalculatesPhaseProfitability(t *testing.T) {
	service := NewFinancialService(phaseFinancialRepository{
		phases: map[PhaseID]FinancialInputs{
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
	})
	found, err := service.CalculatePhases(
		context.Background(),
		projectPrincipal("project.financial.read"),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"project-id",
	)
	if err != nil {
		t.Fatalf("CalculatePhases() error = %v", err)
	}
	if found["phase-id"].Profit.Minor != 5000 ||
		found["phase-id"].ProjectedProfit.Minor != 6500 {
		t.Fatalf("found=%+v", found)
	}
}

func TestFinancialSummaryRejectsMixedCurrencies(t *testing.T) {
	inputs := FinancialInputs{
		OriginalBudget: Money{Minor: 1, Currency: "USD"},
		CurrentBudget:  Money{Minor: 1, Currency: "CAD"},
		PlannedLabor:   Money{Currency: "USD"},
		ActualLabor:    Money{Currency: "USD"},
		CostActuals:    Money{Currency: "USD"},
		CommittedCost:  Money{Currency: "USD"},
		BillableWork:   Money{Currency: "USD"},
	}
	service := NewFinancialService(financialRepository{inputs: inputs})
	_, err := service.Calculate(
		context.Background(),
		projectPrincipal("project.financial.read"),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"project-id",
	)
	if !errors.Is(err, ErrInvalidFinancialInputs) {
		t.Fatalf("Calculate() error = %v, want ErrInvalidFinancialInputs", err)
	}
}
