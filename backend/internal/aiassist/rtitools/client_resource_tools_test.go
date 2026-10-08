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

type resourceDirectoryStub struct {
	directory    organizations.Directory
	err          error
	commands     []organizations.ListDirectoryCommand
	resolved     organizations.Client
	resolveCalls []resourceClientResolution
}

type resourceClientResolution struct {
	capability string
	reference  string
}

func (s *resourceDirectoryStub) List(
	_ context.Context,
	command organizations.ListDirectoryCommand,
) (organizations.Directory, error) {
	s.commands = append(s.commands, command)
	return s.directory, s.err
}

func (s *resourceDirectoryStub) ResolveActiveClient(
	_ context.Context,
	_ authorization.Principal,
	capability string,
	reference string,
) (organizations.Client, error) {
	s.resolveCalls = append(s.resolveCalls, resourceClientResolution{capability: capability, reference: reference})
	if s.err != nil {
		return organizations.Client{}, s.err
	}
	if s.resolved.ID != "" {
		return s.resolved, nil
	}
	return organizations.ResolveClientReference(reference, s.directory.Clients)
}

type resourceCatalogStub struct {
	items               []clientresources.ResourceDetail
	resolved            clientresources.ResourceDetail
	current             clientresources.ResourceDetail
	byID                map[string]clientresources.ResourceDetail
	queryCommand        clientresources.QueryCommand
	resolve             clientresources.ResolveCommand
	get                 clientresources.GetCommand
	trustedResolve      clientresources.TrustedResolveCommand
	trustedGet          clientresources.TrustedGetCommand
	queryCalls          int
	resolveCalls        int
	getCalls            int
	trustedResolveCalls int
	trustedGetCalls     int
	err                 error
}

func (s *resourceCatalogStub) ResolveTrusted(
	_ context.Context,
	command clientresources.TrustedResolveCommand,
) (clientresources.ResourceDetail, error) {
	s.trustedResolveCalls++
	s.trustedResolve = command
	if command.Kind == clientresources.LocationKind {
		if item, ok := s.byID["location-1"]; ok {
			return item, s.err
		}
	}
	return s.resolved, s.err
}

func (s *resourceCatalogStub) GetTrusted(
	_ context.Context,
	command clientresources.TrustedGetCommand,
) (clientresources.ResourceDetail, error) {
	s.trustedGetCalls++
	s.trustedGet = command
	if command.ID == s.current.ID {
		return s.current, s.err
	}
	if item, ok := s.byID[command.ID]; ok {
		return item, s.err
	}
	return s.current, s.err
}

func (s *resourceCatalogStub) Query(
	_ context.Context,
	command clientresources.QueryCommand,
) ([]clientresources.ResourceDetail, error) {
	s.queryCalls++
	s.queryCommand = command
	return s.items, s.err
}

func (s *resourceCatalogStub) Resolve(
	_ context.Context,
	command clientresources.ResolveCommand,
) (clientresources.ResourceDetail, error) {
	s.resolveCalls++
	s.resolve = command
	return s.resolved, s.err
}

func (s *resourceCatalogStub) Get(
	_ context.Context,
	command clientresources.GetCommand,
) (clientresources.ResourceDetail, error) {
	s.getCalls++
	s.get = command
	return s.current, s.err
}

type resourceWriterStub struct {
	location                                                             clientresources.CreateLocationCommand
	contact                                                              clientresources.CreateContactCommand
	asset                                                                clientresources.CreateAssetCommand
	service                                                              clientresources.CreateServiceCommand
	contract                                                             clientresources.CreateContractCommand
	locationCalls, contactCalls, assetCalls, serviceCalls, contractCalls int
}

type serializedPreviewProposalStore struct {
	composedProposalStore
}

func (s *serializedPreviewProposalStore) GetProposal(
	ctx context.Context,
	target scope.Target,
	id string,
	principalID string,
) (aiassist.ActionProposal, error) {
	proposal, err := s.composedProposalStore.GetProposal(ctx, target, id, principalID)
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

func (s *resourceWriterStub) CreateLocation(_ context.Context, command clientresources.CreateLocationCommand) (clientresources.Location, error) {
	s.locationCalls++
	s.location = command
	return clientresources.Location{Envelope: resourceEnvelope(command.Prepared.ResourceID, command.Target.ClientID, command.DisplayID), Name: command.Name}, nil
}

func (s *resourceWriterStub) CreateContact(_ context.Context, command clientresources.CreateContactCommand) (clientresources.Contact, error) {
	s.contactCalls++
	s.contact = command
	return clientresources.Contact{Envelope: resourceEnvelope(command.Prepared.ResourceID, command.Target.ClientID, command.DisplayID), LocationID: command.Location.ID, DisplayName: command.DisplayName, Email: command.Email, Phone: command.Phone}, nil
}

func (s *resourceWriterStub) CreateAsset(_ context.Context, command clientresources.CreateAssetCommand) (clientresources.Asset, error) {
	s.assetCalls++
	s.asset = command
	return clientresources.Asset{Envelope: resourceEnvelope(command.Prepared.ResourceID, command.Target.ClientID, command.DisplayID), LocationID: command.Location.ID, Name: command.Name, AssetType: command.AssetType, Provenance: clientresources.Provenance{SourceSystem: command.SourceSystem, ExternalID: command.ExternalID, Authority: command.Authority}}, nil
}

func (s *resourceWriterStub) CreateService(_ context.Context, command clientresources.CreateServiceCommand) (clientresources.ServiceRecord, error) {
	s.serviceCalls++
	s.service = command
	return clientresources.ServiceRecord{Envelope: resourceEnvelope(command.Prepared.ResourceID, command.Target.ClientID, command.DisplayID), Name: command.Name, Criticality: command.Criticality}, nil
}

func (s *resourceWriterStub) CreateContract(_ context.Context, command clientresources.CreateContractCommand) (clientresources.Contract, error) {
	s.contractCalls++
	s.contract = command
	return clientresources.Contract{Envelope: resourceEnvelope(command.Prepared.ResourceID, command.Target.ClientID, command.DisplayID), Name: command.Name, StartsOn: command.StartsOn, EndsOn: command.EndsOn}, nil
}

func resourceEnvelope(id, clientID, displayID string) object.Envelope {
	return object.Envelope{ID: id, ObjectType: "resource", MSPID: "msp-1", ClientID: clientID, DisplayID: displayID, LifecycleState: "active", Version: 1}
}

func resourcePrincipal(capability string) authorization.Principal {
	return authorization.Principal{ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet(capability, "search.read")}
}

func activeResourceDirectory() *resourceDirectoryStub {
	client := organizations.Client{
		Envelope: object.Envelope{ID: "client-1", MSPID: "msp-1", ClientID: "client-1", DisplayID: "NW-100", LifecycleState: "active", Version: 1},
		Name:     "Northwind Legal",
	}
	return &resourceDirectoryStub{resolved: client, directory: organizations.Directory{Clients: []organizations.Client{client}}}
}

func activeLocationCatalog() *resourceCatalogStub {
	location := clientresources.ResourceDetail{Summary: clientresources.Summary{ID: "location-1", Kind: string(clientresources.LocationKind), DisplayID: "LOC-1", Name: "Head Office", LifecycleState: "active"}}
	return &resourceCatalogStub{resolved: location, current: location}
}

func preparedResourceTool(t *testing.T, tool aiassist.Tool, principal authorization.Principal, raw string) json.RawMessage {
	t.Helper()
	preparer, ok := tool.(aiassist.ToolInputPreparer)
	if !ok {
		t.Fatal("resource create tool must prepare trusted identities")
	}
	prepared, err := preparer.Prepare(context.Background(), principal, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := tool.Validate(prepared); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	return prepared
}

func TestClientResourceListToolResolvesActiveClientServerSide(t *testing.T) {
	directory := activeResourceDirectory()
	catalog := activeLocationCatalog()
	catalog.items = []clientresources.ResourceDetail{{Summary: clientresources.Summary{ID: "asset-1", Kind: "asset", DisplayID: "AST-1", Name: "Firewall", LifecycleState: "active", Authority: clientresources.Discovered}}}
	tool := NewClientResourceListTool(directory, catalog)
	principal := resourcePrincipal("search.read")

	preparer := tool.(aiassist.ToolInputPreparer)
	prepared, err := preparer.Prepare(context.Background(), principal, json.RawMessage(`{"client":"Northwind Legal","kind":"asset","query":"firewall","limit":25}`))
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if strings := string(prepared); strings == `{"client":"Northwind Legal"}` || containsAny(strings, "client_id") == false {
		t.Fatalf("prepared input did not replace public Client reference: %s", strings)
	}
	result, err := tool.Execute(context.Background(), principal, prepared, "ignored")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(directory.resolveCalls) != 1 || directory.resolveCalls[0] != (resourceClientResolution{capability: "search.read", reference: "Northwind Legal"}) {
		t.Fatalf("active Client resolution=%+v", directory.resolveCalls)
	}
	if catalog.queryCalls != 1 || catalog.queryCommand.Target != (scope.Target{MSPID: "msp-1", ClientID: "client-1"}) ||
		catalog.queryCommand.Query != (clientresources.Query{Kind: clientresources.AssetKind, Literal: "firewall", Limit: 25}) {
		t.Fatalf("catalog query command = %+v calls=%d", catalog.queryCommand, catalog.queryCalls)
	}
	wantItems := []clientresources.Summary{{
		ID: "asset-1", Kind: "asset", DisplayID: "AST-1", Name: "Firewall",
		LifecycleState: "active", Authority: clientresources.Discovered,
	}}
	if !reflect.DeepEqual(result.Data["resources"], wantItems) {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientResourceListToolRejectsUnknownOrInvalidClosedQueryInput(t *testing.T) {
	tool := NewClientResourceListTool(activeResourceDirectory(), activeLocationCatalog())
	preparer := tool.(aiassist.ToolInputPreparer)
	principal := resourcePrincipal("search.read")
	for _, raw := range []string{
		`{"client":"Northwind Legal","kind":"asset","unexpected":true}`,
		`{"client":"Northwind Legal","kind":"other"}`,
		`{"client":"Northwind Legal","kind":"asset","limit":51}`,
		`{"client":"Northwind Legal","kind":"asset","query":" "}`,
	} {
		if _, err := preparer.Prepare(context.Background(), principal, json.RawMessage(raw)); !errors.Is(err, aiassist.ErrInvalidTool) {
			t.Fatalf("Prepare(%s) error=%v, want ErrInvalidTool", raw, err)
		}
	}
}

func TestClientResourceCreateToolsAcceptOnlyCompleteOptionalBusinessValues(t *testing.T) {
	directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
	asset := NewAssetCreateTool(directory, catalog, writer, resourceID)
	prepared := preparedResourceTool(t, asset, resourcePrincipal("asset.create"), `{"client":"Northwind Legal","display_id":"AST-2","name":"Router","asset_type":"router","source_system":"rmm","external_id":"device-2"}`)
	preview, err := asset.Preview(context.Background(), resourcePrincipal("asset.create"), prepared)
	if err != nil || preview.Changes["source_system"].After != "rmm" || preview.Changes["external_id"].After != "device-2" {
		t.Fatalf("asset preview=%+v error=%v", preview, err)
	}

	for _, criticality := range []string{"low", "normal", "high", "critical"} {
		service := NewServiceCreateTool(activeResourceDirectory(), &resourceWriterStub{}, resourceID)
		if _, err := service.(aiassist.ToolInputPreparer).Prepare(context.Background(), resourcePrincipal("service.create"), json.RawMessage(`{"client":"Northwind Legal","display_id":"SVC-`+criticality+`","name":"Managed Service","criticality":"`+criticality+`"}`)); err != nil {
			t.Fatalf("Prepare(%q) error=%v", criticality, err)
		}
	}
}

func TestClientResourceCreateLocationResolutionUsesAdvertisedCapabilityWithoutSearchRead(t *testing.T) {
	directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
	principal := authorization.Principal{
		ID: "technician-1", Scope: scope.Principal{MSPID: "msp-1"},
		Capabilities: authorization.NewCapabilitySet("contact.create"),
	}
	tool := NewContactCreateTool(directory, catalog, writer, resourceID)
	if _, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), principal, json.RawMessage(
		`{"client":"Northwind Legal","display_id":"CON-2","display_name":"Ada","location":"LOC-1"}`,
	)); err != nil {
		t.Fatalf("Prepare() error=%v", err)
	}
	if catalog.trustedResolveCalls != 1 || catalog.trustedResolve.Capability != "contact.create" ||
		catalog.resolveCalls != 0 {
		t.Fatalf("trusted=%+v trusted calls=%d search resolver calls=%d", catalog.trustedResolve, catalog.trustedResolveCalls, catalog.resolveCalls)
	}
}

func TestClientResourceCreatePreviewCarriesCanonicalLocationIdentityWithoutSearchRead(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		raw        string
		tool       func(*resourceDirectoryStub, *resourceCatalogStub, *resourceWriterStub) aiassist.Tool
	}{
		{
			name:       "contact",
			capability: "contact.create",
			raw:        `{"client":"Northwind Legal","display_id":"CON-2","display_name":"Ada","location":"LOC-1"}`,
			tool: func(directory *resourceDirectoryStub, catalog *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewContactCreateTool(directory, catalog, writer, resourceID)
			},
		},
		{
			name:       "asset",
			capability: "asset.create",
			raw:        `{"client":"Northwind Legal","display_id":"AST-2","name":"Firewall","asset_type":"firewall","location":"LOC-1"}`,
			tool: func(directory *resourceDirectoryStub, catalog *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewAssetCreateTool(directory, catalog, writer, resourceID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
			principal := authorization.Principal{
				ID:           "technician-1",
				Scope:        scope.Principal{MSPID: "msp-1"},
				Capabilities: authorization.NewCapabilitySet(tt.capability),
			}
			tool := tt.tool(directory, catalog, writer)
			prepared := preparedResourceTool(t, tool, principal, tt.raw)
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
			if catalog.trustedGetCalls != 1 || catalog.trustedGet.Capability != tt.capability {
				t.Fatalf("trusted Get=%+v calls=%d", catalog.trustedGet, catalog.trustedGetCalls)
			}
		})
	}
}

func TestClientResourceCreateToolsPreparePreviewAndExecuteOnlySuppliedValues(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		raw        string
		tool       func(*resourceDirectoryStub, *resourceCatalogStub, *resourceWriterStub) aiassist.Tool
		assert     func(*testing.T, aiassist.Preview, *resourceWriterStub)
	}{
		{
			name: "location", capability: "location.create",
			raw: `{"client":"Northwind Legal","display_id":"LOC-2","name":"Branch Office"}`,
			tool: func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
				return NewLocationCreateTool(d, w, resourceID)
			},
			assert: func(t *testing.T, preview aiassist.Preview, writer *resourceWriterStub) {
				t.Helper()
				if preview.Changes["name"].After != "Branch Office" || writer.location.Name != "Branch Office" || writer.location.Prepared.ResourceID != resourceID() {
					t.Fatalf("preview=%+v command=%+v", preview, writer.location)
				}
			},
		},
		{
			name: "contact", capability: "contact.create",
			raw: `{"client":"Northwind Legal","display_id":"CON-1","display_name":"Ada Lovelace","email":"ada@example.com","phone":"555-0100","location":"Head Office"}`,
			tool: func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
				return NewContactCreateTool(d, c, w, resourceID)
			},
			assert: func(t *testing.T, preview aiassist.Preview, writer *resourceWriterStub) {
				t.Helper()
				if preview.Changes["email"].After != "ada@example.com" || preview.Changes["phone"].After != "555-0100" || preview.Changes["location"].After != "LOC-1" || writer.contact.Location.ID != "location-1" || writer.contact.Phone != "555-0100" {
					t.Fatalf("preview=%+v command=%+v", preview, writer.contact)
				}
			},
		},
		{
			name: "asset", capability: "asset.create",
			raw: `{"client":"Northwind Legal","display_id":"AST-1","name":"Firewall","asset_type":"firewall"}`,
			tool: func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
				return NewAssetCreateTool(d, c, w, resourceID)
			},
			assert: func(t *testing.T, preview aiassist.Preview, writer *resourceWriterStub) {
				t.Helper()
				if _, ok := preview.Changes["location"]; ok {
					t.Fatalf("preview invented location: %+v", preview.Changes)
				}
				if _, ok := preview.Changes["source_system"]; ok {
					t.Fatalf("preview invented source system: %+v", preview.Changes)
				}
				if _, ok := preview.Changes["external_id"]; ok {
					t.Fatalf("preview invented external id: %+v", preview.Changes)
				}
				if preview.Changes["authority"].After != clientresources.TechnicianConfirmed || writer.asset.Authority != clientresources.TechnicianConfirmed {
					t.Fatalf("preview=%+v command=%+v", preview, writer.asset)
				}
			},
		},
		{
			name: "service", capability: "service.create",
			raw: `{"client":"Northwind Legal","display_id":"SVC-1","name":"Managed Firewall","criticality":"high"}`,
			tool: func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
				return NewServiceCreateTool(d, w, resourceID)
			},
			assert: func(t *testing.T, preview aiassist.Preview, writer *resourceWriterStub) {
				t.Helper()
				if preview.Changes["criticality"].After != "high" || writer.service.Criticality != "high" {
					t.Fatalf("preview=%+v command=%+v", preview, writer.service)
				}
			},
		},
		{
			name: "contract", capability: "contract.create",
			raw: `{"client":"Northwind Legal","display_id":"CTR-1","name":"Support Agreement","starts_on":"2026-08-05"}`,
			tool: func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
				return NewContractCreateTool(d, w, resourceID)
			},
			assert: func(t *testing.T, preview aiassist.Preview, writer *resourceWriterStub) {
				t.Helper()
				if preview.Changes["starts_on"].After != "2026-08-05" || writer.contract.StartsOn != time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC) || writer.contract.EndsOn != nil {
					t.Fatalf("preview=%+v command=%+v", preview, writer.contract)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
			tool := tt.tool(directory, catalog, writer)
			principal := resourcePrincipal(tt.capability)
			prepared := preparedResourceTool(t, tool, principal, tt.raw)
			var fields map[string]any
			if err := json.Unmarshal(prepared, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["resource_id"] != resourceID() || fields["client_id"] != "client-1" ||
				fields["client_name"] != "Northwind Legal" || fields["client_display_id"] != "NW-100" ||
				fields["correlation_id"] != nil {
				t.Fatalf("prepared=%+v", fields)
			}
			preview, err := tool.Preview(context.Background(), principal, prepared)
			if err != nil {
				t.Fatalf("Preview() error = %v", err)
			}
			if preview.TargetID != resourceID() || preview.TargetVersion != 0 || preview.Changes["lifecycle_state"].After != "active" {
				t.Fatalf("preview=%+v", preview)
			}
			if !reflect.DeepEqual(preview.Changes["client"].After, map[string]string{"display_id": "NW-100", "name": "Northwind Legal"}) {
				t.Fatalf("preview lacks canonical Client identity: %+v", preview.Changes)
			}
			if len(directory.commands) != 0 || len(directory.resolveCalls) != 2 || directory.resolveCalls[0].capability != tt.capability || directory.resolveCalls[1].capability != tt.capability {
				t.Fatalf("directory calls=%+v resolutions=%+v", directory.commands, directory.resolveCalls)
			}
			if _, err := tool.Execute(context.Background(), principal, prepared, correlationID()); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got := resourceWriterCorrelation(tt.name, writer); got != correlationID() {
				t.Fatalf("correlation=%q, want %q", got, correlationID())
			}
			tt.assert(t, preview, writer)
		})
	}
}

func resourceWriterCorrelation(kind string, writer *resourceWriterStub) string {
	switch kind {
	case "location":
		return writer.location.Prepared.CorrelationID
	case "contact":
		return writer.contact.Prepared.CorrelationID
	case "asset":
		return writer.asset.Prepared.CorrelationID
	case "service":
		return writer.service.Prepared.CorrelationID
	case "contract":
		return writer.contract.Prepared.CorrelationID
	default:
		return ""
	}
}

func TestClientResourceCreateToolsRejectUntrustedIncompleteAndUnresolvableInput(t *testing.T) {
	tests := []struct {
		name, capability, raw string
		tool                  func(*resourceDirectoryStub, *resourceCatalogStub, *resourceWriterStub) aiassist.Tool
	}{
		{"unknown", "asset.create", `{"client":"Northwind Legal","display_id":"AST-1","name":"Firewall","asset_type":"firewall","authority":"discovered"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewAssetCreateTool(d, c, w, resourceID)
		}},
		{"missing", "location.create", `{"client":"Northwind Legal","display_id":"LOC-1"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewLocationCreateTool(d, w, resourceID)
		}},
		{"trusted id", "service.create", `{"client":"Northwind Legal","display_id":"SVC-1","name":"Managed Firewall","resource_id":"caller-id"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewServiceCreateTool(d, w, resourceID)
		}},
		{"asset source only", "asset.create", `{"client":"Northwind Legal","display_id":"AST-1","name":"Firewall","asset_type":"firewall","source_system":"rmm"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewAssetCreateTool(d, c, w, resourceID)
		}},
		{"asset external only", "asset.create", `{"client":"Northwind Legal","display_id":"AST-1","name":"Firewall","asset_type":"firewall","external_id":"device-1"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewAssetCreateTool(d, c, w, resourceID)
		}},
		{"invalid criticality", "service.create", `{"client":"Northwind Legal","display_id":"SVC-1","name":"Managed Firewall","criticality":"urgent"}`, func(d *resourceDirectoryStub, c *resourceCatalogStub, w *resourceWriterStub) aiassist.Tool {
			return NewServiceCreateTool(d, w, resourceID)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := tt.tool(activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{})
			_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), resourcePrincipal(tt.capability), json.RawMessage(tt.raw))
			if !errors.Is(err, aiassist.ErrInvalidTool) {
				t.Fatalf("Prepare() error=%v, want ErrInvalidTool", err)
			}
		})
	}

	for _, lifecycle := range []string{"inactive", "ambiguous"} {
		t.Run("location "+lifecycle, func(t *testing.T) {
			directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
			if lifecycle == "inactive" {
				catalog.resolved.LifecycleState = "inactive"
			} else {
				catalog.err = clientresources.ErrAmbiguousResource
			}
			tool := NewContactCreateTool(directory, catalog, writer, resourceID)
			_, err := tool.(aiassist.ToolInputPreparer).Prepare(context.Background(), resourcePrincipal("contact.create"), json.RawMessage(`{"client":"Northwind Legal","display_id":"CON-1","display_name":"Ada","location":"Head Office"}`))
			want := aiassist.ErrInvalidTool
			if lifecycle == "inactive" {
				want = clientresources.ErrLifecycleConflict
			} else if lifecycle == "ambiguous" {
				want = clientresources.ErrAmbiguousResource
			}
			if !errors.Is(err, want) {
				t.Fatalf("Prepare() error=%v, want %v", err, want)
			}
		})
	}
}

func TestClientResourceCreateRegistryPreservesTypedResolutionErrors(t *testing.T) {
	principal := resourcePrincipal("contact.create")
	request := aiassist.ToolRequest{
		Name: "contact.create", ConversationID: "conversation-1",
		Input: json.RawMessage(`{"client":"Northwind Legal","display_id":"CON-1","display_name":"Ada","location":"Head Office"}`),
	}
	for _, test := range []struct {
		name string
		err  error
		set  func(*resourceDirectoryStub, *resourceCatalogStub, error)
	}{
		{
			name: "ambiguous resource", err: clientresources.ErrAmbiguousResource,
			set: func(_ *resourceDirectoryStub, catalog *resourceCatalogStub, err error) { catalog.err = err },
		},
		{
			name: "missing resource", err: scope.ErrNotFound,
			set: func(_ *resourceDirectoryStub, catalog *resourceCatalogStub, err error) { catalog.err = err },
		},
		{
			name: "ambiguous Client", err: organizations.ErrClientReferenceAmbiguous,
			set: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, err error) { directory.err = err },
		},
		{
			name: "missing Client", err: organizations.ErrClientReferenceNotFound,
			set: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, err error) { directory.err = err },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory, catalog := activeResourceDirectory(), activeLocationCatalog()
			test.set(directory, catalog, test.err)
			registry, err := aiassist.NewRegistry(
				[]aiassist.Tool{NewContactCreateTool(directory, catalog, &resourceWriterStub{}, resourceID)},
				&composedProposalStore{},
				func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
				time.Now, func() string { return "id" }, &composedTargetAuthorizer{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Propose(context.Background(), principal, request); !errors.Is(err, test.err) {
				t.Fatalf("Propose() error=%v, want %v", err, test.err)
			}
		})
	}
}

func TestClientResourceCreateRegistryReauthorizesRepreviewsAndNeverWritesRejectedProposal(t *testing.T) {
	now := time.Date(2026, time.August, 5, 20, 0, 0, 0, time.UTC)
	directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
	tool := NewContactCreateTool(directory, catalog, writer, resourceID)
	principal := resourcePrincipal("contact.create")
	store := &composedProposalStore{}
	targets := &composedTargetAuthorizer{}
	ids := []string{"proposal-1", correlationID()}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, targets)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{Name: "contact.create", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"Northwind Legal","display_id":"CON-1","display_name":"Ada","location":"Head Office"}`)})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	catalog.current.DisplayID = "LOC-CHANGED"
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
	}
	if writer.contactCalls != 0 {
		t.Fatalf("writer calls=%d after stale preview", writer.contactCalls)
	}
	catalog.current.DisplayID = "LOC-1"
	if err := registry.Reject(context.Background(), principal, proposal.ID, proposal.Version); err != nil {
		t.Fatalf("Reject() error=%v", err)
	}
	if writer.contactCalls != 0 || store.proposal.State != aiassist.ProposalRejected {
		t.Fatalf("writer calls=%d proposal=%+v", writer.contactCalls, store.proposal)
	}
}

func TestClientResourceCreateRegistryConfirmsEveryCreateAfterPersistedJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		raw        string
		tool       func(*resourceDirectoryStub, *resourceCatalogStub, *resourceWriterStub) aiassist.Tool
		calls      func(*resourceWriterStub) int
	}{
		{
			name: "location", capability: "location.create",
			raw: `{"client":"Northwind Legal","display_id":"LOC-2","name":"Branch Office"}`,
			tool: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewLocationCreateTool(directory, writer, resourceID)
			},
			calls: func(writer *resourceWriterStub) int { return writer.locationCalls },
		},
		{
			name: "contact", capability: "contact.create",
			raw: `{"client":"Northwind Legal","display_id":"CON-2","display_name":"Ada Lovelace"}`,
			tool: func(directory *resourceDirectoryStub, catalog *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewContactCreateTool(directory, catalog, writer, resourceID)
			},
			calls: func(writer *resourceWriterStub) int { return writer.contactCalls },
		},
		{
			name: "asset", capability: "asset.create",
			raw: `{"client":"Northwind Legal","display_id":"AST-2","name":"Firewall","asset_type":"firewall"}`,
			tool: func(directory *resourceDirectoryStub, catalog *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewAssetCreateTool(directory, catalog, writer, resourceID)
			},
			calls: func(writer *resourceWriterStub) int { return writer.assetCalls },
		},
		{
			name: "service", capability: "service.create",
			raw: `{"client":"Northwind Legal","display_id":"SVC-2","name":"Managed Firewall"}`,
			tool: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewServiceCreateTool(directory, writer, resourceID)
			},
			calls: func(writer *resourceWriterStub) int { return writer.serviceCalls },
		},
		{
			name: "contract", capability: "contract.create",
			raw: `{"client":"Northwind Legal","display_id":"CTR-2","name":"Support Agreement","starts_on":"2026-08-06"}`,
			tool: func(directory *resourceDirectoryStub, _ *resourceCatalogStub, writer *resourceWriterStub) aiassist.Tool {
				return NewContractCreateTool(directory, writer, resourceID)
			},
			calls: func(writer *resourceWriterStub) int { return writer.contractCalls },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
			directory, catalog, writer := activeResourceDirectory(), activeLocationCatalog(), &resourceWriterStub{}
			principal := resourcePrincipal(test.capability)
			store := &serializedPreviewProposalStore{}
			ids := []string{"proposal-" + test.name, correlationID()}
			registry, err := aiassist.NewRegistry(
				[]aiassist.Tool{test.tool(directory, catalog, writer)},
				store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) {
					return principal, nil
				},
				func() time.Time { return now },
				func() string {
					value := ids[0]
					ids = ids[1:]
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
					Name: test.name + ".create", ConversationID: "conversation-1",
					Input: json.RawMessage(test.raw),
				},
			)
			if err != nil {
				t.Fatalf("Propose() error=%v", err)
			}
			result, err := registry.Confirm(
				context.Background(), principal, proposal.ID, proposal.Version,
			)
			if err != nil {
				t.Fatalf("Confirm() after persisted JSON round trip error=%v", err)
			}
			if proposal.Preview.TargetID != resourceID() ||
				result.Data["resource_type"] != test.name ||
				result.Data["resource_id"] != resourceID() ||
				resourceWriterCorrelation(test.name, writer) != correlationID() ||
				test.calls(writer) != 1 {
				t.Fatalf("result=%+v writer=%+v", result, writer)
			}
		})
	}
}

func TestClientResourceCreateRegistryRejectsCanonicalClientIdentityDrift(t *testing.T) {
	now := time.Date(2026, time.August, 5, 20, 30, 0, 0, time.UTC)
	directory, writer := activeResourceDirectory(), &resourceWriterStub{}
	tool := NewLocationCreateTool(directory, writer, resourceID)
	principal := resourcePrincipal("location.create")
	store := &composedProposalStore{}
	ids := []string{"proposal-client-drift", correlationID()}
	registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
		func(context.Context, authorization.Principal) (authorization.Principal, error) { return principal, nil },
		func() time.Time { return now }, func() string { value := ids[0]; ids = ids[1:]; return value }, &composedTargetAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := registry.Propose(context.Background(), principal, aiassist.ToolRequest{
		Name: "location.create", ConversationID: "conversation-client-drift",
		Input: json.RawMessage(`{"client":"Northwind Legal","display_id":"LOC-2","name":"Branch Office"}`),
	})
	if err != nil {
		t.Fatalf("Propose() error=%v", err)
	}
	directory.resolved.DisplayID = "NW-101"
	if _, err := registry.Confirm(context.Background(), principal, proposal.ID, proposal.Version); !errors.Is(err, aiassist.ErrProposalStale) {
		t.Fatalf("Confirm() error=%v, want ErrProposalStale", err)
	}
	if writer.locationCalls != 0 {
		t.Fatalf("writer calls=%d after Client identity drift", writer.locationCalls)
	}
}

func TestClientResourceCreateRegistryDeniesCapabilityAndCrossClientBeforeProposal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		principal authorization.Principal
		directory *resourceDirectoryStub
	}{
		{
			name:      "capability",
			principal: resourcePrincipal("search.read"),
			directory: activeResourceDirectory(),
		},
		{
			name: "cross client",
			principal: func() authorization.Principal {
				principal := resourcePrincipal("location.create")
				principal.Scope.ClientID = "client-1"
				return principal
			}(),
			directory: &resourceDirectoryStub{directory: organizations.Directory{Clients: []organizations.Client{{
				Envelope: object.Envelope{ID: "client-2", MSPID: "msp-1", ClientID: "client-2", DisplayID: "OTHER-1", LifecycleState: "active"}, Name: "Other Client",
			}}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writer := &resourceWriterStub{}
			tool := NewLocationCreateTool(tt.directory, writer, resourceID)
			store := &composedProposalStore{}
			registry, err := aiassist.NewRegistry([]aiassist.Tool{tool}, store,
				func(context.Context, authorization.Principal) (authorization.Principal, error) {
					return tt.principal, nil
				},
				time.Now, func() string { return correlationID() }, &composedTargetAuthorizer{})
			if err != nil {
				t.Fatal(err)
			}
			client := "Northwind Legal"
			if tt.name == "cross client" {
				client = "Other Client"
			}
			_, err = registry.Propose(context.Background(), tt.principal, aiassist.ToolRequest{Name: "location.create", ConversationID: "conversation-1", Input: json.RawMessage(`{"client":"` + client + `","display_id":"LOC-2","name":"Branch Office"}`)})
			if err == nil || store.proposal.ID != "" || writer.locationCalls != 0 {
				t.Fatalf("Propose() error=%v proposal=%+v writer calls=%d", err, store.proposal, writer.locationCalls)
			}
		})
	}
}

func resourceID() string    { return "019fb3c2-0000-7000-8000-000000000501" }
func correlationID() string { return "019fb3c2-0000-7000-8000-000000000502" }

func containsAny(value string, keys ...string) bool {
	for _, key := range keys {
		if !contains(value, key) {
			return false
		}
	}
	return true
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
