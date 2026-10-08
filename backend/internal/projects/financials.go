package projects

import (
	"context"
	"errors"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidFinancialInputs = errors.New("invalid project financial inputs")

type FinancialInputs struct {
	OriginalBudget      Money
	CurrentBudget       Money
	PlannedLabor        Money
	ActualLabor         Money
	CostActuals         Money
	CommittedCost       Money
	BillableWork        Money
	ActualLaborComplete bool
}

type FinancialSummary struct {
	OriginalBudget      Money `json:"original_budget"`
	CurrentBudget       Money `json:"current_budget"`
	PlannedLabor        Money `json:"planned_labor"`
	ActualLabor         Money `json:"actual_labor"`
	CostActuals         Money `json:"cost_actuals"`
	CommittedCost       Money `json:"committed_cost"`
	BillableWork        Money `json:"billable_work"`
	Profit              Money `json:"profit"`
	ProjectedProfit     Money `json:"projected_profit"`
	MarginBasisPoints   int64 `json:"margin_basis_points"`
	ActualLaborComplete bool  `json:"actual_labor_complete"`
	ProfitAvailable     bool  `json:"profit_available"`
}

type FinancialRepository interface {
	LoadFinancialInputs(context.Context, scope.Target, ProjectID) (FinancialInputs, error)
}

type PhaseFinancialRepository interface {
	LoadPhaseFinancialInputs(
		context.Context,
		scope.Target,
		ProjectID,
	) (map[PhaseID]FinancialInputs, error)
}

type FinancialService struct {
	repository FinancialRepository
}

func NewFinancialService(repository FinancialRepository) *FinancialService {
	return &FinancialService{repository: repository}
}

func (s *FinancialService) Calculate(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	projectID ProjectID,
) (FinancialSummary, error) {
	target = projectTarget(principal, target)
	if target.ClientID == "" || projectID == "" {
		return FinancialSummary{}, ErrInvalidFinancialInputs
	}
	if err := authorization.Authorize(principal, "project.financial.read", target); err != nil {
		return FinancialSummary{}, err
	}
	inputs, err := s.repository.LoadFinancialInputs(ctx, target, projectID)
	if err != nil {
		return FinancialSummary{}, err
	}
	return calculateFinancialSummary(inputs)
}

func (s *FinancialService) CalculatePhases(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	projectID ProjectID,
) (map[PhaseID]FinancialSummary, error) {
	target = projectTarget(principal, target)
	if target.ClientID == "" || projectID == "" {
		return nil, ErrInvalidFinancialInputs
	}
	if err := authorization.Authorize(
		principal, "project.financial.read", target,
	); err != nil {
		return nil, err
	}
	repository, ok := s.repository.(PhaseFinancialRepository)
	if !ok {
		return nil, ErrInvalidFinancialInputs
	}
	inputs, err := repository.LoadPhaseFinancialInputs(ctx, target, projectID)
	if err != nil {
		return nil, err
	}
	found := make(map[PhaseID]FinancialSummary, len(inputs))
	for phaseID, phaseInputs := range inputs {
		summary, err := calculateFinancialSummary(phaseInputs)
		if err != nil {
			return nil, err
		}
		found[phaseID] = summary
	}
	return found, nil
}

func calculateFinancialSummary(
	inputs FinancialInputs,
) (FinancialSummary, error) {
	currency := inputs.OriginalBudget.Currency
	if len(currency) != 3 ||
		!validFinancialMoney(currency,
			inputs.OriginalBudget, inputs.CurrentBudget, inputs.PlannedLabor,
			inputs.ActualLabor, inputs.CostActuals, inputs.CommittedCost,
			inputs.BillableWork,
		) {
		return FinancialSummary{}, ErrInvalidFinancialInputs
	}
	profit := int64(0)
	if inputs.ActualLaborComplete {
		profit = inputs.BillableWork.Minor -
			inputs.ActualLabor.Minor - inputs.CostActuals.Minor
	}
	projected := inputs.CurrentBudget.Minor - inputs.PlannedLabor.Minor -
		inputs.CostActuals.Minor - inputs.CommittedCost.Minor
	marginBPS := int64(0)
	if inputs.ActualLaborComplete && inputs.BillableWork.Minor > 0 {
		marginBPS = profit * 10000 / inputs.BillableWork.Minor
	}
	return FinancialSummary{
		OriginalBudget: inputs.OriginalBudget, CurrentBudget: inputs.CurrentBudget,
		PlannedLabor: inputs.PlannedLabor, ActualLabor: inputs.ActualLabor,
		CostActuals: inputs.CostActuals, CommittedCost: inputs.CommittedCost,
		BillableWork:        inputs.BillableWork,
		Profit:              Money{Minor: profit, Currency: currency},
		ProjectedProfit:     Money{Minor: projected, Currency: currency},
		MarginBasisPoints:   marginBPS,
		ActualLaborComplete: inputs.ActualLaborComplete,
		ProfitAvailable:     inputs.ActualLaborComplete,
	}, nil
}

func validFinancialMoney(currency string, values ...Money) bool {
	for _, value := range values {
		if value.Currency != currency || value.Minor < 0 {
			return false
		}
	}
	return true
}
