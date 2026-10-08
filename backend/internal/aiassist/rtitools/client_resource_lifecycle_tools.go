package rtitools

import (
	"context"
	"encoding/json"
	"net/mail"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientresources"
)

type ClientResourceLifecycle interface {
	Preflight(context.Context, clientresources.LifecyclePreflightCommand) (clientresources.LifecyclePreflight, error)
	Update(context.Context, clientresources.UpdateCommand) (clientresources.ResourceDetail, error)
	Deactivate(context.Context, clientresources.LifecycleCommand) (clientresources.ResourceDetail, error)
	Reactivate(context.Context, clientresources.LifecycleCommand) (clientresources.ResourceDetail, error)
}

type resourceUpdateRequest struct {
	Client   string                    `json:"client"`
	Resource string                    `json:"resource"`
	Patch    resourceUpdatePublicPatch `json:"patch"`
	Reason   string                    `json:"reason"`
}

type resourceLifecycleRequest struct {
	Client   string `json:"client"`
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
}

type resourceUpdatePublicPatch struct {
	Name        *string `json:"name,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Location    *string `json:"location,omitempty"`
	AssetType   *string `json:"asset_type,omitempty"`
	Criticality *string `json:"criticality,omitempty"`
	StartsOn    *string `json:"starts_on,omitempty"`
	EndsOn      *string `json:"ends_on,omitempty"`
	ClearEndsOn bool    `json:"clear_ends_on,omitempty"`
}

type resourceUpdatePreparedPatch struct {
	Name              *string `json:"name,omitempty"`
	DisplayName       *string `json:"display_name,omitempty"`
	Email             *string `json:"email,omitempty"`
	Phone             *string `json:"phone,omitempty"`
	LocationID        *string `json:"location_id,omitempty"`
	LocationReference *string `json:"location_reference,omitempty"`
	AssetType         *string `json:"asset_type,omitempty"`
	Criticality       *string `json:"criticality,omitempty"`
	StartsOn          *string `json:"starts_on,omitempty"`
	EndsOn            *string `json:"ends_on,omitempty"`
	ClearEndsOn       bool    `json:"clear_ends_on,omitempty"`
}

type resourceLifecyclePrepared struct {
	ClientID          string                      `json:"client_id"`
	ClientReference   string                      `json:"client_reference"`
	ClientName        string                      `json:"client_name"`
	ClientDisplayID   string                      `json:"client_display_id"`
	ResourceID        string                      `json:"resource_id"`
	ResourceReference string                      `json:"resource_reference"`
	ResourceName      string                      `json:"resource_name"`
	ResourceDisplayID string                      `json:"resource_display_id"`
	CurrentVersion    int64                       `json:"current_version"`
	Patch             resourceUpdatePreparedPatch `json:"patch,omitempty"`
	Reason            string                      `json:"reason"`
}

type resourceLifecycleOperation string

const (
	resourceUpdate     resourceLifecycleOperation = "update"
	resourceDeactivate resourceLifecycleOperation = "deactivate"
	resourceReactivate resourceLifecycleOperation = "reactivate"
)

func NewLocationUpdateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.LocationKind, resourceUpdate, d, c, a)
}
func NewLocationDeactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.LocationKind, resourceDeactivate, d, c, a)
}
func NewLocationReactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.LocationKind, resourceReactivate, d, c, a)
}
func NewContactUpdateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContactKind, resourceUpdate, d, c, a)
}
func NewContactDeactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContactKind, resourceDeactivate, d, c, a)
}
func NewContactReactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContactKind, resourceReactivate, d, c, a)
}
func NewAssetUpdateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.AssetKind, resourceUpdate, d, c, a)
}
func NewAssetDeactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.AssetKind, resourceDeactivate, d, c, a)
}
func NewAssetReactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.AssetKind, resourceReactivate, d, c, a)
}
func NewServiceUpdateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ServiceKind, resourceUpdate, d, c, a)
}
func NewServiceDeactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ServiceKind, resourceDeactivate, d, c, a)
}
func NewServiceReactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ServiceKind, resourceReactivate, d, c, a)
}
func NewContractUpdateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContractKind, resourceUpdate, d, c, a)
}
func NewContractDeactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContractKind, resourceDeactivate, d, c, a)
}
func NewContractReactivateTool(d ActiveClientResolver, c ClientResourceCatalog, a ClientResourceLifecycle) aiassist.Tool {
	return newResourceLifecycleTool(clientresources.ContractKind, resourceReactivate, d, c, a)
}

func newResourceLifecycleTool(kind clientresources.Kind, operation resourceLifecycleOperation, directory ActiveClientResolver, catalog ClientResourceCatalog, actions ClientResourceLifecycle) aiassist.Tool {
	capability := string(kind) + ".update"
	if operation != resourceUpdate {
		capability = string(kind) + ".lifecycle"
	}
	name := string(kind) + "." + string(operation)
	return &functionalTool{
		name: name, capability: capability, kind: aiassist.ToolWrite,
		prepare: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (json.RawMessage, error) {
			if directory == nil || catalog == nil || actions == nil {
				return nil, aiassist.ErrInvalidTool
			}
			var clientReference, resourceReference, reason string
			var patch resourceUpdatePreparedPatch
			if operation == resourceUpdate {
				request, err := decode[resourceUpdateRequest](raw)
				if err != nil {
					return nil, aiassist.ErrInvalidTool
				}
				clientReference, resourceReference, reason = request.Client, request.Resource, request.Reason
				patch, err = prepareResourceUpdatePatch(ctx, catalog, principal, capability, kind, request.Patch, "")
				if err != nil {
					return nil, err
				}
			} else {
				request, err := decode[resourceLifecycleRequest](raw)
				if err != nil {
					return nil, aiassist.ErrInvalidTool
				}
				clientReference, resourceReference, reason = request.Client, request.Resource, request.Reason
			}
			clientReference, resourceReference, reason = strings.TrimSpace(clientReference), strings.TrimSpace(resourceReference), strings.TrimSpace(reason)
			if clientReference == "" || resourceReference == "" || reason == "" {
				return nil, aiassist.ErrInvalidTool
			}
			client, err := resolveActiveResourceClient(ctx, directory, principal, capability, clientReference)
			if err != nil {
				return nil, err
			}
			includeInactive := operation == resourceReactivate
			resource, err := catalog.ResolveTrusted(ctx, clientresources.TrustedResolveCommand{
				Principal: principal, Target: target(principal, client.ID), Kind: kind,
				Reference: resourceReference, Capability: capability, IncludeInactive: includeInactive,
			})
			if err != nil {
				return nil, err
			}
			if err := validLifecycleTarget(resource, kind, operation); err != nil {
				return nil, err
			}
			if operation == resourceUpdate && patch.LocationReference != nil && *patch.LocationReference != "" {
				patch, err = prepareResourceUpdatePatch(ctx, catalog, principal, capability, kind, resourceUpdatePublicPatch{
					Name: patch.Name, DisplayName: patch.DisplayName, Email: patch.Email, Phone: patch.Phone,
					Location: patch.LocationReference, AssetType: patch.AssetType, Criticality: patch.Criticality,
					StartsOn: patch.StartsOn, EndsOn: patch.EndsOn, ClearEndsOn: patch.ClearEndsOn,
				}, client.ID)
				if err != nil {
					return nil, err
				}
			}
			if _, err := actions.Preflight(ctx, lifecyclePreflightCommand(
				principal, client.ID, kind, operation, resource.ID, resource.Version, patch,
			)); err != nil {
				return nil, err
			}
			return marshalPrepared(resourceLifecyclePrepared{
				ClientID: client.ID, ClientReference: clientReference, ClientName: client.Name, ClientDisplayID: client.DisplayID,
				ResourceID: resource.ID, ResourceReference: resourceReference, ResourceName: resource.Name,
				ResourceDisplayID: resource.DisplayID, CurrentVersion: resource.Version, Patch: patch, Reason: reason,
			})
		},
		validate: func(raw json.RawMessage) error {
			_, err := validResourceLifecyclePrepared(raw, kind, operation)
			return err
		},
		resolve: resolveClientResourceScope,
		preview: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage) (aiassist.Preview, error) {
			input, err := validResourceLifecyclePrepared(raw, kind, operation)
			if err != nil {
				return aiassist.Preview{}, err
			}
			return resourceLifecyclePreview(ctx, directory, catalog, actions, principal, capability, kind, operation, input)
		},
		execute: func(ctx context.Context, principal authorization.Principal, raw json.RawMessage, correlationID string) (aiassist.ToolResult, error) {
			input, err := validResourceLifecyclePrepared(raw, kind, operation)
			if err != nil || actions == nil || strings.TrimSpace(correlationID) == "" {
				return aiassist.ToolResult{}, aiassist.ErrInvalidTool
			}
			if operation == resourceUpdate {
				result, err := actions.Update(ctx, clientresources.UpdateCommand{
					Principal: principal, Target: target(principal, input.ClientID), Kind: kind, ResourceID: input.ResourceID,
					ExpectedVersion: input.CurrentVersion, Patch: domainUpdatePatch(input.Patch), Reason: input.Reason,
					ActorID: principal.ID, Source: "ai_workspace", CorrelationID: correlationID,
				})
				return lifecycleToolResult(operation, kind, result, err)
			}
			command := clientresources.LifecycleCommand{
				Principal: principal, Target: target(principal, input.ClientID), Kind: kind, ResourceID: input.ResourceID,
				ExpectedVersion: input.CurrentVersion, Reason: input.Reason, ActorID: principal.ID,
				Source: "ai_workspace", CorrelationID: correlationID,
			}
			if operation == resourceReactivate {
				result, err := actions.Reactivate(ctx, command)
				return lifecycleToolResult(operation, kind, result, err)
			}
			result, err := actions.Deactivate(ctx, command)
			return lifecycleToolResult(operation, kind, result, err)
		},
	}
}

func prepareResourceUpdatePatch(ctx context.Context, catalog ClientResourceCatalog, principal authorization.Principal, capability string, kind clientresources.Kind, public resourceUpdatePublicPatch, clientID string) (resourceUpdatePreparedPatch, error) {
	patch := resourceUpdatePreparedPatch{
		Name: public.Name, DisplayName: public.DisplayName, Email: public.Email, Phone: public.Phone,
		LocationReference: public.Location, AssetType: public.AssetType, Criticality: public.Criticality,
		StartsOn: public.StartsOn, EndsOn: public.EndsOn, ClearEndsOn: public.ClearEndsOn,
	}
	if _, err := normalizedDomainPatch(kind, patch); err != nil {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	if public.Location == nil {
		return patch, nil
	}
	locationReference := strings.TrimSpace(*public.Location)
	patch.LocationReference = &locationReference
	if locationReference == "" {
		cleared := ""
		patch.LocationID = &cleared
		return patch, nil
	}
	if clientID == "" || catalog == nil {
		return patch, nil
	}
	location, err := catalog.ResolveTrusted(ctx, clientresources.TrustedResolveCommand{
		Principal: principal, Target: target(principal, clientID), Kind: clientresources.LocationKind,
		Reference: locationReference, Capability: capability,
	})
	if err != nil {
		return resourceUpdatePreparedPatch{}, err
	}
	if !isActiveLocation(location) {
		return resourceUpdatePreparedPatch{}, clientresources.ErrLifecycleConflict
	}
	patch.LocationID = stringPointer(location.ID)
	return patch, nil
}

func validResourceLifecyclePrepared(raw json.RawMessage, kind clientresources.Kind, operation resourceLifecycleOperation) (resourceLifecyclePrepared, error) {
	input, err := decode[resourceLifecyclePrepared](raw)
	if err != nil || strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.ClientReference) == "" ||
		strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.ClientDisplayID) == "" ||
		strings.TrimSpace(input.ResourceID) == "" || strings.TrimSpace(input.ResourceReference) == "" ||
		strings.TrimSpace(input.ResourceName) == "" || strings.TrimSpace(input.ResourceDisplayID) == "" ||
		input.CurrentVersion < 1 || strings.TrimSpace(input.Reason) == "" {
		return resourceLifecyclePrepared{}, aiassist.ErrInvalidTool
	}
	if operation == resourceUpdate {
		normalized, err := normalizedDomainPatch(kind, input.Patch)
		if err != nil {
			return resourceLifecyclePrepared{}, aiassist.ErrInvalidTool
		}
		input.Patch = normalized
	} else if patchFieldCount(input.Patch) != 0 {
		return resourceLifecyclePrepared{}, aiassist.ErrInvalidTool
	}
	return input, nil
}

func normalizedDomainPatch(kind clientresources.Kind, patch resourceUpdatePreparedPatch) (resourceUpdatePreparedPatch, error) {
	if patch.LocationReference != nil && patch.LocationID == nil && strings.TrimSpace(*patch.LocationReference) == "" {
		patch.LocationID = stringPointer("")
	}
	domain := domainUpdatePatch(patch)
	// Mirror the domain's closed field sets before execution; the service still
	// performs the authoritative normalization and validation.
	count := patchFieldCount(patch)
	allowed := 0
	switch kind {
	case clientresources.LocationKind:
		allowed = boolInt(patch.Name != nil)
	case clientresources.ContactKind:
		allowed = boolInt(patch.DisplayName != nil) + boolInt(patch.Email != nil) + boolInt(patch.Phone != nil) + boolInt(patch.LocationReference != nil)
	case clientresources.AssetKind:
		allowed = boolInt(patch.Name != nil) + boolInt(patch.AssetType != nil) + boolInt(patch.LocationReference != nil)
	case clientresources.ServiceKind:
		allowed = boolInt(patch.Name != nil) + boolInt(patch.Criticality != nil)
	case clientresources.ContractKind:
		allowed = boolInt(patch.Name != nil) + boolInt(patch.StartsOn != nil) + boolInt(patch.EndsOn != nil) + boolInt(patch.ClearEndsOn)
	}
	if count == 0 || allowed != count || (patch.ClearEndsOn && patch.EndsOn != nil) {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	if domain.Name != nil && strings.TrimSpace(*domain.Name) == "" || domain.DisplayName != nil && strings.TrimSpace(*domain.DisplayName) == "" ||
		domain.AssetType != nil && strings.TrimSpace(*domain.AssetType) == "" {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	if patch.StartsOn != nil && !validDate(*patch.StartsOn) || patch.EndsOn != nil && !validDate(*patch.EndsOn) {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	if patch.Email != nil && !validLifecycleEmail(*patch.Email) ||
		patch.Phone != nil && !validLifecyclePhone(*patch.Phone) ||
		patch.Criticality != nil && strings.TrimSpace(*patch.Criticality) != "" && !validCriticality(*patch.Criticality) {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	if patch.StartsOn != nil && patch.EndsOn != nil && !dateNotBefore(*patch.EndsOn, *patch.StartsOn) {
		return resourceUpdatePreparedPatch{}, aiassist.ErrInvalidTool
	}
	return patch, nil
}

func domainUpdatePatch(patch resourceUpdatePreparedPatch) clientresources.UpdatePatch {
	result := clientresources.UpdatePatch{
		Name: patch.Name, DisplayName: patch.DisplayName, Email: patch.Email, Phone: patch.Phone,
		LocationID: patch.LocationID, AssetType: patch.AssetType, Criticality: patch.Criticality,
		ClearEndsOn: patch.ClearEndsOn,
	}
	if patch.StartsOn != nil {
		value, err := parseDate(*patch.StartsOn)
		if err == nil {
			result.StartsOn = &value
		}
	}
	if patch.EndsOn != nil {
		value, err := parseDate(*patch.EndsOn)
		if err == nil {
			result.EndsOn = &value
		}
	}
	return result
}

func resourceLifecyclePreview(ctx context.Context, directory ActiveClientResolver, catalog ClientResourceCatalog, actions ClientResourceLifecycle, principal authorization.Principal, capability string, kind clientresources.Kind, operation resourceLifecycleOperation, input resourceLifecyclePrepared) (aiassist.Preview, error) {
	client, err := resolveActiveResourceClient(ctx, directory, principal, capability, input.ClientReference)
	if err != nil {
		return aiassist.Preview{}, err
	}
	if client.ID != input.ClientID {
		return aiassist.Preview{}, aiassist.ErrProposalStale
	}
	includeInactive := operation == resourceReactivate
	resolved, err := catalog.ResolveTrusted(ctx, clientresources.TrustedResolveCommand{
		Principal: principal, Target: target(principal, input.ClientID), Kind: kind,
		Reference: input.ResourceReference, Capability: capability, IncludeInactive: includeInactive,
	})
	if err != nil {
		return aiassist.Preview{}, err
	}
	if resolved.ID != input.ResourceID {
		return aiassist.Preview{}, aiassist.ErrProposalStale
	}
	current, err := catalog.GetTrusted(ctx, clientresources.TrustedGetCommand{
		Principal: principal, Target: target(principal, input.ClientID), Kind: kind,
		ID: input.ResourceID, Capability: capability, IncludeInactive: includeInactive,
	})
	if err != nil {
		return aiassist.Preview{}, err
	}
	if current.ID != input.ResourceID {
		return aiassist.Preview{}, aiassist.ErrProposalStale
	}
	if err := validLifecycleTarget(current, kind, operation); err != nil {
		return aiassist.Preview{}, err
	}
	preflight, err := actions.Preflight(ctx, lifecyclePreflightCommand(
		principal, input.ClientID, kind, operation, input.ResourceID, input.CurrentVersion, input.Patch,
	))
	if err != nil {
		return aiassist.Preview{}, err
	}
	changes := map[string]aiassist.Change{
		"client":   {Before: nil, After: map[string]string{"display_id": client.DisplayID, "name": client.Name}},
		"resource": {Before: nil, After: map[string]string{"display_id": current.DisplayID, "name": current.Name}},
		"reason":   {Before: nil, After: input.Reason},
	}
	var locationTarget *aiassist.PreviewTarget
	if operation == resourceUpdate {
		locationTarget, err = appendResourceUpdateChanges(ctx, catalog, principal, capability, input.ClientID, current, input.Patch, changes)
		if err != nil {
			return aiassist.Preview{}, err
		}
		if !hasEffectiveResourceChange(changes) {
			return aiassist.Preview{}, aiassist.ErrInvalidTool
		}
	} else if operation == resourceDeactivate {
		changes["lifecycle_state"] = aiassist.Change{Before: "active", After: "inactive"}
	} else {
		changes["lifecycle_state"] = aiassist.Change{Before: "inactive", After: "active"}
	}
	if preflight.DependencyStatus != "" && preflight.DependencyStatus != "not_applicable" {
		changes["dependency_status"] = aiassist.Change{Before: nil, After: preflight.DependencyStatus}
	}
	if preflight.RelationshipStatus != "" && preflight.RelationshipStatus != "not_applicable" {
		changes["relationship_status"] = aiassist.Change{Before: nil, After: preflight.RelationshipStatus}
	}
	return aiassist.Preview{
		Summary:        strings.Title(string(operation)) + " " + string(kind) + " " + current.DisplayID,
		TargetType:     string(kind),
		TargetID:       current.ID,
		TargetVersion:  current.Version,
		LocationTarget: locationTarget,
		Changes:        changes,
	}, nil
}

func lifecyclePreflightCommand(
	principal authorization.Principal,
	clientID string,
	kind clientresources.Kind,
	operation resourceLifecycleOperation,
	resourceID string,
	version int64,
	patch resourceUpdatePreparedPatch,
) clientresources.LifecyclePreflightCommand {
	preflightOperation := clientresources.PreflightUpdate
	switch operation {
	case resourceDeactivate:
		preflightOperation = clientresources.PreflightDeactivate
	case resourceReactivate:
		preflightOperation = clientresources.PreflightReactivate
	}
	return clientresources.LifecyclePreflightCommand{
		Principal: principal, Target: target(principal, clientID), Kind: kind,
		ResourceID: resourceID, ExpectedVersion: version,
		Patch: domainUpdatePatch(patch), Operation: preflightOperation,
	}
}

func appendResourceUpdateChanges(ctx context.Context, catalog ClientResourceCatalog, principal authorization.Principal, capability, clientID string, current clientresources.ResourceDetail, patch resourceUpdatePreparedPatch, changes map[string]aiassist.Change) (*aiassist.PreviewTarget, error) {
	addStringChange(changes, "name", current.Name, patch.Name)
	addStringChange(changes, "display_name", current.Name, patch.DisplayName)
	addStringChange(changes, "email", current.Email, patch.Email)
	addStringChange(changes, "phone", current.Phone, patch.Phone)
	addStringChange(changes, "asset_type", current.AssetType, patch.AssetType)
	addStringChange(changes, "criticality", current.Criticality, patch.Criticality)
	if patch.StartsOn != nil {
		changes["starts_on"] = aiassist.Change{Before: dateValue(current.StartsOn), After: strings.TrimSpace(*patch.StartsOn)}
	}
	if patch.EndsOn != nil {
		changes["ends_on"] = aiassist.Change{Before: dateValue(current.EndsOn), After: strings.TrimSpace(*patch.EndsOn)}
	} else if patch.ClearEndsOn {
		changes["ends_on"] = aiassist.Change{Before: dateValue(current.EndsOn), After: nil}
	}
	if patch.LocationReference != nil {
		before, err := canonicalLocationDisplayID(ctx, catalog, principal, capability, clientID, current.LocationID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(*patch.LocationReference) == "" {
			changes["location"] = aiassist.Change{Before: before, After: nil}
		} else {
			location, err := catalog.ResolveTrusted(ctx, clientresources.TrustedResolveCommand{
				Principal: principal, Target: target(principal, clientID), Kind: clientresources.LocationKind,
				Reference: *patch.LocationReference, Capability: capability,
			})
			if err != nil {
				return nil, err
			}
			if patch.LocationID == nil || location.ID != *patch.LocationID {
				return nil, aiassist.ErrProposalStale
			}
			if !isActiveLocation(location) {
				return nil, clientresources.ErrLifecycleConflict
			}
			changes["location"] = aiassist.Change{Before: before, After: location.DisplayID}
			return previewLocationTarget(clientID, location), nil
		}
	}
	return nil, nil
}

func canonicalLocationDisplayID(ctx context.Context, catalog ClientResourceCatalog, principal authorization.Principal, capability, clientID, locationID string) (any, error) {
	if locationID == "" {
		return nil, nil
	}
	location, err := catalog.GetTrusted(ctx, clientresources.TrustedGetCommand{
		Principal: principal, Target: target(principal, clientID), Kind: clientresources.LocationKind,
		ID: locationID, Capability: capability,
	})
	if err != nil {
		return nil, err
	}
	if !isActiveLocation(location) {
		return nil, clientresources.ErrLifecycleConflict
	}
	return location.DisplayID, nil
}

func validLifecycleTarget(resource clientresources.ResourceDetail, kind clientresources.Kind, operation resourceLifecycleOperation) error {
	if resource.ID == "" || resource.Kind != string(kind) || resource.Version < 1 {
		return aiassist.ErrInvalidTool
	}
	wantState := "active"
	if operation == resourceReactivate {
		wantState = "inactive"
	}
	if resource.LifecycleState != wantState {
		return clientresources.ErrLifecycleConflict
	}
	if kind == clientresources.AssetKind && resource.Authority != clientresources.TechnicianConfirmed {
		return clientresources.ErrResourceAuthorityConflict
	}
	return nil
}

func lifecycleToolResult(operation resourceLifecycleOperation, kind clientresources.Kind, result clientresources.ResourceDetail, err error) (aiassist.ToolResult, error) {
	if err != nil {
		return aiassist.ToolResult{}, err
	}
	return aiassist.ToolResult{Summary: strings.Title(string(operation)) + "d " + result.DisplayID, Data: map[string]any{
		"resource_type": kind, "resource_id": result.ID, "display_id": result.DisplayID,
		"name": result.Name, "lifecycle_state": result.LifecycleState, "version": result.Version,
	}}, nil
}

func patchFieldCount(p resourceUpdatePreparedPatch) int {
	return boolInt(p.Name != nil) + boolInt(p.DisplayName != nil) + boolInt(p.Email != nil) + boolInt(p.Phone != nil) +
		boolInt(p.LocationReference != nil) + boolInt(p.AssetType != nil) + boolInt(p.Criticality != nil) +
		boolInt(p.StartsOn != nil) + boolInt(p.EndsOn != nil) + boolInt(p.ClearEndsOn)
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func stringPointer(value string) *string { return &value }
func addStringChange(changes map[string]aiassist.Change, key, before string, after *string) {
	if after != nil {
		changes[key] = aiassist.Change{Before: before, After: strings.TrimSpace(*after)}
	}
}
func dateValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format("2006-01-02")
}

func validLifecycleEmail(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validLifecyclePhone(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	digits := 0
	for _, character := range value {
		if unicode.IsDigit(character) {
			digits++
			continue
		}
		if !strings.ContainsRune(" +()-xX.", character) {
			return false
		}
	}
	return digits >= 7 && len(value) <= 32
}

func hasEffectiveResourceChange(changes map[string]aiassist.Change) bool {
	for key, change := range changes {
		switch key {
		case "client", "resource", "reason":
			continue
		}
		if !reflect.DeepEqual(change.Before, change.After) {
			return true
		}
	}
	return false
}
