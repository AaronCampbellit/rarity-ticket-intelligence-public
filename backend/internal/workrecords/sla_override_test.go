package workrecords

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
)

type slaOverrideRepository struct {
	record   Record
	sla      AppliedSLA
	accepted SLAOverrideMutation
}

func (r *slaOverrideRepository) Find(context.Context, scope.Target, string) (Record, error) {
	return r.record, nil
}

func (r *slaOverrideRepository) FindAppliedSLA(
	context.Context,
	scope.Target,
	string,
) (AppliedSLA, error) {
	return r.sla, nil
}

func (r *slaOverrideRepository) OverrideSLAAtomic(
	_ context.Context,
	accepted SLAOverrideMutation,
) error {
	r.accepted = accepted
	return nil
}

func TestSLAOverrideShiftsWarningAndPersistsReasonedEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	repository := &slaOverrideRepository{
		record: Record{
			Envelope: object.Envelope{
				ID: "work", ObjectType: "work_record", MSPID: "msp",
				ClientID: "client", Version: 3,
			},
		},
		sla: AppliedSLA{
			ID: "sla", Version: 2,
			ResponseWarningAt:   now.Add(-30 * time.Minute),
			ResponseDueAt:       now.Add(30 * time.Minute),
			ResolutionWarningAt: now.Add(time.Hour),
			ResolutionDueAt:     now.Add(2 * time.Hour),
			ResponseState:       sla.Warning, ResolutionState: sla.Running,
		},
	}
	ids := []string{"override", "correlation", "audit", "event"}
	service := NewSLAOverrideService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	newResponseDue := now.Add(90 * time.Minute)
	record, err := service.Override(context.Background(), SLAOverrideCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("sla.override"),
		},
		WorkRecordID: "work", ExpectedVersion: 3, SLAExpectedVersion: 2,
		ResponseDueAt: &newResponseDue,
		Reason:        "Vendor outage excluded by contract",
		Actor:         Actor{Type: "technician", ID: "actor", Source: "api"},
	})
	if err != nil {
		t.Fatalf("Override() error = %v", err)
	}
	if record.Version != 4 || repository.accepted.SLA.Version != 3 ||
		repository.accepted.SLA.ResponseDueAt != newResponseDue ||
		repository.accepted.SLA.ResponseWarningAt != now.Add(30*time.Minute) ||
		repository.accepted.SLA.ResponseState != sla.Running ||
		repository.accepted.Evidence.Reason != "Vendor outage excluded by contract" ||
		repository.accepted.Audit.Action != "sla.deadline.overridden" {
		t.Fatalf("unexpected override: %+v", repository.accepted)
	}
}
