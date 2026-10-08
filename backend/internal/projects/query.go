package projects

import (
	"context"
	"sort"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type ProjectSummary struct {
	ID             ProjectID `json:"id"`
	DisplayID      string    `json:"display_id"`
	Name           string    `json:"name"`
	LifecycleState string    `json:"lifecycle_state"`
	PlannedStart   string    `json:"planned_start,omitempty"`
	PlannedEnd     string    `json:"planned_end,omitempty"`
	Version        int64     `json:"version"`
}

type ProjectPhaseView struct {
	ID                 PhaseID           `json:"id"`
	Position           int               `json:"position"`
	Name               string            `json:"name"`
	State              string            `json:"state"`
	OwnerID            string            `json:"owner_id,omitempty"`
	ParticipatingTeams []string          `json:"participating_teams"`
	PlannedStart       string            `json:"planned_start,omitempty"`
	PlannedEnd         string            `json:"planned_end,omitempty"`
	PlannedMinutes     int64             `json:"planned_minutes"`
	ActualMinutes      int64             `json:"actual_minutes"`
	Budget             Money             `json:"budget"`
	Deliverables       []string          `json:"deliverables"`
	CompletionCriteria []string          `json:"completion_criteria"`
	Tasks              []ProjectTaskView `json:"tasks"`
	Financials         *FinancialSummary `json:"financials,omitempty"`
	Version            int64             `json:"version"`
}

type ChangeOrderView struct {
	Order    ChangeOrder          `json:"order"`
	Version  *ChangeOrderVersion  `json:"current_version,omitempty"`
	Decision *ChangeOrderDecision `json:"decision,omitempty"`
}

type ProjectTaskView struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	OwnerID         string `json:"owner_id,omitempty"`
	OwnerName       string `json:"owner_name,omitempty"`
	Subtasks        int64  `json:"subtasks"`
	EstimateMinutes int    `json:"estimate_minutes"`
	ActualMinutes   int64  `json:"actual_minutes"`
	Version         int64  `json:"version"`
}

type ResourcePlanView struct {
	ID             string `json:"id"`
	PhaseID        string `json:"phase_id,omitempty"`
	ResourceType   string `json:"resource_type"`
	ResourceName   string `json:"resource_name"`
	StartsOn       string `json:"starts_on"`
	EndsOn         string `json:"ends_on"`
	PlannedMinutes int64  `json:"planned_minutes"`
	Version        int64  `json:"version"`
}

type CostActualView struct {
	ID          string    `json:"id"`
	PhaseID     string    `json:"phase_id,omitempty"`
	CostType    string    `json:"cost_type"`
	Description string    `json:"description"`
	Amount      Money     `json:"amount"`
	Committed   bool      `json:"committed"`
	IncurredAt  time.Time `json:"incurred_at"`
	Version     int64     `json:"version"`
}

type CapacityResourceView struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	AvailableMinutes  int64  `json:"available_minutes"`
	ScheduledMinutes  int64  `json:"scheduled_minutes"`
	ActualMinutes     int64  `json:"actual_minutes"`
	RemainingMinutes  int64  `json:"remaining_minutes"`
	OverbookedMinutes int64  `json:"overbooked_minutes"`
}

type ProjectWorkspace struct {
	ID                      ProjectID              `json:"id"`
	DisplayID               string                 `json:"display_id"`
	Name                    string                 `json:"name"`
	ClientName              string                 `json:"client_name"`
	LifecycleState          string                 `json:"lifecycle_state"`
	PlannedStart            string                 `json:"planned_start,omitempty"`
	PlannedEnd              string                 `json:"planned_end,omitempty"`
	OriginalProposalVersion int64                  `json:"original_proposal_version"`
	OriginalBaseline        BudgetBaseline         `json:"original_baseline"`
	CurrentBaseline         BudgetBaseline         `json:"current_baseline"`
	Phases                  []ProjectPhaseView     `json:"phases"`
	ProjectTasks            []ProjectTaskView      `json:"project_tasks"`
	ResourcePlans           []ResourcePlanView     `json:"resource_plans"`
	CostActuals             []CostActualView       `json:"cost_actuals"`
	Capacity                []CapacityResourceView `json:"capacity"`
	Financials              *FinancialSummary      `json:"financials,omitempty"`
	ChangeOrders            []ChangeOrderView      `json:"change_orders"`
	FinancialsVisible       bool                   `json:"financials_visible"`
	Version                 int64                  `json:"version"`
}

type ProjectQueryRepository interface {
	ListProjects(context.Context, scope.Target, int) ([]ProjectSummary, error)
	FindProjectsByReference(
		context.Context,
		scope.Target,
		string,
		int,
	) ([]ProjectSummary, error)
	LoadProjectWorkspace(context.Context, scope.Target, ProjectID) (ProjectWorkspace, error)
}

type QueryService struct {
	repository ProjectQueryRepository
	financials *FinancialService
	capacity   *CapacityService
}

func (s *QueryService) WithCapacity(capacity *CapacityService) *QueryService {
	s.capacity = capacity
	return s
}

func NewQueryService(
	repository ProjectQueryRepository,
	financials ...*FinancialService,
) *QueryService {
	service := &QueryService{repository: repository}
	if len(financials) > 0 {
		service.financials = financials[0]
	}
	return service
}

func (s *QueryService) List(ctx context.Context, principal authorization.Principal, target scope.Target, limit int) ([]ProjectSummary, error) {
	target = projectTarget(principal, target)
	if target.ClientID == "" || s.repository == nil {
		return nil, ErrInvalidProject
	}
	if err := authorization.Authorize(principal, "project.read", target); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return s.repository.ListProjects(ctx, target, limit)
}

func (s *QueryService) ResolveReference(
	ctx context.Context,
	principal authorization.Principal,
	target scope.Target,
	reference string,
) ([]ProjectSummary, error) {
	target = projectTarget(principal, target)
	if target.ClientID == "" ||
		normalizeProjectReference(reference) == "" ||
		s.repository == nil {
		return nil, ErrInvalidProject
	}
	if err := authorization.Authorize(principal, "project.read", target); err != nil {
		return nil, err
	}
	matches, err := s.repository.FindProjectsByReference(ctx, target, reference, 2)
	if err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, ErrAmbiguousReference
	}
	return matches, nil
}

func (s *QueryService) Get(ctx context.Context, principal authorization.Principal, target scope.Target, id ProjectID) (ProjectWorkspace, error) {
	return s.get(ctx, principal, target, id, true)
}

// GetOperational is the minimized operational-detail boundary. It retains
// normal scoped project and financial authorization while deliberately
// avoiding capacity planning, which is not part of the returned DTO.
func (s *QueryService) GetOperational(ctx context.Context, principal authorization.Principal, target scope.Target, id ProjectID) (ProjectWorkspace, error) {
	return s.get(ctx, principal, target, id, false)
}

func (s *QueryService) get(ctx context.Context, principal authorization.Principal, target scope.Target, id ProjectID, includeCapacity bool) (ProjectWorkspace, error) {
	target = projectTarget(principal, target)
	if target.ClientID == "" || id == "" || s.repository == nil {
		return ProjectWorkspace{}, ErrInvalidProject
	}
	if err := authorization.Authorize(principal, "project.read", target); err != nil {
		return ProjectWorkspace{}, err
	}
	workspace, err := s.repository.LoadProjectWorkspace(ctx, target, id)
	if err != nil {
		return ProjectWorkspace{}, err
	}
	if authorization.Authorize(principal, "project.financial.read", target) != nil {
		workspace.OriginalBaseline = BudgetBaseline{}
		workspace.CurrentBaseline = BudgetBaseline{}
		for index := range workspace.Phases {
			workspace.Phases[index].Budget = Money{}
			workspace.Phases[index].Financials = nil
		}
		for index := range workspace.ChangeOrders {
			if workspace.ChangeOrders[index].Version != nil {
				workspace.ChangeOrders[index].Version.RevenueDeltaMinor = 0
				workspace.ChangeOrders[index].Version.CostDeltaMinor = 0
			}
		}
		workspace.CostActuals = []CostActualView{}
		workspace.Financials = nil
	} else {
		workspace.FinancialsVisible = true
		if s.financials != nil {
			summary, err := s.financials.Calculate(
				ctx, principal, target, id,
			)
			if err != nil {
				return ProjectWorkspace{}, err
			}
			workspace.Financials = &summary
			phaseSummaries, err := s.financials.CalculatePhases(
				ctx, principal, target, id,
			)
			if err != nil {
				return ProjectWorkspace{}, err
			}
			for index := range workspace.Phases {
				phaseID := workspace.Phases[index].ID
				if phaseSummary, exists := phaseSummaries[phaseID]; exists {
					workspace.Phases[index].Financials = &phaseSummary
				}
			}
		}
	}
	workspace.Capacity = []CapacityResourceView{}
	if includeCapacity && s.capacity != nil &&
		authorization.Authorize(principal, "project.resource.plan", target) == nil {
		if err := s.populateCapacity(ctx, target, &workspace); err != nil {
			return ProjectWorkspace{}, err
		}
	}
	return workspace, nil
}

func (s *QueryService) populateCapacity(
	ctx context.Context,
	target scope.Target,
	workspace *ProjectWorkspace,
) error {
	start, startErr := time.Parse("2006-01-02", workspace.PlannedStart)
	end, endErr := time.Parse("2006-01-02", workspace.PlannedEnd)
	if startErr != nil || endErr != nil || end.Before(start) {
		return nil
	}
	resourceSet := make(map[string]struct{})
	for _, task := range workspace.ProjectTasks {
		if task.OwnerID != "" {
			resourceSet[task.OwnerID] = struct{}{}
		}
	}
	for _, phase := range workspace.Phases {
		for _, task := range phase.Tasks {
			if task.OwnerID != "" {
				resourceSet[task.OwnerID] = struct{}{}
			}
		}
	}
	resourceIDs := make([]string, 0, len(resourceSet))
	for resourceID := range resourceSet {
		resourceIDs = append(resourceIDs, resourceID)
	}
	sort.Strings(resourceIDs)
	if len(resourceIDs) == 0 {
		return nil
	}
	view, err := s.capacity.Calculate(
		// Technician capacity is an MSP-wide scheduling lens. The workspace
		// remains client-scoped, while consumption includes work from every
		// client the authenticated internal technician is authorized to serve.
		ctx, scope.Target{MSPID: target.MSPID},
		CapacityWindow{Start: start.UTC(), End: end.AddDate(0, 0, 1).UTC()},
		resourceIDs,
	)
	if err != nil {
		return err
	}
	for _, resourceID := range resourceIDs {
		capacity, exists := view.Resources[resourceID]
		if !exists {
			continue
		}
		workspace.Capacity = append(workspace.Capacity, CapacityResourceView{
			ID: resourceID, Name: capacity.Name,
			AvailableMinutes:  int64(capacity.Available / time.Minute),
			ScheduledMinutes:  int64(capacity.Scheduled / time.Minute),
			ActualMinutes:     int64(capacity.Actual / time.Minute),
			RemainingMinutes:  int64(capacity.Remaining / time.Minute),
			OverbookedMinutes: int64(capacity.Overbooked / time.Minute),
		})
	}
	return nil
}
