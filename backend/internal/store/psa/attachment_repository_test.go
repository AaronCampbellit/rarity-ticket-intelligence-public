package psa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/attachments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestCreateAttachmentValidatesWorkAndUploaderThenWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	err := NewAttachmentRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		attachments.UploadMutation{
			Attachment: attachments.Attachment{
				ID: "attachment", MSPID: "msp", ClientID: "client",
				WorkRecordID: "work", Filename: "notes.txt",
				ContentType: "text/plain", SizeBytes: 7,
				SHA256: [32]byte{1}, StorageKey: "msp/client/attachment",
				Version: 1, UploadedBy: "actor", CreatedAt: at,
			},
			Audit: validAudit(at, "attachment.created", "attachment", "attachment"),
			Event: validEvent(at, "attachment.created", "attachment", "attachment"),
		},
	)
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO attachments", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateAttachmentRejectsInaccessibleWorkBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewAttachmentRepository(&fakeSalesDB{tx: tx}).CreateAtomic(
		context.Background(),
		attachments.UploadMutation{Attachment: attachments.Attachment{
			ID: "attachment", MSPID: "msp", ClientID: "client",
			WorkRecordID: "missing", UploadedBy: "actor",
		}},
	)
	if !errors.Is(err, scope.ErrNotFound) ||
		len(tx.queries) != 1 || !tx.rolledBack {
		t.Fatalf("inaccessible attachment wrote facts: err=%v tx=%+v", err, tx)
	}
}

func TestCreateAndListOpportunityAttachmentUseCompositeScope(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 31, 21, 0, 0, 0, time.UTC)
	repository := NewAttachmentRepository(&fakeSalesDB{tx: tx})
	err := repository.CreateAtomic(
		context.Background(),
		attachments.UploadMutation{
			Attachment: attachments.Attachment{
				ID: "attachment", MSPID: "msp", ClientID: "client",
				OpportunityID: "opportunity", Filename: "scope.txt",
				ContentType: "text/plain", SizeBytes: 5,
				SHA256: [32]byte{1}, StorageKey: "msp/client/attachment",
				Version: 1, UploadedBy: "actor", CreatedAt: at,
			},
			Audit: validAudit(at, "attachment.created", "attachment", "attachment"),
			Event: validEvent(at, "attachment.created", "attachment", "attachment"),
		},
	)
	if err != nil || !strings.Contains(tx.queries[0], "FROM opportunities") {
		t.Fatalf("opportunity CreateAtomic() error=%v query=%s", err, tx.queries[0])
	}

	digest := make([]byte, 32)
	db := &fakeSalesDB{
		queryRow: fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*bool) = true
		}},
		queryRows: &fakeRows{scans: []func(...any){func(destinations ...any) {
			*destinations[0].(*string) = "attachment"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "client"
			*destinations[3].(*string) = "opportunity"
			*destinations[4].(*string) = "scope.txt"
			*destinations[5].(*string) = "text/plain"
			*destinations[6].(*int64) = 5
			*destinations[7].(*[]byte) = digest
			*destinations[8].(*int64) = 1
			*destinations[9].(*string) = "actor"
			*destinations[10].(*time.Time) = at
		}}},
	}
	found, err := NewAttachmentRepository(db).ListOpportunity(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"},
		"opportunity", 100,
	)
	if err != nil || len(found) != 1 ||
		found[0].OpportunityID != "opportunity" ||
		!strings.Contains(db.query, "client_id = $3") {
		t.Fatalf("ListOpportunity() found=%+v error=%v query=%s", found, err, db.query)
	}
}
