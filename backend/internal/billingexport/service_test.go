package billingexport

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

type exportGuardRepository struct{ meaningful map[string]bool }

func (r exportGuardRepository) Get(_ context.Context, target tagging.TargetRef) (tagging.TaggedObject, error) {
	if !r.meaningful[target.ObjectID] {
		return tagging.TaggedObject{}, nil
	}
	return tagging.TaggedObject{Effective: []tagging.Assignment{{Tag: tagging.Tag{ID: "meaningful", InternalKey: "network", State: tagging.StateActive}}}}, nil
}
func (exportGuardRepository) ResolveTags(context.Context, string, []string) ([]tagging.Tag, error) {
	return nil, nil
}
func (exportGuardRepository) Accepted(context.Context, tagging.TargetRef, string) (tagging.TaggedObject, bool, error) {
	return tagging.TaggedObject{}, false, nil
}
func (exportGuardRepository) ReplaceDirect(context.Context, tagging.AssociationMutation) (tagging.TaggedObject, error) {
	return tagging.TaggedObject{}, nil
}
func (exportGuardRepository) History(context.Context, tagging.TargetRef) ([]tagging.HistoryEntry, error) {
	return nil, nil
}
func exportGuard(entries map[string]bool) *tagging.TerminalGuard {
	return tagging.NewTerminalGuard(tagging.NewAssociationService(exportGuardRepository{meaningful: entries}))
}

type runtimeRepository struct {
	entry         ApprovalEntry
	entries       []Entry
	approved      ApprovalMutation
	exported      ExportMutation
	approvalCalls int
	exportCalls   int
	listCalls     int
}

func (r *runtimeRepository) ListApprovalEntries(
	context.Context, scope.Target, ApprovalState, int,
) ([]ApprovalEntry, error) {
	return []ApprovalEntry{r.entry}, nil
}

func (r *runtimeRepository) LoadApprovalEntry(
	context.Context,
	scope.Target,
	string,
) (ApprovalEntry, error) {
	return r.entry, nil
}

func (r *runtimeRepository) DecideApprovalAtomic(
	_ context.Context,
	accepted ApprovalMutation,
) error {
	r.approvalCalls++
	r.approved = accepted
	return nil
}

func (r *runtimeRepository) ListEntries(
	context.Context,
	scope.Target,
	time.Time,
	time.Time,
	int,
) ([]Entry, error) {
	r.listCalls++
	return r.entries, nil
}

func (r *runtimeRepository) RecordExportAtomic(
	_ context.Context,
	accepted ExportMutation,
) error {
	r.exportCalls++
	r.exported = accepted
	return nil
}

func TestApproveVersionsTimeEntryAndPersistsDecisionEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 29, 19, 0, 0, 0, time.UTC)
	repository := &runtimeRepository{entry: ApprovalEntry{
		Entry: Entry{
			ID: "entry", WorkRecordID: "work", TechnicianID: "tech",
			DurationSeconds: 3600, Billable: true, Version: 2,
		},
		MSPID: "msp", ClientID: "client", ApprovalState: PendingApproval,
	}}
	ids := []string{"decision", "audit", "event", "correlation"}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, exportGuard(map[string]bool{"entry": true}))
	result, err := service.DecideApproval(context.Background(), ApprovalCommand{
		Principal: authorization.Principal{
			ID: "manager", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("time_entry.approve"),
		},
		EntryID: "entry", ExpectedVersion: 2, Decision: Approved,
		Reason: "Reviewed against contract", ActorID: "manager", Source: "api",
	})
	if err != nil {
		t.Fatalf("DecideApproval() error = %v", err)
	}
	if result.Version != 3 || result.ApprovalState != Approved ||
		repository.approvalCalls != 1 ||
		repository.approved.Decision.ID != "decision" ||
		repository.approved.Audit.Action != "time_entry.approved" ||
		repository.approved.Event.EventType != "time_entry.approved" {
		t.Fatalf("approval incomplete: result=%+v mutation=%+v", result, repository.approved)
	}
}

func TestListApprovalsAllowsApproverOrExporterWithinClient(t *testing.T) {
	repository := &runtimeRepository{entry: ApprovalEntry{
		Entry: Entry{ID: "entry"}, MSPID: "msp", ClientID: "client",
		ApprovalState: PendingApproval,
	}}
	service := NewService(repository, time.Now, func() string { return "id" }, exportGuard(map[string]bool{}))
	for _, capability := range []string{"time_entry.approve", "time_entry.export"} {
		principal := authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet(capability),
		}
		entries, err := service.ListApprovals(
			context.Background(),
			ListApprovalsCommand{
				Principal: principal, State: PendingApproval, Limit: 100,
			},
		)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s ListApprovals() entries=%+v error=%v", capability, entries, err)
		}
	}
}

func TestApprovalRejectsStaleVersionOrMissingReason(t *testing.T) {
	repository := &runtimeRepository{entry: ApprovalEntry{
		Entry: Entry{ID: "entry", Billable: true, Version: 4},
		MSPID: "msp", ClientID: "client", ApprovalState: PendingApproval,
	}}
	service := NewService(repository, time.Now, func() string { return "id" }, exportGuard(map[string]bool{}))
	command := ApprovalCommand{
		Principal: authorization.Principal{
			ID: "manager", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("time_entry.approve"),
		},
		EntryID: "entry", ExpectedVersion: 3, Decision: Rejected,
		Reason: "Incorrect contract", ActorID: "manager", Source: "api",
	}
	if _, err := service.DecideApproval(context.Background(), command); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("stale approval error=%v", err)
	}
	command.ExpectedVersion = 4
	command.Reason = ""
	if _, err := service.DecideApproval(context.Background(), command); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing reason error=%v", err)
	}
	if repository.approvalCalls != 0 {
		t.Fatal("invalid approval reached repository")
	}
}

func TestExportRecordsExactApprovedEntryVersionsAndCSVHash(t *testing.T) {
	now := time.Date(2026, time.July, 29, 19, 0, 0, 0, time.UTC)
	repository := &runtimeRepository{entries: []Entry{{
		ID: "entry", WorkRecordID: "work", TechnicianID: "tech",
		DurationSeconds: 3600, Billable: true, Approved: true, Version: 3,
	}}}
	ids := []string{"export", "audit", "event", "correlation"}
	service := NewService(repository, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, exportGuard(map[string]bool{"entry": true}))
	from, through := now.AddDate(0, 0, -7), now
	result, err := service.Export(context.Background(), ExportCommand{
		Principal: authorization.Principal{
			ID: "manager", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("time_entry.export"),
		},
		From: from, Through: through, ActorID: "manager", Source: "api",
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	expectedHash := sha256.Sum256(result.CSV)
	if result.ID != "export" || len(result.CSV) == 0 ||
		repository.exportCalls != 1 ||
		repository.exported.Export.EntryVersions["entry"] != 3 ||
		repository.exported.Export.SHA256 != expectedHash ||
		len(repository.exported.Entries) != 1 ||
		repository.exported.Entries[0].DurationSeconds != 3600 ||
		repository.exported.Event.EventType != "billing_export.created" {
		t.Fatalf("export evidence incomplete: result=%+v mutation=%+v", result, repository.exported)
	}
}

func TestExportDoesNotRecordEvidenceWhenBillableEntryIsUnapproved(t *testing.T) {
	repository := &runtimeRepository{entries: []Entry{{
		ID: "entry", WorkRecordID: "work", DurationSeconds: 60,
		Billable: true, Approved: false, Version: 1,
	}}}
	service := NewService(repository, time.Now, func() string { return "id" }, exportGuard(map[string]bool{"entry": true}))
	_, err := service.Export(context.Background(), ExportCommand{
		Principal: authorization.Principal{
			ID: "manager", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet("time_entry.export"),
		},
		From: time.Now().Add(-time.Hour), Through: time.Now(),
		ActorID: "manager", Source: "api",
	})
	if !errors.Is(err, ErrApprovalRequired) || repository.exportCalls != 0 {
		t.Fatalf("unapproved export error=%v calls=%d", err, repository.exportCalls)
	}
}

func TestExportAuthorizesBeforeReadingEntries(t *testing.T) {
	repository := &runtimeRepository{}
	service := NewService(repository, time.Now, func() string { return "id" }, exportGuard(map[string]bool{}))
	_, err := service.Export(context.Background(), ExportCommand{
		Principal: authorization.Principal{
			ID: "actor", Scope: scope.Principal{MSPID: "msp", ClientID: "client"},
			Capabilities: authorization.NewCapabilitySet(),
		},
		From: time.Now().Add(-time.Hour), Through: time.Now(),
		ActorID: "actor", Source: "api",
	})
	if !errors.Is(err, authorization.ErrForbidden) || repository.listCalls != 0 {
		t.Fatalf("unauthorized export error=%v list_calls=%d", err, repository.listCalls)
	}
}

func TestExportReturnsBlockingEntryForClassificationRecoveryAndAllowsRetry(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	repository := &runtimeRepository{entries: []Entry{{ID: "blocked", WorkRecordID: "work", TechnicianID: "tech", DurationSeconds: 60, Billable: false, Version: 1}}}
	command := ExportCommand{Principal: authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp", ClientID: "client"}, Capabilities: authorization.NewCapabilitySet("time_entry.export")}, From: now.Add(-time.Hour), Through: now, ActorID: "manager", Source: "api"}
	service := NewService(repository, func() time.Time { return now }, func() string { return "id" }, exportGuard(map[string]bool{}))
	_, err := service.Export(context.Background(), command)
	var blocking *ClassificationRequiredError
	if !errors.As(err, &blocking) || blocking.EntryID != "blocked" || repository.exportCalls != 0 {
		t.Fatalf("Export() error=%v blocking=%+v calls=%d", err, blocking, repository.exportCalls)
	}
	service = NewService(repository, func() time.Time { return now }, func() string { return "id" }, exportGuard(map[string]bool{"blocked": true}))
	if _, err := service.Export(context.Background(), command); err != nil || repository.exportCalls != 1 {
		t.Fatalf("retry Export() error=%v calls=%d", err, repository.exportCalls)
	}
}
