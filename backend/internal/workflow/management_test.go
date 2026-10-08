package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type managementRepository struct {
	current  []Published
	mutation PublishMutation
}

func (r *managementRepository) ListPublished(
	context.Context,
	scope.Target,
) ([]Published, error) {
	return append([]Published(nil), r.current...), nil
}

func (r *managementRepository) PublishAtomic(
	_ context.Context,
	mutation PublishMutation,
) error {
	r.mutation = mutation
	return nil
}

func TestPublishCreatesImmutableFallbackVersionWithEvidence(t *testing.T) {
	repository := &managementRepository{}
	ids := []string{"workflow-id", "audit-id", "event-id", "correlation-id"}
	service := NewManagementService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	published, err := service.Publish(context.Background(), PublishCommand{
		Principal: workflowPrincipal(), Key: "default", Name: "Default",
		Enabled: true, Fallback: true,
		Definition: Definition{
			States:      []State{{Key: "new"}, {Key: "in_progress", RequiresOwner: true}},
			Transitions: []Transition{{From: "new", To: "in_progress"}},
		},
		ActorID: "actor-id", Source: "api",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.ID != "workflow-id" || published.Version != 1 ||
		!repository.mutation.Created ||
		repository.mutation.Audit.Action != "workflow.published" ||
		repository.mutation.Event.EventType != "workflow.published" {
		t.Fatalf("unexpected publication: %+v %+v", published, repository.mutation)
	}
}

func TestPublishRejectsConfigurationWithoutExactlyOneFallback(t *testing.T) {
	service := NewManagementService(&managementRepository{}, time.Now, func() string { return "workflow-id" })
	_, err := service.Publish(context.Background(), PublishCommand{
		Principal: workflowPrincipal(), Key: "conditional", Name: "Conditional",
		Enabled: true, Conditions: Conditions{RecordType: "incident"},
		Definition: Definition{States: []State{{Key: "new"}}},
		ActorID:    "actor-id", Source: "api",
	})
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Publish() error = %v, want invalid configuration", err)
	}
}

func workflowPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "actor-id",
		Scope: scope.Principal{
			MSPID: "msp-id",
		},
		Capabilities: authorization.NewCapabilitySet("workflow.publish"),
	}
}
