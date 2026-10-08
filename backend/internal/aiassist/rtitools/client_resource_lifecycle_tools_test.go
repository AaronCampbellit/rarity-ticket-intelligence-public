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
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type resourceLifecycleWriterStub struct {
	update          clientresources.UpdateCommand
	lifecycle       clientresources.LifecycleCommand
	preflights      []clientresources.LifecyclePreflight
	preflightErrors []error
	preflightCalls  []clientresources.LifecyclePreflightCommand
	preflight       clientresources.LifecyclePreflight
	result          clientresources.ResourceDetail
	err             error
	updateCalls     int
	deactivateCalls int
	reactivateCalls int
}

func (s *resourceLifecycleWriterStub) Preflight(_ context.Context, command clientresources.LifecyclePreflightCommand) (clientresources.LifecyclePreflight, error) {
	index := len(s.preflightCalls)
	s.preflightCalls = append(s.preflightCalls, command)
	if index < len(s.preflightErrors) && s.preflightErrors[index] != nil {
		return clientresources.LifecyclePreflight{}, s.preflightErrors[index]
	}
	if index < len(s.preflights) {
		return s.preflights[index], nil
	}
	return s.preflight, nil
}

func (s *resourceLifecycleWriterStub) Update(_ context.Context, command clientresources.UpdateCommand) (clientresources.ResourceDetail, error) {
	s.updateCalls++
	s.update = command
	return s.result, s.err
}
func (s *resourceLifecycleWriterStub) Deactivate(_ context.Context, command clientresources.LifecycleCommand) (clientresources.ResourceDetail, error) {
	s.deactivateCalls++
	s.lifecycle = command
	return s.result, s.err
}
func (s *resourceLifecycleWriterStub) Reactivate(_ context.Context, command clientresources.LifecycleCommand) (clientresources.ResourceDetail, error) {
	s.reactivateCalls++
	s.lifecycle = command
	return s.result, s.err
}

func lifecyclePrincipal(capability string) authorization.Principal {
	return authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet(capability),
	}
}

func lifecycleContact() clientresources.ResourceDetail {
	return clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "contact-1", Kind: "contact", DisplayID: "CON-1", Name: "Ada Lovelace", LocationID: "location-1", Version: 7, LifecycleState: "active"}, Email: "old@example.com", Phone: "555-0100",
	}
}

func lifecycleCatalog(resource clientresources.ResourceDetail) *resourceCatalogStub {
	location := clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Head Office", Version: 2, LifecycleState: "active"},
	}
	byID := map[string]clientresources.ResourceDetail{"location-1": location}
	byID[resource.ID] = resource
	return &resourceCatalogStub{resolved: resource, current: resource, byID: byID}
}

func TestClientResourceUpdateToolPreparesTrustedIdentityWithAdvertisedCapability(t *testing.T) {
	directory := activeResourceDirectory()
	catalog := lifecycleCatalog(lifecycleContact())
	writer := &resourceLifecycleWriterStub{
		result: lifecycleContact(),
		preflight: clientresources.LifecyclePreflight{
			RelationshipStatus: "active",
		},
	}
	tool := NewContactUpdateTool(directory, catalog, writer)
	principal := lifecyclePrincipal("contact.update")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","resource":"CON-1","patch":{"email":"","location":""},"reason":"Remove stale contact details"}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(prepared, &values); err != nil {
		t.Fatal(err)
	}
	if values["client_id"] != "client-1" || values["resource_id"] != "contact-1" || values["current_version"] != float64(7) ||
		values["actor_id"] != nil || values["source"] != nil || values["correlation_id"] != nil || values["expected_version"] != nil {
		t.Fatalf("prepared=%+v", values)
	}
	if len(directory.resolveCalls) != 1 || directory.resolveCalls[0].capability != "contact.update" ||
		catalog.trustedResolveCalls != 1 || catalog.trustedResolve.Capability != "contact.update" || catalog.trustedResolve.IncludeInactive {
		t.Fatalf("directory=%+v catalog=%+v", directory.resolveCalls, catalog.trustedResolve)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil {
		t.Fatalf("Preview() error=%v", err)
	}
	if preview.TargetID != "contact-1" || preview.TargetVersion != 7 ||
		preview.Changes["email"] != (aiassist.Change{Before: "old@example.com", After: ""}) ||
		preview.Changes["location"] != (aiassist.Change{Before: "LOC-1", After: nil}) ||
		preview.Changes["relationship_status"].After != "active" ||
		preview.Changes["reason"].After != "Remove stale contact details" ||
		!reflect.DeepEqual(preview.Changes["client"].After, map[string]string{"display_id": "NW-100", "name": "Northwind Legal"}) ||
		!reflect.DeepEqual(preview.Changes["resource"].After, map[string]string{"display_id": "CON-1", "name": "Ada Lovelace"}) {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := tool.Execute(context.Background(), principal, prepared, correlationID()); err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if writer.updateCalls != 1 || writer.update.Kind != clientresources.ContactKind || writer.update.ResourceID != "contact-1" ||
		writer.update.ExpectedVersion != 7 || writer.update.Source != "ai_workspace" || writer.update.ActorID != principal.ID ||
		writer.update.CorrelationID != correlationID() || writer.update.Patch.Email == nil || *writer.update.Patch.Email != "" ||
		writer.update.Patch.LocationID == nil || *writer.update.Patch.LocationID != "" {
		t.Fatalf("command=%+v patch=%+v", writer.update, writer.update.Patch)
	}
}

func TestClientResourceUpdatePreviewCarriesCanonicalLocationIdentityWithUpdateCapabilityAlone(t *testing.T) {
	resource := lifecycleContact()
	resource.LocationID = ""
	catalog := lifecycleCatalog(resource)
	tool := NewContactUpdateTool(activeResourceDirectory(), catalog, &resourceLifecycleWriterStub{
		result: resource,
		preflight: clientresources.LifecyclePreflight{
			RelationshipStatus: "active",
		},
	})
	principal := lifecyclePrincipal("contact.update")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","resource":"CON-1","patch":{"location":"LOC-1"},"reason":"Assign primary office"}`,
	))
	if err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil {
		t.Fatalf("Preview() error=%v", err)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("Marshal() error=%v", err)
	}
	var body struct {
		LocationTarget map[string]any `json:"location_target"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("Unmarshal() error=%v", err)
	}
	want := map[string]any{
		"id":         "location-1",
		"client_id":  "client-1",
		"kind":       "location",
		"display_id": "LOC-1",
		"name":       "Head Office",
	}
	if !reflect.DeepEqual(body.LocationTarget, want) {
		t.Fatalf("location_target=%#v, want %#v; preview JSON=%s", body.LocationTarget, want, encoded)
	}
	if preview.Changes["location"] != (aiassist.Change{Before: nil, After: "LOC-1"}) {
		t.Fatalf("location change=%+v", preview.Changes["location"])
	}
}

func TestClientResourceUpdateToolsRejectUntrustedOrInvalidPublicInput(t *testing.T) {
	tool := NewLocationUpdateTool(activeResourceDirectory(), lifecycleCatalog(clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Office", Version: 3, LifecycleState: "active"},
	}), &resourceLifecycleWriterStub{})
	principal := lifecyclePrincipal("location.update")
	for _, raw := range []string{
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{"name":"Branch"}}`,
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{},"reason":"Rename"}`,
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{"email":"x@example.com"},"reason":"Wrong field"}`,
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{"name":"Branch"},"reason":"Rename","resource_id":"caller-id"}`,
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{"name":"Branch"},"reason":"Rename","expected_version":3}`,
	} {
		if _, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(raw)); !errors.Is(err, aiassist.ErrInvalidTool) {
			t.Fatalf("Prepare(%s) error=%v, want ErrInvalidTool", raw, err)
		}
	}
}

func TestClientResourceUpdateToolsRejectMalformedBusinessValuesBeforeProposal(t *testing.T) {
	for _, test := range []struct {
		name, capability, raw string
		tool                  aiassist.Tool
	}{
		{name: "email", capability: "contact.update", raw: `{"client":"Northwind Legal","resource":"CON-1","patch":{"email":"not-an-email"},"reason":"Correct"}`, tool: NewContactUpdateTool(activeResourceDirectory(), lifecycleCatalog(lifecycleContact()), &resourceLifecycleWriterStub{})},
		{name: "phone", capability: "contact.update", raw: `{"client":"Northwind Legal","resource":"CON-1","patch":{"phone":"abc"},"reason":"Correct"}`, tool: NewContactUpdateTool(activeResourceDirectory(), lifecycleCatalog(lifecycleContact()), &resourceLifecycleWriterStub{})},
		{name: "criticality", capability: "service.update", raw: `{"client":"Northwind Legal","resource":"SVC-1","patch":{"criticality":"urgent"},"reason":"Correct"}`, tool: NewServiceUpdateTool(activeResourceDirectory(), lifecycleCatalog(clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "service-1", Kind: "service", DisplayID: "SVC-1", Name: "Service", Version: 2, LifecycleState: "active"}}), &resourceLifecycleWriterStub{})},
		{name: "contract dates", capability: "contract.update", raw: `{"client":"Northwind Legal","resource":"CTR-1","patch":{"starts_on":"2026-09-01","ends_on":"2026-08-01"},"reason":"Correct"}`, tool: NewContractUpdateTool(activeResourceDirectory(), lifecycleCatalog(clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "contract-1", Kind: "contract", DisplayID: "CTR-1", Name: "Agreement", Version: 2, LifecycleState: "active"}}), &resourceLifecycleWriterStub{})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), lifecyclePrincipal(test.capability), json.RawMessage(test.raw)); !errors.Is(err, aiassist.ErrInvalidTool) {
				t.Fatalf("Prepare() error=%v, want ErrInvalidTool", err)
			}
		})
	}
}

func TestClientResourceUpdateToolRejectsLiveNoOpPreview(t *testing.T) {
	resource := clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Head Office", Version: 3, LifecycleState: "active"},
	}
	tool := NewLocationUpdateTool(activeResourceDirectory(), lifecycleCatalog(resource), &resourceLifecycleWriterStub{})
	principal := lifecyclePrincipal("location.update")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","resource":"LOC-1","patch":{"name":"Head Office"},"reason":"No change"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Preview(context.Background(), principal, prepared); !errors.Is(err, aiassist.ErrInvalidTool) {
		t.Fatalf("Preview() error=%v, want no-op ErrInvalidTool", err)
	}
}

func TestClientResourceLifecycleToolsPreviewExactStateAndRejectAuthority(t *testing.T) {
	location := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Branch", Version: 4, LifecycleState: "active"}}
	tool := NewLocationDeactivateTool(activeResourceDirectory(), lifecycleCatalog(location), &resourceLifecycleWriterStub{
		preflight: clientresources.LifecyclePreflight{DependencyStatus: "clear"},
	})
	principal := lifecyclePrincipal("location.lifecycle")
	prepared, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(`{"client":"Northwind Legal","resource":"LOC-1","reason":"Office closed"}`))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := tool.Preview(context.Background(), principal, prepared)
	if err != nil || preview.TargetVersion != 4 ||
		preview.Changes["lifecycle_state"] != (aiassist.Change{Before: "active", After: "inactive"}) ||
		preview.Changes["dependency_status"].After != "clear" ||
		preview.Changes["reason"].After != "Office closed" {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}

	discovered := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "asset-1", Kind: "asset", DisplayID: "AST-1", Name: "RMM Asset", Version: 2, LifecycleState: "active", Authority: clientresources.Discovered}}
	asset := NewAssetDeactivateTool(activeResourceDirectory(), lifecycleCatalog(discovered), &resourceLifecycleWriterStub{})
	if _, err := asset.(aiassist.ToolInputPreparer).Prepare(context.Background(), lifecyclePrincipal("asset.lifecycle"), json.RawMessage(`{"client":"Northwind Legal","resource":"AST-1","reason":"Retire"}`)); !errors.Is(err, clientresources.ErrResourceAuthorityConflict) {
		t.Fatalf("Prepare(discovered asset) error=%v, want authority conflict", err)
	}
}

func TestClientResourceLifecycleRegistryPreservesTypedResolutionAndVersionErrors(t *testing.T) {
	principal := lifecyclePrincipal("contact.update")
	raw := json.RawMessage(`{"client":"Northwind Legal","resource":"CON-1","patch":{"email":"new@example.com"},"reason":"Correct address"}`)
	for _, test := range []struct {
		name string
		err  error
		set  func(*resourceDirectoryStub, *resourceCatalogStub, *resourceLifecycleWriterStub, error)
	}{
		{
			name: "ambiguous resource", err: clientresources.ErrAmbiguousResource,
			set: func(_ *resourceDirectoryStub, catalog *resourceCatalogStub, _ *resourceLifecycleWriterStub, err error) {
				catalog.err = err
			},
		},
		{
			name: "missing resource", err: scope.ErrNotFound,
			set: func(_ *resourceDirectoryStub, catalog *resourceCatalogStub, _ *resourceLifecycleWriterStub, err error) {
				catalog.err = err
			},
		},
		{
			name: "ambiguous Client", err: organizations.ErrClientReferenceAmbiguous,
			set: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, _ *resourceLifecycleWriterStub, err error) {
				directory.err = err
			},
		},
		{
			name: "version conflict", err: object.ErrVersionConflict,
			set: func(_ *resourceDirectoryStub, _ *resourceCatalogStub, writer *resourceLifecycleWriterStub, err error) {
				writer.preflightErrors = []error{err}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory, catalog, writer := activeResourceDirectory(), lifecycleCatalog(lifecycleContact()), &resourceLifecycleWriterStub{}
			test.set(directory, catalog, writer, test.err)
			registry, err := aiassist.NewRegistry(
				[]aiassist.Tool{NewContactUpdateTool(directory, catalog, writer)},
				&composedProposalStore{},
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				time.Now, func() string { return "id" }, &composedTargetAuthorizer{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
				Name: "contact.update", ConversationID: "conversation-1", Input: raw,
			}); !errors.Is(err, test.err) {
				t.Fatalf("Propose() error=%v, want %v", err, test.err)
			}
		})
	}
}

func TestClientResourceLifecycleRegistryRejectsStateAndVersionDriftWithoutWrite(t *testing.T) {
	now := time.Date(2026, time.August, 5, 21, 0, 0, 0, time.UTC)
	resource := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "service-1", Kind: "service", DisplayID: "SVC-1", Name: "Managed Service", Version: 5, LifecycleState: "active"}, Criticality: "high"}
	catalog := lifecycleCatalog(resource)
	writer := &resourceLifecycleWriterStub{result: resource}
	tool := NewServiceDeactivateTool(activeResourceDirectory(), catalog, writer)
	principal := lifecyclePrincipal("service.lifecycle")
	store := &composedProposalStore{}
	ids := []string{"proposal-lifecycle", correlationID()}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "service.deactivate", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"Northwind Legal","resource":"SVC-1","reason":"Service ended"}`)})
	if err != nil {
		t.Fatal(err)
	}
	catalog.current.Version = 6
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm(version drift) error=%v, want ErrProposalStale", err)
	}
	if writer.deactivateCalls != 0 {
		t.Fatalf("writes=%d after stale confirmation", writer.deactivateCalls)
	}
	catalog.current.Version = 5
	catalog.current.LifecycleState = "inactive"
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err == nil {
		t.Fatal("Confirm(state drift) unexpectedly succeeded")
	}
	if writer.deactivateCalls != 0 {
		t.Fatalf("writes=%d after state drift", writer.deactivateCalls)
	}
}

func TestClientResourceLifecyclePreflightBlocksDependencyAndRelationshipDriftBeforeWriter(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		capability string
		resource   clientresources.ResourceDetail
		input      string
		blocker    error
	}{
		{
			name: "location dependency appears", toolName: "location.deactivate", capability: "location.lifecycle",
			resource: clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Branch", Version: 4, LifecycleState: "active"}},
			input:    `{"client":"Northwind Legal","resource":"LOC-1","reason":"Office closed"}`,
			blocker:  clientresources.ErrResourceInUse,
		},
		{
			name: "contact location becomes inactive", toolName: "contact.update", capability: "contact.update",
			resource: lifecycleContact(),
			input:    `{"client":"Northwind Legal","resource":"CON-1","patch":{"email":"new@example.com"},"reason":"Correct email"}`,
			blocker:  scope.ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clear := clientresources.LifecyclePreflight{
				Resource: test.resource, DependencyStatus: "clear", RelationshipStatus: "active",
			}
			writer := &resourceLifecycleWriterStub{
				preflights:      []clientresources.LifecyclePreflight{clear, clear},
				preflightErrors: []error{nil, nil, test.blocker},
				result:          test.resource,
			}
			var tool aiassist.Tool
			if test.toolName == "location.deactivate" {
				tool = NewLocationDeactivateTool(activeResourceDirectory(), lifecycleCatalog(test.resource), writer)
			} else {
				tool = NewContactUpdateTool(activeResourceDirectory(), lifecycleCatalog(test.resource), writer)
			}
			principal := lifecyclePrincipal(test.capability)
			store := &composedProposalStore{}
			ids := []string{"proposal-preflight", correlationID()}
			registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				time.Now, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
				Name: test.toolName, ConversationID: "conversation-1", Input: json.RawMessage(test.input),
			})
			if err != nil {
				t.Fatalf("Propose() error=%v", err)
			}
			if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, test.blocker) {
				t.Fatalf("Confirm() error=%v, want %v", err, test.blocker)
			}
			if len(writer.preflightCalls) != 3 || writer.updateCalls != 0 || writer.deactivateCalls != 0 || writer.reactivateCalls != 0 {
				t.Fatalf("preflights=%d writes=(%d,%d,%d)", len(writer.preflightCalls), writer.updateCalls, writer.deactivateCalls, writer.reactivateCalls)
			}
		})
	}
}

func TestClientResourceLifecyclePreflightBlocksProposalPreparation(t *testing.T) {
	resource := clientresources.ResourceDetail{
		Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Branch", Version: 4, LifecycleState: "active"},
	}
	writer := &resourceLifecycleWriterStub{preflightErrors: []error{clientresources.ErrResourceInUse}}
	tool := NewLocationDeactivateTool(activeResourceDirectory(), lifecycleCatalog(resource), writer)
	_, err := tool.(aiassist.ToolInputPreparer).Prepare(
		context.Background(),
		lifecyclePrincipal("location.lifecycle"),
		json.RawMessage(`{"client":"Northwind Legal","resource":"LOC-1","reason":"Office closed"}`),
	)
	if !errors.Is(err, clientresources.ErrResourceInUse) || len(writer.preflightCalls) != 1 ||
		writer.deactivateCalls != 0 {
		t.Fatalf("Prepare() error=%v preflights=%d writes=%d", err, len(writer.preflightCalls), writer.deactivateCalls)
	}
}

func TestClientResourceLifecycleRegistryRepreviewsAndCarriesProposalCorrelation(t *testing.T) {
	now := time.Date(2026, time.August, 5, 21, 30, 0, 0, time.UTC)
	resource := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "service-1", Kind: "service", DisplayID: "SVC-1", Name: "Managed Service", Version: 5, LifecycleState: "active"}, Criticality: "high"}
	catalog := lifecycleCatalog(resource)
	writer := &resourceLifecycleWriterStub{result: clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "service-1", Kind: "service", DisplayID: "SVC-1", Name: "Managed Service", Version: 6, LifecycleState: "inactive"}}}
	tool := NewServiceDeactivateTool(activeResourceDirectory(), catalog, writer)
	principal := lifecyclePrincipal("service.lifecycle")
	store := &composedProposalStore{}
	ids := []string{"proposal-correlation", correlationID()}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "service.deactivate", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"Northwind Legal","resource":"SVC-1","reason":"Service ended"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatal(err)
	}
	if writer.deactivateCalls != 1 || writer.lifecycle.CorrelationID != correlationID() || writer.lifecycle.Source != "ai_workspace" {
		t.Fatalf("writes=%d command=%+v", writer.deactivateCalls, writer.lifecycle)
	}
}

func TestClientResourceLifecycleRejectionNeverWrites(t *testing.T) {
	resource := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "location-1", Kind: "location", DisplayID: "LOC-1", Name: "Branch", Version: 4, LifecycleState: "active"}}
	writer := &resourceLifecycleWriterStub{}
	tool := NewLocationDeactivateTool(activeResourceDirectory(), lifecycleCatalog(resource), writer)
	principal := lifecyclePrincipal("location.lifecycle")
	store := &composedProposalStore{}
	ids := []string{"proposal-reject", correlationID()}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil }, time.Now,
		func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "location.deactivate", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"Northwind Legal","resource":"LOC-1","reason":"Closed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Reject(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatal(err)
	}
	if writer.deactivateCalls != 0 || writer.updateCalls != 0 || writer.reactivateCalls != 0 {
		t.Fatalf("rejected proposal wrote: %+v", writer)
	}
}
