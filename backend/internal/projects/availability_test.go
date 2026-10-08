package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type availabilityRepositoryStub struct {
	mutation AvailabilityMutation
}

func (r *availabilityRepositoryStub) CreateAvailabilityAtomic(
	_ context.Context,
	mutation AvailabilityMutation,
) error {
	r.mutation = mutation
	return nil
}

func TestCreateTechnicianAvailabilityUsesMSPAdministration(t *testing.T) {
	repository := &availabilityRepositoryStub{}
	ids := []string{"availability-id", "audit-id", "event-id", "correlation-id"}
	service := NewAvailabilityService(repository, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	start := time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC)
	found, err := service.Create(context.Background(), CreateAvailabilityCommand{
		Principal: authorization.Principal{
			ID:           "manager-id",
			Scope:        scope.Principal{MSPID: "msp-id"},
			Capabilities: authorization.NewCapabilitySet("organization.manage"),
		},
		TechnicianID:     "technician-id",
		StartsAt:         start,
		EndsAt:           start.Add(8 * time.Hour),
		AvailableMinutes: 480,
		ActorID:          "manager-id",
		Source:           "web",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if found.MSPID != "msp-id" || found.TechnicianID != "technician-id" ||
		found.AvailableMinutes != 480 ||
		repository.mutation.Audit.ClientID != "" ||
		repository.mutation.Event.EventType != "technician.availability.created" {
		t.Fatalf("found=%+v mutation=%+v", found, repository.mutation)
	}
}

func TestCreateTechnicianAvailabilityRejectsClientScopedAdministration(t *testing.T) {
	service := NewAvailabilityService(
		&availabilityRepositoryStub{}, time.Now, func() string { return "id" },
	)
	_, err := service.Create(context.Background(), CreateAvailabilityCommand{
		Principal: authorization.Principal{
			ID: "manager-id",
			Scope: scope.Principal{
				MSPID: "msp-id", ClientID: "client-id",
			},
			Capabilities: authorization.NewCapabilitySet("organization.manage"),
		},
		TechnicianID: "technician-id",
		StartsAt:     time.Now(), EndsAt: time.Now().Add(time.Hour),
		AvailableMinutes: 60, ActorID: "manager-id", Source: "web",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("Create() error = %v", err)
	}
}
