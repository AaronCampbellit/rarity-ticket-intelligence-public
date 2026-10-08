package commitments

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const (
	testActorID  = "00000000-0000-4000-8000-000000000001"
	testMSPID    = "00000000-0000-4000-8000-000000000002"
	testClientID = "00000000-0000-4000-8000-000000000003"
	testOwnerID  = "00000000-0000-4000-8000-000000000004"
	testRecordID = "00000000-0000-4000-8000-000000000005"
	testScopeID  = "00000000-0000-4000-8000-000000000006"
)

type maintenanceRepositoryStub struct {
	resourcesBelongToMSP bool
	mutation             MaintenanceMutation
	current              MaintenanceWindow
	transitioned         bool
	updated              bool
}

func (r *maintenanceRepositoryStub) FindMaintenance(context.Context, string, string) (MaintenanceWindow, error) {
	return r.current, nil
}

func (r *maintenanceRepositoryStub) ResolveMaintenanceScopes(_ context.Context, _ string, refs []ScopeRef) ([]ResolvedScope, error) {
	if !r.resourcesBelongToMSP {
		return nil, scope.ErrNotFound
	}
	out := make([]ResolvedScope, 0, len(refs))
	for _, v := range refs {
		out = append(out, ResolvedScope{ClientID: v.ID, Type: v.Type, ResourceID: v.ID})
	}
	return out, nil
}
func (r *maintenanceRepositoryStub) CreateMaintenanceAtomic(_ context.Context, m MaintenanceMutation) error {
	r.mutation = m
	return nil
}
func (r *maintenanceRepositoryStub) UpdateMaintenanceAtomic(_ context.Context, m MaintenanceMutation, _ int64) error {
	r.mutation = m
	r.updated = true
	return nil
}
func (r *maintenanceRepositoryStub) TransitionMaintenanceAtomic(_ context.Context, m MaintenanceMutation, _ int64) error {
	r.mutation = m
	r.transitioned = true
	return nil
}
func commitmentPrincipal(cap string) authorization.Principal {
	return authorization.Principal{ID: testActorID, Scope: scope.Principal{MSPID: testMSPID}, Capabilities: authorization.NewCapabilitySet(cap)}
}
func commitmentIDs() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("00000000-0000-4000-8001-%012d", n) }
}

func TestMaintenanceTransitionPreservesScopesAndHardBlockPolicy(t *testing.T) {
	scopes := []ResolvedScope{{ID: "00000000-0000-4000-8000-000000000011", ClientID: "00000000-0000-4000-8000-000000000012", Type: ScopeAsset, ResourceID: "00000000-0000-4000-8000-000000000013"}}
	r := &maintenanceRepositoryStub{current: MaintenanceWindow{ID: "00000000-0000-4000-8000-000000000014", MSPID: "00000000-0000-4000-8000-000000000002", Status: "planned", Version: 1, Protected: true, ConflictPolicy: PolicyHardBlock, Scopes: scopes, CreatedBy: "00000000-0000-4000-8000-000000000001"}}
	s := NewMaintenanceService(r, time.Now, commitmentIDs())
	found, err := s.Transition(context.Background(), TransitionMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), WindowID: r.current.ID, ExpectedVersion: 1, ToStatus: "active", ActorID: testActorID, Source: "api", IdempotencyKey: "transition-preserves-scopes"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.transitioned || r.updated || len(found.Scopes) != 1 || found.Scopes[0] != scopes[0] || found.ConflictPolicy != PolicyHardBlock || !found.Protected {
		t.Fatalf("found=%+v mutation=%+v transitioned=%v updated=%v", found, r.mutation.Window, r.transitioned, r.updated)
	}
}

func TestMaintenanceRejectsCrossMSPScopes(t *testing.T) {
	s := NewMaintenanceService(&maintenanceRepositoryStub{resourcesBelongToMSP: false}, time.Now, commitmentIDs())
	_, err := s.Create(context.Background(), CreateMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), Title: "Core upgrade", StartsAt: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), Timezone: "UTC", Scopes: []ScopeRef{{Type: ScopeAsset, ID: testScopeID}}, ActorID: testActorID, Source: "api", IdempotencyKey: "cross"})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}
func TestMaintenanceRequiresTimedEndAndCanonicalPolicy(t *testing.T) {
	s := NewMaintenanceService(&maintenanceRepositoryStub{resourcesBelongToMSP: true}, time.Now, commitmentIDs())
	base := CreateMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), Title: "x", StartsAt: time.Now(), Timezone: "UTC", Scopes: []ScopeRef{{Type: ScopeClient, ID: testClientID}}, ActorID: testActorID, Source: "api", IdempotencyKey: "invalid"}
	if _, err := s.Create(context.Background(), base); !errors.Is(err, ErrInvalidMaintenance) {
		t.Fatalf("missing end error=%v", err)
	}
	base.EndsAt = base.StartsAt.Add(time.Hour)
	base.ConflictPolicy = "anything"
	if _, err := s.Create(context.Background(), base); !errors.Is(err, ErrInvalidMaintenance) {
		t.Fatalf("policy error=%v", err)
	}
}

func TestMaintenanceUpdateCarriesExpectedVersion(t *testing.T) {
	r := &maintenanceRepositoryStub{resourcesBelongToMSP: true, current: MaintenanceWindow{ID: testRecordID, MSPID: testMSPID, Status: "planned", Version: 4}}
	r.ResolveMaintenanceScopes(context.Background(), "", nil)
	s := NewMaintenanceService(r, time.Now, commitmentIDs())
	_, err := s.Update(context.Background(), UpdateMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), WindowID: testRecordID, ExpectedVersion: 4, Title: "Updated", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), Timezone: "UTC", Scopes: []ScopeRef{{Type: ScopeClient, ID: testClientID}}, ActorID: testActorID, Source: "api", IdempotencyKey: "update"})
	if err == nil && r.mutation.Window.Version != 5 {
		t.Fatalf("version=%d", r.mutation.Window.Version)
	}
}
