package clientresources

import (
	"context"
	"errors"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type queryRepositoryStub struct {
	results            []ResourceDetail
	resolveResults     []ResourceDetail
	result             ResourceDetail
	queryTarget        scope.Target
	query              Query
	getTarget          scope.Target
	getKind            Kind
	getID              string
	getIncludeInactive bool
	queryCalls         int
	resolveCalls       int
	getCalls           int
}

type activeTargetAuthorizerStub struct {
	err    error
	target scope.Target
	calls  int
}

func (s *activeTargetAuthorizerStub) AuthorizeExecutionTarget(
	_ context.Context,
	_ authorization.Principal,
	target scope.Target,
) error {
	s.calls++
	s.target = target
	return s.err
}

func newQueryCatalogService(
	repository *queryRepositoryStub,
) (*CatalogService, *activeTargetAuthorizerStub) {
	targets := &activeTargetAuthorizerStub{}
	return NewCatalogService(repository).WithActiveTargetAuthorizer(targets), targets
}

func (r *queryRepositoryStub) QueryResources(
	_ context.Context,
	target scope.Target,
	query Query,
) ([]ResourceDetail, error) {
	r.queryCalls++
	r.queryTarget, r.query = target, query
	return r.results, nil
}

func (r *queryRepositoryStub) ResolveResource(
	_ context.Context,
	target scope.Target,
	kind Kind,
	reference string,
	includeInactive bool,
) ([]ResourceDetail, error) {
	r.resolveCalls++
	r.queryTarget = target
	r.query = Query{
		Kind: kind, Literal: reference, Limit: 2,
		IncludeInactive: includeInactive,
	}
	if r.resolveResults == nil {
		return r.results, nil
	}
	return r.resolveResults, nil
}

func (r *queryRepositoryStub) GetResource(
	_ context.Context,
	target scope.Target,
	kind Kind,
	id string,
	includeInactive bool,
) (ResourceDetail, error) {
	r.getCalls++
	r.getTarget, r.getKind, r.getID = target, kind, id
	r.getIncludeInactive = includeInactive
	return r.result, nil
}

func activePrincipal() authorization.Principal {
	return authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "northwind-id"},
		Capabilities: authorization.NewCapabilitySet("search.read"),
	}
}

func northwind() scope.Target {
	return scope.Target{MSPID: "msp-id", ClientID: "northwind-id"}
}

func TestCatalogQueryValidatesKindAndBounds(t *testing.T) {
	service, _ := newQueryCatalogService(&queryRepositoryStub{})
	for _, query := range []Query{
		{Kind: "other", Limit: 1},
		{Kind: LocationKind, Limit: 0},
		{Kind: LocationKind, Limit: 501},
	} {
		if _, err := service.Query(context.Background(), QueryCommand{
			Principal: activePrincipal(), Target: northwind(), Query: query,
		}); !errors.Is(err, ErrInvalidCatalog) {
			t.Fatalf("Query(%+v) error = %v, want ErrInvalidCatalog", query, err)
		}
	}
}

func TestCatalogQueryUsesAuthorizedClientAndActiveOnlyByDefault(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{{
		Summary: Summary{ID: "location-id", Kind: string(LocationKind), DisplayID: "LOC-1", Name: "Head Office", LifecycleState: "active"},
	}}}
	service, targets := newQueryCatalogService(repository)
	items, err := service.Query(context.Background(), QueryCommand{
		Principal: activePrincipal(), Target: northwind(),
		Query: Query{Kind: LocationKind, Literal: "  head office  ", Limit: 50},
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("Query() items=%+v error=%v", items, err)
	}
	if repository.queryTarget != northwind() ||
		repository.query.Literal != "head office" ||
		repository.query.IncludeInactive || repository.query.Limit != 50 {
		t.Fatalf("Query() escaped active Client query contract: target=%+v query=%+v", repository.queryTarget, repository.query)
	}
	if targets.calls != 1 || targets.target != northwind() {
		t.Fatalf("Query() did not resolve active target: calls=%d target=%+v", targets.calls, targets.target)
	}
}

func TestCatalogResolveUsesDedicatedExactRepositoryBoundary(t *testing.T) {
	repository := &queryRepositoryStub{resolveResults: []ResourceDetail{{
		Summary: Summary{
			ID: "location-id", Kind: string(LocationKind),
			DisplayID: "LOC-501", Name: "North Warehouse",
			LifecycleState: "active",
		},
	}}}
	service, _ := newQueryCatalogService(repository)

	item, err := service.Resolve(context.Background(), ResolveCommand{
		Principal: activePrincipal(), Target: northwind(),
		Kind: LocationKind, Reference: " LOC-501 ",
	})
	if err != nil || item.ID != "location-id" {
		t.Fatalf("Resolve() item=%+v error=%v", item, err)
	}
	if repository.resolveCalls != 1 || repository.queryCalls != 0 ||
		repository.query.Literal != "LOC-501" ||
		repository.query.IncludeInactive {
		t.Fatalf("Resolve() used broad query boundary: repository=%+v", repository)
	}
}

func TestCatalogGetOnlyLoadsActiveResources(t *testing.T) {
	repository := &queryRepositoryStub{result: ResourceDetail{
		Summary: Summary{ID: "location-id", Kind: string(LocationKind), DisplayID: "LOC-1", Name: "Old Office", LifecycleState: "active"},
	}}
	service, _ := newQueryCatalogService(repository)
	item, err := service.Get(context.Background(), GetCommand{
		Principal: activePrincipal(), Target: northwind(), Kind: LocationKind,
		ID: " location-id ",
	})
	if err != nil || item.LifecycleState != "active" {
		t.Fatalf("Get() item=%+v error=%v", item, err)
	}
	if repository.getTarget != northwind() || repository.getKind != LocationKind ||
		repository.getID != "location-id" || repository.getIncludeInactive {
		t.Fatalf("Get() escaped exact active contract: target=%+v kind=%q id=%q includeInactive=%v", repository.getTarget, repository.getKind, repository.getID, repository.getIncludeInactive)
	}
}

func TestCatalogLifecycleQueriesRequireLifecycleCapability(t *testing.T) {
	repository := &queryRepositoryStub{result: ResourceDetail{
		Summary: Summary{ID: "location-id", Kind: string(LocationKind), DisplayID: "LOC-1", Name: "Old Office", LifecycleState: "inactive"},
	}}
	service, _ := newQueryCatalogService(repository)
	command := LifecycleGetCommand{
		Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, ID: "location-id",
	}
	if _, err := service.GetForLifecycle(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("GetForLifecycle() error=%v, want ErrForbidden", err)
	}
	if repository.getCalls != 0 {
		t.Fatalf("unauthorized lifecycle Get reached repository %d times", repository.getCalls)
	}
	command.Principal.Capabilities = authorization.NewCapabilitySet("search.read", "location.lifecycle")
	item, err := service.GetForLifecycle(context.Background(), command)
	if err != nil || item.LifecycleState != "inactive" || !repository.getIncludeInactive {
		t.Fatalf("GetForLifecycle() item=%+v query=%+v error=%v", item, repository, err)
	}
}

func TestCatalogLifecycleListSetsInactiveFilterInsideTrustedBoundary(t *testing.T) {
	repository := &queryRepositoryStub{}
	service, _ := newQueryCatalogService(repository)
	command := LifecycleQueryCommand{
		Principal: activePrincipal(), Target: northwind(),
		Query: Query{Kind: LocationKind, Literal: "Old", Limit: 25, IncludeInactive: false},
	}
	if _, err := service.QueryForLifecycle(context.Background(), command); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("QueryForLifecycle() error=%v, want ErrForbidden", err)
	}
	if repository.queryCalls != 0 {
		t.Fatalf("unauthorized lifecycle query reached repository %d times", repository.queryCalls)
	}
	command.Principal.Capabilities = authorization.NewCapabilitySet("search.read", "location.lifecycle")
	if _, err := service.QueryForLifecycle(context.Background(), command); err != nil {
		t.Fatalf("QueryForLifecycle() error=%v", err)
	}
	if !repository.query.IncludeInactive {
		t.Fatalf("QueryForLifecycle() did not apply trusted inactive filter: %+v", repository.query)
	}
}

func TestClientResourceLifecycleTrustedResolverUsesAdvertisedCapabilityWithoutSearchRead(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{{
		Summary: Summary{ID: "location-id", Kind: "location", DisplayID: "LOC-1", Name: "Head Office", Version: 3, LifecycleState: "active"},
	}}}
	service, _ := newQueryCatalogService(repository)
	principal := activePrincipal()
	principal.Capabilities = authorization.NewCapabilitySet("contact.update")
	item, err := service.ResolveTrusted(context.Background(), TrustedResolveCommand{
		Principal: principal, Target: northwind(), Kind: LocationKind,
		Reference: "LOC-1", Capability: "contact.update",
	})
	if err != nil || item.ID != "location-id" || repository.query.IncludeInactive {
		t.Fatalf("ResolveTrusted() item=%+v query=%+v error=%v", item, repository.query, err)
	}
	principal.Capabilities = authorization.NewCapabilitySet("search.read")
	if _, err := service.ResolveTrusted(context.Background(), TrustedResolveCommand{
		Principal: principal, Target: northwind(), Kind: LocationKind,
		Reference: "LOC-1", Capability: "contact.update",
	}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("ResolveTrusted() error=%v, want advertised capability denial", err)
	}
}

func TestClientResourceReactivateTrustedResolverCanResolveExactInactiveTarget(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{{
		Summary: Summary{ID: "service-id", Kind: "service", DisplayID: "SVC-1", Name: "Managed Service", Version: 4, LifecycleState: "inactive"},
	}}}
	service, _ := newQueryCatalogService(repository)
	principal := activePrincipal()
	principal.Capabilities = authorization.NewCapabilitySet("service.lifecycle")
	item, err := service.ResolveTrusted(context.Background(), TrustedResolveCommand{
		Principal: principal, Target: northwind(), Kind: ServiceKind,
		Reference: "SVC-1", Capability: "service.lifecycle", IncludeInactive: true,
	})
	if err != nil || item.ID != "service-id" || !repository.query.IncludeInactive {
		t.Fatalf("ResolveTrusted() item=%+v query=%+v error=%v", item, repository.query, err)
	}
}

func TestClientResourceTrustedResolverRejectsUnapprovedCapability(t *testing.T) {
	repository := &queryRepositoryStub{}
	service, _ := newQueryCatalogService(repository)
	principal := activePrincipal()
	principal.Capabilities = authorization.NewCapabilitySet("role.manage")
	if _, err := service.GetTrusted(context.Background(), TrustedGetCommand{
		Principal: principal, Target: northwind(), Kind: LocationKind,
		ID: "location-id", Capability: "role.manage",
	}); !errors.Is(err, ErrInvalidCatalog) || repository.getCalls != 0 {
		t.Fatalf("GetTrusted() error=%v calls=%d", err, repository.getCalls)
	}
}

func TestCatalogResolvePrefersExactDisplayID(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{
		{Summary: Summary{ID: "display-id", DisplayID: "LOC-1", Name: "Remote Office"}},
		{Summary: Summary{ID: "name-id", DisplayID: "LOC-2", Name: "LOC-1"}},
	}}
	service, _ := newQueryCatalogService(repository)
	item, err := service.Resolve(context.Background(), ResolveCommand{
		Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, Reference: " LOC-1 ",
	})
	if err != nil || item.ID != "display-id" {
		t.Fatalf("Resolve() item=%+v error=%v", item, err)
	}
	if repository.resolveCalls != 1 || repository.queryCalls != 0 ||
		repository.query.IncludeInactive || repository.query.Limit != 2 {
		t.Fatalf("Resolve() did not use exact active bounded query: %+v", repository)
	}
}

func TestCatalogResolveNormalizesExactName(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{{
		Summary: Summary{ID: "location-id", DisplayID: "LOC-1", Name: "Head   Office"},
	}}}
	service, _ := newQueryCatalogService(repository)
	item, err := service.Resolve(context.Background(), ResolveCommand{
		Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, Reference: "  head office ",
	})
	if err != nil || item.ID != "location-id" {
		t.Fatalf("Resolve() item=%+v error=%v", item, err)
	}
}

func TestCatalogResolveRequiresUniqueExactReference(t *testing.T) {
	repository := &queryRepositoryStub{results: []ResourceDetail{
		{Summary: Summary{ID: "a", DisplayID: "LOC-A", Name: "Head Office"}},
		{Summary: Summary{ID: "b", DisplayID: "LOC-B", Name: "Head Office"}},
	}}
	service, _ := newQueryCatalogService(repository)
	_, err := service.Resolve(
		context.Background(),
		ResolveCommand{Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, Reference: "Head Office"},
	)
	if !errors.Is(err, ErrAmbiguousResource) {
		t.Fatalf("error = %v, want ErrAmbiguousResource", err)
	}
}

func TestCatalogResolveReturnsNotFoundForMissingReference(t *testing.T) {
	service, _ := newQueryCatalogService(&queryRepositoryStub{})
	_, err := service.Resolve(context.Background(), ResolveCommand{
		Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, Reference: "Missing",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrNotFound", err)
	}
}

func TestCatalogQueryRejectsCrossClientTargetBeforeRepositoryLookup(t *testing.T) {
	repository := &queryRepositoryStub{}
	service, targets := newQueryCatalogService(repository)
	targets.err = scope.ErrNotFound
	_, err := service.Query(context.Background(), QueryCommand{
		Principal: activePrincipal(),
		Target:    scope.Target{MSPID: "msp-id", ClientID: "other-client-id"},
		Query:     Query{Kind: LocationKind, Limit: 1},
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Query() error = %v, want ErrNotFound", err)
	}
	if repository.queryCalls != 0 {
		t.Fatalf("cross-Client Query() reached repository %d times", repository.queryCalls)
	}
}

func TestCatalogReadsRequireActiveTargetBeforeRepositoryAccess(t *testing.T) {
	for name, read := range map[string]func(*CatalogService) error{
		"query": func(service *CatalogService) error {
			_, err := service.Query(context.Background(), QueryCommand{
				Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("search.read")},
				Target:    northwind(), Query: Query{Kind: LocationKind, Limit: 1},
			})
			return err
		},
		"resolve": func(service *CatalogService) error {
			_, err := service.Resolve(context.Background(), ResolveCommand{
				Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("search.read")},
				Target:    northwind(), Kind: LocationKind, Reference: "Head Office",
			})
			return err
		},
		"get": func(service *CatalogService) error {
			_, err := service.Get(context.Background(), GetCommand{
				Principal: authorization.Principal{Scope: scope.Principal{MSPID: "msp-id"}, Capabilities: authorization.NewCapabilitySet("search.read")},
				Target:    northwind(), Kind: LocationKind, ID: "location-id",
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &queryRepositoryStub{}
			service, targets := newQueryCatalogService(repository)
			targets.err = scope.ErrNotFound
			if err := read(service); !errors.Is(err, scope.ErrNotFound) {
				t.Fatalf("%s error=%v, want ErrNotFound", name, err)
			}
			if repository.queryCalls != 0 || repository.getCalls != 0 || targets.calls != 1 {
				t.Fatalf("%s bypassed active target boundary: repository=%+v targets=%+v", name, repository, targets)
			}
		})
	}
}

func TestCatalogQueryRejectsCallerControlledInactiveFilter(t *testing.T) {
	repository := &queryRepositoryStub{}
	service, _ := newQueryCatalogService(repository)
	_, err := service.Query(context.Background(), QueryCommand{
		Principal: activePrincipal(), Target: northwind(),
		Query: Query{Kind: LocationKind, Limit: 1, IncludeInactive: true},
	})
	if !errors.Is(err, ErrInvalidCatalog) || repository.queryCalls != 0 {
		t.Fatalf("Query() error=%v calls=%d, want invalid without repository access", err, repository.queryCalls)
	}
}

func TestCatalogQueryFailsClosedWithoutActiveTargetAuthorizer(t *testing.T) {
	repository := &queryRepositoryStub{}
	_, err := NewCatalogService(repository).Query(context.Background(), QueryCommand{
		Principal: activePrincipal(), Target: northwind(),
		Query: Query{Kind: LocationKind, Limit: 1},
	})
	if !errors.Is(err, ErrInvalidCatalog) || repository.queryCalls != 0 {
		t.Fatalf("Query() error=%v calls=%d, want invalid without repository access", err, repository.queryCalls)
	}
}
