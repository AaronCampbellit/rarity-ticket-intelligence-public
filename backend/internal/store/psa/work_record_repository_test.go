package psa

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/routing"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

func TestCreateWorkRecordWritesRecordAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	record := workrecords.Record{
		Envelope: object.Envelope{
			ID: "work", MSPID: "msp", ClientID: "client",
			DisplayID: "INC-100", LifecycleState: "active", Version: 1,
			CreatedAt: at, CreatedBy: "actor", UpdatedAt: at, UpdatedBy: "actor",
		},
		Type: workrecords.Incident, Title: "Email unavailable",
		Description: "Multiple users affected", Status: "new", Priority: "high",
		ServiceID: "service", ContractID: "contract",
	}
	err := repository.CreateAtomic(context.Background(), workrecords.CreateMutation{
		Record: record, ExpectedClientVersion: 11,
		ExpectedServiceVersion: 13, ExpectedContractVersion: 17,
		Routing: workrecords.RoutingSelection{
			RuleSetID: "routing-set", RuleSetVersion: 2, DecidedAt: at,
			Decision: routing.Decision{
				QueueID: "queue", RuleID: "routing-rule", Explanation: "matched fallback",
			},
		},
		SLA: workrecords.AppliedSLA{
			ID: "sla", PolicyID: "policy", PolicyVersion: 3,
			CalendarID: "calendar", CalendarVersion: 2,
			ResponseWarningAt:   at.Add(time.Hour),
			ResponseDueAt:       at.Add(2 * time.Hour),
			ResolutionWarningAt: at.Add(3 * time.Hour),
			ResolutionDueAt:     at.Add(4 * time.Hour),
			ResponseState:       sla.Running, ResolutionState: sla.Running,
			Version: 1, SelectionTrace: []sla.PolicyTraceEntry{
				{PolicyID: "policy", Version: 3, Matched: true},
			},
		},
		Workflow: workflow.Selection{
			WorkflowID: "workflow", Version: 4, EvaluatedAt: at,
			Trace: []workflow.TraceEntry{{WorkflowID: "workflow", Version: 4, Matched: true}},
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor", Action: "work_record.created",
			SubjectType: "work_record", SubjectID: "work", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "work_record.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "work_record", SubjectID: "work", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		InitialTags: testInitialTags(at),
	})
	if err != nil {
		t.Fatalf("CreateAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "FROM services", "FROM contracts", "FROM queues",
		"FROM routing_rule_sets", "FROM routing_rule_set_versions",
		"FROM workflows", "FROM workflow_versions",
		"FROM sla_policies", "FROM sla_policy_versions",
		"FROM business_calendars", "FROM business_calendar_versions",
		"INSERT INTO work_records",
		"INSERT INTO work_record_routing",
		"INSERT INTO work_record_workflows",
		"INSERT INTO work_record_slas",
		"INSERT INTO object_tag_assignments", "INSERT INTO tag_assignment_events",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	for index, want := range []int64{11, 13, 17} {
		if !strings.Contains(tx.queries[index], "version =") ||
			tx.args[index][len(tx.args[index])-1] != want {
			t.Fatalf("version fence query %d args=%#v query=%s", index, tx.args[index], tx.queries[index])
		}
	}
}

func TestFindWorkRecordIsClientScoped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewWorkRecordRepository(db).Find(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"work",
	)
	if !strings.Contains(db.query, "msp_id = $2 AND client_id = $3") {
		t.Fatalf("work-record lookup is not Client scoped: %s", db.query)
	}
}

func TestResolveWorkRecordForAssignmentIsActiveExactDisplayIDFirstAndBounded(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewWorkRecordRepository(db).ResolveWorkRecordForAssignment(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "  INC-2042  ", 10,
	)
	if err != nil {
		t.Fatalf("ResolveWorkRecordForAssignment() error=%v", err)
	}
	for _, fragment := range []string{
		"msp_id = $1", "client_id = $2::uuid", "deleted_at IS NULL", "lifecycle_state = 'active'",
		"lower(regexp_replace(btrim(display_id), '\\s+', ' ', 'g')) = $3",
		"lower(regexp_replace(btrim(title), '\\s+', ' ', 'g')) = $3",
		"CASE WHEN lower(regexp_replace(btrim(display_id), '\\s+', ' ', 'g')) = $3 THEN 0 ELSE 1 END",
		"ORDER BY", "LIMIT $4",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("assignment resolver missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 4 || db.args[0] != "msp" || db.args[1] != "client" || db.args[2] != "inc-2042" || db.args[3] != 2 {
		t.Fatalf("assignment resolver args=%+v", db.args)
	}
}

func TestWorkRecordDisplayIDAvailabilityIsReadOnlyAndConflictTyped(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*bool)) = true
	}}}
	err := NewWorkRecordRepository(db).EnsureDisplayIDAvailable(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "INC-100",
	)
	if !errors.Is(err, workrecords.ErrDisplayIDConflict) {
		t.Fatalf("EnsureDisplayIDAvailable() error=%v, want ErrDisplayIDConflict", err)
	}
	if db.beginCalls != 0 || !strings.Contains(db.query, "EXISTS") ||
		!strings.Contains(db.query, "FROM work_records") ||
		len(db.args) != 3 || db.args[2] != "INC-100" {
		t.Fatalf("query=%q args=%+v begins=%d", db.query, db.args, db.beginCalls)
	}
}

func TestWorkRecordWriteErrorRetainsDisplayIDConflictMapping(t *testing.T) {
	err := workRecordWriteError(&pgconn.PgError{
		Code: "23505", ConstraintName: "work_records_msp_id_client_id_display_id_key",
	})
	if !errors.Is(err, workrecords.ErrDisplayIDConflict) {
		t.Fatalf("workRecordWriteError() error=%v, want ErrDisplayIDConflict", err)
	}
}

func TestListWorkRecordsUsesScopedStableWorklistFilters(t *testing.T) {
	updatedAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			values := []any{
				"work", "msp", "client", "INC-100", workrecords.Incident,
				"Email unavailable", "Multiple users affected", "in_progress",
				"high", "queue", "technician", "service", "contract", "active",
				int64(3), updatedAt.Add(-time.Hour), "creator", updatedAt, "updater",
				(*time.Time)(nil), "", "",
			}
			for index, value := range values {
				setScanDestination(destinations[index], value)
			}
		},
	}}}
	filter := workrecords.ListFilter{
		Target: scope.Target{MSPID: "msp", ClientID: "client"},
		Status: "in_progress", QueueID: "queue", PrimaryOwnerID: "technician",
		BeforeUpdatedAt: updatedAt, BeforeID: "cursor", Limit: 25,
	}

	records, err := NewWorkRecordRepository(db).List(context.Background(), filter)
	if err != nil || len(records) != 1 || records[0].ID != "work" {
		t.Fatalf("List() records=%+v error=%v", records, err)
	}
	for _, fragment := range []string{
		"msp_id = $1", "client_id = $2", "status = $3",
		"queue_id = NULLIF($4, '')::uuid",
		"primary_owner_id = NULLIF($5, '')::uuid",
		"(updated_at, id) < ($6, NULLIF($7, '')::uuid)",
		"ORDER BY updated_at DESC, id DESC", "LIMIT $8",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("worklist query missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 11 || db.args[0] != "msp" || db.args[1] != "client" ||
		db.args[7] != 25 {
		t.Fatalf("unexpected worklist arguments: %+v", db.args)
	}
}

func setScanDestination(destination, value any) {
	reflect.ValueOf(destination).Elem().Set(reflect.ValueOf(value))
}

func TestValidateWorkRecordReferencesIsClientScopedAndEffective(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*(destinations[0].(*bool)) = true
	}}}
	err := NewWorkRecordRepository(db).ValidateReferences(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		workrecords.ContextReferences{ServiceID: "service", ContractID: "contract"},
		time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ValidateReferences() error = %v", err)
	}
	for _, fragment := range []string{
		"services", "contracts", "msp_id = $1", "client_id = $2",
		"starts_on <= $5", "ends_on >= $5",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("reference validation missing %q: %s", fragment, db.query)
		}
	}
}

func TestValidateWorkRecordReferencesRejectsInactiveClientBeforeCreate(t *testing.T) {
	db := &fakeSalesDB{}
	db.queryRow = fakeRow{scan: func(destinations ...any) {
		activeClientCheck := strings.Contains(db.query, "FROM client_organizations") &&
			strings.Contains(db.query, "lifecycle_state = 'active'")
		*(destinations[0].(*bool)) = !activeClientCheck
	}}

	err := NewWorkRecordRepository(db).ValidateReferences(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "inactive-client"},
		workrecords.ContextReferences{ServiceID: "service", ContractID: "contract"},
		time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("ValidateReferences() error = %v, want inactive Client not found", err)
	}
}

func TestAssignWorkRecordValidatesOwnerAndWritesFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 13, 0, 0, 0, time.UTC)
	err := repository.AssignAtomic(context.Background(), workrecords.AssignmentMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 3, UpdatedAt: at, UpdatedBy: "actor",
			},
			PrimaryOwnerID: "owner",
		},
		Audit: validAudit(at, "work_record.owner.changed", "work_record", "work"),
		Event: validEvent(at, "work_record.owner.changed", "work_record", "work"),
	})
	if err != nil {
		t.Fatalf("AssignAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "FROM work_records", "FROM technicians t",
		"UPDATE work_records",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestAssignWorkRecordRejectsInactiveClientAtCommitBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 1}
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).AssignAtomic(context.Background(), workrecords.AssignmentMutation{
		Record:                workrecords.Record{Envelope: object.Envelope{ID: "work", MSPID: "msp", ClientID: "client", Version: 3}, PrimaryOwnerID: "owner"},
		ExpectedClientVersion: 7, ExpectedOwnerVersion: 6,
	})
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 1 || len(tx.calls) != 1 || !tx.rolledBack {
		t.Fatalf("inactive Client assignment error=%v queries=%+v calls=%+v", err, tx.queries, tx.calls)
	}
	if !strings.Contains(tx.calls[0], "lifecycle_state = 'active'") || !strings.Contains(tx.calls[0], "version = $3") || !strings.Contains(tx.calls[0], "FOR SHARE") {
		t.Fatalf("Client commit guard=%s", tx.calls[0])
	}
}

func TestAssignWorkRecordRejectsTechnicianVersionDriftAtCommitBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 3}
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).AssignAtomic(context.Background(), workrecords.AssignmentMutation{
		Record:                workrecords.Record{Envelope: object.Envelope{ID: "work", MSPID: "msp", ClientID: "client", Version: 3}, PrimaryOwnerID: "owner"},
		ExpectedClientVersion: 7, ExpectedOwnerVersion: 6,
	})
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 3 || len(tx.calls) != 3 || !tx.rolledBack {
		t.Fatalf("technician drift assignment error=%v queries=%+v calls=%+v", err, tx.queries, tx.calls)
	}
	if !strings.Contains(tx.calls[2], "t.version = $4") ||
		!strings.Contains(tx.calls[2], "ORDER BY ra.id") ||
		!strings.Contains(tx.calls[2], "FOR SHARE OF t, ra") {
		t.Fatalf("technician commit guard=%s", tx.calls[2])
	}
}

func TestAssignWorkRecordRejectsInvalidCandidateBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 3}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	err := repository.AssignAtomic(context.Background(), workrecords.AssignmentMutation{
		Record: workrecords.Record{Envelope: object.Envelope{
			ID: "work", MSPID: "msp", ClientID: "client", Version: 3,
		}, PrimaryOwnerID: "invalid-owner"},
	})
	if !errors.Is(err, scope.ErrNotFound) || len(tx.queries) != 3 ||
		len(tx.calls) != 3 || !tx.rolledBack {
		t.Fatalf("invalid candidate result error=%v queries=%+v calls=%+v", err, tx.queries, tx.calls)
	}
	if !strings.Contains(tx.calls[0], "FROM client_organizations") ||
		!strings.Contains(tx.calls[1], "FROM work_records") ||
		!strings.Contains(tx.calls[1], "FOR UPDATE") ||
		!strings.Contains(tx.calls[2], "role_assignments") ||
		!strings.Contains(tx.calls[2], "ra.expires_at IS NULL OR ra.expires_at > $5") ||
		!strings.Contains(tx.calls[2], "FOR SHARE OF t, ra") {
		t.Fatalf("candidate validation does not enforce current scoped role: %s", tx.calls)
	}
}

func TestChangePriorityRetainsSLAWithFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 13, 15, 0, 0, time.UTC)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).ChangePriorityAtomic(
		context.Background(),
		workrecords.PriorityMutation{
			Record: workrecords.Record{
				Envelope: object.Envelope{
					ID: "work", MSPID: "msp", ClientID: "client",
					Version: 3, UpdatedAt: at, UpdatedBy: "actor",
				},
				Priority: "critical",
			},
			SLA:      workrecords.AppliedSLA{ID: "sla", Version: 2},
			Audit:    validAudit(at, "work_record.priority.changed", "work_record", "work"),
			Event:    validEvent(at, "work_record.priority.changed", "work_record", "work"),
			SLAAudit: validAudit(at, "sla.policy.retained", "work_record_sla", "sla"),
			SLAEvent: validEvent(at, "sla.policy.retained", "work_record_sla", "sla"),
		},
	)
	if err != nil {
		t.Fatalf("ChangePriorityAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "UPDATE work_records",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestOverrideSLAWritesDeadlinesEvidenceAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 14, 0, 0, 0, time.UTC)
	before, after := at.Add(time.Hour), at.Add(2*time.Hour)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).OverrideSLAAtomic(
		context.Background(),
		workrecords.SLAOverrideMutation{
			Record: workrecords.Record{Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 4, UpdatedAt: at, UpdatedBy: "actor",
			}},
			SLA: workrecords.AppliedSLA{
				ID: "sla", Version: 3,
				ResponseWarningAt:   at.Add(90 * time.Minute),
				ResponseDueAt:       after,
				ResolutionWarningAt: at.Add(3 * time.Hour),
				ResolutionDueAt:     at.Add(4 * time.Hour),
				ResponseState:       sla.Running, ResolutionState: sla.Running,
			},
			Evidence: workrecords.SLAOverrideEvidence{
				ID: "override", SLAID: "sla", WorkRecordID: "work",
				MSPID: "msp", ClientID: "client", WorkRecordVersion: 4,
				SLAVersionBefore: 2, SLAVersionAfter: 3,
				ResponseDueAtBefore: &before, ResponseDueAtAfter: &after,
				Reason: "Contract exclusion", OverriddenAt: at, OverriddenBy: "actor",
			},
			Audit: validAudit(at, "sla.deadline.overridden", "work_record_sla", "sla"),
			Event: validEvent(at, "sla.deadline.overridden", "work_record_sla", "sla"),
		},
	)
	if err != nil {
		t.Fatalf("OverrideSLAAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE work_records", "UPDATE work_record_slas",
		"INSERT INTO sla_overrides",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestTransitionWorkRecordWritesStatusAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 13, 30, 0, 0, time.UTC)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).TransitionAtomic(
		context.Background(),
		workrecords.TransitionMutation{
			Record: workrecords.Record{
				Envelope: object.Envelope{
					ID: "work", MSPID: "msp", ClientID: "client",
					Version: 3, UpdatedAt: at, UpdatedBy: "actor",
				},
				Status: "in_progress",
			},
			PreviousStatus:  "new",
			WorkflowID:      "workflow",
			WorkflowVersion: 4,
			Audit:           validAudit(at, "work_record.transitioned", "work_record", "work"),
			Event:           validEvent(at, "work_record.transitioned", "work_record", "work"),
		},
	)
	if err != nil {
		t.Fatalf("TransitionAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "UPDATE work_records",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[1], "work_record_workflows") {
		t.Fatalf("transition does not pin exact selected workflow: %s", tx.queries[1])
	}
}

func TestTransitionWorkRecordWritesSLAStateAndFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 30, 13, 45, 0, 0, time.UTC)
	err := NewWorkRecordRepository(&fakeSalesDB{tx: tx}).TransitionAtomic(
		context.Background(),
		workrecords.TransitionMutation{
			Record: workrecords.Record{
				Envelope: object.Envelope{
					ID: "work", MSPID: "msp", ClientID: "client",
					Version: 3, UpdatedAt: at, UpdatedBy: "actor",
				},
				Status: "waiting",
			},
			WorkflowID: "workflow", WorkflowVersion: 4,
			SLAChanged: true,
			SLA: workrecords.AppliedSLA{
				ID: "sla", Version: 2, PausedAt: &at,
				ResponseWarningAt: at.Add(time.Hour), ResponseDueAt: at.Add(2 * time.Hour),
				ResolutionWarningAt: at.Add(3 * time.Hour), ResolutionDueAt: at.Add(4 * time.Hour),
				ResponseState: sla.Paused, ResolutionState: sla.Paused,
			},
			Audit:    validAudit(at, "work_record.transitioned", "work_record", "work"),
			Event:    validEvent(at, "work_record.transitioned", "work_record", "work"),
			SLAAudit: validAudit(at, "sla.paused", "work_record_sla", "sla"),
			SLAEvent: validEvent(at, "sla.paused", "work_record_sla", "sla"),
		},
	)
	if err != nil {
		t.Fatalf("TransitionAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "UPDATE work_records",
		"UPDATE work_record_slas",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestFindAppliedSLAUsesExactClientAndCalendarVersion(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: context.Canceled}}
	_, _ = NewWorkRecordRepository(db).FindAppliedSLA(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"work",
	)
	for _, fragment := range []string{
		"wrs.msp_id = $2", "wrs.client_id = $3",
		"cv.calendar_id = wrs.calendar_id", "cv.version = wrs.calendar_version",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("SLA lookup missing %q: %s", fragment, db.query)
		}
	}
}

func TestAssignWorkRecordRejectsStaleTargetBeforeOwnerAndFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	err := repository.AssignAtomic(context.Background(), workrecords.AssignmentMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client", Version: 3,
			},
			PrimaryOwnerID: "invalid-owner",
		},
	})
	if !errors.Is(err, object.ErrVersionConflict) || len(tx.queries) != 2 ||
		len(tx.calls) != 2 || !tx.rolledBack {
		t.Fatalf("stale assignment wrote dependent facts: err=%v tx=%+v", err, tx)
	}
}

func TestTransferQueueValidatesScopedQueueAndWritesFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 13, 0, 0, 0, time.UTC)
	err := repository.TransferQueueAtomic(context.Background(), workrecords.QueueMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 4, UpdatedAt: at, UpdatedBy: "actor",
			},
			QueueID: "queue",
		},
		Queue: workrecords.QueueRef{
			ID: "queue", MSPID: "msp", ClientID: "client",
			Key: "service-desk", Name: "Service Desk", Version: 3,
		},
		Audit: validAudit(at, "work_record.queue.changed", "work_record", "work"),
		Event: validEvent(at, "work_record.queue.changed", "work_record", "work"),
	})
	if err != nil {
		t.Fatalf("TransferQueueAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "UPDATE work_records",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	for _, fragment := range []string{
		"queues.version = $8", "queues.key = $9", "queues.name = $10",
	} {
		if !strings.Contains(tx.queries[1], fragment) {
			t.Fatalf("queue write omitted prepared fact %q: %s", fragment, tx.queries[1])
		}
	}
}

func TestTransferQueueFactRaceRollsBackBeforeTicketOrFacts(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	err := repository.TransferQueueAtomic(context.Background(), workrecords.QueueMutation{
		Record: workrecords.Record{
			Envelope: object.Envelope{
				ID: "work", MSPID: "msp", ClientID: "client",
				Version: 4, UpdatedAt: time.Now(), UpdatedBy: "actor",
			},
			QueueID: "queue",
		},
		Queue: workrecords.QueueRef{
			ID: "queue", MSPID: "msp", ClientID: "client",
			Key: "service-desk", Name: "Service Desk", Version: 3,
		},
	})
	if !errors.Is(err, object.ErrVersionConflict) || !tx.rolledBack || tx.committed {
		t.Fatalf("queue race error=%v transaction=%+v", err, tx)
	}
	if len(tx.queries) != 2 {
		t.Fatalf("queue race wrote facts: %v", tx.queries)
	}
}

func TestMergeWorkRecordsReparentsOperationalChildrenAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 14, 0, 0, 0, time.UTC)
	err := repository.MergeAtomic(context.Background(), workrecords.MergeMutation{
		Winner: workrecords.Record{Envelope: object.Envelope{
			ID: "winner", MSPID: "msp", ClientID: "client",
			Version: 6, UpdatedAt: at, UpdatedBy: "actor",
		}},
		Duplicate: workrecords.Record{Envelope: object.Envelope{
			ID: "duplicate", MSPID: "msp", ClientID: "client",
			LifecycleState: "deleted", Version: 4,
			UpdatedAt: at, UpdatedBy: "actor",
		}, DeletedAt: &at, DeletedBy: "actor", MergedIntoID: "winner"},
		ReparentChildren: true,
		Audit:            validAudit(at, "work_record.merged", "work_record", "duplicate"),
		Event:            validEvent(at, "work_record.merged", "work_record", "duplicate"),
	})
	if err != nil {
		t.Fatalf("MergeAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"lock_mention_authorization_revision", "UPDATE work_records", "UPDATE work_records",
		"UPDATE tasks", "UPDATE comments", "UPDATE attachments", "UPDATE time_entries",
		"UPDATE work_record_participants", "UPDATE work_record_participants",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestMergeWorkRecordsRollsBackIfEitherVersionIsStale(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 3}
	repository := NewWorkRecordRepository(&fakeSalesDB{tx: tx})
	err := repository.MergeAtomic(context.Background(), workrecords.MergeMutation{
		Winner: workrecords.Record{Envelope: object.Envelope{
			ID: "winner", MSPID: "msp", ClientID: "client", Version: 6,
		}},
		Duplicate: workrecords.Record{Envelope: object.Envelope{
			ID: "duplicate", MSPID: "msp", ClientID: "client", Version: 4,
		}, MergedIntoID: "winner"},
		ReparentChildren: true,
	})
	if !errors.Is(err, object.ErrVersionConflict) || len(tx.queries) != 3 || !tx.rolledBack {
		t.Fatalf("stale merge wrote child facts: err=%v tx=%+v", err, tx)
	}
}
