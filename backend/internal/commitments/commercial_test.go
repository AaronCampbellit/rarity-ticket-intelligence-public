package commitments

import (
	"context"
	"errors"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"testing"
	"time"
)

type commercialRepositoryStub struct {
	relationships bool
	mutation      CommercialMutation
	current       CommercialCommitment
}

func (r *commercialRepositoryStub) CommercialRelationshipsBelongToClient(context.Context, scope.Target, CommercialLinks) (bool, error) {
	return r.relationships, nil
}
func (r *commercialRepositoryStub) FindCommercial(context.Context, scope.Target, string) (CommercialCommitment, error) {
	return r.current, nil
}
func (r *commercialRepositoryStub) CreateCommercialAtomic(_ context.Context, m CommercialMutation) error {
	r.mutation = m
	return nil
}
func (r *commercialRepositoryStub) UpdateCommercialAtomic(_ context.Context, m CommercialMutation, _ int64) error {
	r.mutation = m
	return nil
}
func TestCommercialRequiresTypedDatesMoneyAndSameClientLinks(t *testing.T) {
	s := NewCommercialService(&commercialRepositoryStub{relationships: true}, time.Now, commitmentIDs())
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	base := CreateCommercialCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), ClientID: testClientID, Type: License, Title: "M365", Vendor: "Microsoft", OwnerID: testOwnerID, EffectiveOn: start, ExpirationOn: end, QuantityUnits: CommercialQuantityScale, ActorID: testActorID, Source: "api", IdempotencyKey: "commercial-create"}
	base.CostMinor = -1
	if _, err := s.Create(context.Background(), base); !errors.Is(err, ErrInvalidCommercial) {
		t.Fatalf("negative cost=%v", err)
	}
	base.CostMinor = 100
	base.HasCost = true
	if _, err := s.Create(context.Background(), base); !errors.Is(err, ErrInvalidCommercial) {
		t.Fatalf("missing currency=%v", err)
	}
	s = NewCommercialService(&commercialRepositoryStub{relationships: false}, time.Now, commitmentIDs())
	base.Currency = "USD"
	base.ServiceID = testScopeID
	if _, err := s.Create(context.Background(), base); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross client=%v", err)
	}
}

func TestCommercialUpdateCarriesExpectedVersion(t *testing.T) {
	r := &commercialRepositoryStub{relationships: true, current: CommercialCommitment{ID: testRecordID, MSPID: testMSPID, ClientID: testClientID, Type: Renewal, Status: "active", Version: 2}}
	s := NewCommercialService(r, time.Now, commitmentIDs())
	start := time.Now()
	found, err := s.Update(context.Background(), UpdateCommercialCommand{Principal: commitmentPrincipal("calendar.commitment.manage"), CommitmentID: testRecordID, ClientID: testClientID, ExpectedVersion: 2, Type: Renewal, Title: "Renewal", Vendor: "Vendor", OwnerID: testOwnerID, EffectiveOn: start, ExpirationOn: start.AddDate(1, 0, 0), ActorID: testActorID, Source: "api", IdempotencyKey: "commercial-update"})
	if err != nil || found.Version != 3 {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}
