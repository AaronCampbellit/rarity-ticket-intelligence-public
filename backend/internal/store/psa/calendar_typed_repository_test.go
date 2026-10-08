package psa

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

const (
	repoMSP     = "00000000-0000-4000-8000-000000000401"
	repoClient  = "00000000-0000-4000-8000-000000000402"
	repoActor   = "00000000-0000-4000-8000-000000000403"
	repoSubject = "00000000-0000-4000-8000-000000000404"
	repoParent  = "00000000-0000-4000-8000-000000000405"
	repoOwner   = "00000000-0000-4000-8000-000000000406"
	repoChild   = "00000000-0000-4000-8000-000000000407"
	repoField   = "00000000-0000-4000-8000-000000000408"
	repoValue   = "00000000-0000-4000-8000-000000000409"
	repoRequest = "00000000-0000-4000-8000-000000000410"
)

func calendarFacts(at time.Time, msp, client, subject string) (mutation.AuditRecord, mutation.EventRecord) {
	return mutation.AuditRecord{ID: repoRequest, OccurredAt: at, MSPID: msp, ClientID: client, ActorType: "technician", ActorID: repoActor, Action: "created", SubjectType: "typed", SubjectID: subject, SubjectVersion: 1, Source: "test", CorrelationID: repoValue}, mutation.EventRecord{EventID: repoField, EventType: "created", SchemaVersion: 1, OccurredAt: at, MSPID: msp, ClientID: client, ActorType: "technician", ActorID: repoActor, SubjectType: "typed", SubjectID: subject, SubjectVersion: 1, Source: "test", CorrelationID: repoValue}
}

func TestCreateMilestoneAtomicValidatesCompositeProjectThenWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, repoClient, repoSubject)
	err := NewProjectRepository(&fakeSalesDB{tx: tx}).CreateMilestoneAtomic(context.Background(), projects.MilestoneMutation{Milestone: projects.Milestone{ID: repoSubject, MSPID: repoMSP, ClientID: repoClient, ProjectID: projects.ProjectID(repoParent), Name: "Cutover", Priority: "normal", Status: "planned", DueOn: at, OwnerID: repoOwner, Version: 1, CreatedAt: at, UpdatedAt: at, CreatedBy: repoActor, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest})
	if err != nil {
		t.Fatal(err)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO calendar_domain_requests", "INSERT INTO project_milestones", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if !strings.Contains(tx.queries[1], "project.id = $3") || !strings.Contains(tx.queries[1], "project.msp_id = $2") || !strings.Contains(tx.queries[1], "project.client_id = $4") {
		t.Fatalf("missing composite scope: %s", tx.queries[1])
	}
}

func TestTypedAtomicRepositoryRollsBackWithoutFactsOnSourceFailure(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, repoClient, repoSubject)
	err := NewCommitmentRepository(&fakeSalesDB{tx: tx}).CreateCommercialAtomic(context.Background(), commitments.CommercialMutation{Commitment: commitments.CommercialCommitment{ID: repoSubject, MSPID: repoMSP, ClientID: repoClient, Type: commitments.License, Title: "License", Vendor: "Vendor", OwnerID: repoOwner, EffectiveOn: at, ExpirationOn: at.AddDate(1, 0, 0), Status: "active", Version: 1, CreatedAt: at, UpdatedAt: at, CreatedBy: repoActor, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("committed=%v rollback=%v", tx.committed, tx.rolledBack)
	}
	if len(tx.queries) != 2 {
		t.Fatalf("facts written after failure: %v", tx.queries)
	}
}

func TestPublishScheduleAtomicLocksVersionAndWritesWindowsAndFacts(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(d ...any) { *d[0].(*int64) = 0 }}}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	err := NewWorkforceRepository(&fakeSalesDB{tx: tx}).PublishScheduleAtomic(context.Background(), workforce.ScheduleMutation{Schedule: workforce.Schedule{ID: repoSubject, MSPID: repoMSP, TechnicianID: repoOwner, Timezone: "UTC", EffectiveFrom: at, Version: 1, CreatedAt: at, CreatedBy: repoActor, Windows: []workforce.WeeklyWindow{{ID: repoChild, Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020, CapacityPercent: 100}}}, ExpectedVersion: 0, Audit: a, Event: e, RequestID: repoRequest})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tx.query, "FOR UPDATE") {
		t.Fatalf("version not locked: %s", tx.query)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO calendar_domain_requests", "INSERT INTO technician_schedule_versions", "INSERT INTO technician_schedule_windows", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
}

func TestUpdatePTOAtomicReturnsConflictForStaleVersion(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(d ...any) {
		*d[0].(*string) = repoOwner
		*d[1].(*workforce.PTOState) = workforce.Requested
		*d[2].(*int64) = 3
	}}}}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	err := NewWorkforceRepository(&fakeSalesDB{tx: tx}).UpdatePTOAtomic(context.Background(), workforce.PTOMutation{Request: workforce.PTORequest{ID: repoSubject, MSPID: repoMSP, TechnicianID: repoOwner, PTOType: "vacation", State: workforce.Approved, Version: 2, CreatedBy: repoOwner, UpdatedAt: at, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest, Authority: "requester"}, 1)
	if !errors.Is(err, workforce.ErrWorkforceVersionConflict) {
		t.Fatalf("error=%v", err)
	}
	if !tx.rolledBack {
		t.Fatal("stale mutation did not roll back")
	}
}

func TestUpdatePTOAtomicDecisionWritesPrivateSafeCanonicalFact(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(d ...any) {
			*d[0].(*string) = repoOwner
			*d[1].(*workforce.PTOState) = workforce.Requested
			*d[2].(*int64) = 1
		}},
		fakeRow{scan: func(d ...any) { *d[0].(*bool) = true }},
		fakeRow{scan: func(d ...any) { *d[0].(*bool) = true }},
	}}
	at := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	e.CorrelationID = repoValue
	p := workforce.PTORequest{
		ID: repoSubject, MSPID: repoMSP, TechnicianID: repoOwner,
		PTOType: "sick", State: workforce.Rejected, Version: 2,
		DecidedBy: repoActor, DecisionReason: "private medical detail",
		CreatedBy: repoOwner, UpdatedBy: repoActor, UpdatedAt: at,
	}
	err := NewWorkforceRepository(&fakeSalesDB{tx: tx}).UpdatePTOAtomic(context.Background(), workforce.PTOMutation{
		Request: p, Audit: a, Event: e, RequestID: repoRequest, Authority: "decision",
		IdempotencyKey: "pto-decision", RequestFingerprint: "fingerprint",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	payload := canonicalEventPayload(t, tx)
	assertCanonicalCalendarPayload(t, payload, repoOwner, "pto", "routine", "pto", repoSubject, "", "unavailability", 2)
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, forbidden := range []string{"private medical detail", "sick", "decision_reason", "pto_type"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("canonical PTO payload leaked %q: %s", forbidden, raw)
		}
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed=%v rolled_back=%v", tx.committed, tx.rolledBack)
	}
}

func TestUpdatePTOAtomicDecisionRollsBackWhenCanonicalFactFails(t *testing.T) {
	want := errors.New("canonical event unavailable")
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(d ...any) {
			*d[0].(*string) = repoOwner
			*d[1].(*workforce.PTOState) = workforce.Requested
			*d[2].(*int64) = 1
		}},
		fakeRow{scan: func(d ...any) { *d[0].(*bool) = true }},
		fakeRow{scan: func(d ...any) { *d[0].(*bool) = true }},
	}, failAt: 7, failError: want}
	at := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	p := workforce.PTORequest{ID: repoSubject, MSPID: repoMSP, TechnicianID: repoOwner, PTOType: "vacation", State: workforce.Approved, Version: 2, DecidedBy: repoActor, CreatedBy: repoOwner, UpdatedBy: repoActor, UpdatedAt: at}
	err := NewWorkforceRepository(&fakeSalesDB{tx: tx}).UpdatePTOAtomic(context.Background(), workforce.PTOMutation{Request: p, Audit: a, Event: e, RequestID: repoRequest, Authority: "decision", IdempotencyKey: "pto-decision", RequestFingerprint: "fingerprint"}, 1)
	if !errors.Is(err, want) || !tx.rolledBack || tx.committed {
		t.Fatalf("error=%v committed=%v rolled_back=%v queries=%d", err, tx.committed, tx.rolledBack, len(tx.queries))
	}
}

func TestUpdateMaintenanceAtomicReturnsConflictForStaleVersion(t *testing.T) {
	tx := &fakeSalesTx{zeroRowsAt: 2, queryRow: fakeRow{scan: func(d ...any) { *d[0].(*int64) = 4 }}}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	err := NewCommitmentRepository(&fakeSalesDB{tx: tx}).UpdateMaintenanceAtomic(context.Background(), commitments.MaintenanceMutation{Window: commitments.MaintenanceWindow{ID: repoSubject, MSPID: repoMSP, Title: "Window", StartsAt: at, EndsAt: at.Add(time.Hour), Timezone: "UTC", ConflictPolicy: commitments.PolicyWarning, Status: "planned", Version: 3, CreatedBy: repoActor, UpdatedAt: at, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest}, 2)
	if !errors.Is(err, commitments.ErrCommitmentVersionConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestTransitionMaintenanceAtomicNeverWritesScopeRows(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Now()
	a, e := calendarFacts(at, repoMSP, "", repoSubject)
	err := NewCommitmentRepository(&fakeSalesDB{tx: tx}).TransitionMaintenanceAtomic(context.Background(), commitments.MaintenanceMutation{Window: commitments.MaintenanceWindow{ID: repoSubject, MSPID: repoMSP, Status: "active", ConflictPolicy: commitments.PolicyHardBlock, Protected: true, Version: 2, CreatedBy: repoActor, UpdatedAt: at, UpdatedBy: repoActor, Scopes: []commitments.ResolvedScope{{ID: repoChild, ClientID: repoClient, Type: commitments.ScopeAsset, ResourceID: repoParent}}}, Audit: a, Event: e, RequestID: repoRequest}, 1)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tx.queries, "\n")
	if strings.Contains(joined, "maintenance_window_scopes") || strings.Contains(joined, "conflict_policy=") || strings.Contains(joined, "protected=") {
		t.Fatalf("transition mutated immutable window data: %s", joined)
	}
}

func TestSetCustomDateAtomicBindsDefinitionAndExactSourceRevision(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(d ...any) {
			*d[0].(*customfields.ValueKind) = customfields.DateKind
			*d[1].(*string) = "active"
			*d[2].(*int64) = 1
		}},
		fakeRow{scan: func(d ...any) {
			*d[0].(*string) = repoSubject
			*d[1].(*string) = repoMSP
			*d[2].(*string) = repoClient
			*d[3].(*int64) = 4
		}},
	}}
	at := time.Now()
	d := at
	a, e := calendarFacts(at, repoMSP, repoClient, repoSubject)
	err := NewCustomDateRepository(&fakeSalesDB{tx: tx}).SetDateAtomic(context.Background(), customfields.DateMutation{Definition: customfields.DateDefinition{ID: repoField, MSPID: repoMSP, ObjectType: customfields.ObjectWorkRecord, ValueKind: customfields.DateKind, LifecycleState: "active", Version: 1}, Source: customfields.SourceRecord{ID: repoSubject, MSPID: repoMSP, ClientID: repoClient, Type: customfields.ObjectWorkRecord, Version: 4}, Value: customfields.DateValue{ID: repoValue, FieldID: repoField, MSPID: repoMSP, ClientID: repoClient, ObjectType: customfields.ObjectWorkRecord, ObjectID: repoSubject, SourceRevision: 4, Version: 1, DateValue: &d, CreatedAt: at, UpdatedAt: at, CreatedBy: repoActor, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest})
	if err != nil {
		t.Fatal(err)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO calendar_domain_requests", "INSERT INTO object_custom_date_values", "INSERT INTO audit_ledger", "INSERT INTO event_outbox")
	if !strings.Contains(tx.calls[1], "calendar_custom_date_fields") || !strings.Contains(tx.calls[1], "FOR SHARE") || !strings.Contains(tx.calls[2], "work_records") || !strings.Contains(tx.calls[2], "FOR SHARE") {
		t.Fatalf("unbound custom date locks: %v", tx.calls)
	}
}

func TestSetCustomDateAtomicMapsCompetingCreateToStableConflict(t *testing.T) {
	tx := &fakeSalesTx{failAt: 2, failError: &pgconn.PgError{Code: "23505"}, queryRows: []row{
		fakeRow{scan: func(d ...any) {
			*d[0].(*customfields.ValueKind) = customfields.DateKind
			*d[1].(*string) = "active"
			*d[2].(*int64) = 1
		}},
		fakeRow{scan: func(d ...any) {
			*d[0].(*string) = repoSubject
			*d[1].(*string) = repoMSP
			*d[2].(*string) = repoClient
			*d[3].(*int64) = 4
		}},
	}}
	at := time.Now()
	d := at
	a, e := calendarFacts(at, repoMSP, repoClient, repoSubject)
	err := NewCustomDateRepository(&fakeSalesDB{tx: tx}).SetDateAtomic(context.Background(), customfields.DateMutation{Definition: customfields.DateDefinition{ID: repoField, MSPID: repoMSP, ObjectType: customfields.ObjectWorkRecord, ValueKind: customfields.DateKind, LifecycleState: "active", Version: 1}, Source: customfields.SourceRecord{ID: repoSubject, MSPID: repoMSP, ClientID: repoClient, Type: customfields.ObjectWorkRecord, Version: 4}, Value: customfields.DateValue{ID: repoValue, FieldID: repoField, MSPID: repoMSP, ClientID: repoClient, ObjectType: customfields.ObjectWorkRecord, ObjectID: repoSubject, SourceRevision: 4, Version: 1, DateValue: &d, CreatedAt: at, UpdatedAt: at, CreatedBy: repoActor, UpdatedBy: repoActor}, Audit: a, Event: e, RequestID: repoRequest})
	if !errors.Is(err, customfields.ErrCustomDateVersionConflict) || !tx.rolledBack {
		t.Fatalf("error=%v rolledBack=%v", err, tx.rolledBack)
	}
}

func TestCustomDateSourceLookupUsesOnlyTypedTable(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}}
	_, err := NewCustomDateRepository(db).FindDateSource(context.Background(), scope.Target{MSPID: repoMSP, ClientID: repoClient}, customfields.ObjectType("generic_event"), repoSubject)
	if !errors.Is(err, customfields.ErrInvalidCustomDate) {
		t.Fatalf("error=%v", err)
	}
}

func TestTask3RepositoriesRejectNoncanonicalUUIDBeforeCast(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{err: errors.New("query must not run")}}
	if _, err := NewProjectRepository(db).FindMilestone(context.Background(), scope.Target{MSPID: "urn:uuid:" + repoMSP, ClientID: repoClient}, repoSubject); !errors.Is(err, projects.ErrInvalidMilestone) {
		t.Fatalf("project error=%v", err)
	}
	if _, err := NewCommitmentRepository(db).FindMaintenance(context.Background(), repoMSP, "00000000000040008000000000000404"); !errors.Is(err, commitments.ErrInvalidMaintenance) {
		t.Fatalf("maintenance error=%v", err)
	}
	if _, err := NewWorkforceRepository(db).FindPTO(context.Background(), repoMSP, "{"+repoSubject+"}"); !errors.Is(err, workforce.ErrInvalidPTO) {
		t.Fatalf("pto error=%v", err)
	}
	if _, err := NewCustomDateRepository(db).FindDateDefinition(context.Background(), repoMSP, "00000000-0000-4000-8000-0000000004AA", customfields.ObjectWorkRecord); !errors.Is(err, customfields.ErrInvalidCustomDate) {
		t.Fatalf("custom date error=%v", err)
	}
}
