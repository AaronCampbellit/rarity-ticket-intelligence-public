package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type projectActionsStub struct {
	update projects.UpdatePhaseCommand
}

func (*projectActionsStub) Create(
	context.Context,
	projects.CreateCommand,
) (projects.Project, error) {
	return projects.Project{}, nil
}

func (s *projectActionsStub) UpdatePhase(
	_ context.Context,
	command projects.UpdatePhaseCommand,
) (projects.Phase, error) {
	s.update = command
	return projects.Phase{ID: command.ID, Version: command.ExpectedVersion + 1}, nil
}

type projectQueryActionsStub struct {
	target       scope.Target
	id           projects.ProjectID
	limit        int
	listCalls    int
	resolveCalls int
	reference    string
	projects     []projects.ProjectSummary
	resolveErr   error
}

type changeOrderActionsStub struct {
	create projects.CreateChangeOrderCommand
}

type costActualActionsStub struct {
	create projects.CreateCostActualCommand
}

type availabilityActionsStub struct {
	create projects.CreateAvailabilityCommand
}

type financialInputActionsStub struct {
	rate        projects.CreateLaborCostRateCommand
	recognition projects.RecognizeBillableWorkCommand
}

func (s *financialInputActionsStub) CreateLaborCostRate(
	_ context.Context,
	command projects.CreateLaborCostRateCommand,
) (projects.TechnicianLaborCostRate, error) {
	s.rate = command
	return projects.TechnicianLaborCostRate{ID: "rate-id", Version: 1}, nil
}

func (s *financialInputActionsStub) RecognizeBillableWork(
	_ context.Context,
	command projects.RecognizeBillableWorkCommand,
) (projects.BillableWorkRecognition, error) {
	s.recognition = command
	return projects.BillableWorkRecognition{
		ID: "recognition-id", Version: 1,
	}, nil
}

func (s *availabilityActionsStub) Create(
	_ context.Context,
	command projects.CreateAvailabilityCommand,
) (projects.TechnicianAvailability, error) {
	s.create = command
	return projects.TechnicianAvailability{ID: "availability-id", Version: 1}, nil
}

func (s *costActualActionsStub) Create(
	_ context.Context,
	command projects.CreateCostActualCommand,
) (projects.CostActual, error) {
	s.create = command
	return projects.CostActual{ID: "cost-id", Version: 1}, nil
}

func (s *changeOrderActionsStub) Create(
	_ context.Context,
	command projects.CreateChangeOrderCommand,
) (projects.ChangeOrder, error) {
	s.create = command
	return projects.ChangeOrder{ID: "order-id", Version: 1}, nil
}

func (*changeOrderActionsStub) IssueVersion(context.Context, projects.IssueChangeOrderCommand) (projects.ChangeOrderVersion, error) {
	return projects.ChangeOrderVersion{}, nil
}

func (*changeOrderActionsStub) Approve(context.Context, projects.ApprovalCommand) (projects.ChangeOrderVersion, error) {
	return projects.ChangeOrderVersion{}, nil
}

func (*changeOrderActionsStub) OverrideApproval(context.Context, projects.OverrideCommand) (projects.ChangeOrderVersion, error) {
	return projects.ChangeOrderVersion{}, nil
}

func (*changeOrderActionsStub) Apply(context.Context, projects.ApplyChangeOrderCommand) (projects.Project, error) {
	return projects.Project{}, nil
}

func (s *projectQueryActionsStub) List(_ context.Context, _ authorization.Principal, target scope.Target, limit int) ([]projects.ProjectSummary, error) {
	s.target, s.limit = target, limit
	s.listCalls++
	if s.projects != nil {
		found := append([]projects.ProjectSummary(nil), s.projects...)
		if len(found) > limit {
			found = found[:limit]
		}
		return found, nil
	}
	return []projects.ProjectSummary{{ID: "project-id", DisplayID: "PRJ-1", Version: 1}}, nil
}

func (s *projectQueryActionsStub) ResolveReference(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	reference string,
) ([]projects.ProjectSummary, error) {
	s.target, s.reference = target, reference
	s.resolveCalls++
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	found := make([]projects.ProjectSummary, 0, 2)
	matchesByID := make(map[projects.ProjectID]struct{}, 2)
	for _, project := range s.projects {
		if !projects.ProjectReferenceMatches(
			reference, project.Name, project.DisplayID,
		) {
			continue
		}
		if _, exists := matchesByID[project.ID]; exists {
			continue
		}
		matchesByID[project.ID] = struct{}{}
		found = append(found, project)
		if len(found) == 2 {
			break
		}
	}
	return found, nil
}

func (s *projectQueryActionsStub) Get(_ context.Context, _ authorization.Principal, target scope.Target, id projects.ProjectID) (projects.ProjectWorkspace, error) {
	s.target, s.id = target, id
	return projects.ProjectWorkspace{
		ID: id, DisplayID: "PRJ-1", ClientName: "Northwind",
		Phases: []projects.ProjectPhaseView{}, ChangeOrders: []projects.ChangeOrderView{},
		Financials: &projects.FinancialSummary{
			OriginalBudget: projects.Money{Minor: 20000, Currency: "USD"},
			Profit:         projects.Money{Minor: 8000, Currency: "USD"},
		},
		Version: 2,
	}, nil
}

func projectQueryPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{
		ID:           "technician-id",
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("project.read"),
	}, nil
}

func TestProjectQueryRoutesUseTrustedScopeAndReturnVersion(t *testing.T) {
	actions := &projectQueryActionsStub{}
	handler := NewRouter(Dependencies{Principal: projectQueryPrincipal, ProjectQueries: actions})

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=25", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || actions.limit != 25 ||
		actions.target.ClientID != "client-id" ||
		!strings.Contains(listResponse.Body.String(), `"display_id":"PRJ-1"`) {
		t.Fatalf("status=%d target=%+v limit=%d body=%s", listResponse.Code, actions.target, actions.limit, listResponse.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-id", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("ETag") != `"2"` ||
		actions.id != "project-id" || strings.Contains(getResponse.Body.String(), "msp-id") ||
		!strings.Contains(getResponse.Body.String(), `"original_budget"`) ||
		!strings.Contains(getResponse.Body.String(), `"profit":{"minor":8000`) {
		t.Fatalf("status=%d id=%s body=%s", getResponse.Code, actions.id, getResponse.Body.String())
	}
}

func TestUpdatePhaseRouteUsesTrustedScopeAndIfMatch(t *testing.T) {
	actions := &projectActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: projectQueryPrincipal,
		Projects:  actions,
	})
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/phases/phase-id",
		bytes.NewBufferString(`{
			"expected_version":2,
			"name":"Delivery",
			"planned_minutes":600,
			"budget":{"minor":10000,"currency":"USD"}
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"2"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		actions.update.Target.ClientID != "client-id" ||
		actions.update.ExpectedVersion != 2 ||
		actions.update.ActorID != "technician-id" ||
		response.Header().Get("ETag") != `"3"` {
		t.Fatalf("status=%d command=%+v headers=%v body=%s",
			response.Code, actions.update, response.Header(), response.Body.String())
	}
}

func TestCreateChangeOrderRouteUsesTrustedProjectScope(t *testing.T) {
	actions := &changeOrderActionsStub{}
	handler := NewRouter(Dependencies{
		Principal:    projectQueryPrincipal,
		ChangeOrders: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/projects/project-id/change-orders",
		bytes.NewBufferString(`{"display_id":"CO-1042"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.create.ProjectID != "project-id" ||
		actions.create.Target.ClientID != "client-id" ||
		actions.create.DisplayID != "CO-1042" ||
		response.Header().Get("ETag") != `"1"` {
		t.Fatalf("status=%d command=%+v headers=%v body=%s",
			response.Code, actions.create, response.Header(), response.Body.String())
	}
}

func TestCreateCostActualRouteUsesTrustedProjectAndPhaseScope(t *testing.T) {
	actions := &costActualActionsStub{}
	handler := NewRouter(Dependencies{
		Principal:   projectQueryPrincipal,
		CostActuals: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/projects/project-id/cost-actuals",
		bytes.NewBufferString(`{
			"phase_id":"phase-id",
			"cost_type":"license",
			"description":"Security license",
			"amount":{"minor":250000,"currency":"USD"},
			"committed":true,
			"incurred_at":"2026-07-30T18:00:00Z"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.create.ProjectID != "project-id" ||
		actions.create.PhaseID != "phase-id" ||
		actions.create.Target.ClientID != "client-id" ||
		!actions.create.Committed ||
		actions.create.ActorID != "technician-id" ||
		response.Header().Get("ETag") != `"1"` {
		t.Fatalf("status=%d command=%+v headers=%v body=%s",
			response.Code, actions.create, response.Header(), response.Body.String())
	}
}

func TestCreateTechnicianAvailabilityRouteUsesTrustedMSPScope(t *testing.T) {
	actions := &availabilityActionsStub{}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "manager-id", Scope: scope.Principal{MSPID: "msp-id"},
			}, nil
		},
		Availability: actions,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/technicians/technician-id/availability",
		bytes.NewBufferString(`{
			"starts_at":"2026-08-03T14:00:00Z",
			"ends_at":"2026-08-03T22:00:00Z",
			"available_minutes":480
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		actions.create.TechnicianID != "technician-id" ||
		actions.create.Principal.Scope.ClientID != "" ||
		actions.create.ActorID != "manager-id" ||
		response.Header().Get("ETag") != `"1"` {
		t.Fatalf("status=%d command=%+v headers=%v body=%s",
			response.Code, actions.create, response.Header(), response.Body.String())
	}
}

func TestProjectFinancialInputRoutesUseTrustedScopes(t *testing.T) {
	actions := &financialInputActionsStub{}
	globalHandler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{
				ID: "manager-id", Scope: scope.Principal{MSPID: "msp-id"},
			}, nil
		},
		FinancialInputs: actions,
	})
	rateRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/technicians/technician-id/labor-cost-rates",
		bytes.NewBufferString(`{
			"hourly_rate":{"minor":8500,"currency":"USD"},
			"effective_at":"2026-08-01T00:00:00Z"
		}`),
	)
	rateRequest.Header.Set("Content-Type", "application/json")
	rateResponse := httptest.NewRecorder()
	globalHandler.ServeHTTP(rateResponse, rateRequest)
	if rateResponse.Code != http.StatusCreated ||
		actions.rate.TechnicianID != "technician-id" ||
		actions.rate.ActorID != "manager-id" {
		t.Fatalf("status=%d command=%+v body=%s",
			rateResponse.Code, actions.rate, rateResponse.Body.String())
	}

	clientHandler := NewRouter(Dependencies{
		Principal:       projectQueryPrincipal,
		FinancialInputs: actions,
	})
	recognitionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/projects/project-id/billable-work",
		bytes.NewBufferString(`{
			"phase_id":"phase-id",
			"description":"Accepted milestone",
			"amount":{"minor":1200000,"currency":"USD"},
			"recognized_at":"2026-08-15T18:00:00Z"
		}`),
	)
	recognitionRequest.Header.Set("Content-Type", "application/json")
	recognitionResponse := httptest.NewRecorder()
	clientHandler.ServeHTTP(recognitionResponse, recognitionRequest)
	if recognitionResponse.Code != http.StatusCreated ||
		actions.recognition.ProjectID != "project-id" ||
		actions.recognition.PhaseID != "phase-id" ||
		actions.recognition.Target.ClientID != "client-id" {
		t.Fatalf("status=%d command=%+v body=%s",
			recognitionResponse.Code, actions.recognition,
			recognitionResponse.Body.String())
	}
}
