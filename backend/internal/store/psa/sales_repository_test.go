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
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func testInitialTags(at time.Time) tagging.InitialAssignmentSet {
	return tagging.InitialAssignmentSet{Direct: []tagging.Assignment{{
		Tag:    tagging.Tag{ID: "tag", InternalKey: "network", State: tagging.StateActive},
		Source: tagging.SourceHuman,
	}}}.WithProvenance(tagging.InitialAssignmentProvenance{
		ActorType: "technician", ActorID: "actor", OccurredAt: at,
		CorrelationID: "correlation", Evidence: map[string]any{"request": "create"},
	})
}

type fakeSalesDB struct {
	tx         *fakeSalesTx
	beginCalls int
	query      string
	args       []any
	queryRow   row
	queryQueue []row
	queries    []string
	queryRows  rows
	session    *fakeSession
}

type fakeSession struct {
	db       *fakeSalesDB
	calls    []string
	released bool
}

func (db *fakeSalesDB) Acquire(context.Context) (executionSession, error) {
	if db.session == nil {
		return nil, errors.New("unexpected Acquire")
	}
	db.session.db = db
	return db.session, nil
}

func (session *fakeSession) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	session.calls = append(session.calls, query)
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (session *fakeSession) QueryRow(_ context.Context, query string, args ...any) row {
	session.calls = append(session.calls, query)
	if session.db.queryRow == nil {
		return fakeRow{err: errors.New("unexpected session QueryRow")}
	}
	return session.db.queryRow
}

func (session *fakeSession) Release() {
	session.released = true
}

func (db *fakeSalesDB) Begin(context.Context) (transaction, error) {
	db.beginCalls++
	return db.tx, nil
}

func (db *fakeSalesDB) QueryRow(_ context.Context, query string, args ...any) row {
	db.query, db.args = query, args
	db.queries = append(db.queries, query)
	if len(db.queryQueue) > 0 {
		result := db.queryQueue[0]
		db.queryQueue = db.queryQueue[1:]
		return result
	}
	if db.queryRow == nil {
		return fakeRow{err: errors.New("unexpected QueryRow")}
	}
	return db.queryRow
}

func (db *fakeSalesDB) Query(_ context.Context, query string, args ...any) (rows, error) {
	db.query, db.args = query, args
	if db.queryRows == nil {
		return nil, errors.New("unexpected Query")
	}
	return db.queryRows, nil
}

type fakeSalesTx struct {
	queries     []string
	args        [][]any
	query       string
	queryArgs   []any
	queryRow    row
	queryRows   []row
	queryResult rows
	queryErr    error
	calls       []string
	failAt      int
	failError   error
	zeroRowsAt  int
	committed   bool
	rolledBack  bool
	commitErr   error
}

func (tx *fakeSalesTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.calls = append(tx.calls, query)
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	if tx.failAt > 0 && len(tx.queries) == tx.failAt {
		if tx.failError != nil {
			return pgconn.CommandTag{}, tx.failError
		}
		return pgconn.CommandTag{}, errors.New("write failed")
	}
	if tx.zeroRowsAt > 0 && len(tx.queries) == tx.zeroRowsAt {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *fakeSalesTx) Query(
	_ context.Context,
	query string,
	args ...any,
) (rows, error) {
	tx.calls = append(tx.calls, query)
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	if tx.queryResult == nil {
		return &fakeRows{}, nil
	}
	return tx.queryResult, nil
}

func (tx *fakeSalesTx) QueryRow(_ context.Context, query string, args ...any) row {
	tx.calls = append(tx.calls, query)
	tx.query, tx.queryArgs = query, args
	if len(tx.queryRows) > 0 {
		result := tx.queryRows[0]
		tx.queryRows = tx.queryRows[1:]
		return result
	}
	if tx.queryRow != nil {
		return tx.queryRow
	}
	return fakeRow{err: errors.New("unexpected QueryRow")}
}

func (tx *fakeSalesTx) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *fakeSalesTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type fakeRow struct {
	err  error
	scan func(...any)
}

type fakeRows struct {
	index int
	scans []func(...any)
	err   error
}

func (r *fakeRows) Next() bool { return r.index < len(r.scans) }
func (r *fakeRows) Scan(destinations ...any) error {
	r.scans[r.index](destinations...)
	r.index++
	return nil
}
func (r *fakeRows) Err() error { return r.err }
func (r *fakeRows) Close()     {}

func (r fakeRow) Scan(destinations ...any) error {
	if r.scan != nil {
		r.scan(destinations...)
	}
	return r.err
}

func pipelineStageRows(stages ...sales.PipelineStage) rows {
	scans := make([]func(...any), 0, len(stages))
	for _, value := range stages {
		stage := value
		scans = append(scans, func(destinations ...any) {
			*destinations[0].(*string) = string(stage.ID)
			*destinations[1].(*string) = stage.PipelineID
			*destinations[2].(*string) = stage.Key
			*destinations[3].(*string) = stage.Name
			*destinations[4].(*int32) = int32(stage.Position)
			*destinations[5].(*int32) = int32(stage.Probability)
			*destinations[6].(*string) = string(stage.Category)
			required, _ := json.Marshal(stage.RequiredFields)
			allowed, _ := json.Marshal(stage.AllowedNext)
			*destinations[7].(*[]byte) = required
			*destinations[8].(*[]byte) = allowed
			*destinations[9].(*bool) = stage.RequiresProposal
			*destinations[10].(*bool) = stage.RequiresApproval
			*destinations[11].(*int64) = stage.Version
		})
	}
	return &fakeRows{scans: scans}
}

func activityFenceMutation(at time.Time) sales.CreateOpportunityActivityMutation {
	return sales.CreateOpportunityActivityMutation{
		Activity: sales.OpportunityActivity{
			ID: "activity", MSPID: "msp", ClientID: "client",
			OpportunityID: "opportunity", Kind: "call", Summary: "Discovery",
			OccurredAt: at, CreatedAt: at, CreatedBy: "actor",
		},
		ExpectedClientVersion:      4,
		ExpectedOpportunityVersion: 8,
		Pipeline:                   sales.Pipeline{ID: "pipeline", MSPID: "msp", Version: 5},
		Stage: sales.PipelineStage{
			ID: "stage", PipelineID: "pipeline", Key: "qualified", Name: "Qualified",
			Position: 1, Category: sales.Weighted, Version: 6,
		},
		Audit: validAudit(at, "opportunity.activity.created", "opportunity_activity", "activity"),
		Event: validEvent(at, "opportunity.activity.created", "opportunity_activity", "activity"),
	}
}

func TestCreateOpportunityActivityValidatesScopedParentThenWritesFacts(t *testing.T) {
	stage := activityFenceMutation(time.Now()).Stage
	tx := &fakeSalesTx{queryResult: pipelineStageRows(stage)}
	at := time.Date(2026, time.July, 30, 18, 0, 0, 0, time.UTC)
	err := NewSalesRepository(&fakeSalesDB{tx: tx}).CreateOpportunityActivityAtomic(
		context.Background(), activityFenceMutation(at),
	)
	if err != nil {
		t.Fatalf("CreateOpportunityActivityAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"FROM client_organizations", "FROM opportunities", "FROM pipelines",
		"FROM pipeline_stages", "INSERT INTO opportunity_activities",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateOpportunityActivityRejectsCommitFenceDriftBeforeFacts(t *testing.T) {
	at := time.Date(2026, time.August, 6, 18, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		zeroRowsAt int
		stage      sales.PipelineStage
	}{
		{name: "client", zeroRowsAt: 1, stage: activityFenceMutation(at).Stage},
		{name: "opportunity", zeroRowsAt: 2, stage: activityFenceMutation(at).Stage},
		{name: "pipeline", zeroRowsAt: 3, stage: activityFenceMutation(at).Stage},
		{name: "stage version", stage: func() sales.PipelineStage {
			stage := activityFenceMutation(at).Stage
			stage.Version++
			return stage
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{zeroRowsAt: test.zeroRowsAt, queryResult: pipelineStageRows(test.stage)}
			err := NewSalesRepository(&fakeSalesDB{tx: tx}).CreateOpportunityActivityAtomic(
				context.Background(), activityFenceMutation(at),
			)
			if err == nil || tx.committed || !tx.rolledBack {
				t.Fatalf("fence drift error=%v committed=%v rolledBack=%v", err, tx.committed, tx.rolledBack)
			}
			for _, query := range tx.queries {
				if strings.Contains(query, "INSERT INTO opportunity_activities") ||
					strings.Contains(query, "INSERT INTO audit_ledger") ||
					strings.Contains(query, "INSERT INTO event_outbox") {
					t.Fatalf("fence drift wrote mutation: %s", query)
				}
			}
		})
	}
}

func TestForecastAggregatesScopedWeightedPipelineAmounts(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "pipeline"
			*destinations[1].(*sales.PipelineStageID) = "stage"
			*destinations[2].(*string) = "Qualified"
			*destinations[3].(*sales.ForecastCategory) = sales.Weighted
			*destinations[4].(*uint8) = 50
			*destinations[5].(*int64) = 2
			*destinations[6].(*string) = "USD"
			*destinations[7].(*int64) = 10000
			*destinations[8].(*int64) = 5000
		},
	}}}
	buckets, err := NewSalesRepository(db).Forecast(
		context.Background(), scope.Target{MSPID: "msp", ClientID: "client"}, "pipeline",
	)
	if err != nil || len(buckets) != 1 ||
		buckets[0].WeightedAmount.Minor != 5000 ||
		buckets[0].Amount.Currency != "USD" {
		t.Fatalf("Forecast() buckets=%+v error=%v", buckets, err)
	}
	if !strings.Contains(db.query, "sum(o.amount_minor * s.probability / 100)") {
		t.Fatalf("forecast query lacks weighted aggregation: %s", db.query)
	}
}

func TestListOpportunitiesUsesStableScopedCursor(t *testing.T) {
	at := time.Date(2026, time.July, 30, 21, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "opportunity"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "client"
			*destinations[3].(*string) = ""
			*destinations[4].(*string) = "pipeline"
			*destinations[5].(*string) = "stage"
			*destinations[6].(*string) = "OPP-100"
			*destinations[7].(*string) = "Network refresh"
			*destinations[8].(*int64) = 10000
			*destinations[9].(*string) = "USD"
			*destinations[10].(*int64) = 2
			*destinations[11].(*time.Time) = at
			*destinations[12].(*string) = "actor"
			*destinations[13].(*[]byte) = []byte(`{"description":"Refresh"}`)
			*destinations[14].(*[]byte) = []byte(`{}`)
			*destinations[15].(*string) = "team"
			*destinations[16].(*[]byte) = []byte(`["contact"]`)
			*destinations[17].(*bool) = true
			*destinations[18].(*bool) = false
		},
	}}}
	found, err := NewSalesRepository(db).ListOpportunities(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		sales.OpportunityListFilter{PipelineID: "pipeline", Limit: 25},
	)
	if err != nil || len(found) != 1 || found[0].Amount.Currency != "USD" {
		t.Fatalf("ListOpportunities() found=%+v error=%v", found, err)
	}
	if !strings.Contains(db.query, "(o.updated_at, o.id) <") ||
		!strings.Contains(db.query, "ORDER BY o.updated_at DESC, o.id DESC") {
		t.Fatalf("opportunity list lacks stable cursor: %s", db.query)
	}
}

func TestResolveOpportunityReferenceIsActiveClientScopedDisplayIDFirstAndBounded(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewSalesRepository(db).FindOpportunitiesByReference(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"  Network\u00a0Refresh  ",
		9,
	)
	if err != nil {
		t.Fatalf("FindOpportunitiesByReference() error=%v", err)
	}
	for _, fragment := range []string{
		"o.msp_id = $1", "o.client_id = $2::uuid", "o.lifecycle_state = 'active'",
		"translate(o.display_id, $4", "translate(o.name, $4",
		"CASE WHEN lower(btrim(regexp_replace(",
		"ORDER BY", "o.id", "LIMIT $5",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("opportunity resolver missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 5 || db.args[0] != "msp" || db.args[1] != "client" ||
		db.args[2] != "network refresh" || db.args[3] != unicodeReferenceWhitespace ||
		db.args[4] != 2 {
		t.Fatalf("opportunity resolver args=%+v", db.args)
	}
}

func TestResolveStageReferenceIsBoundToOpportunityPipelineAndClient(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	_, err := NewSalesRepository(db).FindStagesByReference(
		context.Background(),
		scope.Target{MSPID: "msp", ClientID: "client"},
		"opportunity",
		"  Qualified  ",
		7,
	)
	if err != nil {
		t.Fatalf("FindStagesByReference() error=%v", err)
	}
	for _, fragment := range []string{
		"JOIN opportunities o", "o.id = $3", "o.msp_id = $1",
		"o.client_id = $2::uuid", "o.lifecycle_state = 'active'",
		"stage.pipeline_id = o.pipeline_id", "stage.id::text = $4",
		"translate(stage.key, $6", "translate(stage.name, $6",
		"ORDER BY", "stage.position", "stage.id", "LIMIT $7",
	} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("stage resolver missing %q: %s", fragment, db.query)
		}
	}
	if len(db.args) != 7 || db.args[0] != "msp" || db.args[1] != "client" ||
		db.args[2] != sales.OpportunityID("opportunity") || db.args[3] != "Qualified" ||
		db.args[4] != "qualified" || db.args[5] != unicodeReferenceWhitespace ||
		db.args[6] != 2 {
		t.Fatalf("stage resolver args=%+v", db.args)
	}
}

func TestListProspectsIsMSPScopedAndActiveOnly(t *testing.T) {
	at := time.Date(2026, time.July, 30, 21, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRows: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*destinations[0].(*string) = "prospect"
			*destinations[1].(*string) = "msp"
			*destinations[2].(*string) = "LEAD-100"
			*destinations[3].(*string) = "Northwind"
			*destinations[4].(*string) = "buyer@example.test"
			*destinations[5].(*string) = ""
			*destinations[6].(*int64) = 1
			*destinations[7].(*time.Time) = at
			*destinations[8].(*string) = "actor"
		},
	}}}
	found, err := NewSalesRepository(db).ListProspects(
		context.Background(), "msp", 100,
	)
	if err != nil || len(found) != 1 || found[0].DisplayID != "LEAD-100" {
		t.Fatalf("ListProspects() found=%+v error=%v", found, err)
	}
	if len(db.args) != 2 || db.args[0] != "msp" ||
		!strings.Contains(db.query, "msp_id = $1") ||
		!strings.Contains(db.query, "lifecycle_state = 'active'") {
		t.Fatalf("prospect list is not safely scoped: query=%s args=%v", db.query, db.args)
	}
}

func TestCreateProspectAtomicWritesRecordAuditAndOutboxInOneTransaction(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	err := repository.CreateProspectAtomic(context.Background(), sales.CreateProspectMutation{
		Prospect: sales.Prospect{
			ID: "11111111-1111-4111-8111-111111111111", MSPID: "22222222-2222-4222-8222-222222222222",
			DisplayID: "LEAD-1", Name: "Northwind", Email: "buyer@example.com",
			Version: 1, CreatedAt: at, CreatedBy: "33333333-3333-4333-8333-333333333333",
		},
		Audit: mutation.AuditRecord{
			ID: "44444444-4444-4444-8444-444444444444", OccurredAt: at,
			MSPID:     "22222222-2222-4222-8222-222222222222",
			ActorType: "technician", ActorID: "33333333-3333-4333-8333-333333333333",
			Action: "prospect.created", SubjectType: "prospect",
			SubjectID: "11111111-1111-4111-8111-111111111111", SubjectVersion: 1,
			Source: "api", CorrelationID: "55555555-5555-4555-8555-555555555555",
		},
		Event: mutation.EventRecord{
			EventID:   "66666666-6666-4666-8666-666666666666",
			EventType: "prospect.created", SchemaVersion: 1, OccurredAt: at,
			MSPID:     "22222222-2222-4222-8222-222222222222",
			ActorType: "technician", ActorID: "33333333-3333-4333-8333-333333333333",
			SubjectType: "prospect", SubjectID: "11111111-1111-4111-8111-111111111111",
			SubjectVersion: 1, Source: "api",
			CorrelationID: "55555555-5555-4555-8555-555555555555",
		},
	})

	if err != nil {
		t.Fatalf("CreateProspectAtomic() error = %v", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
	assertQueryOrder(
		t, tx.queries,
		"pg_advisory_xact_lock", "SELECT name, display_id",
		"INSERT INTO prospects", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestCreateProspectAtomicSerializesNormalizedAllLifecycleIdentityBeforeFacts(t *testing.T) {
	tx := &fakeSalesTx{queryResult: &fakeRows{scans: []func(...any){
		func(destinations ...any) {
			*(destinations[0].(*string)) = "\u00a0NORTHWIND\u2003LEGAL "
			*(destinations[1].(*string)) = "PRO-OLD"
		},
	}}}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	err := repository.CreateProspectAtomic(context.Background(), sales.CreateProspectMutation{
		Prospect: sales.Prospect{
			ID: "prospect", MSPID: "msp", DisplayID: "PRO-NEW",
			Name: "northwind legal", Version: 1, CreatedAt: time.Now(), CreatedBy: "actor",
		},
	})
	if !errors.Is(err, sales.ErrProspectIdentityConflict) {
		t.Fatalf("CreateProspectAtomic() error=%v, want identity conflict", err)
	}
	if tx.committed || !tx.rolledBack || len(tx.queries) != 2 {
		t.Fatalf("conflict transaction=%+v", tx)
	}
	if !strings.Contains(tx.queries[0], "pg_advisory_xact_lock") ||
		!strings.Contains(tx.queries[1], "SELECT name, display_id") ||
		strings.Contains(tx.queries[1], "lifecycle_state") {
		t.Fatalf("identity operations=%v", tx.queries)
	}
	for _, query := range tx.queries {
		if strings.Contains(query, "INSERT INTO") {
			t.Fatalf("identity conflict reached write: %s", query)
		}
	}
}

func TestCreateOpportunityAtomicValidatesScopedSourceStageAndOwner(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	err := repository.CreateOpportunityAtomic(context.Background(), sales.CreateOpportunityMutation{
		Opportunity: sales.Opportunity{
			ID: "opportunity", MSPID: "msp", ClientID: "client",
			PipelineID: "pipeline", StageID: "stage", DisplayID: "OPP-100",
			Name:   "Network refresh",
			Amount: sales.Money{Minor: 1250000, Currency: "USD"},
			Fields: map[sales.FieldKey]string{
				"description": "Replace core network",
				"owner_id":    "owner",
			},
			Version: 1, UpdatedAt: at, UpdatedBy: "actor",
		},
		Audit: mutation.AuditRecord{
			ID: "audit", OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			Action: "opportunity.created", SubjectType: "opportunity",
			SubjectID: "opportunity", SubjectVersion: 1,
			Source: "api", CorrelationID: "correlation",
		},
		Event: mutation.EventRecord{
			EventID: "event", EventType: "opportunity.created", SchemaVersion: 1,
			OccurredAt: at, MSPID: "msp", ClientID: "client",
			ActorType: "technician", ActorID: "actor",
			SubjectType: "opportunity", SubjectID: "opportunity",
			SubjectVersion: 1, Source: "api", CorrelationID: "correlation",
		},
	})
	if err != nil {
		t.Fatalf("CreateOpportunityAtomic() error=%v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"INSERT INTO opportunities", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	for _, fragment := range []string{
		"FROM pipelines", "pipeline_stages", "client_organizations",
		"technicians", "lifecycle_state = 'active'",
	} {
		if !strings.Contains(tx.queries[0], fragment) {
			t.Fatalf("opportunity insert missing %q: %s", fragment, tx.queries[0])
		}
	}
}

func TestNewSalesRepositoryFromPoolBuildsProductionAdapter(t *testing.T) {
	repository := NewSalesRepositoryFromPool(nil)
	if repository == nil || repository.db == nil {
		t.Fatal("production repository did not retain a pool adapter")
	}
}

func TestWriteMutationFactsPreservesAutomationCausation(t *testing.T) {
	tx := &fakeSalesTx{}
	err := writeMutationFacts(
		context.Background(), tx,
		mutation.AuditRecord{
			ID: "audit", OccurredAt: time.Now(), MSPID: "msp",
			ActorType: "automation", ActorID: "automation-id",
			Action:      "work_record.owner.changed",
			SubjectType: "work_record", SubjectID: "work-id",
			SubjectVersion: 2, Source: "automation",
			CorrelationID: "correlation",
		},
		mutation.EventRecord{
			EventID: "event", EventType: "work_record.owner.changed",
			SchemaVersion: 1, OccurredAt: time.Now(), MSPID: "msp",
			ActorType: "automation", ActorID: "automation-id",
			SubjectType: "work_record", SubjectID: "work-id",
			SubjectVersion: 2, Source: "automation",
			CorrelationID: "correlation", CausationID: "run-id",
			Data: map[string]any{"owner_id": "owner-id"},
		},
	)
	if err != nil || len(tx.queries) != 2 ||
		!strings.Contains(tx.queries[1], "causation_id") ||
		!strings.Contains(tx.queries[1], "$15::jsonb") ||
		len(tx.args[1]) != 15 ||
		tx.args[1][12] != "run-id" {
		t.Fatalf(
			"writeMutationFacts() error=%v query=%v args=%v",
			err, tx.queries, tx.args,
		)
	}
}

func TestCreatePipelineAtomicWritesEveryStageBeforeAuditAndOutbox(t *testing.T) {
	tx := &fakeSalesTx{}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	mutation := sales.CreatePipelineMutation{
		Pipeline: sales.Pipeline{
			ID: "11111111-1111-4111-8111-111111111111", MSPID: "22222222-2222-4222-8222-222222222222",
			Key: "default", Name: "Default", Version: 1, CreatedAt: at,
			CreatedBy: "33333333-3333-4333-8333-333333333333",
			Stages: []sales.PipelineStage{
				{ID: "44444444-4444-4444-8444-444444444444", PipelineID: "11111111-1111-4111-8111-111111111111", Probability: 10, Category: sales.PipelineCategory},
				{ID: "55555555-5555-4555-8555-555555555555", PipelineID: "11111111-1111-4111-8111-111111111111", Probability: 50, Category: sales.Weighted},
			},
		},
		Audit: validAudit(at, "pipeline.created", "pipeline", "11111111-1111-4111-8111-111111111111"),
		Event: validEvent(at, "pipeline.created", "pipeline", "11111111-1111-4111-8111-111111111111"),
	}

	if err := repository.CreatePipelineAtomic(context.Background(), mutation); err != nil {
		t.Fatalf("CreatePipelineAtomic() error = %v", err)
	}

	assertQueryOrder(t, tx.queries,
		"INSERT INTO pipelines", "INSERT INTO pipeline_stages", "INSERT INTO pipeline_stages",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
}

func TestAtomicMutationRollsBackWhenAuditWriteFails(t *testing.T) {
	tx := &fakeSalesTx{failAt: 4}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})

	err := repository.CreateProspectAtomic(context.Background(), sales.CreateProspectMutation{
		Prospect: sales.Prospect{ID: "record", MSPID: "msp", DisplayID: "LEAD-1", Name: "Northwind"},
		Audit:    validAudit(time.Now(), "prospect.created", "prospect", "record"),
		Event:    validEvent(time.Now(), "prospect.created", "prospect", "record"),
	})

	if err == nil {
		t.Fatal("CreateProspectAtomic() unexpectedly passed")
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("transaction state committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
	}
}

func TestTransitionAtomicUsesPreviousVersionAndCommitsFacts(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	accepted := sales.TransitionMutation{
		Opportunity: sales.Opportunity{
			ID:         "11111111-1111-4111-8111-111111111111",
			MSPID:      "22222222-2222-4222-8222-222222222222",
			ClientID:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			PipelineID: "66666666-6666-4666-8666-666666666666",
			StageID:    "44444444-4444-4444-8444-444444444444",
			Version:    4, UpdatedAt: at,
			UpdatedBy: "33333333-3333-4333-8333-333333333333",
		},
		PreviousStage:         "55555555-5555-4555-8555-555555555555",
		ExpectedClientVersion: 7,
		Pipeline: sales.Pipeline{
			ID:    "66666666-6666-4666-8666-666666666666",
			MSPID: "22222222-2222-4222-8222-222222222222", Version: 3,
		},
		CurrentStage: sales.PipelineStage{
			ID:         "55555555-5555-4555-8555-555555555555",
			PipelineID: "66666666-6666-4666-8666-666666666666",
			Key:        "qualified", Name: "Qualified", Position: 1,
			Category:    sales.Weighted,
			AllowedNext: []sales.PipelineStageID{"44444444-4444-4444-8444-444444444444"},
			Version:     4,
		},
		DestinationStage: sales.PipelineStage{
			ID:         "44444444-4444-4444-8444-444444444444",
			PipelineID: "66666666-6666-4666-8666-666666666666",
			Key:        "committed", Name: "Committed", Position: 2,
			Category: sales.Committed, Version: 5,
		},
		Audit: validAudit(at, "opportunity.stage.changed", "opportunity", "11111111-1111-4111-8111-111111111111"),
		Event: validEvent(at, "opportunity.stage.changed", "opportunity", "11111111-1111-4111-8111-111111111111"),
	}
	tx := &fakeSalesTx{queryResult: pipelineStageRows(accepted.DestinationStage, accepted.CurrentStage)}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	err := repository.TransitionAtomic(context.Background(), accepted)

	if err != nil {
		t.Fatalf("TransitionAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"FROM client_organizations", "FROM opportunities", "FROM pipelines",
		"FROM pipeline_stages",
		"UPDATE opportunities", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[4], "version = $5") ||
		!strings.Contains(tx.queries[1], "FOR UPDATE") ||
		!strings.Contains(tx.queries[3], "ORDER BY id") ||
		!strings.Contains(tx.queries[3], "FOR SHARE") {
		t.Fatalf("transition lacks deterministic configuration/version fence: %+v", tx.queries)
	}
}

func TestTransitionAtomicReturnsVersionConflictWithoutFacts(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	accepted := transitionFenceMutation(at)
	tx := &fakeSalesTx{zeroRowsAt: 2, queryResult: pipelineStageRows(accepted.CurrentStage, accepted.DestinationStage)}
	repository := NewSalesRepository(&fakeSalesDB{tx: tx})
	err := repository.TransitionAtomic(context.Background(), accepted)

	if !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("TransitionAtomic() error = %v", err)
	}
	if len(tx.queries) != 2 || tx.committed || !tx.rolledBack {
		t.Fatalf("stale transition wrote facts or committed: %+v", tx)
	}
}

func transitionFenceMutation(at time.Time) sales.TransitionMutation {
	return sales.TransitionMutation{
		Opportunity: sales.Opportunity{
			ID: "opportunity", MSPID: "msp", ClientID: "client", PipelineID: "pipeline",
			StageID: "destination", Version: 4, UpdatedAt: at, UpdatedBy: "actor",
		},
		PreviousStage: "current", ExpectedClientVersion: 7,
		Pipeline: sales.Pipeline{ID: "pipeline", MSPID: "msp", Version: 3},
		CurrentStage: sales.PipelineStage{
			ID: "current", PipelineID: "pipeline", Key: "current", Name: "Current",
			Position: 1, Category: sales.Weighted,
			AllowedNext: []sales.PipelineStageID{"destination"}, Version: 4,
		},
		DestinationStage: sales.PipelineStage{
			ID: "destination", PipelineID: "pipeline", Key: "destination", Name: "Destination",
			Position: 2, Category: sales.Committed, Version: 5,
		},
		Audit: validAudit(at, "opportunity.stage.changed", "opportunity", "opportunity"),
		Event: validEvent(at, "opportunity.stage.changed", "opportunity", "opportunity"),
	}
}

func TestTransitionAtomicRejectsClientPipelineOrStageDriftBeforeUpdateAndFacts(t *testing.T) {
	at := time.Date(2026, time.August, 6, 19, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		zeroRowsAt int
		stages     func(sales.TransitionMutation) []sales.PipelineStage
	}{
		{name: "client", zeroRowsAt: 1},
		{name: "opportunity version", zeroRowsAt: 2},
		{name: "pipeline", zeroRowsAt: 3},
		{name: "current stage version", stages: func(accepted sales.TransitionMutation) []sales.PipelineStage {
			accepted.CurrentStage.Version++
			return []sales.PipelineStage{accepted.CurrentStage, accepted.DestinationStage}
		}},
		{name: "destination configuration", stages: func(accepted sales.TransitionMutation) []sales.PipelineStage {
			accepted.DestinationStage.RequiresApproval = true
			return []sales.PipelineStage{accepted.CurrentStage, accepted.DestinationStage}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			accepted := transitionFenceMutation(at)
			stages := []sales.PipelineStage{accepted.CurrentStage, accepted.DestinationStage}
			if test.stages != nil {
				stages = test.stages(accepted)
			}
			tx := &fakeSalesTx{zeroRowsAt: test.zeroRowsAt, queryResult: pipelineStageRows(stages...)}
			err := NewSalesRepository(&fakeSalesDB{tx: tx}).TransitionAtomic(context.Background(), accepted)
			if err == nil || tx.committed || !tx.rolledBack {
				t.Fatalf("fence drift error=%v committed=%v rolledBack=%v", err, tx.committed, tx.rolledBack)
			}
			for _, query := range tx.queries {
				if strings.Contains(query, "UPDATE opportunities") ||
					strings.Contains(query, "INSERT INTO audit_ledger") ||
					strings.Contains(query, "INSERT INTO event_outbox") {
					t.Fatalf("fence drift wrote transition: %s", query)
				}
			}
		})
	}
}

func TestReplaceOpportunityFieldsAtomicUsesScopeVersionAndFacts(t *testing.T) {
	tx := &fakeSalesTx{}
	at := time.Date(2026, time.July, 31, 20, 0, 0, 0, time.UTC)
	err := NewSalesRepository(&fakeSalesDB{tx: tx}).ReplaceOpportunityFieldsAtomic(
		context.Background(),
		sales.ReplaceOpportunityFieldsMutation{
			Opportunity: sales.Opportunity{
				ID: "opportunity", MSPID: "msp", ClientID: "client", Version: 4,
				CustomFields: map[string]string{"risk_summary": "Weekend cutover"},
				UpdatedAt:    at, UpdatedBy: "actor",
			},
			Audit: validAudit(at, "opportunity.custom_fields.replaced", "opportunity", "opportunity"),
			Event: validEvent(at, "opportunity.custom_fields.replaced", "opportunity", "opportunity"),
		},
	)
	if err != nil {
		t.Fatalf("ReplaceOpportunityFieldsAtomic() error = %v", err)
	}
	assertQueryOrder(
		t, tx.queries,
		"UPDATE opportunities", "INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.queries[0], "client_id = $3") ||
		!strings.Contains(tx.queries[0], "version = $4") ||
		!strings.Contains(string(tx.args[0][4].([]byte)), "risk_summary") {
		t.Fatalf("field update lost scope/version/value contract: %s args=%v",
			tx.queries[0], tx.args[0])
	}
}

func TestReplaceOpportunityParticipantsValidatesTeamContactsAndWritesFacts(t *testing.T) {
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "team"
	}}}
	at := time.Date(2026, time.July, 31, 22, 0, 0, 0, time.UTC)
	err := NewSalesRepository(&fakeSalesDB{tx: tx}).ReplaceOpportunityParticipantsAtomic(
		context.Background(),
		sales.ReplaceOpportunityParticipantsMutation{
			Opportunity: sales.Opportunity{
				ID: "opportunity", MSPID: "msp", ClientID: "client", Version: 5,
				TeamID: "team", ContactIDs: []string{"contact"},
				UpdatedAt: at, UpdatedBy: "actor",
			},
			Audit: validAudit(at, "opportunity.participants.replaced", "opportunity", "opportunity"),
			Event: validEvent(at, "opportunity.participants.replaced", "opportunity", "opportunity"),
		},
	)
	if err != nil {
		t.Fatalf("ReplaceOpportunityParticipantsAtomic() error = %v", err)
	}
	assertQueryOrder(t, tx.queries,
		"UPDATE opportunities", "DELETE FROM opportunity_contacts",
		"INSERT INTO opportunity_contacts", "INSERT INTO audit_ledger",
		"INSERT INTO event_outbox",
	)
	if !strings.Contains(tx.query, "FROM teams") ||
		!strings.Contains(tx.queries[2], "contact.client_id = $3") {
		t.Fatalf("participants lost scoped validation: query=%s writes=%v",
			tx.query, tx.queries)
	}
}

func TestFindStageMapsPersistedConfiguration(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		*destinations[0].(*string) = "stage-id"
		*destinations[1].(*string) = "pipeline-id"
		*destinations[2].(*string) = "proposal"
		*destinations[3].(*string) = "Proposal"
		*destinations[4].(*int32) = 2
		*destinations[5].(*int32) = 60
		*destinations[6].(*string) = "weighted"
		*destinations[7].(*[]byte) = []byte(`["expected_close_on"]`)
		*destinations[8].(*[]byte) = []byte(`["next-stage"]`)
		*destinations[9].(*bool) = true
		*destinations[10].(*bool) = false
		*destinations[11].(*int64) = 9
	}}}
	repository := NewSalesRepository(db)

	stage, err := repository.FindStage(context.Background(), "pipeline-id", "stage-id")

	if err != nil {
		t.Fatalf("FindStage() error = %v", err)
	}
	if stage.Key != "proposal" || stage.Name != "Proposal" || stage.Position != 2 ||
		stage.RequiredFields[0] != "expected_close_on" ||
		stage.AllowedNext[0] != "next-stage" || !stage.RequiresProposal ||
		stage.Version != 9 {
		t.Fatalf("unexpected stage: %+v", stage)
	}
	if !strings.Contains(db.query, "FROM pipeline_stages") {
		t.Fatalf("unexpected query: %s", db.query)
	}
}

func TestFindStageHidesMissingRowsBehindDomainError(t *testing.T) {
	repository := NewSalesRepository(&fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}})

	_, err := repository.FindStage(context.Background(), "pipeline-id", "stage-id")

	if !errors.Is(err, sales.ErrStageNotFound) {
		t.Fatalf("FindStage() error = %v", err)
	}
}

func TestFindOpportunityMapsScopedWorkflowState(t *testing.T) {
	at := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(destinations ...any) {
		values := []string{
			"opportunity-id", "msp-id", "client-id", "", "pipeline-id",
			"stage-id", "OPP-1", "Modernization",
		}
		for index, value := range values {
			*destinations[index].(*string) = value
		}
		*destinations[8].(*int64) = 4800000
		*destinations[9].(*string) = "USD"
		*destinations[10].(*int64) = 3
		*destinations[11].(*time.Time) = at
		*destinations[12].(*string) = "actor-id"
		*destinations[13].(*[]byte) = []byte(`{"expected_close_on":"2026-08-31"}`)
		*destinations[14].(*[]byte) = []byte(`{"risk_summary":"Weekend cutover"}`)
		*destinations[15].(*string) = "team-id"
		*destinations[16].(*[]byte) = []byte(`["contact-id"]`)
		*destinations[17].(*bool) = true
		*destinations[18].(*bool) = true
	}}}
	repository := NewSalesRepository(db)

	opportunity, err := repository.FindOpportunity(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"opportunity-id",
	)

	if err != nil {
		t.Fatalf("FindOpportunity() error = %v", err)
	}
	if opportunity.Amount != (sales.Money{Minor: 4800000, Currency: "USD"}) ||
		opportunity.Fields["expected_close_on"] != "2026-08-31" ||
		!opportunity.ProposalIssued || !opportunity.ApprovalGranted ||
		opportunity.Version != 3 {
		t.Fatalf("unexpected opportunity: %+v", opportunity)
	}
	if !strings.Contains(db.query, "o.client_id = NULLIF($3, '')::uuid") {
		t.Fatalf("opportunity lookup is not client scoped: %s", db.query)
	}
}

func TestFindOpportunityReturnsEnumerationSafeNotFound(t *testing.T) {
	repository := NewSalesRepository(&fakeSalesDB{queryRow: fakeRow{err: pgx.ErrNoRows}})

	_, err := repository.FindOpportunity(
		context.Background(),
		scope.Target{MSPID: "msp-id", ClientID: "client-id"},
		"opportunity-id",
	)

	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("FindOpportunity() error = %v", err)
	}
}

func assertQueryOrder(t *testing.T, queries []string, fragments ...string) {
	t.Helper()
	if len(queries) != len(fragments) {
		t.Fatalf("queries=%d, want %d: %#v", len(queries), len(fragments), queries)
	}
	for index, fragment := range fragments {
		if !strings.Contains(queries[index], fragment) {
			t.Fatalf("query %d does not contain %q: %s", index, fragment, queries[index])
		}
	}
}

func validAudit(at time.Time, action, subjectType, subjectID string) mutation.AuditRecord {
	return mutation.AuditRecord{
		ID: "77777777-7777-4777-8777-777777777777", OccurredAt: at,
		MSPID:     "22222222-2222-4222-8222-222222222222",
		ActorType: "technician", ActorID: "33333333-3333-4333-8333-333333333333",
		Action: action, SubjectType: subjectType, SubjectID: subjectID,
		SubjectVersion: 1, Source: "api",
		CorrelationID: "88888888-8888-4888-8888-888888888888",
	}
}

func validEvent(at time.Time, eventType, subjectType, subjectID string) mutation.EventRecord {
	return mutation.EventRecord{
		EventID:   "99999999-9999-4999-8999-999999999999",
		EventType: eventType, SchemaVersion: 1, OccurredAt: at,
		MSPID:     "22222222-2222-4222-8222-222222222222",
		ActorType: "technician", ActorID: "33333333-3333-4333-8333-333333333333",
		SubjectType: subjectType, SubjectID: subjectID, SubjectVersion: 1,
		Source: "api", CorrelationID: "88888888-8888-4888-8888-888888888888",
	}
}
