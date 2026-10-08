package aiassist

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type toolStub struct {
	name         string
	kind         ToolKind
	capability   string
	target       scope.Target
	preview      Preview
	executeCalls int
}

func (t *toolStub) Name() string                   { return t.name }
func (t *toolStub) Version() int                   { return 1 }
func (t *toolStub) Kind() ToolKind                 { return t.kind }
func (t *toolStub) RequiredCapability() string     { return t.capability }
func (t *toolStub) Validate(json.RawMessage) error { return nil }
func (t *toolStub) ResolveScope(context.Context, authorization.Principal, json.RawMessage) (scope.Target, error) {
	return t.target, nil
}
func (t *toolStub) Preview(context.Context, authorization.Principal, json.RawMessage) (Preview, error) {
	return t.preview, nil
}
func (t *toolStub) Execute(context.Context, authorization.Principal, json.RawMessage, string) (ToolResult, error) {
	t.executeCalls++
	return ToolResult{Summary: "updated"}, nil
}

type proposalStoreStub struct {
	proposal        ActionProposal
	getTargets      []scope.Target
	confirmTargets  []scope.Target
	completeTargets []scope.Target
	rejectTargets   []scope.Target
}

type preparableToolStub struct {
	*toolStub
	prepareErr error
}

type targetAuthorizerStub struct {
	err     error
	targets []scope.Target
}

func (s *targetAuthorizerStub) AuthorizeExecutionTarget(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
) error {
	s.targets = append(s.targets, target)
	return s.err
}

func (t *preparableToolStub) Prepare(
	context.Context,
	authorization.Principal,
	json.RawMessage,
) (json.RawMessage, error) {
	if t.prepareErr != nil {
		return nil, t.prepareErr
	}
	return json.RawMessage(`{"name":"Onboarding","project_id":"system-id"}`), nil
}

func TestRegistryPreservesTypedPreparerErrors(t *testing.T) {
	typed := errors.New("typed preparer conflict")
	tool := &preparableToolStub{
		toolStub: &toolStub{
			name: "project.create", kind: ToolWrite,
			capability: "project.create",
			target:     scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		},
		prepareErr: typed,
	}
	principal := authorizedToolPrincipal()
	registry, err := NewRegistry(
		[]Tool{tool}, &proposalStoreStub{},
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now, func() string { return "id" }, &targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Propose(context.Background(), principal, ToolRequest{
		Name: tool.name, ConversationID: "conversation-1",
		Input: json.RawMessage(`{"client":"Northwind"}`),
	})
	if !errors.Is(err, typed) || errors.Is(err, ErrInvalidTool) {
		t.Fatalf("Propose() error=%v, want typed preparer error", err)
	}
}

func TestExpiredWriteProposalDoesNotExecuteTool(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	tool := &toolStub{
		name: "ticket.create", kind: ToolWrite, capability: "work_record.create",
		target:  scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{Summary: "Create ticket", TargetType: "work_record", TargetID: "ticket-1"},
	}
	principal := authorization.Principal{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("work_record.create")}
	store := &proposalStoreStub{}
	registry, err := NewRegistry([]Tool{tool}, store, func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, func() time.Time { return now }, func() string { return "proposal-1" }, &targetAuthorizerStub{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, ToolRequest{Name: "ticket.create", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"Northwind Legal"}`)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, ErrProposalExpired) {
		t.Fatalf("Confirm() error=%v, want ErrProposalExpired", err)
	}
	if tool.executeCalls != 0 {
		t.Fatalf("Execute calls=%d, want 0", tool.executeCalls)
	}
}

func (s *proposalStoreStub) CreateProposal(_ context.Context, proposal ActionProposal) error {
	s.proposal = proposal
	return nil
}
func (s *proposalStoreStub) GetProposal(_ context.Context, target scope.Target, _ string, _ string) (ActionProposal, error) {
	s.getTargets = append(s.getTargets, target)
	return s.proposal, nil
}
func (s *proposalStoreStub) ConfirmProposal(_ context.Context, target scope.Target, id, principalID string, version int64, at time.Time) (ActionProposal, error) {
	s.confirmTargets = append(s.confirmTargets, target)
	if s.proposal.ID != id || s.proposal.PrincipalID != principalID ||
		s.proposal.Version != version || s.proposal.State != ProposalPending {
		return ActionProposal{}, ErrProposalConflict
	}
	s.proposal.State = ProposalConfirmed
	s.proposal.ConfirmedAt = &at
	s.proposal.Version++
	return s.proposal, nil
}
func (s *proposalStoreStub) CompleteProposal(_ context.Context, target scope.Target, id string, version int64, result ToolResult, at time.Time) error {
	s.completeTargets = append(s.completeTargets, target)
	if s.proposal.ID != id || s.proposal.Version != version {
		return ErrProposalConflict
	}
	s.proposal.Result = &result
	s.proposal.UpdatedAt = at
	s.proposal.Version++
	return nil
}
func (s *proposalStoreStub) FailProposal(context.Context, scope.Target, string, int64, string, time.Time) error {
	return nil
}
func (s *proposalStoreStub) RejectProposal(_ context.Context, target scope.Target, _ string, _ string, _ int64, _ time.Time) error {
	s.rejectTargets = append(s.rejectTargets, target)
	return nil
}
func (s *proposalStoreStub) ExpireProposal(context.Context, scope.Target, string, string, int64, time.Time) error {
	return nil
}

func TestWriteProposalRequiresActiveAuthorizedExecutionTarget(t *testing.T) {
	tool := &toolStub{
		name: "project.create", kind: ToolWrite, capability: "project.create",
		target: scope.Target{MSPID: "msp-1", ClientID: "inactive-client"},
		preview: Preview{
			Summary: "Create project", TargetType: "project", TargetID: "project-1",
		},
	}
	store := &proposalStoreStub{}
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("project.create"),
	}
	targets := &targetAuthorizerStub{err: scope.ErrNotFound}
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string { return "proposal-1" },
		targets,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: "project.create", ConversationID: "conversation-1",
			Input: json.RawMessage(`{"client_id":"inactive-client"}`),
		},
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Propose() error=%v, want enumeration-safe ErrNotFound", err)
	}
	if store.proposal.ID != "" || tool.executeCalls != 0 {
		t.Fatalf("proposal=%+v execute calls=%d", store.proposal, tool.executeCalls)
	}
	want := []scope.Target{{MSPID: "msp-1", ClientID: "inactive-client"}}
	if len(targets.targets) != 1 || targets.targets[0] != want[0] {
		t.Fatalf("authorized targets=%+v want=%+v", targets.targets, want)
	}
}

func TestWriteConfirmationReauthorizesTargetLifecycleBeforeExecution(t *testing.T) {
	tool := &toolStub{
		name: "task.create", kind: ToolWrite, capability: "task.create",
		target: scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{
			Summary: "Create task", TargetType: "project", TargetID: "project-1",
		},
	}
	store := &proposalStoreStub{}
	principal := authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("task.create"),
	}
	targets := &targetAuthorizerStub{}
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string { return "proposal-1" },
		targets,
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: "task.create", ConversationID: "conversation-1",
			Input: json.RawMessage(`{"client_id":"client-1"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	targets.err = scope.ErrNotFound

	_, err = registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Confirm() error=%v, want enumeration-safe ErrNotFound", err)
	}
	if len(store.confirmTargets) != 0 || tool.executeCalls != 0 {
		t.Fatalf(
			"confirmed targets=%+v execute calls=%d",
			store.confirmTargets, tool.executeCalls,
		)
	}
	if len(targets.targets) != 2 {
		t.Fatalf("target authorization calls=%d, want proposal plus confirmation", len(targets.targets))
	}
}

func TestMSPGlobalProposalUsesCallerScopeAndClientExecutionTarget(t *testing.T) {
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	executionTarget := scope.Target{MSPID: "msp-1", ClientID: "client-1"}
	accessTarget := scope.Target{MSPID: "msp-1"}
	tool := &toolStub{
		name: "ticket.update", kind: ToolWrite, capability: "work_record.update",
		target: executionTarget,
		preview: Preview{
			Summary: "Update", TargetType: "work_record", TargetID: "ticket-1",
		},
	}
	store := &proposalStoreStub{}
	principal := authorizedToolPrincipal()
	principal.Scope.ClientID = ""
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		func() time.Time { return now },
		func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: tool.name, ConversationID: "conversation-1",
			Input: json.RawMessage(`{"id":"ticket-1"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ClientID != "" || proposal.TargetClientID != "client-1" {
		t.Fatalf(
			"proposal access client=%q target client=%q",
			proposal.ClientID, proposal.TargetClientID,
		)
	}
	if _, err := registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	); err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if tool.executeCalls != 1 {
		t.Fatalf("executor called %d times", tool.executeCalls)
	}
	for operation, targets := range map[string][]scope.Target{
		"get": store.getTargets, "confirm": store.confirmTargets,
		"complete": store.completeTargets,
	} {
		if len(targets) != 1 || targets[0] != accessTarget {
			t.Fatalf("%s targets=%+v want=%+v", operation, targets, accessTarget)
		}
	}

	rejected, err := registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: tool.name, ConversationID: "conversation-1",
			Input: json.RawMessage(`{"id":"ticket-2"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Reject(
		context.Background(), principal, rejected.ID, rejected.Version,
	); err != nil {
		t.Fatalf("Reject() error=%v", err)
	}
	if len(store.rejectTargets) != 1 || store.rejectTargets[0] != accessTarget {
		t.Fatalf("reject targets=%+v want=%+v", store.rejectTargets, accessTarget)
	}
	if tool.executeCalls != 1 {
		t.Fatalf("rejected proposal executed; calls=%d", tool.executeCalls)
	}
}

func TestProposalConfirmationRejectsChangedExecutionTarget(t *testing.T) {
	tool := &toolStub{
		name: "ticket.update", kind: ToolWrite, capability: "work_record.update",
		target: scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{
			Summary: "Update", TargetType: "work_record", TargetID: "ticket-1",
		},
	}
	store := &proposalStoreStub{}
	principal := authorizedToolPrincipal()
	principal.Scope.ClientID = ""
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: tool.name, ConversationID: "conversation-1",
			Input: json.RawMessage(`{"id":"ticket-1"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	tool.target.ClientID = "client-2"

	_, err = registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	)
	if !errors.Is(err, ErrProposalStale) {
		t.Fatalf("Confirm() error=%v", err)
	}
	if tool.executeCalls != 0 {
		t.Fatalf("executor called %d times", tool.executeCalls)
	}
}

func TestClientScopedProposalKeepsMatchingAccessAndExecutionScopes(t *testing.T) {
	tool := &toolStub{
		name: "ticket.update", kind: ToolWrite, capability: "work_record.update",
		target: scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{
			Summary: "Update", TargetType: "work_record", TargetID: "ticket-1",
		},
	}
	store := &proposalStoreStub{}
	principal := authorizedToolPrincipal()
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(
		context.Background(), principal,
		ToolRequest{
			Name: tool.name, ConversationID: "conversation-1",
			Input: json.RawMessage(`{"id":"ticket-1"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ClientID != "client-1" || proposal.TargetClientID != "client-1" {
		t.Fatalf(
			"proposal access client=%q target client=%q",
			proposal.ClientID, proposal.TargetClientID,
		)
	}
	if _, err := registry.Confirm(
		context.Background(), principal, proposal.ID, proposal.Version,
	); err != nil {
		t.Fatalf("Confirm() error=%v", err)
	}
	if len(store.getTargets) != 1 ||
		store.getTargets[0] != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) {
		t.Fatalf("get targets=%+v", store.getTargets)
	}
}

func TestWriteToolRequiresPreviewAndLiveReauthorization(t *testing.T) {
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	tool := &toolStub{
		name: "ticket.update", kind: ToolWrite, capability: "work_record.update",
		target: scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{
			Summary: "Update ticket status", TargetType: "work_record",
			TargetID: "ticket-1", TargetVersion: 3,
			Changes: map[string]Change{"status": {Before: "open", After: "resolved"}},
		},
	}
	store := &proposalStoreStub{}
	current := authorizedToolPrincipal()
	registry, err := NewRegistry(
		[]Tool{tool},
		store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return current, nil },
		func() time.Time { return now },
		func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(
		context.Background(),
		current,
		ToolRequest{Name: "ticket.update", ConversationID: "conversation-1", Input: json.RawMessage(`{"id":"ticket-1"}`)},
	)
	if err != nil || proposal.Preview.Changes["status"].After != "resolved" {
		t.Fatalf("proposal=%+v error=%v", proposal, err)
	}

	current.Capabilities = authorization.NewCapabilitySet()
	_, err = registry.Confirm(context.Background(), current, proposal.ID, proposal.Version)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("Confirm() error=%v", err)
	}
	if tool.executeCalls != 0 {
		t.Fatalf("executor called %d times", tool.executeCalls)
	}
}

func TestWriteToolPersistsPreparedSystemMetadataInExactProposal(t *testing.T) {
	tool := &preparableToolStub{toolStub: &toolStub{
		name: "project.create", kind: ToolWrite, capability: "project.create",
		target: scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{
			Summary: "Create Onboarding", TargetType: "project",
			TargetID: "system-id",
		},
	}}
	store := &proposalStoreStub{}
	principal := authorizedToolPrincipal()
	principal.Capabilities = authorization.NewCapabilitySet("project.create")
	registry, err := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		time.Now,
		func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := registry.Propose(
		context.Background(),
		principal,
		ToolRequest{
			Name: "project.create", ConversationID: "conversation-1",
			Input: json.RawMessage(`{"name":"Onboarding"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(proposal.NormalizedInput) !=
		`{"name":"Onboarding","project_id":"system-id"}` {
		t.Fatalf("normalized input=%s", proposal.NormalizedInput)
	}
}

func TestWriteToolRejectsChangedPreviewBeforeExecution(t *testing.T) {
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	tool := &toolStub{
		name: "ticket.update", kind: ToolWrite, capability: "work_record.update",
		target:  scope.Target{MSPID: "msp-1", ClientID: "client-1"},
		preview: Preview{Summary: "Update", TargetType: "work_record", TargetID: "ticket-1", TargetVersion: 3},
	}
	store := &proposalStoreStub{}
	principal := authorizedToolPrincipal()
	registry, _ := NewRegistry(
		[]Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now }, func() string { return "proposal-1" },
		&targetAuthorizerStub{},
	)
	proposal, err := registry.Propose(context.Background(), principal, ToolRequest{Name: tool.name, ConversationID: "conversation-1", Input: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	tool.preview.TargetVersion = 4

	_, err = registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version)
	if !errors.Is(err, ErrProposalStale) || tool.executeCalls != 0 {
		t.Fatalf("Confirm() error=%v calls=%d", err, tool.executeCalls)
	}
}

func authorizedToolPrincipal() authorization.Principal {
	return authorization.Principal{
		ID:           "technician-1",
		Scope:        scope.Principal{MSPID: "msp-1", ClientID: "client-1"},
		Capabilities: authorization.NewCapabilitySet("work_record.update"),
	}
}
