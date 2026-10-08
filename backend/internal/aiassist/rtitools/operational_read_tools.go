package rtitools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const operationalReadDefaultLimit = 25

type ProjectOperationalQueries interface {
	List(context.Context, authorization.Principal, scope.Target, int) ([]projects.ProjectSummary, error)
	ResolveReference(context.Context, authorization.Principal, scope.Target, string) ([]projects.ProjectSummary, error)
	GetOperational(context.Context, authorization.Principal, scope.Target, projects.ProjectID) (projects.ProjectWorkspace, error)
}

type KnowledgeOperationalQueries interface {
	List(context.Context, knowledge.ListCommand) ([]knowledge.Article, error)
	ResolveReference(context.Context, knowledge.ResolveReferenceCommand) ([]knowledge.Article, error)
	Find(context.Context, knowledge.FindCommand) (knowledge.ArticleDetail, error)
}

type ProspectOperationalQueries interface {
	ListProspectsAt(context.Context, authorization.Principal, scope.Target, int) ([]sales.Prospect, error)
}

type operationalClientListRequest struct {
	Client string `json:"client"`
	Limit  *int   `json:"limit,omitempty"`
}

type operationalClientListInput struct {
	ClientID string `json:"client_id"`
	Limit    int    `json:"limit"`
}

type projectGetRequest struct {
	Client  string `json:"client"`
	Project string `json:"project"`
}

type knowledgeSearchRequest struct {
	Client string  `json:"client"`
	Query  *string `json:"query,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
}

type knowledgeSearchInput struct {
	ClientID string `json:"client_id"`
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
}

type knowledgeGetRequest struct {
	Client  string `json:"client"`
	Article string `json:"article"`
}

type operationalGetInput struct {
	ClientID  string `json:"client_id"`
	Reference string `json:"reference"`
}

type prospectListRequest struct {
	Limit *int `json:"limit,omitempty"`
}

type prospectListInput struct {
	Limit int `json:"limit"`
}

type projectSearchResult struct {
	ProjectID      string `json:"project_id"`
	DisplayID      string `json:"display_id"`
	Name           string `json:"name"`
	LifecycleState string `json:"lifecycle_state"`
	PlannedStart   string `json:"planned_start,omitempty"`
	PlannedEnd     string `json:"planned_end,omitempty"`
	Version        int64  `json:"version"`
	MSPID          string `json:"-"`
	ClientID       string `json:"-"`
}

type projectTaskResult struct {
	TaskID  string `json:"task_id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	OwnerID string `json:"owner_id,omitempty"`
	Version int64  `json:"version"`
}

type projectPhaseResult struct {
	PhaseID        string          `json:"phase_id"`
	Position       int             `json:"position"`
	Name           string          `json:"name"`
	State          string          `json:"state"`
	PlannedStart   string          `json:"planned_start,omitempty"`
	PlannedEnd     string          `json:"planned_end,omitempty"`
	PlannedMinutes int64           `json:"planned_minutes"`
	ActualMinutes  int64           `json:"actual_minutes"`
	Budget         *projects.Money `json:"budget,omitempty"`
	Version        int64           `json:"version"`
}

type projectBaselineResult struct {
	Currency       string `json:"currency"`
	RevenueMinor   int64  `json:"revenue_minor"`
	CostMinor      int64  `json:"cost_minor"`
	PlannedMinutes int64  `json:"planned_minutes"`
}

type projectFinancialResult struct {
	OriginalBaseline projectBaselineResult `json:"original_baseline"`
	CurrentBaseline  projectBaselineResult `json:"current_baseline"`
}

type projectGetResult struct {
	ProjectID      string                  `json:"project_id"`
	DisplayID      string                  `json:"display_id"`
	Name           string                  `json:"name"`
	ClientName     string                  `json:"client_name"`
	LifecycleState string                  `json:"lifecycle_state"`
	PlannedStart   string                  `json:"planned_start,omitempty"`
	PlannedEnd     string                  `json:"planned_end,omitempty"`
	Phases         []projectPhaseResult    `json:"phases"`
	Tasks          []projectTaskResult     `json:"tasks"`
	Financials     *projectFinancialResult `json:"financials,omitempty"`
	Version        int64                   `json:"version"`
}

type knowledgeSearchResult struct {
	ArticleID      string          `json:"article_id"`
	DisplayID      string          `json:"display_id"`
	Title          string          `json:"title"`
	State          knowledge.State `json:"state"`
	CurrentVersion int64           `json:"current_version"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type knowledgeGetResult struct {
	ArticleID      string          `json:"article_id"`
	DisplayID      string          `json:"display_id"`
	Title          string          `json:"title"`
	State          knowledge.State `json:"state"`
	CurrentVersion int64           `json:"current_version"`
	Body           string          `json:"body"`
	UpdatedAt      time.Time       `json:"updated_at"`
	MSPID          string          `json:"-"`
	ClientID       string          `json:"-"`
	UpdatedBy      string          `json:"-"`
}

type prospectListResult struct {
	ProspectID string `json:"prospect_id"`
	DisplayID  string `json:"display_id"`
	Name       string `json:"name"`
	Version    int64  `json:"version"`
	Email      string `json:"-"`
	Phone      string `json:"-"`
	MSPID      string `json:"-"`
}

func NewProjectSearchTool(directory ActiveClientResolver, queries ProjectOperationalQueries) aiassist.Tool {
	return &functionalTool{
		name: "project.search", capability: "project.read", kind: aiassist.ToolRead,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[operationalClientListRequest](raw)
			if err != nil || queries == nil {
				return nil, aiassist.ErrInvalidTool
			}
			input, err := prepareOperationalClientList(ctx, directory, principal, "project.read", request)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(input)
		},
		validate: validateOperationalClientList,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := validOperationalClientList(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			items, err := queries.List(ctx, principal, target(principal, input.ClientID), input.Limit)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			result := make([]projectSearchResult, 0, len(items))
			for _, item := range items {
				result = append(result, projectSearchResult{ProjectID: string(item.ID), DisplayID: item.DisplayID, Name: item.Name, LifecycleState: item.LifecycleState, PlannedStart: item.PlannedStart, PlannedEnd: item.PlannedEnd, Version: item.Version})
			}
			return aiassist.ToolResult{Summary: "Listed projects", Data: map[string]any{"projects": result}}, nil
		},
	}
}

func NewProjectGetTool(directory ActiveClientResolver, queries ProjectOperationalQueries) aiassist.Tool {
	return newProjectGetTool(directory, queries)
}

func newProjectGetTool(directory ActiveClientResolver, queries ProjectOperationalQueries) aiassist.Tool {
	return &functionalTool{
		name: "project.get", capability: "project.read", kind: aiassist.ToolRead,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[projectGetRequest](raw)
			if err != nil || queries == nil || strings.TrimSpace(request.Project) == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "project.read", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(operationalGetInput{ClientID: client.ID, Reference: strings.TrimSpace(request.Project)})
		},
		validate: validateOperationalGet,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := validOperationalGet(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			target := target(principal, input.ClientID)
			matches, err := queries.ResolveReference(ctx, principal, target, input.Reference)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			if len(matches) > 1 {
				return aiassist.ToolResult{}, projects.ErrAmbiguousReference
			}
			if len(matches) == 0 || strings.TrimSpace(string(matches[0].ID)) == "" {
				return aiassist.ToolResult{}, scope.ErrNotFound
			}
			workspace, err := queries.GetOperational(ctx, principal, target, matches[0].ID)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Found project " + workspace.DisplayID, Data: map[string]any{"project": minimizeProjectWorkspace(workspace, authorization.Authorize(principal, "project.financial.read", target) == nil)}}, nil
		},
	}
}

func NewKnowledgeSearchTool(directory ActiveClientResolver, queries KnowledgeOperationalQueries) aiassist.Tool {
	return &functionalTool{
		name: "knowledge.search", capability: "knowledge.read", kind: aiassist.ToolRead,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[knowledgeSearchRequest](raw)
			if err != nil || queries == nil || !optionalValueValid(request.Query) {
				return nil, aiassist.ErrInvalidTool
			}
			limit, ok := operationalReadLimit(request.Limit)
			if !ok || len(optionalValue(request.Query)) > 200 {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.read", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(knowledgeSearchInput{ClientID: client.ID, Query: optionalValue(request.Query), Limit: limit})
		},
		validate: func(raw json.RawMessage) error {
			input, err := decode[knowledgeSearchInput](raw)
			if err != nil || strings.TrimSpace(input.ClientID) == "" || input.Limit < 1 || input.Limit > 50 || len(input.Query) > 200 {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: resolveClientResourceScope,
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := decode[knowledgeSearchInput](raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			items, err := queries.List(ctx, knowledge.ListCommand{Principal: principal, Target: target(principal, input.ClientID), Query: input.Query, Limit: input.Limit})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			result := make([]knowledgeSearchResult, 0, len(items))
			for _, item := range items {
				result = append(result, minimizeKnowledgeArticle(item))
			}
			return aiassist.ToolResult{Summary: "Listed knowledge articles", Data: map[string]any{"articles": result}}, nil
		},
	}
}

func NewKnowledgeGetTool(directory ActiveClientResolver, queries KnowledgeOperationalQueries) aiassist.Tool {
	return &functionalTool{
		name: "knowledge.get", capability: "knowledge.read", kind: aiassist.ToolRead,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[knowledgeGetRequest](raw)
			if err != nil || queries == nil || strings.TrimSpace(request.Article) == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "knowledge.read", request.Client)
			if err != nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(operationalGetInput{ClientID: client.ID, Reference: strings.TrimSpace(request.Article)})
		},
		validate: validateOperationalGet,
		resolve:  resolveClientResourceScope,
		preview:  noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := validOperationalGet(raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			target := target(principal, input.ClientID)
			matches, err := queries.ResolveReference(ctx, knowledge.ResolveReferenceCommand{Principal: principal, Target: target, Reference: input.Reference})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			if len(matches) > 1 {
				return aiassist.ToolResult{}, knowledge.ErrAmbiguousReference
			}
			if len(matches) == 0 || strings.TrimSpace(matches[0].ID) == "" {
				return aiassist.ToolResult{}, scope.ErrNotFound
			}
			detail, err := queries.Find(ctx, knowledge.FindCommand{Principal: principal, Target: target, ArticleID: matches[0].ID})
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Found knowledge article " + detail.Article.DisplayID, Data: map[string]any{"article": knowledgeGetResult{ArticleID: detail.Article.ID, DisplayID: detail.Article.DisplayID, Title: detail.Article.Title, State: detail.Article.State, CurrentVersion: detail.Article.CurrentVersion, Body: detail.Version.Body, UpdatedAt: detail.Article.UpdatedAt}}}, nil
		},
	}
}

func NewProspectListTool(queries ProspectOperationalQueries) aiassist.Tool {
	return &functionalTool{
		name: "prospect.list", capability: "opportunity.read", kind: aiassist.ToolRead,
		prepare: func(_ context.Context, _ authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[prospectListRequest](raw)
			limit, ok := operationalReadLimit(request.Limit)
			if err != nil || !ok || queries == nil {
				return nil, aiassist.ErrInvalidTool
			}
			return marshalPrepared(prospectListInput{Limit: limit})
		},
		validate: func(raw json.RawMessage) error {
			input, err := decode[prospectListInput](raw)
			if err != nil || input.Limit < 1 || input.Limit > 50 {
				return aiassist.ErrInvalidTool
			}
			return nil
		},
		resolve: func(principal authorization.Principal, _ json.RawMessage) (scope.Target, error) {
			if strings.TrimSpace(principal.Scope.MSPID) == "" {
				return scope.Target{}, aiassist.ErrInvalidTool
			}
			return scope.Target{MSPID: principal.Scope.MSPID}, nil
		},
		preview: noPreview,
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, _ string) (aiassist.ToolResult, error) {
			input, err := decode[prospectListInput](raw)
			if err != nil || queries == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			items, err := queries.ListProspectsAt(ctx, principal, scope.Target{MSPID: principal.Scope.MSPID}, input.Limit)
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			result := make([]prospectListResult, 0, len(items))
			for _, item := range items {
				result = append(result, prospectListResult{ProspectID: item.ID, DisplayID: item.DisplayID, Name: item.Name, Version: item.Version})
			}
			return aiassist.ToolResult{Summary: "Listed prospects", Data: map[string]any{"prospects": result}}, nil
		},
	}
}

func prepareOperationalClientList(ctx context.Context, directory ActiveClientResolver, principal authorization.Principal, capability string, request operationalClientListRequest) (operationalClientListInput, error) {
	limit, ok := operationalReadLimit(request.Limit)
	if !ok {
		return operationalClientListInput{}, aiassist.ErrInvalidTool
	}
	client, err := resolveActiveResourceClient(ctx, directory, principal, capability, request.Client)
	if err != nil {
		return operationalClientListInput{}, err
	}
	return operationalClientListInput{ClientID: client.ID, Limit: limit}, nil
}

func operationalReadLimit(value *int) (int, bool) {
	if value == nil {
		return operationalReadDefaultLimit, true
	}
	return *value, *value >= 1 && *value <= 50
}

func validateOperationalClientList(raw json.RawMessage) error {
	_, err := validOperationalClientList(raw)
	return err
}

func validOperationalClientList(raw json.RawMessage) (operationalClientListInput, error) {
	input, err := decode[operationalClientListInput](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || input.Limit < 1 || input.Limit > 50 {
		return operationalClientListInput{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validateOperationalGet(raw json.RawMessage) error {
	_, err := validOperationalGet(raw)
	return err
}

func validOperationalGet(raw json.RawMessage) (operationalGetInput, error) {
	input, err := decode[operationalGetInput](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.Reference) == "" {
		return operationalGetInput{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func minimizeProjectWorkspace(workspace projects.ProjectWorkspace, financialsVisible bool) projectGetResult {
	result := projectGetResult{ProjectID: string(workspace.ID), DisplayID: workspace.DisplayID, Name: workspace.Name, ClientName: workspace.ClientName, LifecycleState: workspace.LifecycleState, PlannedStart: workspace.PlannedStart, PlannedEnd: workspace.PlannedEnd, Version: workspace.Version, Phases: make([]projectPhaseResult, 0, len(workspace.Phases)), Tasks: make([]projectTaskResult, 0, len(workspace.ProjectTasks))}
	for _, phase := range workspace.Phases {
		item := projectPhaseResult{PhaseID: string(phase.ID), Position: phase.Position, Name: phase.Name, State: phase.State, PlannedStart: phase.PlannedStart, PlannedEnd: phase.PlannedEnd, PlannedMinutes: phase.PlannedMinutes, ActualMinutes: phase.ActualMinutes, Version: phase.Version}
		if financialsVisible {
			budget := phase.Budget
			item.Budget = &budget
		}
		result.Phases = append(result.Phases, item)
	}
	for _, task := range workspace.ProjectTasks {
		result.Tasks = append(result.Tasks, projectTaskResult{TaskID: task.ID, Title: task.Title, Status: task.Status, OwnerID: task.OwnerID, Version: task.Version})
	}
	if financialsVisible {
		result.Financials = &projectFinancialResult{OriginalBaseline: minimizeProjectBaseline(workspace.OriginalBaseline), CurrentBaseline: minimizeProjectBaseline(workspace.CurrentBaseline)}
	}
	return result
}

func minimizeProjectBaseline(value projects.BudgetBaseline) projectBaselineResult {
	return projectBaselineResult{Currency: value.Currency, RevenueMinor: value.RevenueMinor, CostMinor: value.CostMinor, PlannedMinutes: value.PlannedMinutes}
}

func minimizeKnowledgeArticle(article knowledge.Article) knowledgeSearchResult {
	return knowledgeSearchResult{ArticleID: article.ID, DisplayID: article.DisplayID, Title: article.Title, State: article.State, CurrentVersion: article.CurrentVersion, UpdatedAt: article.UpdatedAt}
}
