package rtitools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
)

type clientIdentityCheck struct {
	principal authorization.Principal
	name      string
	displayID string
}

type clientIdentityConflictStub struct {
	conflict bool
	err      error
	checks   []clientIdentityCheck
}

func (s *clientIdentityConflictStub) HasClientIdentityConflict(
	_ context.Context,
	principal authorization.Principal,
	name string,
	displayID string,
) (bool, error) {
	s.checks = append(s.checks, clientIdentityCheck{
		principal: principal, name: name, displayID: displayID,
	})
	return s.conflict, s.err
}

type clientCreatorStub struct {
	client  organizations.Client
	command organizations.CreateClientCommand
	calls   int
	err     error
}

func (s *clientCreatorStub) CreateClient(
	_ context.Context,
	command organizations.CreateClientCommand,
) (organizations.Client, error) {
	s.calls++
	s.command = command
	if s.err != nil {
		return organizations.Client{}, s.err
	}
	client := s.client
	if client.ID == "" {
		client = organizations.Client{
			Envelope: object.Envelope{
				ID: command.ClientID, ClientID: command.ClientID,
				DisplayID: command.DisplayID, LifecycleState: "active", Version: 1,
			},
			Name: command.Name,
		}
	}
	return client, nil
}

func TestClientCreateToolPreparesStableIdentityAndExecutesOrdinaryService(t *testing.T) {
	conflicts := &clientIdentityConflictStub{}
	creator := &clientCreatorStub{}
	generatedID := "019fb3c2-0000-7000-8000-000000000401"
	generated := 0
	tool := NewClientCreateTool(conflicts, creator, func() string {
		generated++
		return generatedID
	})
	principal := testPrincipal("client.create", "organization.read")

	if tool.Name() != "client.create" ||
		tool.RequiredCapability() != "client.create" ||
		tool.Kind() != aiassist.ToolWrite {
		t.Fatalf(
			"tool name=%q capability=%q kind=%q",
			tool.Name(), tool.RequiredCapability(), tool.Kind(),
		)
	}
	preparer, ok := tool.(aiassist.ToolInputPreparer)
	if !ok {
		t.Fatal("client.create must prepare its trusted client identity")
	}
	prepared, err := preparer.Prepare(
		context.Background(),
		principal,
		json.RawMessage(`{"display_id":" CLIENT-401 ","name":" Alpha Managed Services "}`),
	)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if generated != 1 {
		t.Fatalf("generated IDs = %d, want 1", generated)
	}
	var preparedFields map[string]any
	if err := json.Unmarshal(prepared, &preparedFields); err != nil {
		t.Fatal(err)
	}
	wantPrepared := map[string]any{
		"client_id":  generatedID,
		"display_id": "CLIENT-401",
		"name":       "Alpha Managed Services",
	}
	if !reflect.DeepEqual(preparedFields, wantPrepared) {
		t.Fatalf("prepared = %#v, want %#v", preparedFields, wantPrepared)
	}
	if err := tool.Validate(prepared); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	target, err := tool.ResolveScope(context.Background(), principal, prepared)
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}
	if target.MSPID != principal.Scope.MSPID || target.ClientID != "" {
		t.Fatalf("resolved target = %+v, want MSP-global target", target)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.TargetID != generatedID || preview.TargetVersion != 0 {
		t.Fatalf("preview target = %q version = %d", preview.TargetID, preview.TargetVersion)
	}
	wantChanges := map[string]aiassist.Change{
		"name":            {Before: nil, After: "Alpha Managed Services"},
		"display_id":      {Before: nil, After: "CLIENT-401"},
		"lifecycle_state": {Before: nil, After: "active"},
	}
	if !reflect.DeepEqual(preview.Changes, wantChanges) {
		t.Fatalf("preview changes = %#v, want %#v", preview.Changes, wantChanges)
	}
	if creator.calls != 0 {
		t.Fatal("ordinary organization service ran before Execute")
	}

	correlationID := "019fb3c2-0000-7000-8000-000000000402"
	result, err := tool.Execute(
		context.Background(), principal, prepared, correlationID,
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("ordinary organization service calls = %d, want 1", creator.calls)
	}
	wantCommand := organizations.CreateClientCommand{
		Principal: principal,
		Actor: organizations.Actor{
			Type: "technician", ID: principal.ID, Source: "ai_workspace",
		},
		ClientID: generatedID, CorrelationID: correlationID,
		DisplayID: "CLIENT-401", Name: "Alpha Managed Services",
	}
	if !reflect.DeepEqual(creator.command, wantCommand) {
		t.Fatalf("CreateClient command = %+v, want %+v", creator.command, wantCommand)
	}
	if result.Data["client_id"] != generatedID ||
		result.Data["display_id"] != "CLIENT-401" ||
		result.Data["name"] != "Alpha Managed Services" {
		t.Fatalf("result = %+v", result)
	}
	if generated != 1 {
		t.Fatalf("generated IDs after preview and execute = %d, want 1", generated)
	}
	wantChecks := []clientIdentityCheck{
		{
			principal: principal,
			name:      "Alpha Managed Services",
			displayID: "CLIENT-401",
		},
		{
			principal: principal,
			name:      "Alpha Managed Services",
			displayID: "CLIENT-401",
		},
	}
	if !reflect.DeepEqual(conflicts.checks, wantChecks) {
		t.Fatalf("identity conflict checks = %+v, want %+v", conflicts.checks, wantChecks)
	}
}

func TestClientCreateToolRejectsUntrustedOrIncompletePublicInput(t *testing.T) {
	conflicts := &clientIdentityConflictStub{}
	generated := 0
	tool := NewClientCreateTool(conflicts, &clientCreatorStub{}, func() string {
		generated++
		return "019fb3c2-0000-7000-8000-000000000403"
	})
	preparer := tool.(aiassist.ToolInputPreparer)
	principal := testPrincipal("client.create", "organization.read")

	for _, raw := range []string{
		`{"display_id":"CLIENT-403","name":"Alpha","client_id":"caller-supplied"}`,
		`{"display_id":"CLIENT-403","name":"Alpha","actor_id":"caller-supplied"}`,
		`{"display_id":"","name":"Alpha"}`,
		`{"display_id":"CLIENT-403","name":""}`,
		`{"display_id":"   ","name":"Alpha"}`,
		`{"display_id":"CLIENT-403","name":"   "}`,
	} {
		if _, err := preparer.Prepare(
			context.Background(), principal, json.RawMessage(raw),
		); !errors.Is(err, aiassist.ErrInvalidTool) {
			t.Fatalf("Prepare(%s) error = %v, want ErrInvalidTool", raw, err)
		}
	}
	if generated != 0 || len(conflicts.checks) != 0 {
		t.Fatalf(
			"invalid public input generated %d IDs and made %d conflict checks",
			generated, len(conflicts.checks),
		)
	}
}

func TestClientCreateToolRejectsClientScopedCaller(t *testing.T) {
	conflicts := &clientIdentityConflictStub{}
	generated := 0
	tool := NewClientCreateTool(conflicts, &clientCreatorStub{}, func() string {
		generated++
		return "019fb3c2-0000-7000-8000-000000000404"
	})
	preparer := tool.(aiassist.ToolInputPreparer)
	principal := testPrincipal("client.create", "organization.read")
	principal.Scope.ClientID = "existing-client"

	_, err := preparer.Prepare(
		context.Background(), principal,
		json.RawMessage(`{"display_id":"CLIENT-404","name":"Alpha"}`),
	)
	if !errors.Is(err, aiassist.ErrInvalidTool) {
		t.Fatalf("Prepare() error = %v, want ErrInvalidTool", err)
	}
	if generated != 0 || len(conflicts.checks) != 0 {
		t.Fatalf("client-scoped caller generated %d IDs and made %d conflict checks", generated, len(conflicts.checks))
	}
}

func TestClientCreateToolRejectsIdentityConflictAcrossAllLifecycleStates(t *testing.T) {
	for _, lifecycleState := range []string{"inactive", "archived", "deleted"} {
		t.Run(lifecycleState, func(t *testing.T) {
			conflicts := &clientIdentityConflictStub{conflict: true}
			generated := 0
			tool := NewClientCreateTool(conflicts, &clientCreatorStub{}, func() string {
				generated++
				return "019fb3c2-0000-7000-8000-000000000405"
			})
			preparer := tool.(aiassist.ToolInputPreparer)

			_, err := preparer.Prepare(
				context.Background(),
				testPrincipal("client.create", "organization.read"),
				json.RawMessage(`{
					"display_id":" bravo-200 ",
					"name":"  alpha   managed\tservices "
				}`),
			)
			if !errors.Is(err, aiassist.ErrInvalidTool) {
				t.Fatalf("Prepare() error = %v, want ErrInvalidTool", err)
			}
			if generated != 0 {
				t.Fatalf("duplicate input generated %d IDs, want 0", generated)
			}
			wantChecks := []clientIdentityCheck{{
				principal: testPrincipal("client.create", "organization.read"),
				name:      "alpha   managed\tservices",
				displayID: "bravo-200",
			}}
			if !reflect.DeepEqual(conflicts.checks, wantChecks) {
				t.Fatalf("identity conflict checks = %+v, want %+v", conflicts.checks, wantChecks)
			}
		})
	}
}

func TestClientCreateToolRechecksDuplicatesDuringPreview(t *testing.T) {
	conflicts := &clientIdentityConflictStub{}
	creator := &clientCreatorStub{}
	tool := NewClientCreateTool(conflicts, creator, func() string {
		return "019fb3c2-0000-7000-8000-000000000406"
	})
	preparer := tool.(aiassist.ToolInputPreparer)
	principal := testPrincipal("client.create", "organization.read")
	prepared, err := preparer.Prepare(
		context.Background(), principal,
		json.RawMessage(`{"display_id":"CLIENT-406","name":"Future Client"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Preview(context.Background(), principal, prepared); err != nil {
		t.Fatalf("initial Preview() error = %v", err)
	}
	conflicts.conflict = true

	_, err = tool.Preview(context.Background(), principal, prepared)
	if !errors.Is(err, aiassist.ErrInvalidTool) {
		t.Fatalf("rechecked Preview() error = %v, want ErrInvalidTool", err)
	}
	if creator.calls != 0 {
		t.Fatal("stale duplicate reached ordinary organization service")
	}
	if len(conflicts.checks) != 3 {
		t.Fatalf("conflict checks = %d, want prepare plus two previews", len(conflicts.checks))
	}
}

func TestRejectedClientCreateProposalNeverCallsOrganizationWriter(t *testing.T) {
	now := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	conflicts := &clientIdentityConflictStub{}
	creator := &clientCreatorStub{}
	tool := NewClientCreateTool(
		conflicts,
		creator,
		func() string { return "019fb3c2-0000-7000-8000-000000000408" },
	)
	principal := testPrincipal("client.create", "organization.read")
	store := &composedProposalStore{}
	registryIDs := []string{"proposal-client", "correlation-client"}
	registry, err := aiassist.NewRegistry(
		[]aiassist.Tool{tool},
		store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) {
			return principal, nil
		},
		func() time.Time { return now },
		func() string {
			value := registryIDs[0]
			registryIDs = registryIDs[1:]
			return value
		},
		&composedTargetAuthorizer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(
		context.Background(),
		principal,
		aiassist.ToolRequest{
			Name: "client.create", ConversationID: "conversation-1",
			Input: json.RawMessage(
				`{"display_id":"CLIENT-408","name":"Rejected Client"}`,
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := registry.Reject(
		context.Background(), principal, proposal.ID, proposal.Version,
	); err != nil {
		t.Fatal(err)
	}
	if creator.calls != 0 || store.proposal.State != aiassist.ProposalRejected {
		t.Fatalf(
			"organization writer calls=%d proposal state=%q",
			creator.calls, store.proposal.State,
		)
	}
}

var _ aiassist.Tool = NewClientCreateTool(
	&clientIdentityConflictStub{},
	&clientCreatorStub{},
	func() string { return "019fb3c2-0000-7000-8000-000000000407" },
)
