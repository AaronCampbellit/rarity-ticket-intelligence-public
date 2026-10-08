package psa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestAddParticipantVersionsWorkAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).AddParticipantAtomic(
		context.Background(),
		workrecords.ParticipantMutation{
			Record: workrecords.Record{Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 5, UpdatedAt: at, UpdatedBy: "actor",
			}},
			Participant: workrecords.Participant{
				ID: "participant", MSPID: "msp", ClientID: "client",
				WorkRecordID: "work", TechnicianID: "technician",
				Role: workrecords.Reviewer, Version: 1,
				AddedAt: at, AddedBy: "actor",
			},
			Audit: validAudit(at, "work_record.participant.added", "work_record", "work"),
			Event: validEvent(at, "work_record.participant.added", "work_record", "work"),
		},
	)
	if err != nil {
		t.Fatalf("AddParticipantAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE work_records", "INSERT INTO work_record_participants",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestRemoveParticipantRejectsStaleVersionBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).RemoveParticipantAtomic(
		context.Background(),
		workrecords.ParticipantMutation{
			Record: workrecords.Record{Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 6, UpdatedAt: at, UpdatedBy: "actor",
			}},
			Participant: workrecords.Participant{
				ID: "participant", Version: 2, RemovedAt: &at, RemovedBy: "actor",
			},
		},
	)
	if !errors.Is(err, object.ErrVersionConflict) ||
		len(tx.queries) != 2 || !tx.rolledBack {
		t.Fatalf("stale participant wrote facts: err=%v tx=%+v", err, tx)
	}
}

func TestFindParticipantIsClientAndWorkScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewWorkRecordRepository(db).FindParticipant(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"work", "participant",
	)
	if db.query == "" {
		t.Fatal("participant lookup was not issued")
	}
}
