package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

type aiWorkspaceConversationStub struct {
	createdTitle string
	appended     []aiassist.Message
}

func (s *aiWorkspaceConversationStub) Create(_ context.Context, _ authorization.Principal, title string) (aiassist.Conversation, error) {
	s.createdTitle = title
	return aiassist.Conversation{ID: "conversation-1", Title: title, Version: 1}, nil
}
func (s *aiWorkspaceConversationStub) Get(context.Context, authorization.Principal, string) (aiassist.Conversation, error) {
	return aiassist.Conversation{ID: "conversation-1", Version: 1}, nil
}
func (s *aiWorkspaceConversationStub) List(context.Context, authorization.Principal, bool, int) ([]aiassist.Conversation, error) {
	return []aiassist.Conversation{{ID: "conversation-1", Version: 1}}, nil
}
func (s *aiWorkspaceConversationStub) Archive(context.Context, authorization.Principal, string, int64) error {
	return nil
}
func (s *aiWorkspaceConversationStub) Append(_ context.Context, _ authorization.Principal, conversationID string, role aiassist.MessageRole, text string, references map[string]any) (aiassist.Message, error) {
	message := aiassist.Message{
		ID: "message-1", ConversationID: conversationID, Role: role,
		SafeText: text, ReferencedObjects: references,
	}
	s.appended = append(s.appended, message)
	return message, nil
}
func (s *aiWorkspaceConversationStub) Messages(context.Context, authorization.Principal, string, int) ([]aiassist.Message, error) {
	return append([]aiassist.Message(nil), s.appended...), nil
}

type aiWorkspaceToolStub struct {
	principal  authorization.Principal
	request    aiassist.ToolRequest
	readErr    error
	proposeErr error
	confirmErr error
	proposals  int
	version    int64
}

func (s *aiWorkspaceToolStub) RunRead(_ context.Context, _ authorization.Principal, request aiassist.ToolRequest) (aiassist.ToolResult, error) {
	s.request = request
	if s.readErr != nil {
		return aiassist.ToolResult{}, s.readErr
	}
	return aiassist.ToolResult{
		Summary: "Use the Work queue",
		Data: map[string]any{"citations": []aiassist.Citation{{
			SourceKey: "docs/03-user-guide/work.md", SourceVersion: "v1",
			Section: "Queues", Excerpt: "Open Work and select a queue.",
		}}},
	}, nil
}
func (s *aiWorkspaceToolStub) Propose(_ context.Context, principal authorization.Principal, request aiassist.ToolRequest) (aiassist.ActionProposal, error) {
	s.principal = principal
	s.request = request
	s.proposals++
	if s.proposeErr != nil {
		return aiassist.ActionProposal{}, s.proposeErr
	}
	return aiassist.ActionProposal{
		ID: "proposal-1", ToolName: request.Name, State: aiassist.ProposalPending,
		Preview:   aiassist.Preview{Summary: "Resolve INC-1", TargetType: "work_record", TargetID: "ticket-1"},
		ExpiresAt: time.Now().Add(time.Minute), Version: 1,
	}, nil
}
func (s *aiWorkspaceToolStub) Confirm(_ context.Context, _ authorization.Principal, _ string, version int64) (aiassist.ToolResult, error) {
	s.version = version
	if s.confirmErr != nil {
		return aiassist.ToolResult{}, s.confirmErr
	}
	return aiassist.ToolResult{Summary: "Updated INC-1"}, nil
}
func (s *aiWorkspaceToolStub) Reject(context.Context, authorization.Principal, string, int64) error {
	return nil
}

type aiWorkspaceDirectoryStub struct {
	directoryActionsStub
	err error
}

func (s *aiWorkspaceDirectoryStub) List(
	_ context.Context,
	command organizations.ListDirectoryCommand,
) (organizations.Directory, error) {
	s.list = command
	if s.err != nil {
		return organizations.Directory{}, s.err
	}
	return s.directory, nil
}

type aiWorkspaceClientIdentityCheck struct {
	principal authorization.Principal
	name      string
	displayID string
}

type aiWorkspaceOrganizationStub struct {
	conflict bool
	err      error
	checks   []aiWorkspaceClientIdentityCheck
}

func (s *aiWorkspaceOrganizationStub) CreateClient(
	context.Context,
	organizations.CreateClientCommand,
) (organizations.Client, error) {
	return organizations.Client{}, nil
}

func (s *aiWorkspaceOrganizationStub) HasClientIdentityConflict(
	_ context.Context,
	principal authorization.Principal,
	name string,
	displayID string,
) (bool, error) {
	s.checks = append(s.checks, aiWorkspaceClientIdentityCheck{
		principal: principal, name: name, displayID: displayID,
	})
	return s.conflict, s.err
}

type aiWorkspaceTargetDirectoryRepository struct {
	directory organizations.Directory
}

func (*aiWorkspaceTargetDirectoryRepository) CreateDepartmentAtomic(
	context.Context,
	organizations.DirectoryMutation,
) error {
	return nil
}

func (*aiWorkspaceTargetDirectoryRepository) CreateTeamAtomic(
	context.Context,
	organizations.DirectoryMutation,
) error {
	return nil
}

func (*aiWorkspaceTargetDirectoryRepository) CreateQueueAtomic(
	context.Context,
	organizations.DirectoryMutation,
) error {
	return nil
}

func (r *aiWorkspaceTargetDirectoryRepository) ListDirectory(
	context.Context,
	scope.Target,
) (organizations.Directory, error) {
	return r.directory, nil
}

type aiWorkspaceProposalStore struct {
	proposal aiassist.ActionProposal
}

type serializedAIWorkspaceProposalStore struct {
	aiWorkspaceProposalStore
}

func (s *serializedAIWorkspaceProposalStore) GetProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
) (aiassist.ActionProposal, error) {
	proposal, err := s.aiWorkspaceProposalStore.GetProposal(
		ctx, target, id, principalID,
	)
	if err != nil {
		return aiassist.ActionProposal{}, err
	}
	body, err := json.Marshal(proposal.Preview)
	if err != nil {
		return aiassist.ActionProposal{}, err
	}
	proposal.Preview = aiassist.Preview{}
	if err := json.Unmarshal(body, &proposal.Preview); err != nil {
		return aiassist.ActionProposal{}, err
	}
	return proposal, nil
}

type aiWorkspaceLocationCreator struct {
	calls int
}

func (c *aiWorkspaceLocationCreator) CreateLocation(
	_ context.Context,
	command clientresources.CreateLocationCommand,
) (clientresources.Location, error) {
	c.calls++
	return clientresources.Location{
		Envelope: object.Envelope{
			ID: command.Prepared.ResourceID, MSPID: command.Target.MSPID,
			ClientID: command.Target.ClientID, DisplayID: command.DisplayID,
			LifecycleState: "active", Version: 1,
		},
		Name: command.Name,
	}, nil
}

func (s *aiWorkspaceProposalStore) CreateProposal(
	_ context.Context,
	proposal aiassist.ActionProposal,
) error {
	s.proposal = proposal
	return nil
}

func (s *aiWorkspaceProposalStore) GetProposal(
	context.Context,
	scope.Target,
	string,
	string,
) (aiassist.ActionProposal, error) {
	return s.proposal, nil
}

func (s *aiWorkspaceProposalStore) ConfirmProposal(
	_ context.Context,
	_ scope.Target,
	_, _ string,
	_ int64,
	at time.Time,
) (aiassist.ActionProposal, error) {
	s.proposal.State = aiassist.ProposalConfirmed
	s.proposal.ConfirmedAt = &at
	s.proposal.Version++
	return s.proposal, nil
}

func (s *aiWorkspaceProposalStore) CompleteProposal(
	_ context.Context,
	_ scope.Target,
	_ string,
	_ int64,
	result aiassist.ToolResult,
	_ time.Time,
) error {
	s.proposal.Result = &result
	return nil
}

func (*aiWorkspaceProposalStore) FailProposal(
	context.Context,
	scope.Target,
	string,
	int64,
	string,
	time.Time,
) error {
	return nil
}

func (*aiWorkspaceProposalStore) RejectProposal(
	context.Context,
	scope.Target,
	string,
	string,
	int64,
	time.Time,
) error {
	return nil
}

func (*aiWorkspaceProposalStore) ExpireProposal(
	context.Context,
	scope.Target,
	string,
	string,
	int64,
	time.Time,
) error {
	return nil
}

type aiWorkspaceProjectCreator struct {
	calls int
}

func (s *aiWorkspaceProjectCreator) CreateProject(
	context.Context,
	projects.AIWorkspaceCreateCommand,
) (projects.AIWorkspaceCreateResult, error) {
	s.calls++
	return projects.AIWorkspaceCreateResult{}, nil
}

func aiWorkspaceMSPPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{
		ID:    "tech",
		Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(
			"ai.assist", "client.create", "organization.read",
			"project.create", "task.create",
		),
	}, nil
}

func aiWorkspaceClients() []organizations.Client {
	return []organizations.Client{
		{
			Envelope: object.Envelope{
				ID: "client-1", MSPID: "msp-1", ClientID: "client-1",
				DisplayID: "ALPHA-100", LifecycleState: "active",
			},
			Name: "Alpha Managed Services",
		},
		{
			Envelope: object.Envelope{
				ID: "client-2", MSPID: "msp-1", ClientID: "client-2",
				DisplayID: "BRAVO-200", LifecycleState: "active",
			},
			Name: "Bravo Systems",
		},
	}
}

func aiWorkspaceDirectory(clients []organizations.Client) *aiWorkspaceDirectoryStub {
	return &aiWorkspaceDirectoryStub{directoryActionsStub: directoryActionsStub{
		directory: organizations.Directory{Clients: clients},
	}}
}

func TestAIWorkspaceMessageUsesAuthenticatedProductHelpAndPersistsBothSides(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal:                aiWorkspaceMSPPrincipal,
		AIWorkspaceConversations: conversations,
		AIWorkspaceTools:         tools,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"How do queues work?"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if tools.request.Name != "product.help" || len(conversations.appended) != 2 ||
		conversations.appended[0].Role != aiassist.MessageRoleUser ||
		conversations.appended[1].Role != aiassist.MessageRoleAssistant {
		t.Fatalf("tool=%+v messages=%+v", tools.request, conversations.appended)
	}
}

func TestAIWorkspaceMessageDispatchesClosedClientResourceToolsWithPublicBusinessInput(t *testing.T) {
	tests := []struct {
		name, message, tool string
		input               map[string]any
	}{
		{
			name: "list", message: "List assets for Alpha Managed Services.", tool: "client_resource.list",
			input: map[string]any{"client": "Alpha Managed Services", "kind": "asset"},
		},
		{
			name: "location", message: "Create a location named Branch Office with display ID LOC-2 for Alpha Managed Services.", tool: "location.create",
			input: map[string]any{"client": "Alpha Managed Services", "display_id": "LOC-2", "name": "Branch Office"},
		},
		{
			name: "contact", message: "Create a contact named Ada Lovelace with display ID CON-1 for Alpha Managed Services at Head Office with email ada@example.com.", tool: "contact.create",
			input: map[string]any{"client": "Alpha Managed Services", "display_id": "CON-1", "display_name": "Ada Lovelace", "location": "Head Office", "email": "ada@example.com"},
		},
		{
			name: "asset", message: "Create an asset named Firewall with display ID AST-1 for Alpha Managed Services of type firewall.", tool: "asset.create",
			input: map[string]any{"client": "Alpha Managed Services", "display_id": "AST-1", "name": "Firewall", "asset_type": "firewall"},
		},
		{
			name: "service", message: "Create a service named Managed Firewall with display ID SVC-1 for Alpha Managed Services with criticality high.", tool: "service.create",
			input: map[string]any{"client": "Alpha Managed Services", "display_id": "SVC-1", "name": "Managed Firewall", "criticality": "high"},
		},
		{
			name: "contract", message: "Create a contract named Support Agreement with display ID CTR-1 for Alpha Managed Services starting 2026-08-05 ending 2027-08-04.", tool: "contract.create",
			input: map[string]any{"client": "Alpha Managed Services", "display_id": "CTR-1", "name": "Support Agreement", "starts_on": "2026-08-05", "ends_on": "2027-08-04"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := &aiWorkspaceConversationStub{}
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
				AIWorkspaceTools: tools,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			if response.Code != http.StatusCreated || tools.request.Name != test.tool {
				t.Fatalf("status=%d tool=%+v body=%s", response.Code, tools.request, response.Body.String())
			}
			var input map[string]any
			if err := json.Unmarshal(tools.request.Input, &input); err != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("input=%#v want=%#v error=%v", input, test.input, err)
			}
		})
	}
}

func TestAIWorkspaceClientResourceLifecyclePlansProduceOnlyPublicToolInput(t *testing.T) {
	update, ok := aiWorkspaceClientResourceInput(aiassist.WorkspaceMessagePlan{
		ToolName: "contact.update", ClientName: "Northwind Legal", ClientResourceDisplayID: "CON-1",
		ClientResourcePatchField: "email", ClientResourcePatchValue: "ada@example.com", Reason: "Correct email",
	})
	wantUpdate := map[string]any{
		"client": "Northwind Legal", "resource": "CON-1", "reason": "Correct email",
		"patch": map[string]any{"email": "ada@example.com"},
	}
	if !ok || !reflect.DeepEqual(update, wantUpdate) {
		t.Fatalf("update=%+v ok=%v", update, ok)
	}
	lifecycle, ok := aiWorkspaceClientResourceInput(aiassist.WorkspaceMessagePlan{
		ToolName: "asset.deactivate", ClientName: "Northwind Legal", ClientResourceDisplayID: "AST-1", Reason: "Retired",
	})
	wantLifecycle := map[string]any{"client": "Northwind Legal", "resource": "AST-1", "reason": "Retired"}
	if !ok || !reflect.DeepEqual(lifecycle, wantLifecycle) {
		t.Fatalf("lifecycle=%+v ok=%v", lifecycle, ok)
	}
	for _, forbidden := range []string{"client_id", "resource_id", "current_version", "expected_version", "actor_id", "source", "correlation_id", "authority", "lifecycle_state"} {
		if _, exists := update[forbidden]; exists {
			t.Fatalf("public update input exposed trusted field %q: %+v", forbidden, update)
		}
		if _, exists := lifecycle[forbidden]; exists {
			t.Fatalf("public lifecycle input exposed trusted field %q: %+v", forbidden, lifecycle)
		}
	}
}

func TestAIWorkspaceMessageDispatchesClosedOperationalWritesWithOnlyUserBusinessInput(t *testing.T) {
	tests := []struct {
		name, message, tool string
		input               map[string]any
	}{
		{
			name:    "knowledge create",
			message: "Create a knowledge draft with display ID KB-200 titled VPN recovery for Northwind Legal with body Exact recovery steps.",
			tool:    "knowledge.draft.create",
			input:   map[string]any{"client": "Northwind Legal", "display_id": "KB-200", "title": "VPN recovery", "body": "Exact recovery steps"},
		},
		{
			name:    "knowledge revise",
			message: "Revise knowledge article KB-200 for Northwind Legal at version 3 titled VPN recovery revised with body Exact revised steps.",
			tool:    "knowledge.draft.revise",
			input:   map[string]any{"client": "Northwind Legal", "article": "KB-200", "expected_version": float64(3), "title": "VPN recovery revised", "body": "Exact revised steps"},
		},
		{
			name:    "prospect omitted contacts",
			message: "Create a prospect named Alpha Managed Services with display ID PRO-200.",
			tool:    "prospect.create",
			input:   map[string]any{"display_id": "PRO-200", "name": "Alpha Managed Services"},
		},
		{
			name:    "ticket route",
			message: "Route ticket INC-200 for Northwind Legal to queue Service Desk at version 5 because Escalate specialist issue.",
			tool:    "ticket.route",
			input:   map[string]any{"client": "Northwind Legal", "ticket": "INC-200", "queue": "Service Desk", "expected_version": float64(5), "reason": "Escalate specialist issue"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := &aiWorkspaceConversationStub{}
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
				AIWorkspaceTools: tools,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			if response.Code != http.StatusCreated || tools.request.Name != test.tool {
				t.Fatalf("status=%d tool=%+v body=%s", response.Code, tools.request, response.Body.String())
			}
			var input map[string]any
			if err := json.Unmarshal(tools.request.Input, &input); err != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("input=%#v want=%#v error=%v", input, test.input, err)
			}
			for _, forbidden := range []string{
				"client_id", "article_id", "prospect_id", "ticket_id", "queue_id",
				"actor_id", "source", "correlation_id", "current_version",
			} {
				if _, exposed := input[forbidden]; exposed {
					t.Fatalf("public input exposed trusted field %q: %+v", forbidden, input)
				}
			}
		})
	}
}

func TestAIWorkspaceMessageDispatchesClosedOperationalReads(t *testing.T) {
	tests := []struct {
		message, tool string
		input         map[string]any
	}{
		{"Get ticket INC-200 for Alpha Managed Services.", "ticket.get", map[string]any{"client_id": "client-1", "id": "INC-200"}},
		{"Search tickets for Alpha Managed Services matching VPN outage.", "ticket.search", map[string]any{"client_id": "client-1", "query": "VPN outage", "limit": float64(20)}},
		{"List projects for Alpha Managed Services.", "project.search", map[string]any{"client": "Alpha Managed Services", "limit": float64(25)}},
		{"Get project PRJ-200 for Alpha Managed Services.", "project.get", map[string]any{"client": "Alpha Managed Services", "project": "PRJ-200"}},
		{"Search knowledge for Alpha Managed Services matching VPN recovery.", "knowledge.search", map[string]any{"client": "Alpha Managed Services", "query": "VPN recovery", "limit": float64(25)}},
		{"Get knowledge article KB-200 for Alpha Managed Services.", "knowledge.get", map[string]any{"client": "Alpha Managed Services", "article": "KB-200"}},
		{"List prospects.", "prospect.list", map[string]any{"limit": float64(25)}},
	}
	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			if response.Code != http.StatusCreated || tools.request.Name != test.tool {
				t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
			}
			var input map[string]any
			if err := json.Unmarshal(tools.request.Input, &input); err != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("input=%#v want=%#v error=%v", input, test.input, err)
			}
		})
	}
}

func TestAIWorkspaceMessageDispatchesSecondWaveReadsWithOnlyPublicBusinessInput(t *testing.T) {
	for _, test := range []struct {
		message, tool string
		input         map[string]any
	}{
		{
			"List opportunities for Northwind Legal.",
			"opportunity.list",
			map[string]any{"client": "Northwind Legal"},
		},
		{
			"Get opportunity OPP-2042 for Northwind Legal.",
			"opportunity.get",
			map[string]any{"client": "Northwind Legal", "opportunity": "OPP-2042"},
		},
		{
			"List proposals for Northwind Legal.",
			"proposal.list",
			map[string]any{"client": "Northwind Legal"},
		},
		{
			"Get proposal PROP-2042 for Northwind Legal.",
			"proposal.get",
			map[string]any{"client": "Northwind Legal", "proposal": "PROP-2042"},
		},
	} {
		t.Run(test.tool, func(t *testing.T) {
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools: tools,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			var input map[string]any
			unmarshalErr := json.Unmarshal(tools.request.Input, &input)
			if response.Code != http.StatusCreated || tools.request.Name != test.tool ||
				unmarshalErr != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("status=%d request=%+v input=%#v want=%#v error=%v body=%s",
					response.Code, tools.request, input, test.input, unmarshalErr, response.Body.String())
			}
		})
	}
}

func TestAIWorkspaceMessageDispatchesSecondWaveWritesWithOnlySuppliedBusinessInput(t *testing.T) {
	for _, test := range []struct {
		message, tool string
		input         map[string]any
	}{
		{
			"Create ticket INC-2042 for Northwind Legal of type incident titled VPN outage with description Users cannot connect at status new with priority high using service Managed Network and contract Support Agreement with tag ids tag-meaningful.",
			"ticket.create",
			map[string]any{
				"client": "Northwind Legal", "display_id": "INC-2042",
				"type": "incident", "title": "VPN outage",
				"description": "Users cannot connect", "status": "new",
				"priority": "high", "service": "Managed Network",
				"contract": "Support Agreement", "tag_ids": []any{"tag-meaningful"},
			},
		},
		{
			"Assign ticket INC-2042 for Northwind Legal to alex@example.test at version 4 because network escalation owns it.",
			"ticket.assign",
			map[string]any{
				"client": "Northwind Legal", "ticket": "INC-2042",
				"technician": "alex@example.test", "expected_version": float64(4),
				"reason": "network escalation owns it",
			},
		},
		{
			"Transition opportunity OPP-2042 for Northwind Legal to stage Qualified at version 3 because Discovery completed.",
			"opportunity.transition",
			map[string]any{
				"client": "Northwind Legal", "opportunity": "OPP-2042",
				"stage": "Qualified", "expected_version": float64(3),
				"reason": "Discovery completed",
			},
		},
		{
			"Add call activity to opportunity OPP-2042 for Northwind Legal with summary Reviewed onboarding scope and details Customer confirmed the supplied milestones.",
			"opportunity.activity.create",
			map[string]any{
				"client": "Northwind Legal", "opportunity": "OPP-2042",
				"kind": "call", "summary": "Reviewed onboarding scope",
				"details": "Customer confirmed the supplied milestones",
			},
		},
		{
			"Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042.",
			"proposal.create",
			map[string]any{
				"client": "Northwind Legal", "opportunity": "OPP-2042",
				"display_id": "PROP-2042",
			},
		},
		{
			"Publish knowledge article KB-2042 for Northwind Legal at version 4 because reviewed for internal use.",
			"knowledge.publish",
			map[string]any{
				"client": "Northwind Legal", "article": "KB-2042",
				"expected_version": float64(4), "reason": "reviewed for internal use",
			},
		},
	} {
		t.Run(test.tool, func(t *testing.T) {
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools: tools,
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			var input map[string]any
			unmarshalErr := json.Unmarshal(tools.request.Input, &input)
			if response.Code != http.StatusCreated || tools.request.Name != test.tool ||
				unmarshalErr != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("status=%d request=%+v input=%#v want=%#v error=%v body=%s",
					response.Code, tools.request, input, test.input, unmarshalErr, response.Body.String())
			}
			for _, forbidden := range []string{
				"client_id", "ticket_id", "technician_id", "opportunity_id",
				"proposal_id", "article_id", "actor_id", "source", "correlation_id",
			} {
				if _, exposed := input[forbidden]; exposed {
					t.Fatalf("public input exposed trusted field %q: %+v", forbidden, input)
				}
			}
		})
	}
}

func TestAIWorkspaceSecondWaveErrorsKeepTypedSafeHTTPFamilies(t *testing.T) {
	for _, test := range []struct {
		name, code, message string
		err                 error
		status              int
	}{
		{
			name: "sales ambiguity", err: sales.ErrAmbiguousReference,
			status: http.StatusConflict, code: "ambiguous_reference",
			message: "More than one exact object matches. Use its display ID and try again.",
		},
		{
			name: "technician unavailable", err: scope.ErrNotFound,
			status: http.StatusNotFound, code: "not_found",
			message: "resource not found",
		},
		{
			name: "routing setup missing", err: routing.ErrNoRoute,
			status: http.StatusConflict, code: "configuration_required",
			message: "Ticket routing, workflow, or SLA configuration is required.",
		},
		{
			name: "workflow setup missing", err: workflow.ErrNoWorkflow,
			status: http.StatusConflict, code: "configuration_required",
			message: "Ticket routing, workflow, or SLA configuration is required.",
		},
		{
			name: "SLA setup missing", err: sla.ErrNoPolicy,
			status: http.StatusConflict, code: "configuration_required",
			message: "Ticket routing, workflow, or SLA configuration is required.",
		},
		{
			name: "selection drift", err: object.ErrVersionConflict,
			status: http.StatusConflict, code: "version_conflict",
			message: "resource changed; refresh and retry",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewRouter(Dependencies{
				Principal:                aiWorkspaceMSPPrincipal,
				AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools:         &aiWorkspaceToolStub{proposeErr: test.err},
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost,
				"/api/v1/ai/workspace/conversations/conversation-1/proposals",
				`{"tool_name":"ticket.create","input":{}}`,
			))
			body := response.Body.String()
			if response.Code != test.status ||
				!strings.Contains(body, `"`+test.code+`"`) ||
				!strings.Contains(body, test.message) {
				t.Fatalf("error=%v status=%d body=%s", test.err, response.Code, body)
			}
			if strings.Contains(body, test.err.Error()) && test.err.Error() != test.message {
				t.Fatalf("response leaked internal error %q: %s", test.err, body)
			}
		})
	}
}

func TestAIWorkspaceMessageClarifiesAmbiguousOperationalReferenceWithoutResult(t *testing.T) {
	tests := []struct {
		name, message, reply string
		err                  error
	}{
		{
			name:    "project",
			message: "Get project Modernization for Alpha Managed Services.",
			reply:   "More than one Project matches Modernization for Alpha Managed Services. Use the Project display ID and try again.",
			err:     projects.ErrAmbiguousReference,
		},
		{
			name:    "knowledge",
			message: "Get knowledge article VPN recovery for Alpha Managed Services.",
			reply:   "More than one Knowledge article matches VPN recovery for Alpha Managed Services. Use the Article display ID and try again.",
			err:     knowledge.ErrAmbiguousReference,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := &aiWorkspaceConversationStub{}
			tools := &aiWorkspaceToolStub{readErr: test.err}
			handler := NewRouter(Dependencies{
				Principal:                aiWorkspaceMSPPrincipal,
				AIWorkspaceConversations: conversations,
				AIWorkspaceTools:         tools,
				Directory:                aiWorkspaceDirectory(aiWorkspaceClients()),
			})
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost,
				"/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))

			if response.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if len(conversations.appended) != 2 ||
				conversations.appended[1].SafeText != test.reply ||
				len(conversations.appended[1].ReferencedObjects) != 0 {
				t.Fatalf("messages=%+v", conversations.appended)
			}
		})
	}
}

func TestAIWorkspaceStructuredReadDistinguishesMissingAndAmbiguousReferences(t *testing.T) {
	tests := []struct {
		name, wantCode, wantMessage string
		err                         error
		status                      int
	}{
		{
			name: "missing", status: http.StatusNotFound,
			wantCode: "not_found", wantMessage: "resource not found",
			err: scope.ErrNotFound,
		},
		{
			name: "ambiguous", status: http.StatusConflict,
			wantCode:    "ambiguous_reference",
			wantMessage: "More than one exact object matches. Use its display ID and try again.",
			err:         projects.ErrAmbiguousReference,
		},
		{
			name: "ambiguous Client resource", status: http.StatusConflict,
			wantCode:    "ambiguous_reference",
			wantMessage: "More than one exact object matches. Use its display ID and try again.",
			err:         clientresources.ErrAmbiguousResource,
		},
		{
			name: "ambiguous Client", status: http.StatusConflict,
			wantCode:    "ambiguous_reference",
			wantMessage: "More than one exact object matches. Use its display ID and try again.",
			err:         organizations.ErrClientReferenceAmbiguous,
		},
		{
			name: "missing Client", status: http.StatusNotFound,
			wantCode: "not_found", wantMessage: "resource not found",
			err: organizations.ErrClientReferenceNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewRouter(Dependencies{
				Principal:                aiWorkspaceMSPPrincipal,
				AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools:         &aiWorkspaceToolStub{readErr: test.err},
			})
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost,
				"/api/v1/ai/workspace/conversations/conversation-1/tools/read",
				`{"tool_name":"project.get","input":{"client":"Alpha","project":"Modernization"}}`,
			))

			if response.Code != test.status ||
				!strings.Contains(response.Body.String(), `"`+test.wantCode+`"`) ||
				!strings.Contains(response.Body.String(), test.wantMessage) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAIWorkspaceMessageDispatchesExistingTicketWritesWithClosedInput(t *testing.T) {
	tests := []struct {
		message, tool string
		input         map[string]any
	}{
		{
			"Transition ticket INC-200 for Alpha Managed Services to resolved at version 5 because Issue fixed.",
			"ticket.transition",
			map[string]any{"client_id": "client-1", "id": "INC-200", "expected_version": float64(5), "status": "resolved", "reason": "Issue fixed"},
		},
		{
			"Change priority of ticket INC-200 for Alpha Managed Services to high at version 5 because Customer impact.",
			"ticket.priority",
			map[string]any{"client_id": "client-1", "id": "INC-200", "expected_version": float64(5), "priority": "high", "reason": "Customer impact"},
		},
		{
			"Add internal note to ticket INC-200 for Alpha Managed Services at version 5 with body Technician-only detail.",
			"ticket.note",
			map[string]any{"client_id": "client-1", "id": "INC-200", "expected_version": float64(5), "body": "Technician-only detail"},
		},
		{
			"Add client-visible reply to ticket INC-200 for Alpha Managed Services at version 5 with body We are investigating.",
			"ticket.reply",
			map[string]any{"client_id": "client-1", "id": "INC-200", "expected_version": float64(5), "body": "We are investigating"},
		},
	}
	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			tools := &aiWorkspaceToolStub{}
			handler := NewRouter(Dependencies{
				Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: &aiWorkspaceConversationStub{},
				AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost, "/api/v1/ai/workspace/conversations/conversation-1/messages",
				`{"text":"`+test.message+`"}`,
			))
			if response.Code != http.StatusCreated || tools.request.Name != test.tool {
				t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
			}
			var input map[string]any
			if err := json.Unmarshal(tools.request.Input, &input); err != nil || !reflect.DeepEqual(input, test.input) {
				t.Fatalf("input=%#v want=%#v error=%v", input, test.input, err)
			}
		})
	}
}

func TestAIWorkspaceMessageResolvesProjectClientGloballyByName(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Bravo Systems with the three setup tasks, A, B, C with tag ids tag-1."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if tools.request.Name != "project.create" ||
		tools.request.ConversationID != "conversation-1" ||
		tools.request.MessageID != "message-1" {
		t.Fatalf("request=%+v", tools.request)
	}
	var input struct {
		ClientID string   `json:"client_id"`
		Name     string   `json:"name"`
		Tasks    []string `json:"tasks"`
		TagIDs   []string `json:"tag_ids"`
	}
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ClientID != "client-2" || input.Name != "Onboarding" ||
		len(input.Tasks) != 3 || input.Tasks[0] != "A" ||
		input.Tasks[1] != "B" || input.Tasks[2] != "C" ||
		!reflect.DeepEqual(input.TagIDs, []string{"tag-1"}) {
		t.Fatalf("input=%+v", input)
	}
	if directory.list.Target != (scope.Target{MSPID: "msp-1"}) ||
		directory.list.Principal.Scope.ClientID != "" ||
		tools.principal.Scope.ClientID != "" {
		t.Fatalf("directory=%+v proposal principal=%+v", directory.list, tools.principal)
	}
	var body struct {
		Proposal *aiassist.ActionProposal `json:"proposal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil ||
		body.Proposal == nil || body.Proposal.ID != "proposal-1" {
		t.Fatalf("body=%s error=%v", response.Body.String(), err)
	}
}

func TestAIWorkspaceMessageRehydratesProjectDraftWhenTagsArriveInFollowUp(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
	})
	first := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Bravo Systems with the three setup tasks, A, B, C."}`,
	)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusCreated || tools.proposals != 0 || len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText != "Which classification tag IDs should I apply to this project?" {
		t.Fatalf("first status=%d proposals=%d messages=%+v", firstResponse.Code, tools.proposals, conversations.appended)
	}

	second := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"tag-1, tag-2"}`,
	)
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusCreated || tools.request.Name != "project.create" || tools.proposals != 1 {
		t.Fatalf("second status=%d request=%+v proposals=%d body=%s", secondResponse.Code, tools.request, tools.proposals, secondResponse.Body.String())
	}
	var input struct {
		ClientID string   `json:"client_id"`
		Name     string   `json:"name"`
		Tasks    []string `json:"tasks"`
		TagIDs   []string `json:"tag_ids"`
	}
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ClientID != "client-2" || input.Name != "Onboarding" ||
		!reflect.DeepEqual(input.Tasks, []string{"A", "B", "C"}) ||
		!reflect.DeepEqual(input.TagIDs, []string{"tag-1", "tag-2"}) {
		t.Fatalf("input=%+v", input)
	}
}

func TestAIWorkspaceMessageClarifiesIncompleteProjectWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Northwind Legal"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText != "Which tasks should I add to the project?" {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageProposesClientFromOnlySuppliedBusinessFields(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	organizationsService := &aiWorkspaceOrganizationStub{}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Organizations: organizationsService,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a client named Alpha Managed Services with display ID ALPHA-100."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "client.create" ||
		tools.request.ConversationID != "conversation-1" ||
		tools.request.MessageID != "message-1" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	var input map[string]any
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	wantInput := map[string]any{
		"display_id": "ALPHA-100",
		"name":       "Alpha Managed Services",
	}
	if !reflect.DeepEqual(input, wantInput) {
		t.Fatalf("input=%#v want=%#v", input, wantInput)
	}
	wantChecks := []aiWorkspaceClientIdentityCheck{{
		principal: authorization.Principal{
			ID:    "tech",
			Scope: scope.Principal{MSPID: "msp-1"},
			Capabilities: authorization.NewCapabilitySet(
				"ai.assist", "client.create", "organization.read",
				"project.create", "task.create",
			),
		},
		name: "Alpha Managed Services", displayID: "ALPHA-100",
	}}
	if !reflect.DeepEqual(organizationsService.checks, wantChecks) {
		t.Fatalf("identity checks=%+v want=%+v", organizationsService.checks, wantChecks)
	}
}

func TestAIWorkspaceMessageClarifiesMissingClientCreationFields(t *testing.T) {
	for _, test := range []struct {
		name    string
		message string
		reply   string
	}{
		{
			name: "name", message: "Create a client with display ID ALPHA-100.",
			reply: "What should I call the client?",
		},
		{
			name: "display ID", message: "Create a client named Alpha Managed Services.",
			reply: "What display ID should I use for the client?",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conversations := &aiWorkspaceConversationStub{}
			tools := &aiWorkspaceToolStub{}
			organizationsService := &aiWorkspaceOrganizationStub{}
			handler := NewRouter(Dependencies{
				Principal:                aiWorkspaceMSPPrincipal,
				AIWorkspaceConversations: conversations,
				AIWorkspaceTools:         tools, Organizations: organizationsService,
			})
			body, err := json.Marshal(map[string]string{"text": test.message})
			if err != nil {
				t.Fatal(err)
			}
			request := aiJSONRequest(
				http.MethodPost,
				"/api/v1/ai/workspace/conversations/conversation-1/messages",
				string(body),
			)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusCreated || tools.proposals != 0 ||
				len(organizationsService.checks) != 0 {
				t.Fatalf(
					"status=%d proposals=%d checks=%d body=%s",
					response.Code, tools.proposals, len(organizationsService.checks), response.Body.String(),
				)
			}
			if len(conversations.appended) != 2 ||
				conversations.appended[1].SafeText != test.reply {
				t.Fatalf("messages=%+v", conversations.appended)
			}
		})
	}
}

func TestAIWorkspaceMessageClarifiesClientIdentityConflictWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	organizationsService := &aiWorkspaceOrganizationStub{conflict: true}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Organizations: organizationsService,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a client named Archived Alpha with display ID ARCHIVED-100."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.proposals != 0 {
		t.Fatalf("status=%d proposals=%d body=%s", response.Code, tools.proposals, response.Body.String())
	}
	if len(organizationsService.checks) != 1 || len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"A Client with that name or display ID already exists. Check the values and try again." {
		t.Fatalf("checks=%+v messages=%+v", organizationsService.checks, conversations.appended)
	}
}

func TestAIWorkspaceMessageResolvesTaskClientByDisplayIDBeforeProject(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	projectQueries := &projectQueryActionsStub{projects: []projects.ProjectSummary{{
		ID: "project-2", DisplayID: "PRJ-BR-2042",
		Name: "Bravo Modernization", Version: 7,
	}}}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory, ProjectQueries: projectQueries,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Bravo Modernization for BRAVO-200 with tag ids tag-1."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if tools.request.Name != "task.create" ||
		tools.request.ConversationID != "conversation-1" ||
		tools.request.MessageID != "message-1" {
		t.Fatalf("request=%+v", tools.request)
	}
	var input struct {
		ClientID   string `json:"client_id"`
		ProjectID  string `json:"project_id"`
		ProjectRef string `json:"project_ref"`
		Title      string `json:"title"`
	}
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ClientID != "client-2" || input.ProjectID != "project-2" ||
		input.ProjectRef != "Bravo Modernization" ||
		input.Title != "Verify backup recovery" {
		t.Fatalf("input=%+v", input)
	}
	if projectQueries.target != (scope.Target{MSPID: "msp-1", ClientID: "client-2"}) ||
		projectQueries.reference != "Bravo Modernization" {
		t.Fatalf(
			"target=%+v reference=%q",
			projectQueries.target, projectQueries.reference,
		)
	}
	var body struct {
		Proposal *aiassist.ActionProposal `json:"proposal"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil ||
		body.Proposal == nil || body.Proposal.ID != "proposal-1" {
		t.Fatalf("body=%s error=%v", response.Body.String(), err)
	}
}

func TestAIWorkspaceMessageRehydratesTaskDraftWhenTagsArriveInFollowUp(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	projectQueries := &projectQueryActionsStub{projects: []projects.ProjectSummary{{
		ID: "project-2", DisplayID: "PRJ-BR-2042", Name: "Bravo Modernization", Version: 7,
	}}}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()), ProjectQueries: projectQueries,
	})
	first := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Bravo Modernization for BRAVO-200."}`,
	)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusCreated || tools.proposals != 0 || len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText != "Which classification tag IDs should I apply to this task?" {
		t.Fatalf("first status=%d proposals=%d messages=%+v", firstResponse.Code, tools.proposals, conversations.appended)
	}
	second := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"tag-1 tag-2"}`,
	)
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusCreated || tools.request.Name != "task.create" || tools.proposals != 1 {
		t.Fatalf("second status=%d request=%+v proposals=%d body=%s", secondResponse.Code, tools.request, tools.proposals, secondResponse.Body.String())
	}
	var input struct {
		ClientID  string   `json:"client_id"`
		ProjectID string   `json:"project_id"`
		Title     string   `json:"title"`
		TagIDs    []string `json:"tag_ids"`
	}
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ClientID != "client-2" || input.ProjectID != "project-2" ||
		input.Title != "Verify backup recovery" || !reflect.DeepEqual(input.TagIDs, []string{"tag-1", "tag-2"}) {
		t.Fatalf("input=%+v", input)
	}
}

func TestAIWorkspaceMessageFindsTaskProjectBeyondFirstHundred(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	foundProjects := make([]projects.ProjectSummary, 100, 101)
	for index := range foundProjects {
		foundProjects[index] = projects.ProjectSummary{
			ID:        projects.ProjectID(fmt.Sprintf("project-%03d", index)),
			DisplayID: fmt.Sprintf("PRJ-%03d", index),
			Name:      fmt.Sprintf("Unrelated Project %03d", index),
		}
	}
	foundProjects = append(foundProjects, projects.ProjectSummary{
		ID: "project-older", DisplayID: "PRJ-OLDER",
		Name: "Older Modernization", Version: 7,
	})
	projectQueries := &projectQueryActionsStub{projects: foundProjects}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
		ProjectQueries: projectQueries,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Older Modernization for Bravo Systems with tag ids tag-1."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "task.create" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	var input struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(tools.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ProjectID != "project-older" {
		t.Fatalf("project ID=%q input=%s", input.ProjectID, tools.request.Input)
	}
}

func TestAIWorkspaceMessageFindsTaskProjectAmbiguityBeyondFirstHundred(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	foundProjects := make([]projects.ProjectSummary, 100, 101)
	foundProjects[0] = projects.ProjectSummary{
		ID: "project-newer", DisplayID: "PRJ-NEWER", Name: "Modernization",
	}
	for index := 1; index < len(foundProjects); index++ {
		foundProjects[index] = projects.ProjectSummary{
			ID:        projects.ProjectID(fmt.Sprintf("project-%03d", index)),
			DisplayID: fmt.Sprintf("PRJ-%03d", index),
			Name:      fmt.Sprintf("Unrelated Project %03d", index),
		}
	}
	foundProjects = append(foundProjects, projects.ProjectSummary{
		ID: "project-older", DisplayID: "PRJ-OLDER", Name: "Modernization",
	})
	projectQueries := &projectQueryActionsStub{projects: foundProjects}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
		ProjectQueries: projectQueries,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Modernization for Bravo Systems."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.proposals != 0 {
		t.Fatalf("status=%d proposals=%d body=%s", response.Code, tools.proposals, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"More than one Project matches Modernization. Use the Project display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageClarifiesUnknownTaskProjectWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory,
		ProjectQueries: &projectQueryActionsStub{projects: []projects.ProjectSummary{}},
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Missing Project for Bravo Systems."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"I couldn't find Missing Project for Bravo Systems. Check the Project name or display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageClarifiesAmbiguousTaskProjectWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory,
		ProjectQueries: &projectQueryActionsStub{projects: []projects.ProjectSummary{
			{ID: "project-1", DisplayID: "PRJ-1", Name: "Modernization"},
			{ID: "project-2", DisplayID: "PRJ-2", Name: "Modernization"},
		}},
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Modernization for Bravo Systems."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"More than one Project matches Modernization. Use the Project display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageClarifiesTypedTaskProjectAmbiguityWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(aiWorkspaceClients()),
		ProjectQueries: &projectQueryActionsStub{resolveErr: projects.ErrAmbiguousReference},
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Add a task titled Verify backup recovery to project Modernization for Bravo Systems."}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.request.Name != "" || tools.proposals != 0 {
		t.Fatalf(
			"status=%d request=%+v proposals=%d body=%s",
			response.Code, tools.request, tools.proposals, response.Body.String(),
		)
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"More than one Project matches Modernization. Use the Project display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageClarifiesUnknownClientWithoutProposal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Contoso with tasks A, B, C"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.proposals != 0 {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, tools.request, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"I couldn't find an authorized Client matching Contoso. Check the Client name or display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageClarifiesAmbiguousClientWithoutProposal(t *testing.T) {
	clients := aiWorkspaceClients()
	clients[0].Name = "Shared Client"
	clients[1].Name = " shared   client "
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: aiWorkspaceDirectory(clients),
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Shared Client with tasks A, B, C"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || tools.proposals != 0 {
		t.Fatalf("status=%d proposals=%d body=%s", response.Code, tools.proposals, response.Body.String())
	}
	if len(conversations.appended) != 2 ||
		conversations.appended[1].SafeText !=
			"More than one authorized Client matches Shared Client. Use the Client display ID and try again." {
		t.Fatalf("messages=%+v", conversations.appended)
	}
}

func TestAIWorkspaceMessageDoesNotLeakDirectoryWhenUnauthorized(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	directory := aiWorkspaceDirectory(aiWorkspaceClients())
	directory.err = authorization.ErrForbidden
	handler := NewRouter(Dependencies{
		Principal: aiWorkspaceMSPPrincipal, AIWorkspaceConversations: conversations,
		AIWorkspaceTools: tools, Directory: directory,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/messages",
		`{"text":"Create a project titled Onboarding for Bravo Systems with tasks A, B, C"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || tools.proposals != 0 {
		t.Fatalf("status=%d proposals=%d body=%s", response.Code, tools.proposals, response.Body.String())
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Message != "action is not permitted" {
		t.Fatalf("body=%+v", body)
	}
}

func TestAIWorkspaceCreateConversationRejectsClientScopedPrincipal(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	handler := NewRouter(Dependencies{
		Principal: aiTechnicianPrincipal, AIWorkspaceConversations: conversations,
	})
	request := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations",
		`{"title":"New MSP-wide conversation"}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound || conversations.createdTitle != "" {
		t.Fatalf("status=%d title=%q body=%s", response.Code, conversations.createdTitle, response.Body.String())
	}
}

func TestAIWorkspaceProposalRequiresExplicitConfirmationVersion(t *testing.T) {
	conversations := &aiWorkspaceConversationStub{}
	tools := &aiWorkspaceToolStub{}
	handler := NewRouter(Dependencies{
		Principal:                aiWorkspaceMSPPrincipal,
		AIWorkspaceConversations: conversations,
		AIWorkspaceTools:         tools,
	})
	propose := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/proposals",
		`{"tool_name":"ticket.transition","input":{"client_id":"client-id","id":"ticket-1","expected_version":3,"status":"resolved","reason":"fixed"}}`,
	)
	proposalResponse := httptest.NewRecorder()
	handler.ServeHTTP(proposalResponse, propose)
	if proposalResponse.Code != http.StatusCreated || tools.request.ConversationID != "conversation-1" {
		t.Fatalf("status=%d request=%+v body=%s", proposalResponse.Code, tools.request, proposalResponse.Body.String())
	}
	var proposal aiassist.ActionProposal
	if err := json.Unmarshal(proposalResponse.Body.Bytes(), &proposal); err != nil || proposal.ID != "proposal-1" {
		t.Fatalf("proposal=%+v error=%v", proposal, err)
	}

	confirm := aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/proposals/proposal-1/confirm",
		`{"expected_version":1}`,
	)
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirm)
	if confirmResponse.Code != http.StatusOK || tools.version != 1 {
		t.Fatalf("status=%d version=%d body=%s", confirmResponse.Code, tools.version, confirmResponse.Body.String())
	}
}

func TestAIWorkspaceHTTPConfirmsPersistedClientResourceCreateProposal(t *testing.T) {
	const clientID = "11111111-1111-4111-8111-111111111111"
	principal := authorization.Principal{
		ID:    "technician-1",
		Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(
			"ai.assist", "location.create",
		),
	}
	directory := organizations.NewDirectoryService(
		&aiWorkspaceTargetDirectoryRepository{
			directory: organizations.Directory{Clients: []organizations.Client{{
				Envelope: object.Envelope{
					ID: clientID, MSPID: "msp-1", ClientID: clientID,
					DisplayID: "NORTHWIND", LifecycleState: "active",
				},
				Name: "Northwind Legal",
			}}},
		},
		time.Now,
		func() string { return "directory-id" },
	)
	store := &serializedAIWorkspaceProposalStore{}
	creator := &aiWorkspaceLocationCreator{}
	ids := []string{"proposal-location", "correlation-location"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{
			rtitools.NewLocationCreateTool(
				directory, creator,
				func() string { return "22222222-2222-4222-8222-222222222222" },
			),
		},
		store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string {
			value := ids[0]
			ids = ids[1:]
			return value
		},
		directory,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return principal, nil
		},
		AIWorkspaceConversations: &aiWorkspaceConversationStub{},
		AIWorkspaceTools:         registry,
	})

	proposalResponse := httptest.NewRecorder()
	handler.ServeHTTP(proposalResponse, aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/conversations/conversation-1/proposals",
		`{"tool_name":"location.create","input":{"client":"Northwind Legal","display_id":"ACCEPT-LOC-20260806","name":"Acceptance Test Location 20260806"}}`,
	))
	if proposalResponse.Code != http.StatusCreated {
		t.Fatalf(
			"proposal status=%d body=%s",
			proposalResponse.Code, proposalResponse.Body.String(),
		)
	}
	var proposal aiassist.ActionProposal
	if err := json.Unmarshal(proposalResponse.Body.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}

	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, aiJSONRequest(
		http.MethodPost,
		"/api/v1/ai/workspace/proposals/"+proposal.ID+"/confirm",
		`{"expected_version":1}`,
	))
	if confirmResponse.Code != http.StatusOK || creator.calls != 1 ||
		!strings.Contains(confirmResponse.Body.String(), `"display_id":"ACCEPT-LOC-20260806"`) {
		t.Fatalf(
			"confirm status=%d creator calls=%d body=%s",
			confirmResponse.Code, creator.calls, confirmResponse.Body.String(),
		)
	}
}

func TestAIWorkspaceProposalConfirmationKeepsResolutionErrorsSafe(t *testing.T) {
	for _, test := range []struct {
		name, code string
		err        error
		status     int
	}{
		{name: "ambiguous resource", err: clientresources.ErrAmbiguousResource, status: http.StatusConflict, code: "ambiguous_reference"},
		{name: "ambiguous Client", err: organizations.ErrClientReferenceAmbiguous, status: http.StatusConflict, code: "ambiguous_reference"},
		{name: "missing resource", err: scope.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "missing Client", err: organizations.ErrClientReferenceNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "stale proposal", err: aiassist.ErrProposalStale, status: http.StatusConflict, code: "version_conflict"},
		{name: "version conflict", err: object.ErrVersionConflict, status: http.StatusConflict, code: "version_conflict"},
		{name: "lifecycle conflict", err: clientresources.ErrLifecycleConflict, status: http.StatusConflict, code: "lifecycle_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewRouter(Dependencies{
				Principal:        aiWorkspaceMSPPrincipal,
				AIWorkspaceTools: &aiWorkspaceToolStub{confirmErr: test.err},
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, aiJSONRequest(
				http.MethodPost,
				"/api/v1/ai/workspace/proposals/proposal-1/confirm",
				`{"expected_version":1}`,
			))
			if response.Code != test.status ||
				!strings.Contains(response.Body.String(), `"`+test.code+`"`) {
				t.Fatalf("error=%v status=%d body=%s", test.err, response.Code, response.Body.String())
			}
		})
	}
}

func TestAIWorkspaceStructuredProjectTargetMustRemainActiveThroughConfirmation(t *testing.T) {
	const clientID = "11111111-1111-4111-8111-111111111111"
	principal := authorization.Principal{
		ID:    "tech",
		Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(
			"ai.assist", "project.create", "task.create",
		),
	}
	principalResolver := func(*http.Request) (authorization.Principal, error) {
		return principal, nil
	}

	t.Run("inactive direct UUID is enumeration-safe and never proposed", func(t *testing.T) {
		directoryRepository := &aiWorkspaceTargetDirectoryRepository{
			directory: organizations.Directory{Clients: []organizations.Client{{
				Envelope: object.Envelope{
					ID: clientID, MSPID: "msp-1", ClientID: clientID,
					LifecycleState: "inactive",
				},
			}}},
		}
		directory := organizations.NewDirectoryService(
			directoryRepository, time.Now, func() string { return "directory-id" },
		)
		store := &aiWorkspaceProposalStore{}
		creator := &aiWorkspaceProjectCreator{}
		ids := []string{
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
			"proposal-1", "correlation-1",
		}
		tools, err := aiassist.NewRegistry(
			[]aiassist.Tool{rtitools.NewProjectCreateTool(creator, func() string {
				value := ids[0]
				ids = ids[1:]
				return value
			})},
			store,
			func(context.Context, authorization.Principal) (authorization.Principal, error) {
				return principal, nil
			},
			time.Now,
			func() string {
				value := ids[0]
				ids = ids[1:]
				return value
			},
			directory,
		)
		if err != nil {
			t.Fatal(err)
		}
		handler := NewRouter(Dependencies{
			Principal:                principalResolver,
			AIWorkspaceConversations: &aiWorkspaceConversationStub{},
			AIWorkspaceTools:         tools,
		})
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, aiJSONRequest(
			http.MethodPost,
			"/api/v1/ai/workspace/conversations/conversation-1/proposals",
			`{"tool_name":"project.create","input":{"client_id":"`+clientID+`","name":"Onboarding","tasks":["A"],"tag_ids":["tag-1"]}}`,
		))

		if response.Code != http.StatusNotFound ||
			creator.calls != 0 ||
			store.proposal.ID != "" {
			t.Fatalf(
				"status=%d creator calls=%d proposal=%+v body=%s",
				response.Code, creator.calls, store.proposal, response.Body.String(),
			)
		}
		if !strings.Contains(response.Body.String(), `"message":"resource not found"`) {
			t.Fatalf("response leaked target identity: %s", response.Body.String())
		}
	})

	t.Run("active target becoming inactive blocks confirmation", func(t *testing.T) {
		directoryRepository := &aiWorkspaceTargetDirectoryRepository{
			directory: organizations.Directory{Clients: []organizations.Client{{
				Envelope: object.Envelope{
					ID: clientID, MSPID: "msp-1", ClientID: clientID,
					LifecycleState: "active",
				},
			}}},
		}
		directory := organizations.NewDirectoryService(
			directoryRepository, time.Now, func() string { return "directory-id" },
		)
		store := &aiWorkspaceProposalStore{}
		creator := &aiWorkspaceProjectCreator{}
		ids := []string{
			"44444444-4444-4444-8444-444444444444",
			"55555555-5555-4555-8555-555555555555",
			"proposal-2", "correlation-2",
		}
		tools, err := aiassist.NewRegistry(
			[]aiassist.Tool{rtitools.NewProjectCreateTool(creator, func() string {
				value := ids[0]
				ids = ids[1:]
				return value
			})},
			store,
			func(context.Context, authorization.Principal) (authorization.Principal, error) {
				return principal, nil
			},
			time.Now,
			func() string {
				value := ids[0]
				ids = ids[1:]
				return value
			},
			directory,
		)
		if err != nil {
			t.Fatal(err)
		}
		handler := NewRouter(Dependencies{
			Principal:                principalResolver,
			AIWorkspaceConversations: &aiWorkspaceConversationStub{},
			AIWorkspaceTools:         tools,
		})
		proposalResponse := httptest.NewRecorder()
		handler.ServeHTTP(proposalResponse, aiJSONRequest(
			http.MethodPost,
			"/api/v1/ai/workspace/conversations/conversation-1/proposals",
			`{"tool_name":"project.create","input":{"client_id":"`+clientID+`","name":"Onboarding","tasks":["A"],"tag_ids":["tag-1"]}}`,
		))
		if proposalResponse.Code != http.StatusCreated ||
			store.proposal.TargetClientID != clientID {
			t.Fatalf(
				"status=%d proposal=%+v body=%s",
				proposalResponse.Code, store.proposal, proposalResponse.Body.String(),
			)
		}
		directoryRepository.directory.Clients[0].LifecycleState = "inactive"

		confirmResponse := httptest.NewRecorder()
		handler.ServeHTTP(confirmResponse, aiJSONRequest(
			http.MethodPost,
			"/api/v1/ai/workspace/proposals/proposal-2/confirm",
			`{"expected_version":1}`,
		))

		if confirmResponse.Code != http.StatusNotFound ||
			creator.calls != 0 ||
			store.proposal.State != aiassist.ProposalPending {
			t.Fatalf(
				"status=%d creator calls=%d proposal state=%q body=%s",
				confirmResponse.Code, creator.calls, store.proposal.State,
				confirmResponse.Body.String(),
			)
		}
		if !strings.Contains(confirmResponse.Body.String(), `"message":"resource not found"`) {
			t.Fatalf("response leaked target identity: %s", confirmResponse.Body.String())
		}
	})
}
