package links

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type captureRepository struct {
	mutation CreateMutation
	calls    int
}

func (r *captureRepository) CreateAtomic(_ context.Context, mutation CreateMutation) error {
	r.calls++
	r.mutation = mutation
	return nil
}

func TestCreateRequiresSameClientAndEmitsRelationshipFact(t *testing.T) {
	repository := &captureRepository{}
	ids := []string{"link-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("relationship.create"),
	}
	link, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Source:    Ref{Type: "work_record", ID: "incident-id", MSPID: "msp-id", ClientID: "client-id"},
		Target:    Ref{Type: "asset", ID: "asset-id", MSPID: "msp-id", ClientID: "client-id"},
		LinkType:  "affected_asset", ActorID: "actor-id", RequestSource: "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if link.ID != "link-id" || repository.calls != 1 {
		t.Fatalf("unexpected link: %+v", link)
	}
	if repository.mutation.Event.EventType != "relationship.created" {
		t.Fatal("relationship event not produced")
	}
}

func TestCreateRejectsCrossClientRelationship(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-a"},
		Capabilities: authorization.NewCapabilitySet("relationship.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Source:    Ref{Type: "work_record", ID: "work-id", MSPID: "msp-id", ClientID: "client-a"},
		Target:    Ref{Type: "asset", ID: "asset-id", MSPID: "msp-id", ClientID: "client-b"},
		LinkType:  "affected_asset", ActorID: "actor-id", RequestSource: "api",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v, want ErrNotFound", err)
	}
	if repository.calls != 0 {
		t.Fatal("cross-client link reached repository")
	}
}

func TestCreateRejectsUnregisteredTypeCombination(t *testing.T) {
	repository := &captureRepository{}
	service := NewService(repository, time.Now, func() string { return "unused" })
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("relationship.create"),
	}
	_, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Source:    Ref{Type: "asset", ID: "asset-id", MSPID: "msp-id", ClientID: "client-id"},
		Target:    Ref{Type: "work_record", ID: "work-id", MSPID: "msp-id", ClientID: "client-id"},
		LinkType:  "affected_asset", ActorID: "actor-id", RequestSource: "api",
	})
	if !errors.Is(err, ErrInvalid) || repository.calls != 0 {
		t.Fatalf("unregistered relationship accepted: err=%v calls=%d", err, repository.calls)
	}
}

func TestCreateCanonicalizesSymmetricRelationship(t *testing.T) {
	repository := &captureRepository{}
	ids := []string{"link-id", "audit-id", "event-id", "correlation-id"}
	service := NewService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	principal := authorization.Principal{
		Scope:        scope.Principal{MSPID: "msp-id", ClientID: "client-id"},
		Capabilities: authorization.NewCapabilitySet("relationship.create"),
	}
	link, err := service.Create(context.Background(), CreateCommand{
		Principal: principal,
		Source:    Ref{Type: "work_record", ID: "work-z", MSPID: "msp-id", ClientID: "client-id"},
		Target:    Ref{Type: "work_record", ID: "work-a", MSPID: "msp-id", ClientID: "client-id"},
		LinkType:  "related_to", ActorID: "actor-id", RequestSource: "api",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if link.Source.ID != "work-a" || link.Target.ID != "work-z" {
		t.Fatalf("symmetric relationship was not canonicalized: %+v", link)
	}
}
