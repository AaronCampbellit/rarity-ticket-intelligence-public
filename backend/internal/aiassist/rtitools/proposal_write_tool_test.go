package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const proposalCreatePublicRequest = `{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042"}`

type proposalOperationalActionsStub struct {
	opportunity    sales.Opportunity
	matches        []sales.Opportunity
	resolvedRefs   []string
	preflight      sales.ProposalDraftPreflight
	preflightErr   error
	preflightCalls []sales.CreateProposalCommand
	create         sales.CreateProposalCommand
	createCalls    int
}

func (s *proposalOperationalActionsStub) ResolveOpportunityReference(_ context.Context, _ authorization.Principal, _ scope.Target, reference string, _ int) ([]sales.Opportunity, error) {
	s.resolvedRefs = append(s.resolvedRefs, reference)
	return s.matches, nil
}
func (s *proposalOperationalActionsStub) GetOpportunityInTarget(context.Context, authorization.Principal, scope.Target, sales.OpportunityID) (sales.Opportunity, error) {
	return s.opportunity, nil
}
func (s *proposalOperationalActionsStub) PreflightCreateProposal(_ context.Context, command sales.CreateProposalCommand) (sales.ProposalDraftPreflight, error) {
	s.preflightCalls = append(s.preflightCalls, command)
	return s.preflight, s.preflightErr
}
func (s *proposalOperationalActionsStub) CreateProposal(_ context.Context, command sales.CreateProposalCommand) (sales.Proposal, error) {
	s.createCalls++
	s.create = command
	return sales.Proposal{ID: "proposal-internal", DisplayID: command.DisplayID, OpportunityID: command.OpportunityID, State: sales.ProposalDraft, Version: 1}, nil
}

func proposalCreateFixture() *proposalOperationalActionsStub {
	opportunity := sales.Opportunity{ID: "opportunity-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "OPP-2042", Name: "Northwind onboarding", PipelineID: "pipeline-1", StageID: "stage-1", Version: 3}
	return &proposalOperationalActionsStub{opportunity: opportunity, matches: []sales.Opportunity{opportunity}, preflight: sales.ProposalDraftPreflight{Opportunity: opportunity, DisplayID: "PROP-2042"}}
}

func proposalCreatePrincipal() authorization.Principal {
	return authorization.Principal{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("proposal.create")}
}

func TestProposalCreateToolPreparesCanonicalDraftLinkageAndOnlyCreatesOnConfirmation(t *testing.T) {
	directory, actions := activeResourceDirectory(), proposalCreateFixture()
	directory.resolved.Version = 2
	tool := NewProposalCreateTool(directory, actions)
	store := &composedProposalStore{}
	principal := proposalCreatePrincipal()
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC) },
		func() string { return "proposal-action" }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "proposal.create", ConversationID: "conversation-1", Input: json.RawMessage(proposalCreatePublicRequest)})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	if actions.createCalls != 0 || proposal.Preview.TargetID != "opportunity-1" || proposal.Preview.TargetVersion != 3 {
		t.Fatalf("proposal=%+v creates=%d, want canonical read-only draft preview", proposal, actions.createCalls)
	}
	draft, ok := proposal.Preview.Changes["proposal_draft"]
	if len(proposal.Preview.Changes) != 1 || !ok || draft.Before != nil {
		t.Fatalf("draft preview=%+v, want new draft linkage", proposal.Preview.Changes)
	}
	after, ok := draft.After.(map[string]any)
	if !ok || after["display_id"] != "PROP-2042" || after["state"] != string(sales.ProposalDraft) || after["version"] != int64(1) {
		t.Fatalf("draft preview=%+v, want version-1 supplied display ID linkage", draft)
	}
	result, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if actions.createCalls != 1 || actions.create.OpportunityID != "opportunity-1" || actions.create.ExpectedClientVersion != 2 || actions.create.ExpectedOpportunityVersion != 3 || actions.create.DisplayID != "PROP-2042" || result.Data["id"] != "proposal-internal" {
		t.Fatalf("confirmation create=%+v calls=%d result=%+v", actions.create, actions.createCalls, result)
	}
}

func TestProposalCreateToolStructuredInputStillAcceptsExactOpportunityName(t *testing.T) {
	directory, actions := activeResourceDirectory(), proposalCreateFixture()
	directory.resolved.Version = 2
	tool := NewProposalCreateTool(directory, actions)
	const exactName = "Renewal: Issue Management LLC"

	_, err := tool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(),
		proposalCreatePrincipal(),
		json.RawMessage(`{"client":"Northwind Legal","opportunity":"`+exactName+`","display_id":"PROP-2042"}`),
	)
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if len(actions.resolvedRefs) != 1 || actions.resolvedRefs[0] != exactName {
		t.Fatalf("resolved references=%q, want exact structured name %q", actions.resolvedRefs, exactName)
	}
}

func TestProposalCreateToolRejectsClosedSchemaBeforePreflightOrCreate(t *testing.T) {
	for _, raw := range []string{
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","lines":[]}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","price":100}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","currency":"USD"}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","approval":"approved"}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","issue":true}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","signer":"Alex"}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","acceptance":true}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","conversion":true}`,
		`{"client":"Northwind Legal","opportunity":"OPP-2042","display_id":"PROP-2042","opportunity_id":"spoof"}`,
	} {
		actions := proposalCreateFixture()
		tool := NewProposalCreateTool(activeResourceDirectory(), actions)
		_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), proposalCreatePrincipal(), json.RawMessage(raw))
		if !errors.Is(err, aiassist.ErrInvalidTool) || len(actions.preflightCalls) != 0 || actions.createCalls != 0 {
			t.Fatalf("Prepare(%s) error=%v preflights=%d creates=%d", raw, err, len(actions.preflightCalls), actions.createCalls)
		}
	}
}

func TestProposalCreateToolRejectsConfirmationDriftWithoutCreate(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*resourceDirectoryStub, *proposalOperationalActionsStub, *authorization.Principal)
	}{
		{name: "inactive client", apply: func(directory *resourceDirectoryStub, _ *proposalOperationalActionsStub, _ *authorization.Principal) {
			directory.err = scope.ErrNotFound
		}},
		{name: "opportunity version", apply: func(_ *resourceDirectoryStub, actions *proposalOperationalActionsStub, _ *authorization.Principal) {
			actions.opportunity.Version++
			actions.matches[0] = actions.opportunity
			actions.preflight.Opportunity = actions.opportunity
		}},
		{name: "ineligible opportunity", apply: func(_ *resourceDirectoryStub, actions *proposalOperationalActionsStub, _ *authorization.Principal) {
			actions.preflightErr = scope.ErrNotFound
		}},
		{name: "duplicate display ID", apply: func(_ *resourceDirectoryStub, actions *proposalOperationalActionsStub, _ *authorization.Principal) {
			actions.preflightErr = sales.ErrInvalidProposal
		}},
		{name: "authorization loss", apply: func(_ *resourceDirectoryStub, _ *proposalOperationalActionsStub, principal *authorization.Principal) {
			principal.Capabilities = authorization.NewCapabilitySet()
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			directory, actions, principal := activeResourceDirectory(), proposalCreateFixture(), proposalCreatePrincipal()
			directory.resolved.Version = 2
			tool := NewProposalCreateTool(directory, actions)
			store := &composedProposalStore{}
			registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				func() time.Time { return time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC) },
				func() string { return "proposal-action" }, &composedTargetAuthorizer{})
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "proposal.create", ConversationID: "conversation-1", Input: json.RawMessage(proposalCreatePublicRequest)})
			if err != nil {
				t.Fatal(err)
			}
			mutate.apply(directory, actions, &principal)
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err == nil {
				t.Fatal("Confirm() error=nil, want drift rejection")
			}
			if actions.createCalls != 0 {
				t.Fatalf("CreateProposal calls=%d, want 0", actions.createCalls)
			}
		})
	}
}
