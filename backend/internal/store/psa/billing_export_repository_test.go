package psa

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/billingexport"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func TestBillingExportRepositoryDecidesApprovalWithAtomicEvidence(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	err := NewBillingExportRepository(&fakeSalesDB{tx: tx}).DecideApprovalAtomic(
		context.Background(),
		billingexport.ApprovalMutation{
			ExpectedVersion: 2,
			Entry: billingexport.ApprovalEntry{
				Entry: billingexport.Entry{
					ID: "entry", WorkRecordID: "work", TechnicianID: "tech",
					DurationSeconds: 3600, Billable: true, Approved: true, Version: 3,
				},
				MSPID: "msp", ClientID: "client",
				ApprovalState: billingexport.Approved,
				ApprovedAt:    &at, ApprovedBy: "manager",
			},
			Decision: billingexport.ApprovalDecision{
				ID: "decision", EntryID: "entry", MSPID: "msp", ClientID: "client",
				Version: 3, Decision: billingexport.Approved,
				Reason: "Reviewed", DecidedAt: at, DecidedBy: "manager",
			},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "manager",
				Action: "time_entry.approved", SubjectType: "time_entry",
				SubjectID: "entry", SubjectVersion: 3, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "time_entry.approved",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "manager",
				SubjectType: "time_entry", SubjectID: "entry", SubjectVersion: 3,
				Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("DecideApprovalAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE time_entries",
		"INSERT INTO time_entry_approval_decisions",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestBillingExportRepositoryListsOnlyScopedRangeWithDurableApproval(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewBillingExportRepository(db).ListEntries(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		10001,
	)
	if err != nil {
		t.Fatalf("ListEntries() error=%v", err)
	}
	for _, required := range []string{
		"entry.msp_id = $1",
		"entry.client_id = $2::uuid",
		"entry.started_at >= $3",
		"entry.started_at < $4",
		"entry.work_record_id IS NOT NULL",
		"entry.approval_state = 'approved'",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("billing entry query missing %q: %s", required, db.query)
		}
	}
}

func TestBillingExportRepositoryListsScopedApprovalQueue(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewBillingExportRepository(db).ListApprovalEntries(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		billingexport.PendingApproval, 100,
	)
	if err != nil {
		t.Fatalf("ListApprovalEntries() error=%v", err)
	}
	for _, required := range []string{
		"entry.msp_id = $1", "entry.client_id = $2::uuid",
		"entry.work_record_id IS NOT NULL",
		"entry.approval_state = $3", "ORDER BY entry.started_at DESC",
	} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("approval queue query missing %q: %s", required, db.query)
		}
	}
}

func TestBillingExportRepositoryRecordsHashSnapshotsAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC)
	hash := sha256.Sum256([]byte("csv"))
	err := NewBillingExportRepository(&fakeSalesDB{tx: tx}).RecordExportAtomic(
		context.Background(),
		billingexport.ExportMutation{
			Export: billingexport.ExportEvidence{
				ID: "export", MSPID: "msp", ClientID: "client",
				From: at.Add(-time.Hour), Through: at, EntryCount: 1,
				EntryVersions: map[string]int64{"entry": 3},
				SHA256:        hash, CreatedAt: at, CreatedBy: "manager",
			},
			Entries: []billingexport.Entry{{
				ID: "entry", WorkRecordID: "work", TechnicianID: "tech",
				DurationSeconds: 3600, Billable: true, Approved: true, Version: 3,
			}},
			Audit: mutation.AuditRecord{
				ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "manager",
				Action: "billing_export.created", SubjectType: "billing_export",
				SubjectID: "export", SubjectVersion: 1, Source: "api",
				CorrelationID: "correlation",
			},
			Event: mutation.EventRecord{
				EventID: "event", EventType: "billing_export.created",
				SchemaVersion: 1, OccurredAt: at, MSPID: "msp", ClientID: "client",
				ActorType: "technician", ActorID: "manager",
				SubjectType: "billing_export", SubjectID: "export",
				SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
			},
		},
	)
	if err != nil {
		t.Fatalf("RecordExportAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO billing_exports",
		"INSERT INTO billing_export_entries",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}
