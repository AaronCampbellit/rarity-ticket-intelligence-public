package psa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

func TestCreateLaborRoleAtomicWritesRoleVersionAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		CreateLaborRoleAtomic(
			context.Background(),
			timeentries.LaborRoleMutation{
				Role: timeentries.LaborRole{
					ID: "role", MSPID: "msp", Key: "senior_engineer", Version: 1,
				},
				Version: timeentries.LaborRoleVersion{
					ID: "version", LaborRoleID: "role", MSPID: "msp",
					Name: "Senior Engineer", InternalCostMinor: 7000,
					BillRateMinor: 18000, Currency: "USD",
					EffectiveFrom: at, Enabled: true, CreatedAt: at,
					CreatedBy: "admin",
				},
				Audit: validAudit(at, "labor_role.created", "labor_role", "role"),
				Event: validEvent(at, "labor_role.created", "labor_role", "role"),
			},
		)
	if err != nil {
		t.Fatalf("CreateLaborRoleAtomic() error = %v", err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"INSERT INTO labor_roles",
		"INSERT INTO labor_role_versions",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestListLaborRolesReturnsEffectiveSelectableVersion(t *testing.T) {
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "role"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "senior_engineer"
			*destinations[3].(*int64) = 2
			*destinations[4].(*string) = "role-v2"
			*destinations[5].(*string) = "Senior Engineer"
			*destinations[6].(*int64) = 7000
			*destinations[7].(*int64) = 18000
			*destinations[8].(*string) = "USD"
			*destinations[9].(*time.Time) = at
			*destinations[10].(**time.Time) = nil
			*destinations[11].(*bool) = true
			*destinations[12].(*time.Time) = at
			*destinations[13].(*string) = "admin"
		},
	}}}
	found, err := NewTimeWorkforceRepository(db).ListLaborRoles(
		context.Background(), "msp", at,
	)
	if err != nil || len(found) != 1 ||
		found[0].CurrentVersion.ID != "role-v2" {
		t.Fatalf("ListLaborRoles()=%+v err=%v", found, err)
	}
}

func TestListLaborRolesForManagementReturnsDisabledLatestVersion(t *testing.T) {
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "role"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "legacy"
			*destinations[3].(*int64) = 3
			*destinations[4].(*string) = "role-v3"
			*destinations[5].(*string) = "Legacy"
			*destinations[6].(*int64) = 7000
			*destinations[7].(*int64) = 18000
			*destinations[8].(*string) = "USD"
			*destinations[9].(*time.Time) = at
			*destinations[10].(**time.Time) = nil
			*destinations[11].(*bool) = false
			*destinations[12].(*time.Time) = at
			*destinations[13].(*string) = "admin"
		},
	}}}
	found, err := NewTimeWorkforceRepository(db).
		ListLaborRolesForManagement(context.Background(), "msp")
	if err != nil || len(found) != 1 ||
		found[0].CurrentVersion.Enabled ||
		found[0].CurrentVersion.ID != "role-v3" {
		t.Fatalf("ListLaborRolesForManagement()=%+v err=%v", found, err)
	}
	if !strings.Contains(db.query, "ORDER BY candidate.effective_from DESC") {
		t.Fatalf("management query does not select latest version: %s", db.query)
	}
}

func TestVersionLaborRoleAtomicAppendsWithoutRewritingHistory(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		VersionLaborRoleAtomic(
			context.Background(),
			timeentries.LaborRoleVersionMutation{
				Role: timeentries.LaborRole{
					ID: "role", MSPID: "msp", Key: "senior_engineer", Version: 2,
				},
				Version: timeentries.LaborRoleVersion{
					ID: "version-2", LaborRoleID: "role", MSPID: "msp",
					Name: "Principal Engineer", InternalCostMinor: 8000,
					BillRateMinor: 20000, Currency: "USD",
					EffectiveFrom: at, Enabled: true, CreatedAt: at,
					CreatedBy: "admin",
				},
				Audit: validAudit(at, "labor_role.versioned", "labor_role", "role"),
				Event: validEvent(at, "labor_role.versioned", "labor_role", "role"),
			},
		)
	if err != nil {
		t.Fatalf("VersionLaborRoleAtomic() error = %v", err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"UPDATE labor_roles",
		"INSERT INTO labor_role_versions",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestResolveLaborRateUsesTechnicianOverrideAtEntryStart(t *testing.T) {
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "role-version"
		*destinations[1].(*int64) = 6500
		*destinations[2].(*int64) = 18000
		*destinations[3].(*string) = "USD"
	}}}
	found, err := NewTimeWorkforceRepository(db).ResolveLaborRate(
		context.Background(),
		"msp",
		"role",
		"technician",
		at,
	)
	if err != nil {
		t.Fatalf("ResolveLaborRate() error = %v", err)
	}
	if found.LaborRoleVersionID != "role-version" ||
		found.InternalCostMinor != 6500 ||
		found.BillRateMinor != 18000 ||
		found.Currency != "USD" {
		t.Fatalf("ResolveLaborRate() = %+v", found)
	}
	if !strings.Contains(
		db.query,
		"COALESCE(technician_rate.hourly_rate_minor, role_version.internal_cost_minor)",
	) || !strings.Contains(db.query, "effective_from <= $4") {
		t.Fatalf("rate query does not resolve effective role and override: %s", db.query)
	}
	if len(db.args) != 4 ||
		db.args[0] != "msp" ||
		db.args[1] != "role" ||
		db.args[2] != "technician" ||
		db.args[3] != at {
		t.Fatalf("ResolveLaborRate() args = %#v", db.args)
	}
}

func TestListTimesheetRowsReturnsScopedWeeklyDetails(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			scanTimesheetTestRow(destinations, timeentries.TimesheetRow{
				Entry: timeentries.Entry{
					ID: "entry", MSPID: "msp", ClientID: "client",
					WorkRecordID: "ticket", TechnicianID: "technician",
					StartedAt: startedAt, EndedAt: startedAt.Add(time.Hour),
					DurationSeconds: 3600, Billable: true, Note: "Worked",
					LaborRoleVersionID: "role-version",
					InternalCostMinor:  3500, BillRateMinor: 12500,
					RateCurrency: "USD", Version: 2,
					CreatedAt: startedAt, CreatedBy: "technician",
				},
				ClientName: "Campbell Co", WorkItemTitle: "VPN unavailable",
				LaborRoleName: "Service Desk", ApprovalState: "pending",
				LastAmendment: &timeentries.AmendmentEvidence{
					ID: "amendment", PriorVersion: 1, ResultingVersion: 2,
					BeforeValues: map[string]any{"duration_seconds": float64(1800)},
					AfterValues:  map[string]any{"duration_seconds": float64(3600)},
					Reason:       "correct duration", AmendedAt: startedAt,
					AmendedBy: "reviewer",
				},
			})
		},
	}}}
	found, err := NewTimeWorkforceRepository(db).ListTimesheetRows(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"technician",
		startedAt.Add(-24*time.Hour),
		startedAt.Add(7*24*time.Hour),
	)
	if err != nil || len(found) != 1 ||
		found[0].WorkItemTitle != "VPN unavailable" ||
		found[0].Entry.DurationSeconds != 3600 ||
		found[0].LastAmendment == nil ||
		found[0].LastAmendment.Reason != "correct duration" {
		t.Fatalf("ListTimesheetRows()=%+v err=%v", found, err)
	}
	if !strings.Contains(db.query, "entry.technician_id = $3") ||
		!strings.Contains(db.query, "entry.started_at >= $4") ||
		len(db.args) != 5 {
		t.Fatalf("query=%s args=%#v", db.query, db.args)
	}
}

func TestAmendTimeEntryAtomicWritesBeforeAfterEvidenceAndFacts(t *testing.T) {
	at := time.Date(2026, time.August, 5, 18, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{}
	result := timeentries.TimesheetRow{
		Entry: timeentries.Entry{
			ID: "entry", MSPID: "msp", ClientID: "client",
			StartedAt: at.Add(-90 * time.Minute), EndedAt: at,
			DurationSeconds: 5400, Billable: true, Note: "Corrected",
			Version: 3,
		},
		ApprovalState: "pending",
	}
	found, err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		AmendTimeEntryAtomic(
			context.Background(),
			timeentries.AmendmentMutation{
				ExpectedVersion: 2,
				Result:          result,
				Amendment: timeentries.AmendmentEvidence{
					ID: "amendment", TimeEntryID: "entry",
					MSPID: "msp", ClientID: "client",
					PriorVersion: 2, ResultingVersion: 3,
					BeforeValues: map[string]any{"duration_seconds": int64(3600)},
					AfterValues:  map[string]any{"duration_seconds": int64(5400)},
					Reason:       "correct duration", AmendedAt: at,
					AmendedBy: "reviewer",
				},
				Audit: validAudit(
					at, "time_entry.amended", "time_entry", "entry",
				),
				Event: validEvent(
					at, "time_entry.amended", "time_entry", "entry",
				),
			},
		)
	if err != nil || found.Entry.Version != 3 {
		t.Fatalf("AmendTimeEntryAtomic()=%+v err=%v", found, err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"UPDATE time_entries",
		"INSERT INTO time_entry_amendments",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestReverseTimeEntryAtomicPreservesApprovedOriginal(t *testing.T) {
	at := time.Date(2026, time.August, 5, 18, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{}
	mutation := timeentries.ReversalMutation{
		ExpectedVersion: 2,
		Original: timeentries.TimesheetRow{
			Entry: timeentries.Entry{
				ID: "entry", MSPID: "msp", ClientID: "client", Version: 3,
			},
			ApprovalState: "approved", ReversedAt: &at,
			ReversedBy: "reviewer", ReversalReason: "correct duration",
			ReplacementTimeEntryID: "replacement",
		},
		Replacement: timeentries.TimesheetRow{
			Entry: timeentries.Entry{
				ID: "replacement", MSPID: "msp", ClientID: "client",
				WorkRecordID: "ticket", TechnicianID: "technician",
				StartedAt: at.Add(-90 * time.Minute), EndedAt: at,
				DurationSeconds: 5400, Version: 1, CreatedAt: at,
				CreatedBy: "reviewer",
			},
			ApprovalState: "pending",
		},
		Reason: "correct duration", ReversedAt: at, ReversedBy: "reviewer",
		InitialTags: testInitialTags(at),
		OriginalAudit: validAudit(
			at, "time_entry.reversed", "time_entry", "entry",
		),
		OriginalEvent: validEvent(
			at, "time_entry.reversed", "time_entry", "entry",
		),
		ReplacementAudit: validAudit(
			at, "time_entry.created", "time_entry", "replacement",
		),
		ReplacementEvent: validEvent(
			at, "time_entry.created", "time_entry", "replacement",
		),
	}
	found, err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		ReverseTimeEntryAtomic(context.Background(), mutation)
	if err != nil ||
		found.Original.ReplacementTimeEntryID != "replacement" {
		t.Fatalf("ReverseTimeEntryAtomic()=%+v err=%v", found, err)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"INSERT INTO time_entries",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"UPDATE time_entries",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func TestStartTimerAtomicValidatesTicketAndReturnsCanonicalIdempotentSession(t *testing.T) {
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		scanTimerSession(destinations, timeentries.TimerSession{
			ID: "timer", MSPID: "msp", ClientID: "client",
			WorkRecordID: "ticket", TechnicianID: "technician",
			State: timeentries.TimerRunning, StartedAt: at,
			IdempotencyKey: "start-key", Version: 1,
			CreatedAt: at, UpdatedAt: at,
		})
		*destinations[14].(*bool) = true
	}}}
	found, err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		StartTimerAtomic(context.Background(), timeentries.TimerMutation{
			Session: timeentries.TimerSession{
				ID: "timer", MSPID: "msp", ClientID: "client",
				WorkRecordID: "ticket", TechnicianID: "technician",
				State: timeentries.TimerRunning, StartedAt: at,
				IdempotencyKey: "start-key", Version: 1,
				CreatedAt: at, UpdatedAt: at,
			},
			Audit: validAudit(at, "ticket_timer.started", "ticket_timer", "timer"),
			Event: validEvent(at, "ticket_timer.started", "ticket_timer", "timer"),
		})
	if err != nil {
		t.Fatalf("StartTimerAtomic() error = %v", err)
	}
	if found.ID != "timer" || found.State != timeentries.TimerRunning {
		t.Fatalf("StartTimerAtomic() = %+v", found)
	}
	if !strings.Contains(tx.query, "FROM work_records") ||
		!strings.Contains(tx.query, "ON CONFLICT (msp_id, technician_id, idempotency_key)") {
		t.Fatalf("start timer query does not validate ticket and idempotency: %s", tx.query)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func scanTimesheetTestRow(
	destinations []any,
	row timeentries.TimesheetRow,
) {
	entry := row.Entry
	*destinations[0].(*string) = entry.ID
	*destinations[1].(*string) = entry.MSPID
	*destinations[2].(*string) = entry.ClientID
	*destinations[3].(*string) = entry.WorkRecordID
	*destinations[4].(*string) = entry.TaskID
	*destinations[5].(*string) = entry.TechnicianID
	*destinations[6].(*time.Time) = entry.StartedAt
	*destinations[7].(*time.Time) = entry.EndedAt
	*destinations[8].(*int64) = entry.DurationSeconds
	*destinations[9].(*bool) = entry.Billable
	*destinations[10].(*string) = entry.Note
	*destinations[11].(*string) = entry.LaborRoleVersionID
	*destinations[12].(*int64) = entry.InternalCostMinor
	*destinations[13].(*int64) = entry.BillRateMinor
	*destinations[14].(*string) = entry.RateCurrency
	*destinations[15].(*int64) = entry.Version
	*destinations[16].(*time.Time) = entry.CreatedAt
	*destinations[17].(*string) = entry.CreatedBy
	*destinations[18].(*string) = row.ClientName
	*destinations[19].(*string) = row.WorkItemTitle
	*destinations[20].(*string) = row.LaborRoleName
	*destinations[21].(*string) = row.ApprovalState
	*destinations[22].(**time.Time) = row.ApprovedAt
	*destinations[23].(*string) = row.ApprovedBy
	*destinations[24].(**time.Time) = row.ReversedAt
	*destinations[25].(*string) = row.ReversedBy
	*destinations[26].(*string) = row.ReversalReason
	*destinations[27].(*string) = row.ReplacementTimeEntryID
	if row.LastAmendment == nil {
		*destinations[28].(*string) = ""
		return
	}
	amendment := row.LastAmendment
	*destinations[28].(*string) = amendment.ID
	*destinations[29].(*int64) = amendment.PriorVersion
	*destinations[30].(*int64) = amendment.ResultingVersion
	*destinations[31].(*[]byte) = []byte(`{"duration_seconds":1800}`)
	*destinations[32].(*[]byte) = []byte(`{"duration_seconds":3600}`)
	*destinations[33].(*string) = amendment.Reason
	*destinations[34].(**time.Time) = &amendment.AmendedAt
	*destinations[35].(*string) = amendment.AmendedBy
}

func TestListTicketTimersReturnsOnlyReusableTechnicianCaptures(t *testing.T) {
	at := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			scanTimerSession(destinations, timeentries.TimerSession{
				ID: "running", MSPID: "msp", ClientID: "client",
				WorkRecordID: "ticket", TechnicianID: "technician",
				State: timeentries.TimerRunning, StartedAt: at,
				IdempotencyKey: "key", Version: 1,
				CreatedAt: at, UpdatedAt: at,
			})
		},
	}}}
	found, err := NewTimeWorkforceRepository(db).ListTicketTimers(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"ticket",
		"technician",
	)
	if err != nil {
		t.Fatalf("ListTicketTimers() error = %v", err)
	}
	if len(found) != 1 || found[0].ID != "running" {
		t.Fatalf("ListTicketTimers() = %+v", found)
	}
	if !strings.Contains(db.query, "state IN ('running', 'stopped')") ||
		len(db.args) != 4 ||
		db.args[3] != "technician" {
		t.Fatalf("ListTicketTimers() query=%s args=%#v", db.query, db.args)
	}
}

func TestStopTimerAtomicRequiresRunningExpectedVersion(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	stoppedAt := startedAt.Add(15 * time.Minute)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		scanTimerSession(destinations, timeentries.TimerSession{
			ID: "timer", MSPID: "msp", ClientID: "client",
			WorkRecordID: "ticket", TechnicianID: "technician",
			State: timeentries.TimerStopped, StartedAt: startedAt,
			StoppedAt: &stoppedAt, DurationSeconds: 900,
			IdempotencyKey: "start-key", Version: 2,
			CreatedAt: startedAt, UpdatedAt: stoppedAt,
		})
	}}}
	found, err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		StopTimerAtomic(context.Background(), timeentries.TimerMutation{
			Session: timeentries.TimerSession{
				ID: "timer", MSPID: "msp", ClientID: "client",
				WorkRecordID: "ticket", TechnicianID: "technician",
				State: timeentries.TimerStopped, StartedAt: startedAt,
				StoppedAt: &stoppedAt, DurationSeconds: 900,
				Version: 2, UpdatedAt: stoppedAt,
			},
			Audit: validAudit(stoppedAt, "ticket_timer.stopped", "ticket_timer", "timer"),
			Event: validEvent(stoppedAt, "ticket_timer.stopped", "ticket_timer", "timer"),
		})
	if err != nil {
		t.Fatalf("StopTimerAtomic() error = %v", err)
	}
	if found.DurationSeconds != 900 || found.Version != 2 {
		t.Fatalf("StopTimerAtomic() = %+v", found)
	}
	if !strings.Contains(tx.query, "state = 'running'") ||
		!strings.Contains(tx.query, "version = $9 - 1") {
		t.Fatalf("stop timer query does not enforce state/version: %s", tx.query)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func TestCreateFromCaptureAtomicLocksCaptureResolvesRatesAndConsumesOnce(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	stoppedAt := startedAt.Add(15 * time.Minute)
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*time.Time) = startedAt
			*destinations[1].(*time.Time) = stoppedAt
			*destinations[2].(*int64) = 900
		}},
		fakeRow{scan: func(destinations ...any) {
			*destinations[0].(*string) = "role-version"
			*destinations[1].(*int64) = 6500
			*destinations[2].(*int64) = 18000
			*destinations[3].(*string) = "USD"
		}},
	}}
	at := stoppedAt.Add(time.Minute)
	found, err := NewTimeWorkforceRepository(&fakeSalesDB{tx: tx}).
		CreateFromCaptureAtomic(
			context.Background(),
			timeentries.CaptureMutation{
				EntryID: "entry",
				Target: scope.Target{
					MSPID: "msp", ClientID: "client",
				},
				WorkRecordID: "ticket", TechnicianID: "technician",
				CaptureID: "capture", ExpectedCaptureVersion: 2,
				LaborRoleID: "role", Billable: true, Note: "Investigated",
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
				InitialTags: testInitialTags(at),
			},
		)
	if err != nil {
		t.Fatalf("CreateFromCaptureAtomic() error = %v", err)
	}
	if found.StartedAt != startedAt ||
		found.EndedAt != stoppedAt ||
		found.DurationSeconds != 900 ||
		found.LaborRoleVersionID != "role-version" ||
		found.InternalCostMinor != 6500 ||
		found.BillRateMinor != 18000 ||
		found.RateCurrency != "USD" {
		t.Fatalf("CreateFromCaptureAtomic() = %+v", found)
	}
	if len(tx.calls) < 8 ||
		!strings.Contains(tx.calls[0], "FOR UPDATE") ||
		!strings.Contains(tx.calls[1], "technician_labor_cost_rates") {
		t.Fatalf("capture transaction calls = %#v", tx.calls)
	}
	assertQueryOrder(
		t,
		tx.queries,
		"INSERT INTO time_entries",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"UPDATE ticket_timer_sessions",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
		"INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
}

func scanTimerSession(
	destinations []any,
	session timeentries.TimerSession,
) {
	*destinations[0].(*string) = session.ID
	*destinations[1].(*string) = session.MSPID
	*destinations[2].(*string) = session.ClientID
	*destinations[3].(*string) = session.WorkRecordID
	*destinations[4].(*string) = session.TechnicianID
	*destinations[5].(*timeentries.TimerState) = session.State
	*destinations[6].(*time.Time) = session.StartedAt
	*destinations[7].(**time.Time) = session.StoppedAt
	*destinations[8].(*int64) = session.DurationSeconds
	*destinations[9].(*string) = session.ConsumedTimeEntryID
	*destinations[10].(*string) = session.IdempotencyKey
	*destinations[11].(*int64) = session.Version
	*destinations[12].(*time.Time) = session.CreatedAt
	*destinations[13].(*time.Time) = session.UpdatedAt
}
