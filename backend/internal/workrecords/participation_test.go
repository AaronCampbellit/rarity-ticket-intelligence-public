package workrecords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type participantRepository struct {
	record      Record
	participant Participant
	added       ParticipantMutation
	removed     ParticipantMutation
}

func (r *participantRepository) FindForParticipation(
	_ context.Context,
	_ scope.Target,
	_ string,
) (Record, error) {
	if r.record.ID == "" {
		return Record{}, scope.ErrNotFound
	}
	return r.record, nil
}

func (r *participantRepository) FindParticipant(
	_ context.Context,
	_ scope.Target,
	_ string,
	_ string,
) (Participant, error) {
	if r.participant.ID == "" {
		return Participant{}, scope.ErrNotFound
	}
	return r.participant, nil
}

func (r *participantRepository) AddParticipantAtomic(
	_ context.Context,
	mutation ParticipantMutation,
) error {
	r.added = mutation
	return nil
}

func (r *participantRepository) RemoveParticipantAtomic(
	_ context.Context,
	mutation ParticipantMutation,
) error {
	r.removed = mutation
	return nil
}

func TestParticipationAddVersionsWorkAndProducesEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	repository := &participantRepository{record: Record{Envelope: object.Envelope{
		ID: "work", MSPID: "msp", ClientID: "client",
		LifecycleState: "active", Version: 4,
	}}}
	ids := []string{"participant", "audit", "event", "correlation"}
	service := NewParticipationService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	result, err := service.Add(context.Background(), AddParticipantCommand{
		Principal: participantPrincipal(), WorkRecordID: "work",
		ExpectedVersion: 4, TechnicianID: "technician", Role: Reviewer,
		Actor: Actor{Type: "technician", ID: "actor", Source: "api"},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if result.Record.Version != 5 ||
		result.Participant.ID != "participant" ||
		repository.added.Audit.Action != "work_record.participant.added" ||
		repository.added.Event.SubjectVersion != 5 {
		t.Fatalf("unexpected participation result/evidence: %+v %+v", result, repository.added)
	}
}

func TestParticipationRemoveRequiresReason(t *testing.T) {
	service := NewParticipationService(&participantRepository{}, time.Now, func() string { return "unused" })
	_, err := service.Remove(context.Background(), RemoveParticipantCommand{
		Principal: participantPrincipal(), WorkRecordID: "work",
		ExpectedVersion: 4, ParticipantID: "participant", ParticipantVersion: 1,
		Actor: Actor{Type: "technician", ID: "actor", Source: "api"},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Remove() error = %v, want ErrInvalid", err)
	}
}

func participantPrincipal() authorization.Principal {
	return authorization.Principal{
		ID: "actor",
		Scope: scope.Principal{
			MSPID: "msp", ClientID: "client",
		},
		Capabilities: authorization.NewCapabilitySet("work_record.assign"),
	}
}
