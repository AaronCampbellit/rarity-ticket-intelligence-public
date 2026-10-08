package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type operationalDirectoryStub struct {
	client organizations.Client
	err    error
	calls  []resourceClientResolution
}

func (s *operationalDirectoryStub) ResolveActiveClient(
	_ context.Context,
	_ authorization.Principal,
	capability string,
	reference string,
) (organizations.Client, error) {
	s.calls = append(s.calls, resourceClientResolution{capability: capability, reference: reference})
	if s.err != nil {
		return organizations.Client{}, s.err
	}
	return s.client, nil
}

type operationalProjectQueriesStub struct {
	listed    []projects.ProjectSummary
	resolved  []projects.ProjectSummary
	workspace projects.ProjectWorkspace
	target    scope.Target
	limit     int
	reference string
}

func (s *operationalProjectQueriesStub) List(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	limit int,
) ([]projects.ProjectSummary, error) {
	s.target, s.limit = target, limit
	return s.listed, nil
}

func (s *operationalProjectQueriesStub) ResolveReference(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	reference string,
) ([]projects.ProjectSummary, error) {
	s.target, s.reference = target, reference
	return s.resolved, nil
}

func (s *operationalProjectQueriesStub) GetOperational(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	_ projects.ProjectID,
) (projects.ProjectWorkspace, error) {
	s.target = target
	return s.workspace, nil
}

type operationalKnowledgeQueriesStub struct {
	listed   []knowledge.Article
	resolved []knowledge.Article
	detail   knowledge.ArticleDetail
	target   scope.Target
	limit    int
	query    string
}

func (s *operationalKnowledgeQueriesStub) List(
	_ context.Context,
	command knowledge.ListCommand,
) ([]knowledge.Article, error) {
	s.target, s.limit, s.query = command.Target, command.Limit, command.Query
	return s.listed, nil
}

func (s *operationalKnowledgeQueriesStub) ResolveReference(
	_ context.Context,
	command knowledge.ResolveReferenceCommand,
) ([]knowledge.Article, error) {
	s.target, s.query = command.Target, command.Reference
	return s.resolved, nil
}

func (s *operationalKnowledgeQueriesStub) Find(
	_ context.Context,
	command knowledge.FindCommand,
) (knowledge.ArticleDetail, error) {
	s.target = command.Target
	return s.detail, nil
}

type operationalProspectsStub struct {
	items  []sales.Prospect
	target scope.Target
	limit  int
}

func (s *operationalProspectsStub) ListProspectsAt(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	limit int,
) ([]sales.Prospect, error) {
	s.target, s.limit = target, limit
	return s.items, nil
}

func operationalClient() organizations.Client {
	return organizations.Client{Envelope: object.Envelope{ID: "client-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "NW-100", LifecycleState: "active"}, Name: "Northwind Legal"}
}

func TestOperationalReadProjectSearchRequiresActiveNamedClientAndMinimizesOutput(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	queries := &operationalProjectQueriesStub{listed: []projects.ProjectSummary{{
		ID: "project-1", DisplayID: "PRJ-100", Name: "Network refresh",
		LifecycleState: "active", PlannedStart: "2026-08-01", Version: 4,
	}}}
	tool := NewProjectSearchTool(directory, queries)
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), testPrincipal("project.read"), json.RawMessage(`{"client":"Northwind Legal","limit":25}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(prepared) != `{"client_id":"client-1","limit":25}` || len(directory.calls) != 1 || directory.calls[0].capability != "project.read" {
		t.Fatalf("prepared=%s calls=%+v", prepared, directory.calls)
	}
	result, err := tool.Execute(context.Background(), testPrincipal("project.read"), prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	if queries.target != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) || queries.limit != 25 {
		t.Fatalf("target=%+v limit=%d", queries.target, queries.limit)
	}
	projects, ok := result.Data["projects"].([]projectSearchResult)
	if !ok || len(projects) != 1 || projects[0].ProjectID != "project-1" || projects[0].MSPID != "" || projects[0].ClientID != "" {
		t.Fatalf("projects=%#v", result.Data["projects"])
	}
	if _, exposed := result.Data["financials"]; exposed {
		t.Fatalf("result leaked financials: %+v", result.Data)
	}
}

func TestOperationalReadProjectGetHandlesExactReferencesAndFinancialCapability(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	queries := &operationalProjectQueriesStub{
		resolved: []projects.ProjectSummary{{ID: "project-1"}},
		workspace: projects.ProjectWorkspace{
			ID: "project-1", DisplayID: "PRJ-100", Name: "Network refresh", ClientName: "Northwind Legal",
			LifecycleState: "active", OriginalBaseline: projects.BudgetBaseline{Currency: "USD", RevenueMinor: 12000},
			CurrentBaseline: projects.BudgetBaseline{Currency: "USD", RevenueMinor: 15000},
			Phases:          []projects.ProjectPhaseView{{ID: "phase-1", Name: "Deploy", Budget: projects.Money{Currency: "USD", Minor: 5000}}},
		},
	}
	tool := NewProjectGetTool(directory, queries)
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), testPrincipal("project.read"), json.RawMessage(`{"client":"NW-100","project":"PRJ-100"}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), testPrincipal("project.read"), prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	project := result.Data["project"].(projectGetResult)
	if queries.reference != "PRJ-100" || project.Financials != nil || project.Phases[0].Budget != nil {
		t.Fatalf("reference=%q project=%+v", queries.reference, project)
	}
	financialReader := testPrincipal("project.read", "project.financial.read")
	result, err = tool.Execute(context.Background(), financialReader, prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	project = result.Data["project"].(projectGetResult)
	if project.Financials == nil || project.Financials.CurrentBaseline.RevenueMinor != 15000 || project.Phases[0].Budget == nil {
		t.Fatalf("financial project=%+v", project)
	}
	queries.resolved = nil
	if _, err := tool.Execute(context.Background(), testPrincipal("project.read"), prepared, ""); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("missing error=%v, want scope.ErrNotFound", err)
	}
	queries.resolved = []projects.ProjectSummary{{ID: "a"}, {ID: "b"}}
	if _, err := tool.Execute(context.Background(), testPrincipal("project.read"), prepared, ""); !errors.Is(err, projects.ErrAmbiguousReference) {
		t.Fatalf("ambiguous error=%v, want projects.ErrAmbiguousReference", err)
	}
}

func TestOperationalReadKnowledgeSearchRejectsCrossClientAndHiddenFields(t *testing.T) {
	directory := &operationalDirectoryStub{err: scope.ErrNotFound}
	tool := NewKnowledgeSearchTool(directory, &operationalKnowledgeQueriesStub{})
	_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), testPrincipal("knowledge.read"), json.RawMessage(`{"client":"Inactive Client","limit":10}`))
	if !errors.Is(err, aiassist.ErrInvalidTool) {
		t.Fatalf("inactive client error=%v", err)
	}
	if len(directory.calls) != 1 || directory.calls[0].capability != "knowledge.read" {
		t.Fatalf("calls=%+v", directory.calls)
	}
	directory.err = nil
	directory.client = operationalClient()
	for _, raw := range []string{
		`{"client":"Northwind Legal","limit":51}`,
		`{"client":"Northwind Legal","client_id":"client-2"}`,
	} {
		if _, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), testPrincipal("knowledge.read"), json.RawMessage(raw)); !errors.Is(err, aiassist.ErrInvalidTool) {
			t.Fatalf("Prepare(%s) error=%v", raw, err)
		}
	}
}

func TestOperationalReadKnowledgeGetReturnsMinimizedExactArticle(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	queries := &operationalKnowledgeQueriesStub{
		resolved: []knowledge.Article{{ID: "article-1"}},
		detail: knowledge.ArticleDetail{
			Article: knowledge.Article{ID: "article-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "KB-100", Title: "Reset VPN", State: knowledge.Published, CurrentVersion: 2, UpdatedBy: "technician-1", UpdatedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
			Version: knowledge.Version{ArticleID: "article-1", Version: 2, Body: "Reset steps", CreatedBy: "technician-1"},
		},
	}
	tool := NewKnowledgeGetTool(directory, queries)
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), testPrincipal("knowledge.read"), json.RawMessage(`{"client":"Northwind Legal","article":"KB-100"}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), testPrincipal("knowledge.read"), prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	article := result.Data["article"].(knowledgeGetResult)
	if article.ArticleID != "article-1" || article.Body != "Reset steps" || article.MSPID != "" || article.ClientID != "" || article.UpdatedBy != "" {
		t.Fatalf("article=%+v", article)
	}
	queries.resolved = []knowledge.Article{{ID: "one"}, {ID: "two"}}
	if _, err := tool.Execute(context.Background(), testPrincipal("knowledge.read"), prepared, ""); !errors.Is(err, knowledge.ErrAmbiguousReference) {
		t.Fatalf("ambiguous error=%v, want knowledge.ErrAmbiguousReference", err)
	}
	queries.resolved = nil
	if _, err := tool.Execute(context.Background(), testPrincipal("knowledge.read"), prepared, ""); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("missing error=%v, want scope.ErrNotFound", err)
	}
}

func TestOperationalReadProspectListIsMSPGlobalBoundedAndContactMinimal(t *testing.T) {
	queries := &operationalProspectsStub{items: []sales.Prospect{{
		ID: "prospect-1", MSPID: "msp-1", DisplayID: "PRO-100", Name: "Contoso", Email: "sales@contoso.example", Phone: "555-0100", Version: 4,
	}}}
	tool := NewProspectListTool(queries)
	principal := testPrincipal("opportunity.read")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(`{"limit":50}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), principal, prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	if queries.target != (scope.Target{MSPID: "msp-1"}) || queries.limit != 50 {
		t.Fatalf("target=%+v limit=%d", queries.target, queries.limit)
	}
	prospects := result.Data["prospects"].([]prospectListResult)
	if len(prospects) != 1 || prospects[0].ProspectID != "prospect-1" || prospects[0].Email != "" || prospects[0].Phone != "" || prospects[0].MSPID != "" {
		t.Fatalf("prospects=%+v", prospects)
	}
	if _, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(`{"limit":51}`)); !errors.Is(err, aiassist.ErrInvalidTool) {
		t.Fatalf("unbounded limit error=%v", err)
	}
	clientPrincipal := principal
	clientPrincipal.Scope.ClientID = "client-1"
	if _, err := tool.ResolveScope(context.Background(), clientPrincipal, prepared); err != nil {
		t.Fatalf("scope resolution should defer client-scope denial to authorization: %v", err)
	}
	if err := authorization.Authorize(clientPrincipal, tool.RequiredCapability(), scope.Target{MSPID: "msp-1"}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("client-scoped MSP list authorization error=%v", err)
	}
}
