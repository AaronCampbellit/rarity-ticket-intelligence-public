package rtitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// WorkRecordCreateActions is the ordinary work-record service boundary used by
// the closed AI ticket-create adapter.
type WorkRecordCreateActions interface {
	PreflightCreate(context.Context, workrecords.CreateCommand) (workrecords.CreatePreflight, error)
	Create(context.Context, workrecords.CreateCommand) (workrecords.Record, error)
}

// ticketCreateRequest is the complete public schema. It deliberately contains
// references and business values only; identities, routing, workflow, SLA,
// actor, and correlation are adapter-owned.
type ticketCreateRequest struct {
	Client      string           `json:"client"`
	DisplayID   string           `json:"display_id"`
	Type        workrecords.Type `json:"type"`
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	Status      string           `json:"status"`
	Priority    string           `json:"priority"`
	Service     *string          `json:"service,omitempty"`
	Contract    *string          `json:"contract,omitempty"`
	TagIDs      []string         `json:"tag_ids"`
}

// ticketCreatePrepared is an adapter-only proposal payload. It stores the
// canonical identities and the ordinary service's selection fence so that the
// registry confirmation preview can reject any drift before Create is called.
type ticketCreatePrepared struct {
	ClientID          string                           `json:"client_id"`
	ClientReference   string                           `json:"client_reference"`
	ClientName        string                           `json:"client_name"`
	ClientDisplayID   string                           `json:"client_display_id"`
	ClientVersion     int64                            `json:"client_version"`
	TicketID          string                           `json:"ticket_id"`
	DisplayID         string                           `json:"display_id"`
	Type              workrecords.Type                 `json:"type"`
	Title             string                           `json:"title"`
	Description       string                           `json:"description"`
	Status            string                           `json:"status"`
	Priority          string                           `json:"priority"`
	ServiceID         string                           `json:"service_id,omitempty"`
	ServiceRef        string                           `json:"service_reference,omitempty"`
	ServiceDisplayID  string                           `json:"service_display_id,omitempty"`
	ServiceName       string                           `json:"service_name,omitempty"`
	ServiceVersion    int64                            `json:"service_version,omitempty"`
	ContractID        string                           `json:"contract_id,omitempty"`
	ContractRef       string                           `json:"contract_reference,omitempty"`
	ContractDisplayID string                           `json:"contract_display_id,omitempty"`
	ContractName      string                           `json:"contract_name,omitempty"`
	ContractVersion   int64                            `json:"contract_version,omitempty"`
	TagIDs            []string                         `json:"tag_ids"`
	Fence             workrecords.CreateSelectionFence `json:"selection_fence"`
}

type ticketCreateResource struct {
	ID, Reference, DisplayID, Name string
	Version                        int64
}

func NewTicketCreateTool(
	directory ActiveClientResolver,
	catalog ClientResourceCatalog,
	actions WorkRecordCreateActions,
	newID func() string,
) aiassist.Tool {
	return &functionalTool{
		name: "ticket.create", capability: "work_record.create", kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			request, err := decode[ticketCreateRequest](raw)
			if err != nil || actions == nil || newID == nil || request.Description == nil {
				return nil, aiassist.ErrInvalidTool
			}
			request.Client, request.DisplayID = strings.TrimSpace(request.Client), strings.TrimSpace(request.DisplayID)
			request.Title, request.Status, request.Priority = strings.TrimSpace(request.Title), strings.TrimSpace(request.Status), strings.TrimSpace(request.Priority)
			if request.Client == "" || request.DisplayID == "" || request.Title == "" || request.Status == "" ||
				request.Priority == "" || !validWorkRecordType(request.Type) || !validTicketCreatePriority(request.Priority) ||
				strings.TrimSpace(*request.Description) == "" || !optionalValueValid(request.Service) || !optionalValueValid(request.Contract) ||
				!validTicketCreateTags(request.TagIDs) {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.create", request.Client)
			if err != nil {
				return nil, err
			}
			service, err := resolveTicketCreateResource(ctx, catalog, principal, client.ID, clientresources.ServiceKind, request.Service)
			if err != nil {
				return nil, err
			}
			contract, err := resolveTicketCreateResource(ctx, catalog, principal, client.ID, clientresources.ContractKind, request.Contract)
			if err != nil {
				return nil, err
			}
			ticketID := strings.TrimSpace(newID())
			if ticketID == "" {
				return nil, aiassist.ErrInvalidTool
			}
			input := ticketCreatePrepared{
				ClientID: client.ID, ClientReference: request.Client, ClientName: client.Name, ClientDisplayID: client.DisplayID, ClientVersion: client.Version,
				TicketID: ticketID, DisplayID: request.DisplayID, Type: request.Type, Title: request.Title,
				Description: strings.TrimSpace(*request.Description), Status: request.Status, Priority: request.Priority,
				ServiceID: service.ID, ServiceRef: service.Reference, ServiceDisplayID: service.DisplayID, ServiceName: service.Name, ServiceVersion: service.Version,
				ContractID: contract.ID, ContractRef: contract.Reference, ContractDisplayID: contract.DisplayID, ContractName: contract.Name, ContractVersion: contract.Version,
				TagIDs: append([]string(nil), request.TagIDs...),
			}
			preflight, err := actions.PreflightCreate(ctx, ticketCreateCommand(principal, input, nil))
			if err != nil {
				return nil, err
			}
			if !matchesTicketCreatePreflight(principal, input, preflight) {
				return nil, aiassist.ErrInvalidTool
			}
			input.Fence = preflight.Fence
			return marshalPrepared(input)
		},
		validate: validateTicketCreate,
		resolve:  resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validTicketCreate(raw)
			if err != nil {
				return aiassist.Preview{}, err
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, "work_record.create", input.ClientReference)
			if err != nil || !sameTicketCreateClient(input, client) {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			if err := verifyTicketCreateResource(ctx, catalog, principal, input.ClientID, clientresources.ServiceKind, input.ServiceID, input.ServiceRef, input.ServiceDisplayID, input.ServiceName, input.ServiceVersion); err != nil {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			if err := verifyTicketCreateResource(ctx, catalog, principal, input.ClientID, clientresources.ContractKind, input.ContractID, input.ContractRef, input.ContractDisplayID, input.ContractName, input.ContractVersion); err != nil {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			preflight, err := actions.PreflightCreate(ctx, ticketCreateCommand(principal, input, nil))
			if err != nil || !matchesTicketCreatePreflight(principal, input, preflight) || preflight.Fence != input.Fence {
				return aiassist.Preview{}, aiassist.ErrProposalStale
			}
			return ticketCreatePreview(input), nil
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validTicketCreate(raw)
			if err != nil || strings.TrimSpace(correlationID) == "" || actions == nil {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			record, err := actions.Create(ctx, ticketCreateCommand(principal, input, &input.Fence))
			if err != nil {
				return aiassist.ToolResult{}, err
			}
			return aiassist.ToolResult{Summary: "Created " + record.DisplayID, Data: recordData(record)}, nil
		},
	}
}

func validateTicketCreate(raw json.RawMessage) error { _, err := validTicketCreate(raw); return err }

func validTicketCreate(raw json.RawMessage) (ticketCreatePrepared, error) {
	input, err := decode[ticketCreatePrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" || input.ClientVersion < 1 ||
		strings.TrimSpace(input.TicketID) == "" || strings.TrimSpace(input.DisplayID) == "" ||
		!validWorkRecordType(input.Type) || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Description) == "" ||
		strings.TrimSpace(input.Status) == "" || !validTicketCreatePriority(input.Priority) || !validTicketCreateFence(input.Fence) ||
		!validTicketCreateTags(input.TagIDs) ||
		!validTicketCreateResource(input.ServiceID, input.ServiceRef, input.ServiceDisplayID, input.ServiceName, input.ServiceVersion) ||
		!validTicketCreateResource(input.ContractID, input.ContractRef, input.ContractDisplayID, input.ContractName, input.ContractVersion) {
		return ticketCreatePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func validTicketCreateTags(tagIDs []string) bool {
	if len(tagIDs) == 0 {
		return false
	}
	for _, tagID := range tagIDs {
		if strings.TrimSpace(tagID) == "" {
			return false
		}
	}
	return true
}

func validTicketCreatePriority(priority string) bool {
	switch strings.TrimSpace(priority) {
	case "low", "normal", "high", "critical":
		return true
	default:
		return false
	}
}

func validTicketCreateFence(fence workrecords.CreateSelectionFence) bool {
	return strings.TrimSpace(fence.RuleSetID) != "" && fence.RuleSetVersion > 0 && strings.TrimSpace(fence.QueueID) != "" &&
		strings.TrimSpace(fence.WorkflowID) != "" && fence.WorkflowVersion > 0 && strings.TrimSpace(fence.SLAPolicyID) != "" &&
		fence.SLAPolicyVersion > 0 && strings.TrimSpace(fence.CalendarID) != "" && fence.CalendarVersion > 0
}

func validTicketCreateResource(id, reference, displayID, name string, version int64) bool {
	return (id == "" && reference == "" && displayID == "" && name == "" && version == 0) ||
		(strings.TrimSpace(id) != "" && strings.TrimSpace(reference) != "" && strings.TrimSpace(displayID) != "" && strings.TrimSpace(name) != "" && version > 0)
}

func resolveTicketCreateResource(ctx context.Context, catalog ClientResourceCatalog, principal authorization.Principal, clientID string, kind clientresources.Kind, reference *string) (ticketCreateResource, error) {
	if reference == nil {
		return ticketCreateResource{}, nil
	}
	if catalog == nil || strings.TrimSpace(*reference) == "" {
		return ticketCreateResource{}, aiassist.ErrInvalidTool
	}
	resource, err := catalog.ResolveTrusted(ctx, clientresources.TrustedResolveCommand{Principal: principal, Target: target(principal, clientID), Kind: kind, Reference: strings.TrimSpace(*reference), Capability: "work_record.create"})
	if err != nil {
		return ticketCreateResource{}, err
	}
	if resource.Kind != string(kind) || resource.LifecycleState != "active" || strings.TrimSpace(resource.ID) == "" || resource.Version < 1 {
		return ticketCreateResource{}, scope.ErrNotFound
	}
	return ticketCreateResource{ID: resource.ID, Reference: strings.TrimSpace(*reference), DisplayID: resource.DisplayID, Name: resource.Name, Version: resource.Version}, nil
}

func verifyTicketCreateResource(ctx context.Context, catalog ClientResourceCatalog, principal authorization.Principal, clientID string, kind clientresources.Kind, id, reference, displayID, name string, version int64) error {
	if id == "" {
		return nil
	}
	if catalog == nil {
		return aiassist.ErrInvalidTool
	}
	resolved, err := resolveTicketCreateResource(
		ctx, catalog, principal, clientID, kind, &reference,
	)
	if err != nil {
		return err
	}
	if resolved.ID != id || resolved.DisplayID != displayID ||
		resolved.Name != name || resolved.Version != version {
		return aiassist.ErrProposalStale
	}
	resource, err := catalog.GetTrusted(ctx, clientresources.TrustedGetCommand{Principal: principal, Target: target(principal, clientID), Kind: kind, ID: id, Capability: "work_record.create"})
	if err != nil {
		return err
	}
	if resource.Kind != string(kind) || resource.LifecycleState != "active" || resource.ID != id || resource.DisplayID != displayID || resource.Name != name || resource.Version != version || strings.TrimSpace(reference) == "" {
		return aiassist.ErrProposalStale
	}
	return nil
}

func ticketCreateCommand(principal authorization.Principal, input ticketCreatePrepared, fence *workrecords.CreateSelectionFence) workrecords.CreateCommand {
	return workrecords.CreateCommand{
		RecordID: input.TicketID, Principal: principal, Target: target(principal, input.ClientID),
		Actor:     workrecords.Actor{Type: "technician", ID: principal.ID, Source: "ai_workspace"},
		DisplayID: input.DisplayID, Type: input.Type, Title: input.Title,
		Description: input.Description, Status: input.Status, Priority: input.Priority,
		ServiceID: input.ServiceID, ContractID: input.ContractID,
		ExpectedClientVersion: input.ClientVersion, ExpectedServiceVersion: input.ServiceVersion,
		ExpectedContractVersion: input.ContractVersion, ExpectedSelection: fence,
		TagIDs: append([]string(nil), input.TagIDs...), ClassificationPolicy: tagging.CreationRequireMeaningful,
	}
}

func matchesTicketCreatePreflight(principal authorization.Principal, input ticketCreatePrepared, preflight workrecords.CreatePreflight) bool {
	return preflight.Target == target(principal, input.ClientID) && preflight.ServiceID == input.ServiceID && preflight.ContractID == input.ContractID && validTicketCreateFence(preflight.Fence)
}

func sameTicketCreateClient(input ticketCreatePrepared, client organizations.Client) bool {
	return client.ID == input.ClientID && client.Name == input.ClientName &&
		client.DisplayID == input.ClientDisplayID && client.Version == input.ClientVersion
}

func ticketCreatePreview(input ticketCreatePrepared) aiassist.Preview {
	changes := map[string]aiassist.Change{
		"display_id": {Before: nil, After: input.DisplayID}, "type": {Before: nil, After: input.Type}, "title": {Before: nil, After: input.Title},
		"description": {Before: nil, After: input.Description}, "status": {Before: nil, After: input.Status}, "priority": {Before: nil, After: input.Priority},
		"tag_ids":      {Before: nil, After: append([]string(nil), input.TagIDs...)},
		"client":       {Before: nil, After: map[string]string{"display_id": input.ClientDisplayID, "name": input.ClientName}},
		"queue":        {Before: nil, After: map[string]any{"id": input.Fence.QueueID}},
		"workflow":     {Before: nil, After: map[string]any{"id": input.Fence.WorkflowID, "version": input.Fence.WorkflowVersion}},
		"sla_policy":   {Before: nil, After: map[string]any{"id": input.Fence.SLAPolicyID, "version": input.Fence.SLAPolicyVersion}},
		"sla_calendar": {Before: nil, After: map[string]any{"id": input.Fence.CalendarID, "version": input.Fence.CalendarVersion}},
	}
	if input.ServiceID != "" {
		changes["service"] = aiassist.Change{Before: nil, After: map[string]any{"display_id": input.ServiceDisplayID, "name": input.ServiceName, "version": input.ServiceVersion}}
	}
	if input.ContractID != "" {
		changes["contract"] = aiassist.Change{Before: nil, After: map[string]any{"display_id": input.ContractDisplayID, "name": input.ContractName, "version": input.ContractVersion}}
	}
	return aiassist.Preview{Summary: "Create " + string(input.Type) + " " + input.DisplayID + " for " + input.ClientName, TargetType: "work_record", TargetID: input.TicketID, Changes: changes}
}
