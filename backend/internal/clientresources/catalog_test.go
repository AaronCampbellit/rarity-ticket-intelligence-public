package clientresources

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type catalogRepositoryStub struct {
	target        scope.Target
	gotID         string
	lifecycle     CatalogLifecycle
	inactiveKinds []Kind
	items         []Summary
}

func (r *catalogRepositoryStub) ListCatalog(
	_ context.Context, target scope.Target, _ int, lifecycle CatalogLifecycle,
	inactiveKinds []Kind,
) ([]Summary, error) {
	r.target = target
	r.lifecycle = lifecycle
	r.inactiveKinds = inactiveKinds
	if r.items != nil {
		return r.items, nil
	}
	return []Summary{{ID: "asset-id", Kind: "asset", Name: "Router", LifecycleState: "active"}}, nil
}

func TestCatalogLifecycleListReturnsOnlyInactiveKindsThePrincipalCanManage(t *testing.T) {
	repository := &catalogRepositoryStub{items: []Summary{
		{ID: "asset-id", Kind: "asset", Name: "Router", LifecycleState: "inactive", Authority: Discovered},
		{ID: "contact-id", Kind: "contact", Name: "Alex", LifecycleState: "inactive"},
		{ID: "location-id", Kind: "location", Name: "Current office", LifecycleState: "active"},
	}}
	service := NewCatalogService(repository).WithActiveTargetAuthorizer(&activeTargetAuthorizerStub{})
	principal := authorization.Principal{
		Scope: scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet(
			"search.read", "asset.lifecycle",
		),
	}
	items, err := service.List(context.Background(), ListCatalogCommand{
		Principal: principal, Limit: 500, Lifecycle: CatalogInactive,
	})
	if err != nil || repository.lifecycle != CatalogInactive || len(items) != 1 ||
		items[0].Kind != "asset" || items[0].Authority != Discovered ||
		len(repository.inactiveKinds) != 1 || repository.inactiveKinds[0] != AssetKind {
		t.Fatalf("List() items=%+v lifecycle=%q error=%v", items, repository.lifecycle, err)
	}
	principal.Capabilities = authorization.NewCapabilitySet("search.read")
	if _, err := service.List(context.Background(), ListCatalogCommand{
		Principal: principal, Limit: 500, Lifecycle: CatalogAll,
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unauthorized lifecycle List() error=%v, want ErrForbidden", err)
	}
}

func (r *catalogRepositoryStub) GetCatalog(_ context.Context, target scope.Target, id string) (Summary, error) {
	r.target, r.gotID = target, id
	return Summary{ID: id, Kind: "asset", Name: "Off-page asset"}, nil
}

func TestCatalogGetsExactAuthorizedRecordWithoutAListWindow(t *testing.T) {
	repository := &catalogRepositoryStub{}
	service := NewCatalogService(repository).WithActiveTargetAuthorizer(&activeTargetAuthorizerStub{})
	principal := authorization.Principal{Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("search.read")}
	found, err := service.GetSummary(context.Background(), GetCatalogCommand{Principal: principal, ID: "asset-id"})
	if err != nil || found.ID != "asset-id" || repository.gotID != "asset-id" {
		t.Fatalf("Get() found=%+v id=%q error=%v", found, repository.gotID, err)
	}
}

func TestCatalogListRequiresScopedSearchRead(t *testing.T) {
	repository := &catalogRepositoryStub{}
	targets := &activeTargetAuthorizerStub{}
	service := NewCatalogService(repository).WithActiveTargetAuthorizer(
		targets,
	)
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("search.read"),
	}
	items, err := service.List(context.Background(), ListCatalogCommand{
		Principal: principal, Limit: 500,
	})
	if err != nil || len(items) != 1 ||
		repository.target.ClientID != "client-id" {
		t.Fatalf("List() items=%+v target=%+v error=%v", items, repository.target, err)
	}
	if targets.calls != 1 || targets.target != (scope.Target{MSPID: "msp-id", ClientID: "client-id"}) {
		t.Fatalf("List() did not resolve active Client: calls=%d target=%+v", targets.calls, targets.target)
	}
	principal.Capabilities = authorization.NewCapabilitySet()
	if _, err := service.List(context.Background(), ListCatalogCommand{
		Principal: principal, Limit: 500,
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unauthorized List() error=%v", err)
	}
}
