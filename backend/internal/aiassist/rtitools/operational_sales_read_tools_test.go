package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type operationalOpportunityQueriesStub struct {
	listed           []sales.Opportunity
	resolved         []sales.Opportunity
	found            sales.Opportunity
	stages           []sales.PipelineStage
	listTarget       scope.Target
	resolveTarget    scope.Target
	getTarget        scope.Target
	stageTarget      scope.Target
	filter           sales.OpportunityListFilter
	reference        string
	resolveLimit     int
	stageReference   string
	stageLimit       int
	stageOpportunity sales.OpportunityID
}

func (s *operationalOpportunityQueriesStub) ListOpportunitiesInTarget(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	filter sales.OpportunityListFilter,
) ([]sales.Opportunity, error) {
	s.listTarget, s.filter = target, filter
	return s.listed, nil
}

func (s *operationalOpportunityQueriesStub) GetOpportunityInTarget(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	_ sales.OpportunityID,
) (sales.Opportunity, error) {
	s.getTarget = target
	return s.found, nil
}

func (s *operationalOpportunityQueriesStub) ResolveOpportunityReference(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	reference string,
	limit int,
) ([]sales.Opportunity, error) {
	s.resolveTarget, s.reference, s.resolveLimit = target, reference, limit
	return s.resolved, nil
}

func (s *operationalOpportunityQueriesStub) ResolveStageReference(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	opportunityID sales.OpportunityID,
	reference string,
	limit int,
) ([]sales.PipelineStage, error) {
	s.stageTarget, s.stageOpportunity = target, opportunityID
	s.stageReference, s.stageLimit = reference, limit
	return s.stages, nil
}

type operationalProposalQueriesStub struct {
	listed        []sales.Proposal
	resolved      []sales.Proposal
	found         sales.Proposal
	listTarget    scope.Target
	resolveTarget scope.Target
	getTarget     scope.Target
	filter        sales.ProposalListFilter
	reference     string
	resolveLimit  int
}

func (s *operationalProposalQueriesStub) ListProposals(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	filter sales.ProposalListFilter,
) ([]sales.Proposal, error) {
	s.listTarget, s.filter = target, filter
	return s.listed, nil
}

func (s *operationalProposalQueriesStub) GetProposal(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	_ string,
) (sales.Proposal, error) {
	s.getTarget = target
	return s.found, nil
}

func (s *operationalProposalQueriesStub) ResolveProposalReference(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
	reference string,
	limit int,
) ([]sales.Proposal, error) {
	s.resolveTarget, s.reference, s.resolveLimit = target, reference, limit
	return s.resolved, nil
}

func safeOpportunityFixture() sales.Opportunity {
	return sales.Opportunity{
		ID: "opportunity-id", MSPID: "msp-1", ClientID: "client-1",
		PipelineID: "pipeline-id", StageID: "stage-id",
		DisplayID: "OPP-2042", Name: "Network refresh",
		Amount:       sales.Money{Minor: 2500000, Currency: "USD"},
		Fields:       map[sales.FieldKey]string{"description": "protected"},
		CustomFields: map[string]string{"margin": "protected"},
		TeamID:       "sales-team", ContactIDs: []string{"contact-id"},
		ProposalIssued: true, ApprovalGranted: true, Version: 4,
	}
}

func safeProposalFixture() sales.Proposal {
	return sales.Proposal{
		ID: "proposal-id", MSPID: "msp-1", ClientID: "client-1",
		OpportunityID: "opportunity-id", DisplayID: "PROP-100",
		CurrentVersion: 3, CurrentVersionID: "financial-version-id",
		State: sales.ProposalDraft, Version: 6,
	}
}

func TestOpportunityListResolvesActiveClientAndReturnsExactSafeSummary(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	queries := &operationalOpportunityQueriesStub{
		listed: []sales.Opportunity{safeOpportunityFixture()},
		stages: []sales.PipelineStage{{
			ID: "stage-id", Name: "Qualified", Probability: 80,
			RequiredFields: []sales.FieldKey{"protected"},
		}},
	}
	tool := NewOpportunityListTool(directory, queries)
	principal := testPrincipal("opportunity.read")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(),
		principal,
		json.RawMessage(`{"client":"Northwind Legal","pipeline_id":"pipeline-id","stage_id":"stage-id","limit":25}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPrepared := `{"client_id":"client-1","pipeline_id":"pipeline-id","stage_id":"stage-id","limit":25}`
	if string(prepared) != wantPrepared ||
		len(directory.calls) != 1 ||
		directory.calls[0].capability != "opportunity.read" {
		t.Fatalf("prepared=%s calls=%+v", prepared, directory.calls)
	}
	result, err := tool.Execute(context.Background(), principal, prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	target := scope.Target{MSPID: "msp-1", ClientID: "client-1"}
	if queries.listTarget != target || queries.stageTarget != target ||
		queries.filter.PipelineID != "pipeline-id" ||
		queries.filter.StageID != "stage-id" ||
		queries.filter.Limit != 25 ||
		queries.stageOpportunity != "opportunity-id" ||
		queries.stageReference != "stage-id" || queries.stageLimit != 2 ||
		principal.Scope.ClientID != "" {
		t.Fatalf("queries=%+v principal=%+v", queries, principal.Scope)
	}
	want := []map[string]any{{
		"display_id": "OPP-2042",
		"name":       "Network refresh",
		"stage":      "Qualified",
		"version":    int64(4),
	}}
	if got := result.Data["opportunities"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("opportunities=%#v, want %#v", got, want)
	}
}

func TestOpportunityGetUsesExactResolverAndFailsClosedOnNonUniqueReference(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	opportunity := safeOpportunityFixture()
	queries := &operationalOpportunityQueriesStub{
		resolved: []sales.Opportunity{{ID: opportunity.ID}},
		found:    opportunity,
		stages:   []sales.PipelineStage{{ID: "stage-id", Name: "Qualified"}},
	}
	tool := NewOpportunityGetTool(directory, queries)
	principal := testPrincipal("opportunity.read")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(), principal,
		json.RawMessage(`{"client":"NW-100","opportunity":"Network refresh"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), principal, prepared, "")
	if err != nil {
		t.Fatal(err)
	}
	if queries.reference != "Network refresh" || queries.resolveLimit != 2 ||
		queries.getTarget != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) {
		t.Fatalf("queries=%+v", queries)
	}
	want := map[string]any{
		"display_id": "OPP-2042",
		"name":       "Network refresh",
		"stage":      "Qualified",
		"version":    int64(4),
	}
	if got := result.Data["opportunity"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("opportunity=%#v, want %#v", got, want)
	}

	queries.resolved = nil
	if _, err := tool.Execute(context.Background(), principal, prepared, ""); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("missing error=%v, want scope.ErrNotFound", err)
	}
	queries.resolved = []sales.Opportunity{{ID: "one"}, {ID: "two"}}
	if _, err := tool.Execute(context.Background(), principal, prepared, ""); !errors.Is(err, sales.ErrAmbiguousReference) {
		t.Fatalf("ambiguous error=%v, want sales.ErrAmbiguousReference", err)
	}
}

func TestProposalListAndGetReturnOnlySafeStateAndCanonicalOpportunityLink(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	proposal := safeProposalFixture()
	proposals := &operationalProposalQueriesStub{
		listed:   []sales.Proposal{proposal},
		resolved: []sales.Proposal{{ID: proposal.ID}},
		found:    proposal,
	}
	listTool := NewProposalListTool(directory, proposals)
	principal := testPrincipal("proposal.read")
	preparedList, err := listTool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(), principal,
		json.RawMessage(`{"client":"Northwind Legal","state":"draft","limit":10}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	listResult, err := listTool.Execute(context.Background(), principal, preparedList, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"display_id":     "PROP-100",
		"state":          sales.ProposalDraft,
		"opportunity_id": "opportunity-id",
		"version":        int64(6),
	}
	if got := listResult.Data["proposals"]; !reflect.DeepEqual(got, []map[string]any{want}) {
		t.Fatalf("proposals=%#v", got)
	}
	if proposals.filter.State != sales.ProposalDraft || proposals.filter.Limit != 10 ||
		proposals.listTarget != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) {
		t.Fatalf("proposals=%+v", proposals)
	}

	getTool := NewProposalGetTool(directory, proposals)
	preparedGet, err := getTool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(), principal,
		json.RawMessage(`{"client":"Northwind Legal","proposal":"PROP-100"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	getResult, err := getTool.Execute(context.Background(), principal, preparedGet, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposals.reference != "PROP-100" || proposals.resolveLimit != 2 ||
		proposals.getTarget != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) {
		t.Fatalf("proposals=%+v", proposals)
	}
	if got := getResult.Data["proposal"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("proposal=%#v, want %#v", got, want)
	}

	proposals.resolved = nil
	if _, err := getTool.Execute(context.Background(), principal, preparedGet, ""); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("missing error=%v, want scope.ErrNotFound", err)
	}
	proposals.resolved = []sales.Proposal{{ID: "one"}, {ID: "two"}}
	if _, err := getTool.Execute(context.Background(), principal, preparedGet, ""); !errors.Is(err, sales.ErrAmbiguousReference) {
		t.Fatalf("ambiguous error=%v, want sales.ErrAmbiguousReference", err)
	}
}

func TestSalesReadToolsRejectUnknownAndInvalidPublicInput(t *testing.T) {
	directory := &operationalDirectoryStub{client: operationalClient()}
	opportunities := &operationalOpportunityQueriesStub{}
	proposals := &operationalProposalQueriesStub{}
	tests := []struct {
		name string
		tool aiassist.Tool
		raw  string
	}{
		{
			name: "opportunity list unknown",
			tool: NewOpportunityListTool(directory, opportunities),
			raw:  `{"client":"Northwind Legal","limit":25,"amount":1}`,
		},
		{
			name: "opportunity list blank filter",
			tool: NewOpportunityListTool(directory, opportunities),
			raw:  `{"client":"Northwind Legal","pipeline_id":""}`,
		},
		{
			name: "opportunity get missing reference",
			tool: NewOpportunityGetTool(directory, opportunities),
			raw:  `{"client":"Northwind Legal","opportunity":""}`,
		},
		{
			name: "proposal list invalid state",
			tool: NewProposalListTool(directory, proposals),
			raw:  `{"client":"Northwind Legal","state":"accepted_pending"}`,
		},
		{
			name: "proposal get trusted field",
			tool: NewProposalGetTool(directory, proposals),
			raw:  `{"client":"Northwind Legal","proposal":"PROP-100","client_id":"client-2"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.tool.(aiassist.ToolInputPreparer).Prepare(
				context.Background(), testPrincipal(tt.tool.RequiredCapability()),
				json.RawMessage(tt.raw),
			)
			if !errors.Is(err, aiassist.ErrInvalidTool) {
				t.Fatalf("Prepare() error=%v, want aiassist.ErrInvalidTool", err)
			}
		})
	}
}
