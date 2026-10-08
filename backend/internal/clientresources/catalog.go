package clientresources

import (
	"context"
	"errors"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrInvalidCatalog = errors.New("invalid client resource catalog request")

type Summary struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	DisplayID      string    `json:"display_id"`
	Name           string    `json:"name"`
	Detail         string    `json:"detail,omitempty"`
	LocationID     string    `json:"location_id,omitempty"`
	LifecycleState string    `json:"lifecycle_state"`
	Authority      Authority `json:"authority,omitempty"`
	Version        int64     `json:"version"`
}

type CatalogLifecycle string

const (
	CatalogActive   CatalogLifecycle = "active"
	CatalogInactive CatalogLifecycle = "inactive"
	CatalogAll      CatalogLifecycle = "all"
)

type ListCatalogCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	Limit     int
	Lifecycle CatalogLifecycle
}

type GetCatalogCommand struct {
	Principal authorization.Principal
	Target    scope.Target
	ID        string
}

type CatalogRepository interface {
	ListCatalog(context.Context, scope.Target, int, CatalogLifecycle, []Kind) ([]Summary, error)
	GetCatalog(context.Context, scope.Target, string) (Summary, error)
}

func (s *CatalogService) GetSummary(ctx context.Context, command GetCatalogCommand) (Summary, error) {
	target := command.Target
	if target.MSPID == "" {
		target = scope.Target{MSPID: command.Principal.Scope.MSPID, ClientID: command.Principal.Scope.ClientID}
	}
	if s == nil || s.catalog == nil || target.MSPID == "" || target.ClientID == "" || command.ID == "" {
		return Summary{}, ErrInvalidCatalog
	}
	target, err := s.authorizeCatalogTarget(ctx, command.Principal, target, "search.read")
	if err != nil {
		return Summary{}, err
	}
	return s.catalog.GetCatalog(ctx, target, command.ID)
}

type CatalogService struct {
	catalog       CatalogRepository
	query         QueryRepository
	activeTargets ActiveTargetAuthorizer
}

func NewCatalogService(repository any) *CatalogService {
	catalog, _ := repository.(CatalogRepository)
	query, _ := repository.(QueryRepository)
	return &CatalogService{catalog: catalog, query: query}
}

// WithActiveTargetAuthorizer adds the active Client directory boundary shared
// by every Client-targeted catalog operation.
func (s *CatalogService) WithActiveTargetAuthorizer(
	authorizer ActiveTargetAuthorizer,
) *CatalogService {
	if s != nil {
		s.activeTargets = authorizer
	}
	return s
}

func (s *CatalogService) List(
	ctx context.Context,
	command ListCatalogCommand,
) ([]Summary, error) {
	lifecycle := command.Lifecycle
	if lifecycle == "" {
		lifecycle = CatalogActive
	}
	if s == nil || s.catalog == nil || command.Limit < 1 || command.Limit > 500 ||
		(lifecycle != CatalogActive && lifecycle != CatalogInactive && lifecycle != CatalogAll) {
		return nil, ErrInvalidCatalog
	}
	target, err := s.authorizeCatalogTarget(
		ctx, command.Principal, command.Target, "search.read",
	)
	if err != nil {
		return nil, err
	}
	manageable := make(map[string]bool, len(catalogKinds))
	manageableKinds := make([]Kind, 0, len(catalogKinds))
	if lifecycle != CatalogActive {
		for _, kind := range catalogKinds {
			manageable[string(kind)] = authorization.Authorize(
				command.Principal, string(kind)+".lifecycle", target,
			) == nil
			if manageable[string(kind)] {
				manageableKinds = append(manageableKinds, kind)
			}
		}
		if !hasManagedCatalogKind(manageable) {
			return nil, authorization.ErrForbidden
		}
	}
	items, err := s.catalog.ListCatalog(ctx, target, command.Limit, lifecycle, manageableKinds)
	if err != nil {
		return nil, err
	}
	filtered := make([]Summary, 0, len(items))
	for _, item := range items {
		active := item.LifecycleState == "active"
		inactive := item.LifecycleState == "inactive"
		if lifecycle == CatalogActive && active ||
			lifecycle == CatalogInactive && inactive && manageable[item.Kind] ||
			lifecycle == CatalogAll && (active || inactive && manageable[item.Kind]) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

var catalogKinds = []Kind{
	LocationKind, ContactKind, AssetKind, ServiceKind, ContractKind,
}

func hasManagedCatalogKind(kinds map[string]bool) bool {
	for _, allowed := range kinds {
		if allowed {
			return true
		}
	}
	return false
}
