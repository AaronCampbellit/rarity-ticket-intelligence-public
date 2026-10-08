package psa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestCreateCommentValidatesWorkAndAuthorThenWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewCommentRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 15, 0, 0, 0, time.UTC)
	err := repository.CreateAtomic(context.Background(), comments.CreateMutation{
		Comment: comments.Comment{
			ID: "comment", MSPID: "msp", ClientID: "client",
			WorkRecordID: "work", AuthorID: "actor",
			Visibility: comments.Internal, Body: "Investigating logs",
			Version: 1, CreatedAt: at,
		},
		Audit: validAudit(at, "comment.added", "comment", "comment"),
		Event: validEvent(at, "comment.added", "comment", "comment"),
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO comments",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreatePublicCommentRecordsFirstResponseAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 15, 30, 0, 0, time.UTC)
	err := NewCommentRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		comments.CreateMutation{
			Comment: comments.Comment{
				ID: "comment", MSPID: "msp", ClientID: "client",
				WorkRecordID: "work", AuthorID: "actor",
				Visibility: comments.ClientVisible, Body: "Resolved",
				Version: 1, CreatedAt: at,
			},
			SLAChanged: true,
			SLA: workrecords.AppliedSLA{
				ID: "sla", Version: 2, RespondedAt: &at,
				ResponseWarningAt: at.Add(time.Hour),
				ResponseDueAt:     at.Add(2 * time.Hour), ResponseState: sla.Met,
			},
			Audit:    validAudit(at, "comment.added", "comment", "comment"),
			Event:    validEvent(at, "comment.added", "comment", "comment"),
			SLAAudit: validAudit(at, "sla.response.recorded", "work_record_sla", "sla"),
			SLAEvent: validEvent(at, "sla.response.recorded", "work_record_sla", "sla"),
		},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "INSERT INTO comments",
		"UPDATE work_record_slas",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateCommentRejectsInaccessibleWorkBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	err := NewCommentRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		comments.CreateMutation{Comment: comments.Comment{
			ID: "comment", MSPID: "msp", ClientID: "client",
			WorkRecordID: "missing", AuthorID: "actor",
			Visibility: comments.Internal, Body: "note",
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 2 || !tx.rolledBack {
		t.Fatalf("inaccessible work wrote facts: err=%v tx=%+v", err, tx)
	}
}

func TestCreateCommentWithCaptureCommitsCommentTimeAndFactsTogether(t *testing.T) {
	at := time.Date(2026, time.August, 4, 19, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*time.Time) = at.Add(-15 * time.Minute)
			*destinations[1].(*time.Time) = at
			*destinations[2].(*int64) = 900
		}},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*string) = "role-version"
			*destinations[1].(*int64) = 6500
			*destinations[2].(*int64) = 18000
			*destinations[3].(*string) = "USD"
		}},
	}}
	comment := comments.CreateMutation{
		Comment: comments.Comment{
			ID: "comment", MSPID: "msp", ClientID: "client",
			WorkRecordID: "work", AuthorID: "tech",
			Visibility: comments.Internal, Body: "Investigated logs",
			Version: 1, CreatedAt: at,
		},
		Audit: validAudit(at, "comment.added", "comment", "comment"),
		Event: validEvent(at, "comment.added", "comment", "comment"),
	}
	capture := timeentries.CaptureMutation{
		EntryID: "entry", Target: scope.Target{
			MSPID: "msp", ClientID: "client",
		},
		WorkRecordID: "work", TechnicianID: "tech",
		CaptureID: "capture", ExpectedCaptureVersion: 2,
		LaborRoleID: "role", Billable: true, Note: "Investigated logs",
		CreatedAt: at,
		EntryAudit: validAudit(
			at, "time_entry.created_from_capture", "time_entry", "entry",
		),
		EntryEvent: validEvent(
			at, "time_entry.created_from_capture", "time_entry", "entry",
		),
		TimerAudit: validAudit(
			at, "ticket_timer.consumed", "ticket_timer", "capture",
		),
		TimerEvent: validEvent(
			at, "ticket_timer.consumed", "ticket_timer", "capture",
		),
	}
	err := NewCommentRepository(&fakeSalesDB{tx: tx}).
		CreateWithCaptureAtomic(context.Background(), comment, capture)
	if err != nil {
		t.Fatalf("CreateWithCaptureAtomic() error = %v", err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"FROM client_organizations",
		"INSERT INTO comments",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO time_entries",
		"UPDATE ticket_timer_sessions",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !tx.committed {
		t.Fatal("combined comment and capture did not commit")
	}
}
