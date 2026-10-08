package clientresources

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const maxCatalogQueryLimit = 500

var ErrAmbiguousResource = errors.New("ambiguous client resource")

type Kind string

const (
	LocationKind Kind = "location"
	ContactKind  Kind = "contact"
	AssetKind    Kind = "asset"
	ServiceKind  Kind = "service"
	ContractKind Kind = "contract"
)

type Query struct {
	Kind            Kind
	Literal         string
	Limit           int
	IncludeInactive bool
}

type ResourceDetail struct {
	Summary
	Email        string     `json:"email,omitempty"`
	Phone        string     `json:"phone,omitempty"`
	AssetType    string     `json:"asset_type,omitempty"`
	SourceSystem string     `json:"source_system,omitempty"`
	ExternalID   string     `json:"external_id,omitempty"`
	Criticality  string     `json:"criticality,omitempty"`
	StartsOn     *time.Time `json:"starts_on,omitempty"`
	EndsOn       *time.Time `json:"ends_on,omitempty"`
}

type QueryRepository interface {
	QueryResources(context.Context, scope.Target, Query) ([]ResourceDetail, error)
	ResolveResource(context.Context, scope.Target, Kind, string, bool) ([]ResourceDetail, error)
	GetResource(context.Context, scope.Target, Kind, string, bool) (ResourceDetail, error)
}

// ActiveTargetAuthorizer verifies that a Client target is active and visible
// to the principal without granting a separate directory-read capability.
type ActiveTargetAuthorizer interface {
	AuthorizeExecutionTarget(context.Context, authorization.Principal, scope.Target) error
}

type QueryCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Query     Query
}

type ResolveCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Kind      Kind
	Reference string
}

type GetCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Kind      Kind
	ID        string
}

// LifecycleQueryCommand is a trusted internal boundary for lifecycle tables.
// Callers cannot use Query to broaden an ordinary active-resource list.
type LifecycleQueryCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Query     Query
}

// LifecycleGetCommand is a trusted internal boundary for exact inactive
// lifecycle reads such as Reactivate.
type LifecycleGetCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Kind      Kind
	ID        string
}

// TrustedResolveCommand is an adapter-only exact-reference boundary. The
// caller supplies a constructor-owned capability; public/model input never
// controls Capability or IncludeInactive.
type TrustedResolveCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Kind            Kind
	Reference       string
	Capability      string
	IncludeInactive bool
}

// TrustedGetCommand is the stable-ID counterpart used when rebuilding an AI
// preview at confirmation time.
type TrustedGetCommand struct {
	Principal       authorization.Principal
	Target          scope.Target
	Kind            Kind
	ID              string
	Capability      string
	IncludeInactive bool
}

func (s *CatalogService) Query(
	ctx context.Context,
	command QueryCommand,
) ([]ResourceDetail, error) {
	if command.Query.IncludeInactive {
		return nil, ErrInvalidCatalog
	}
	return s.queryResources(
		ctx, command.Principal, command.Target, command.Query, "search.read",
	)
}

func (s *CatalogService) QueryForLifecycle(
	ctx context.Context,
	command LifecycleQueryCommand,
) ([]ResourceDetail, error) {
	query := command.Query
	query.IncludeInactive = true
	return s.queryResources(
		ctx, command.Principal, command.Target, query, lifecycleCapability(query.Kind),
	)
}

func (s *CatalogService) Resolve(
	ctx context.Context,
	command ResolveCommand,
) (ResourceDetail, error) {
	reference := strings.TrimSpace(command.Reference)
	if reference == "" || !validKind(command.Kind) {
		return ResourceDetail{}, ErrInvalidCatalog
	}
	results, err := s.resolveResource(
		ctx, command.Principal, command.Target, command.Kind, reference, false,
		"search.read",
	)
	if err != nil {
		return ResourceDetail{}, err
	}
	return resolveExactResource(results, reference)
}

func (s *CatalogService) ResolveTrusted(
	ctx context.Context,
	command TrustedResolveCommand,
) (ResourceDetail, error) {
	reference := strings.TrimSpace(command.Reference)
	if reference == "" || !validKind(command.Kind) ||
		!validTrustedResourceCapability(command.Capability) ||
		(command.IncludeInactive && !strings.HasSuffix(command.Capability, ".lifecycle")) {
		return ResourceDetail{}, ErrInvalidCatalog
	}
	results, err := s.resolveResource(
		ctx, command.Principal, command.Target, command.Kind, reference,
		command.IncludeInactive, command.Capability,
	)
	if err != nil {
		return ResourceDetail{}, err
	}
	return resolveExactResource(results, reference)
}

func (s *CatalogService) resolveResource(
	ctx context.Context,
	principal authorization.Principal,
	requested scope.Target,
	kind Kind,
	reference string,
	includeInactive bool,
	capability string,
) ([]ResourceDetail, error) {
	if s == nil || s.query == nil || !validKind(kind) ||
		strings.TrimSpace(reference) == "" {
		return nil, ErrInvalidCatalog
	}
	target, err := s.authorizeCatalogTarget(ctx, principal, requested, capability)
	if err != nil {
		return nil, err
	}
	return s.query.ResolveResource(
		ctx, target, kind, strings.TrimSpace(reference), includeInactive,
	)
}

func resolveExactResource(results []ResourceDetail, reference string) (ResourceDetail, error) {
	if byDisplayID := exactDisplayID(results, reference); len(byDisplayID) == 1 {
		return byDisplayID[0], nil
	} else if len(byDisplayID) > 1 {
		return ResourceDetail{}, ErrAmbiguousResource
	}
	byName := normalizedExactName(results, reference)
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		return ResourceDetail{}, ErrAmbiguousResource
	}
	return ResourceDetail{}, scope.ErrNotFound
}

func (s *CatalogService) Get(
	ctx context.Context,
	command GetCommand,
) (ResourceDetail, error) {
	return s.getResource(
		ctx, command.Principal, command.Target, command.Kind, command.ID, false, "search.read",
	)
}

func (s *CatalogService) GetForLifecycle(
	ctx context.Context,
	command LifecycleGetCommand,
) (ResourceDetail, error) {
	return s.getResource(
		ctx, command.Principal, command.Target, command.Kind, command.ID, true,
		lifecycleCapability(command.Kind),
	)
}

func (s *CatalogService) GetTrusted(
	ctx context.Context,
	command TrustedGetCommand,
) (ResourceDetail, error) {
	if !validTrustedResourceCapability(command.Capability) ||
		(command.IncludeInactive && !strings.HasSuffix(command.Capability, ".lifecycle")) {
		return ResourceDetail{}, ErrInvalidCatalog
	}
	return s.getResource(ctx, command.Principal, command.Target, command.Kind,
		command.ID, command.IncludeInactive, command.Capability)
}

func (s *CatalogService) queryResources(
	ctx context.Context,
	principal authorization.Principal,
	requested scope.Target,
	query Query,
	capability string,
) ([]ResourceDetail, error) {
	if s == nil || s.query == nil || !validKind(query.Kind) ||
		query.Limit < 1 || query.Limit > maxCatalogQueryLimit {
		return nil, ErrInvalidCatalog
	}
	target, err := s.authorizeCatalogTarget(ctx, principal, requested, capability)
	if err != nil {
		return nil, err
	}
	query.Literal = strings.TrimSpace(query.Literal)
	return s.query.QueryResources(ctx, target, query)
}

func (s *CatalogService) getResource(
	ctx context.Context,
	principal authorization.Principal,
	requested scope.Target,
	kind Kind,
	id string,
	includeInactive bool,
	capability string,
) (ResourceDetail, error) {
	if s == nil || s.query == nil || !validKind(kind) || strings.TrimSpace(id) == "" {
		return ResourceDetail{}, ErrInvalidCatalog
	}
	target, err := s.authorizeCatalogTarget(ctx, principal, requested, capability)
	if err != nil {
		return ResourceDetail{}, err
	}
	return s.query.GetResource(ctx, target, kind, strings.TrimSpace(id), includeInactive)
}

func (s *CatalogService) authorizeCatalogTarget(
	ctx context.Context,
	principal authorization.Principal,
	requested scope.Target,
	capability string,
) (scope.Target, error) {
	target := requested
	if target.MSPID == "" {
		target = scope.Target{
			MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
		}
	}
	if s == nil || s.activeTargets == nil || target.ClientID == "" {
		return scope.Target{}, ErrInvalidCatalog
	}
	if err := authorization.Authorize(principal, capability, target); err != nil {
		return scope.Target{}, err
	}
	if err := s.activeTargets.AuthorizeExecutionTarget(ctx, principal, target); err != nil {
		return scope.Target{}, err
	}
	return target, nil
}

func lifecycleCapability(kind Kind) string {
	return string(kind) + ".lifecycle"
}

func validTrustedResourceCapability(capability string) bool {
	parts := strings.Split(capability, ".")
	if len(parts) != 2 || !validKind(Kind(parts[0])) {
		return false
	}
	switch parts[1] {
	case "create", "update", "lifecycle":
		return true
	default:
		return false
	}
}

func validKind(kind Kind) bool {
	switch kind {
	case LocationKind, ContactKind, AssetKind, ServiceKind, ContractKind:
		return true
	default:
		return false
	}
}

func exactDisplayID(resources []ResourceDetail, reference string) []ResourceDetail {
	reference = strings.TrimSpace(reference)
	matching := make([]ResourceDetail, 0, 1)
	for _, resource := range resources {
		if resource.DisplayID == reference {
			matching = append(matching, resource)
		}
	}
	return matching
}

func normalizedExactName(resources []ResourceDetail, reference string) []ResourceDetail {
	normalizedReference := normalizeResourceName(reference)
	matching := make([]ResourceDetail, 0, 1)
	for _, resource := range resources {
		if normalizeResourceName(resource.Name) == normalizedReference {
			matching = append(matching, resource)
		}
	}
	return matching
}

func normalizeResourceName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
