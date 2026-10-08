package workrecords

import (
	"context"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type priorityRepository struct {
	record   Record
	sla      AppliedSLA
	accepted PriorityMutation
}

func (r *priorityRepository) Find(context.Context, scope.Target, string) (Record, error) {
	return r.record, nil
}

func (r *priorityRepository) FindAppliedSLA(
	context.Context,
	scope.Target,
	string,
) (AppliedSLA, error) {
	return r.sla, nil
}

func (r *priorityRepository) ChangePriorityAtomic(
	_ context.Context,
	accepted PriorityMutation,
) error {
	r.accepted = accepted
	return nil
}

func TestPriorityChangeRetainsBoundSLAWithExplicitEvidence(t *testing.T) {
	at := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	repository := &priorityRepository{
		record: Record{
			Envelope: object.Envelope{
				ID: "work", ObjectType: "work_record", MSPID: "msp",
				ClientID: "client", Version: 3,
			},
			Priority: "normal",
		},
		sla: AppliedSLA{ID: "sla", PolicyID: "policy", PolicyVersion: 4, Version: 2},
	}
	ids := []string{"correlation", "audit", "event", "sla-audit", "sla-event"}
	service := NewPriorityService(repository, func() time.Time { return at }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	record, err := service.Change(context.Background(), PriorityCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("work_record.edit"),
		},
		WorkRecordID: "work", ExpectedVersion: 3, Priority: "critical",
		Actor:  Actor{Type: "technician", ID: "actor", Source: "api"},
		Reason: "Major incident declared",
	})
	if err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if record.Priority != "critical" || record.Version != 4 ||
		repository.accepted.PreviousPriority != "normal" ||
		repository.accepted.SLAAudit.Action != "sla.policy.retained" ||
		repository.accepted.SLAAudit.Reason != "Major incident declared" ||
		repository.accepted.SLA.PolicyVersion != 4 {
		t.Fatalf("unexpected priority mutation: %+v", repository.accepted)
	}
}
