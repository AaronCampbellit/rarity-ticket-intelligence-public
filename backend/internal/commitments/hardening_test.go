package commitments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
)

func TestMaintenanceSupportsAllDayRecurrenceAndRejectsDuplicateScopes(t *testing.T) {
	r := &maintenanceRepositoryStub{resourcesBelongToMSP: true}
	s := NewMaintenanceService(r, time.Now, commitmentIDs())
	d := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Create(context.Background(), CreateMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), Title: "Patch", AllDay: true, StartsOn: &d, EndsOn: &d, Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Daily, Interval: 1, Count: 2}, Scopes: []ScopeRef{{Type: ScopeClient, ID: testClientID}}, ActorID: testActorID, Source: "api", IdempotencyKey: "maintenance-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(context.Background(), CreateMaintenanceCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), Title: "Patch", StartsAt: d, EndsAt: d.Add(time.Hour), Timezone: "UTC", Scopes: []ScopeRef{{Type: ScopeClient, ID: testClientID}, {Type: ScopeClient, ID: testClientID}}, ActorID: testActorID, Source: "api", IdempotencyKey: "maintenance-2"})
	if !errors.Is(err, ErrDuplicateMaintenanceScope) {
		t.Fatalf("error=%v", err)
	}
}

func TestCommercialQuantityUsesExactScaledUnits(t *testing.T) {
	r := &commercialRepositoryStub{relationships: true}
	s := NewCommercialService(r, time.Now, commitmentIDs())
	start := time.Now()
	found, err := s.Create(context.Background(), CreateCommercialCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), ClientID: testClientID, Type: License, Title: "License", Vendor: "Vendor", OwnerID: testOwnerID, EffectiveOn: start, ExpirationOn: start.AddDate(1, 0, 0), QuantityUnits: 9_000_000_000_000_001, ActorID: testActorID, Source: "api", IdempotencyKey: "commercial-1"})
	if err != nil || found.QuantityUnits != 9_000_000_000_000_001 {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}

func TestCommercialTransitionStateMachine(t *testing.T) {
	r := &commercialRepositoryStub{relationships: true, current: CommercialCommitment{ID: testRecordID, MSPID: testMSPID, ClientID: testClientID, Type: Renewal, Status: "active", Version: 1}}
	s := NewCommercialService(r, time.Now, commitmentIDs())
	_, err := s.Transition(context.Background(), TransitionCommercialCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), CommitmentID: testRecordID, ClientID: testClientID, ExpectedVersion: 1, ToStatus: "cancelled", ActorID: testActorID, Source: "api", IdempotencyKey: "transition-1"})
	if err != nil {
		t.Fatal(err)
	}
	r.current.Status = "cancelled"
	_, err = s.Transition(context.Background(), TransitionCommercialCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), CommitmentID: testRecordID, ClientID: testClientID, ExpectedVersion: 1, ToStatus: "active", ActorID: testActorID, Source: "api", IdempotencyKey: "transition-2"})
	if !errors.Is(err, ErrInvalidCommercialTransition) {
		t.Fatalf("error=%v", err)
	}
}
