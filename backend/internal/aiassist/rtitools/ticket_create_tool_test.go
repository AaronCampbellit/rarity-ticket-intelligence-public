package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

const ticketCreatePublicRequest = `{
  "client": "Northwind Legal",
  "display_id": "INC-2042",
  "type": "incident",
  "title": "VPN unavailable",
  "description": "Remote staff cannot connect",
  "status": "new",
  "priority": "normal",
  "service": "Managed Network",
  "contract": "Support Agreement",
  "tag_ids": ["tag-meaningful"]
}`

type ticketCreateActionsStub struct {
	preflight      workrecords.CreatePreflight
	preflightErr   error
	preflightCalls []workrecords.CreateCommand
	create         workrecords.CreateCommand
	createCalls    int
}

func (s *ticketCreateActionsStub) PreflightCreate(_ context.Context, command workrecords.CreateCommand) (workrecords.CreatePreflight, error) {
	s.preflightCalls = append(s.preflightCalls, command)
	return s.preflight, s.preflightErr
}

func (s *ticketCreateActionsStub) Create(_ context.Context, command workrecords.CreateCommand) (workrecords.Record, error) {
	s.createCalls++
	s.create = command
	return workrecords.Record{Envelope: object.Envelope{ID: command.RecordID, MSPID: command.Target.MSPID, ClientID: command.Target.ClientID, DisplayID: command.DisplayID, Version: 1}, Type: command.Type, Title: command.Title, Status: command.Status, Priority: command.Priority, QueueID: command.ExpectedSelection.QueueID}, nil
}

type ticketCatalogStub struct {
	resources  map[string]clientresources.ResourceDetail
	resolved   []clientresources.TrustedResolveCommand
	loaded     []clientresources.TrustedGetCommand
	err        error
	resolveErr error
}

func (s *ticketCatalogStub) Query(context.Context, clientresources.QueryCommand) ([]clientresources.ResourceDetail, error) {
	return nil, s.err
}
func (s *ticketCatalogStub) Resolve(context.Context, clientresources.ResolveCommand) (clientresources.ResourceDetail, error) {
	return clientresources.ResourceDetail{}, s.err
}
func (s *ticketCatalogStub) Get(context.Context, clientresources.GetCommand) (clientresources.ResourceDetail, error) {
	return clientresources.ResourceDetail{}, s.err
}
func (s *ticketCatalogStub) ResolveTrusted(_ context.Context, command clientresources.TrustedResolveCommand) (clientresources.ResourceDetail, error) {
	s.resolved = append(s.resolved, command)
	if s.resolveErr != nil {
		return clientresources.ResourceDetail{}, s.resolveErr
	}
	if s.err != nil {
		return clientresources.ResourceDetail{}, s.err
	}
	for _, resource := range s.resources {
		if resource.Kind == string(command.Kind) && (resource.DisplayID == command.Reference || resource.Name == command.Reference) {
			return resource, nil
		}
	}
	return clientresources.ResourceDetail{}, scope.ErrNotFound
}
func (s *ticketCatalogStub) GetTrusted(_ context.Context, command clientresources.TrustedGetCommand) (clientresources.ResourceDetail, error) {
	s.loaded = append(s.loaded, command)
	if s.err != nil {
		return clientresources.ResourceDetail{}, s.err
	}
	resource, ok := s.resources[command.ID]
	if !ok {
		return clientresources.ResourceDetail{}, scope.ErrNotFound
	}
	return resource, nil
}

func ticketCreatePreflight() workrecords.CreatePreflight {
	return workrecords.CreatePreflight{
		Target:    scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		Routing:   workrecords.RoutingSelection{RuleSetID: "routing-1", RuleSetVersion: 4, Decision: routing.Decision{QueueID: "queue-1"}},
		Workflow:  workflow.Selection{WorkflowID: "workflow-1", Version: 3},
		SLA:       workrecords.AppliedSLA{PolicyID: "policy-1", PolicyVersion: 5, CalendarID: "calendar-1", CalendarVersion: 2},
		ServiceID: "service-1", ContractID: "contract-1",
		Fence: workrecords.CreateSelectionFence{RuleSetID: "routing-1", RuleSetVersion: 4, QueueID: "queue-1", WorkflowID: "workflow-1", WorkflowVersion: 3, SLAPolicyID: "policy-1", SLAPolicyVersion: 5, CalendarID: "calendar-1", CalendarVersion: 2},
	}
}

func activeTicketCatalog() *ticketCatalogStub {
	return &ticketCatalogStub{resources: map[string]clientresources.ResourceDetail{
		"service-1":  {Summary: clientresources.Summary{ID: "service-1", Kind: string(clientresources.ServiceKind), DisplayID: "SVC-1", Name: "Managed Network", LifecycleState: "active", Version: 2}},
		"contract-1": {Summary: clientresources.Summary{ID: "contract-1", Kind: string(clientresources.ContractKind), DisplayID: "CTR-1", Name: "Support Agreement", LifecycleState: "active", Version: 7}},
	}}
}

func ticketCreatePrincipal() authorization.Principal {
	return authorization.Principal{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("work_record.create")}
}

func TestTicketCreateToolRejectsUntrustedAndInvalidPublicInput(t *testing.T) {
	tool := NewTicketCreateTool(activeResourceDirectory(), activeTicketCatalog(), &ticketCreateActionsStub{preflight: ticketCreatePreflight()}, func() string { return "ticket-1" })
	preparer := tool.(aiassist.ToolInputPreparer)
	principal := ticketCreatePrincipal()
	for _, raw := range []string{
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","id":"ticket-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","client_id":"client-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","queue":"queue-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","workflow":"workflow-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","sla":"policy-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","actor":"technician-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","correlation":"c-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","service":"Managed Network","contract":"Support Agreement","causation":"c-1"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"invalid","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal"}`,
		`{"client":"Northwind Legal","display_id":"INC-2042","type":"incident","title":"VPN unavailable","description":"Remote staff cannot connect","status":"new","priority":"normal","unexpected":true}`,
	} {
		if _, err := preparer.Prepare(context.Background(), principal, json.RawMessage(raw)); !errors.Is(err, aiassist.ErrInvalidTool) {
			t.Fatalf("Prepare(%s) error=%v, want ErrInvalidTool", raw, err)
		}
	}
}

func TestTicketCreateToolPreviewsCanonicalSelectionAndCreatesWithFence(t *testing.T) {
	directory, catalog := activeResourceDirectory(), activeTicketCatalog()
	actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
	tool := NewTicketCreateTool(directory, catalog, actions, func() string { return "ticket-1" })
	principal := ticketCreatePrincipal()
	store := &composedProposalStore{}
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store, func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.create", ConversationID: "conversation-1", Input: json.RawMessage(ticketCreatePublicRequest)})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	for _, change := range []string{"display_id", "type", "title", "status", "priority", "tag_ids", "client", "queue", "workflow", "sla_policy", "sla_calendar", "service", "contract"} {
		if _, ok := proposal.Preview.Changes[change]; !ok {
			t.Fatalf("preview omitted %q: %+v", change, proposal.Preview)
		}
	}
	result, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version)
	if err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if result.Data["version"] != int64(1) || actions.createCalls != 1 || actions.create.Actor.Source != "ai_workspace" || actions.create.RecordID != "ticket-1" ||
		actions.create.ExpectedClientVersion != 1 || actions.create.ExpectedServiceVersion != 2 ||
		actions.create.ExpectedContractVersion != 7 ||
		actions.create.ExpectedSelection == nil || *actions.create.ExpectedSelection != ticketCreatePreflight().Fence {
		t.Fatalf("result=%+v create=%+v calls=%d", result, actions.create, actions.createCalls)
	}
	if len(actions.create.TagIDs) != 1 || actions.create.TagIDs[0] != "tag-meaningful" ||
		actions.create.ClassificationPolicy != tagging.CreationRequireMeaningful {
		t.Fatalf("classification was not preserved: %+v", actions.create)
	}
	if len(catalog.resolved) != 6 || len(catalog.loaded) != 4 || actions.create.ServiceID != "service-1" || actions.create.ContractID != "contract-1" {
		t.Fatalf("catalog resolve=%+v load=%+v create=%+v", catalog.resolved, catalog.loaded, actions.create)
	}
}

func TestTicketCreateToolRejectsStalePreflightBeforeCreate(t *testing.T) {
	directory, catalog := activeResourceDirectory(), activeTicketCatalog()
	actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
	tool := NewTicketCreateTool(directory, catalog, actions, func() string { return "ticket-1" })
	principal := ticketCreatePrincipal()
	store := &composedProposalStore{}
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store, func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.create", ConversationID: "conversation-1", Input: json.RawMessage(ticketCreatePublicRequest)})
	if err != nil {
		t.Fatal(err)
	}
	actions.preflight.Fence.WorkflowVersion++
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
	}
	if actions.createCalls != 0 {
		t.Fatalf("Create calls=%d, want 0", actions.createCalls)
	}
}

func TestTicketCreateToolRejectsClientVersionDriftBeforeCreate(t *testing.T) {
	directory, catalog := activeResourceDirectory(), activeTicketCatalog()
	actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
	tool := NewTicketCreateTool(directory, catalog, actions, func() string { return "ticket-1" })
	principal := ticketCreatePrincipal()
	store := &composedProposalStore{}
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC) },
		func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "ticket.create", ConversationID: "conversation-1",
		Input: json.RawMessage(ticketCreatePublicRequest),
	})
	if err != nil {
		t.Fatal(err)
	}
	directory.resolved.Version++
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
	}
	if actions.createCalls != 0 {
		t.Fatalf("Create calls=%d, want 0", actions.createCalls)
	}
}

func TestTicketCreateToolRejectsReferenceAmbiguityAtConfirmationBeforeCreate(t *testing.T) {
	directory, catalog := activeResourceDirectory(), activeTicketCatalog()
	actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
	tool := NewTicketCreateTool(directory, catalog, actions, func() string { return "ticket-1" })
	principal := ticketCreatePrincipal()
	store := &composedProposalStore{}
	ids := []string{"proposal-1", "correlation-1"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC) },
		func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.create", ConversationID: "conversation-1", Input: json.RawMessage(ticketCreatePublicRequest)})
	if err != nil {
		t.Fatal(err)
	}
	catalog.resolveErr = clientresources.ErrAmbiguousResource
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
	}
	if actions.createCalls != 0 {
		t.Fatalf("Create calls=%d, want 0", actions.createCalls)
	}
}

func TestTicketCreateToolRejectsUnavailableOrAmbiguousReferencesBeforeCreate(t *testing.T) {
	principal := ticketCreatePrincipal()
	for _, errWant := range []error{scope.ErrNotFound, clientresources.ErrAmbiguousResource} {
		t.Run(errWant.Error(), func(t *testing.T) {
			actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
			catalog := activeTicketCatalog()
			catalog.err = errWant
			tool := NewTicketCreateTool(activeResourceDirectory(), catalog, actions, func() string { return "ticket-1" })
			_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(ticketCreatePublicRequest))
			if !errors.Is(err, errWant) {
				t.Fatalf("Prepare() error=%v, want %v", err, errWant)
			}
			if actions.createCalls != 0 {
				t.Fatalf("Create calls=%d, want 0", actions.createCalls)
			}
		})
	}
}

func TestTicketCreateToolDoesNotCreateAfterReferenceLifecycleOrAuthorizationDrift(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*resourceDirectoryStub, *ticketCatalogStub)
	}{
		{
			name: "inactive client",
			apply: func(directory *resourceDirectoryStub, _ *ticketCatalogStub) {
				directory.err = scope.ErrNotFound
			},
		},
		{
			name: "inactive service",
			apply: func(_ *resourceDirectoryStub, catalog *ticketCatalogStub) {
				resource := catalog.resources["service-1"]
				resource.LifecycleState = "inactive"
				catalog.resources["service-1"] = resource
			},
		},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			directory, catalog := activeResourceDirectory(), activeTicketCatalog()
			actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight()}
			tool := NewTicketCreateTool(directory, catalog, actions, func() string { return "ticket-1" })
			principal := ticketCreatePrincipal()
			store := &composedProposalStore{}
			registry, err := aiassist.NewRegistry(
				[]aiassist.Tool{tool}, store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				func() time.Time { return time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC) },
				func() string { return "id" }, &composedTargetAuthorizer{},
			)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "ticket.create", ConversationID: "conversation-1", Input: json.RawMessage(ticketCreatePublicRequest)})
			if err != nil {
				t.Fatal(err)
			}
			mutate.apply(directory, catalog)
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
				t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
			}
			if actions.createCalls != 0 {
				t.Fatalf("Create calls=%d, want 0", actions.createCalls)
			}
		})
	}
}

func TestTicketCreateToolDuplicatePreflightDoesNotCreate(t *testing.T) {
	duplicate := errors.New("duplicate display ID")
	actions := &ticketCreateActionsStub{preflight: ticketCreatePreflight(), preflightErr: duplicate}
	tool := NewTicketCreateTool(activeResourceDirectory(), activeTicketCatalog(), actions, func() string { return "ticket-1" })
	_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), ticketCreatePrincipal(), json.RawMessage(ticketCreatePublicRequest))
	if !errors.Is(err, duplicate) {
		t.Fatalf("Prepare() error=%v, want duplicate error", err)
	}
	if actions.createCalls != 0 {
		t.Fatalf("Create calls=%d, want 0", actions.createCalls)
	}
}
