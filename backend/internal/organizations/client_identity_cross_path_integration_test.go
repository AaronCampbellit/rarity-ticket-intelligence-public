package organizations_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/clientidentity"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sales"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	psastore "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
)

func TestOrdinaryCreationAndProspectConversionSerializeNormalizedClientIdentity(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for cross-path PostgreSQL concurrency verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database configuration: %v", err)
	}
	if poolConfig.MaxConns < 2 {
		poolConfig.MaxConns = 2
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID                 = "019fa088-78ee-7000-8000-000000000001"
		actorID               = "019fa088-78ee-7000-8000-000000000002"
		prospectID            = "019fa088-78ee-7000-8000-000000000003"
		pipelineID            = "019fa088-78ee-7000-8000-000000000004"
		openStageID           = "019fa088-78ee-7000-8000-000000000005"
		wonStageID            = "019fa088-78ee-7000-8000-000000000006"
		opportunityID         = "019fa088-78ee-7000-8000-000000000007"
		proposalID            = "019fa088-78ee-7000-8000-000000000008"
		proposalVersionID     = "019fa088-78ee-7000-8000-000000000009"
		pdfSnapshotID         = "019fa088-78ee-7000-8000-00000000000a"
		ordinaryClientID      = "019fa088-78ee-7000-8000-000000000011"
		ordinaryAuditID       = "019fa088-78ee-7000-8000-000000000012"
		ordinaryEventID       = "019fa088-78ee-7000-8000-000000000013"
		ordinaryCorrelation   = "019fa088-78ee-7000-8000-000000000014"
		conversionClientID    = "019fa088-78ee-7000-8000-000000000021"
		contactID             = "019fa088-78ee-7000-8000-000000000022"
		projectID             = "019fa088-78ee-7000-8000-000000000023"
		originalBudgetID      = "019fa088-78ee-7000-8000-000000000024"
		currentBudgetID       = "019fa088-78ee-7000-8000-000000000025"
		conversionID          = "019fa088-78ee-7000-8000-000000000026"
		conversionAuditID     = "019fa088-78ee-7000-8000-000000000027"
		conversionEventID     = "019fa088-78ee-7000-8000-000000000028"
		conversionCorrelation = "019fa088-78ee-7000-8000-000000000029"
	)

	if err := cleanupClientIdentityCrossPath(ctx, pool, mspID); err != nil {
		t.Fatalf("clean persistent test records before seed: %v", err)
	}
	t.Cleanup(func() {
		if err := cleanupClientIdentityCrossPath(ctx, pool, mspID); err != nil {
			t.Errorf("clean persistent test records: %v", err)
		}
	})

	now := time.Now().UTC()
	if err := seedProspectConversionSource(
		ctx,
		pool,
		mspID,
		actorID,
		prospectID,
		pipelineID,
		openStageID,
		wonStageID,
		opportunityID,
		proposalID,
		proposalVersionID,
		pdfSnapshotID,
		now,
	); err != nil {
		t.Fatalf("seed Prospect conversion source: %v", err)
	}

	ordinaryMutation := organizations.CreateClientMutation{
		Client: organizations.Client{
			Envelope: object.Envelope{
				ID: ordinaryClientID, ObjectType: "client_organization",
				MSPID: mspID, ClientID: ordinaryClientID,
				DisplayID:      "CLIENT-ORDINARY-CROSS-PATH",
				LifecycleState: "active", Version: 1,
				CreatedAt: now, CreatedBy: actorID,
				UpdatedAt: now, UpdatedBy: actorID,
			},
			Name: "Café Managed Services",
		},
		Audit: mutation.AuditRecord{
			ID: ordinaryAuditID, OccurredAt: now,
			MSPID: mspID, ClientID: ordinaryClientID,
			ActorType: "technician", ActorID: actorID,
			Action: "client.created", SubjectType: "client_organization",
			SubjectID: ordinaryClientID, SubjectVersion: 1,
			Source: "test", CorrelationID: ordinaryCorrelation,
		},
		Event: mutation.EventRecord{
			EventID: ordinaryEventID, EventType: "client.created",
			SchemaVersion: 1, OccurredAt: now,
			MSPID: mspID, ClientID: ordinaryClientID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "client_organization", SubjectID: ordinaryClientID,
			SubjectVersion: 1, Source: "test",
			CorrelationID: ordinaryCorrelation,
		},
	}
	conversionMutation := projects.ConversionMutation{
		Client: &projects.ClientSeed{
			ID: conversionClientID, MSPID: mspID, ProspectID: prospectID,
			DisplayID: "CLIENT-CONVERSION-CROSS-PATH",
			Name:      "\u00a0CAFÉ\u2003Managed\u00a0Services\u00a0",
			Email:     "prospect@example.test", Phone: "555-0100",
			CreatedAt: now, CreatedBy: actorID,
		},
		Project: projects.Project{
			ID: projectID, MSPID: mspID, ClientID: conversionClientID,
			DisplayID: "PROJECT-CROSS-PATH", Name: "Cross-path conversion",
			OriginalProposalVersionID: proposalVersionID,
			LifecycleState:            "planned", Version: 1,
			CreatedAt: now, CreatedBy: actorID,
		},
		OriginalBudget: projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		CurrentBudget:  projects.BudgetBaseline{Currency: "USD", RevenueMinor: 1000},
		Opportunity: sales.Opportunity{
			ID: opportunityID, MSPID: mspID, ClientID: conversionClientID,
			StageID: sales.PipelineStageID(wonStageID), Version: 2,
			UpdatedAt: now, UpdatedBy: actorID,
		},
		Conversion: projects.ConversionRecord{
			ID: conversionID, OpportunityID: sales.OpportunityID(opportunityID),
			ProposalVersionID: proposalVersionID,
			ProjectID:         projects.ProjectID(projectID),
			MSPID:             mspID, ClientID: conversionClientID,
			RequestKey:  "cross-path-request",
			PreviewHash: strings.Repeat("a", 64),
			ConvertedAt: now, ConvertedBy: actorID,
		},
		Audit: mutation.AuditRecord{
			ID: conversionAuditID, OccurredAt: now,
			MSPID: mspID, ClientID: conversionClientID,
			ActorType: "technician", ActorID: actorID,
			Action: "opportunity.converted", SubjectType: "project",
			SubjectID: projectID, SubjectVersion: 1,
			Source: "test", CorrelationID: conversionCorrelation,
		},
		Event: mutation.EventRecord{
			EventID: conversionEventID, EventType: "opportunity.converted",
			SchemaVersion: 1, OccurredAt: now,
			MSPID: mspID, ClientID: conversionClientID,
			ActorType: "technician", ActorID: actorID,
			SubjectType: "project", SubjectID: projectID,
			SubjectVersion: 1, Source: "test",
			CorrelationID: conversionCorrelation,
		},
	}

	ordinaryRepository := organizations.NewPostgresRepository(pool)
	generatedIDs := []string{contactID, originalBudgetID, currentBudgetID}
	nextID := 0
	conversionRepository := psastore.NewConversionRepositoryFromPool(
		pool,
		func() string {
			id := generatedIDs[nextID]
			nextID++
			return id
		},
	)

	lockObservation, err := holdClientIdentityBoundary(
		ctx,
		pool,
		mspID,
		ordinaryMutation.Client.Name,
		ordinaryMutation.Client.DisplayID,
	)
	if err != nil {
		t.Fatalf("hold shared Client identity boundary: %v", err)
	}
	t.Cleanup(func() { lockObservation.Close(context.Background()) })

	type result struct {
		writer string
		err    error
	}
	results := make(chan result, 2)
	writerCtx, cancelWriters := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWriters()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		results <- result{
			writer: "ordinary",
			err:    ordinaryRepository.CreateClientAtomic(writerCtx, ordinaryMutation),
		}
	}()
	go func() {
		defer workers.Done()
		results <- result{
			writer: "conversion",
			err:    conversionRepository.ConvertAtomic(writerCtx, conversionMutation),
		}
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, 15*time.Second)
	waitErr := lockObservation.WaitForWaiters(waitCtx, 2)
	cancelWait()
	releaseErr := lockObservation.Release(ctx)
	if waitErr != nil || releaseErr != nil {
		cancelWriters()
		workers.Wait()
		close(results)
		if waitErr != nil {
			t.Fatalf("observe both Client writers contending on shared advisory lock: %v", waitErr)
		}
		t.Fatalf("release shared Client identity boundary: %v", releaseErr)
	}
	t.Log("observed two distinct repository transactions waiting on the shared Client identity advisory lock")
	workers.Wait()
	close(results)

	succeeded, conflicted := "", ""
	for outcome := range results {
		switch {
		case outcome.err == nil:
			succeeded = outcome.writer
		case errors.Is(outcome.err, organizations.ErrClientIdentityConflict):
			conflicted = outcome.writer
		default:
			t.Fatalf("%s writer error = %v", outcome.writer, outcome.err)
		}
	}
	if succeeded == "" || conflicted == "" || succeeded == conflicted {
		t.Fatalf("cross-path results succeeded=%q conflicted=%q", succeeded, conflicted)
	}

	for table, want := range map[string]int{
		"client_organizations": 1,
		"audit_ledger":         1,
		"event_outbox":         1,
	} {
		if got, err := countMSPRows(ctx, pool, table, mspID); err != nil {
			t.Fatalf("count %s: %v", table, err)
		} else if got != want {
			t.Fatalf("%s rows=%d, want %d", table, got, want)
		}
	}
	conversionRows := 0
	if succeeded == "conversion" {
		conversionRows = 1
	}
	for table, want := range map[string]int{
		"contacts":                conversionRows,
		"projects":                conversionRows,
		"opportunity_conversions": conversionRows,
	} {
		if got, err := countMSPRows(ctx, pool, table, mspID); err != nil {
			t.Fatalf("count %s: %v", table, err)
		} else if got != want {
			t.Fatalf("%s rows=%d, want %d", table, got, want)
		}
	}
}

type clientIdentityLockObservation struct {
	holder    *pgx.Conn
	observer  *pgx.Conn
	tx        pgx.Tx
	holderPID int32
}

func holdClientIdentityBoundary(
	ctx context.Context,
	pool *pgxpool.Pool,
	mspID string,
	name string,
	displayID string,
) (*clientIdentityLockObservation, error) {
	if pool == nil {
		return nil, errors.New("test database pool is required")
	}
	holder, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("connect lock holder: %w", err)
	}
	observation := &clientIdentityLockObservation{holder: holder}
	ready := false
	defer func() {
		if !ready {
			observation.Close(context.Background())
		}
	}()

	observer, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("connect lock observer: %w", err)
	}
	observation.observer = observer

	tx, err := holder.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin lock holder transaction: %w", err)
	}
	observation.tx = tx
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&observation.holderPID); err != nil {
		return nil, fmt.Errorf("read lock holder backend PID: %w", err)
	}
	if err := clientidentity.Enforce(
		ctx,
		mspID,
		name,
		displayID,
		func(ctx context.Context, query string, args ...any) error {
			_, err := tx.Exec(ctx, query, args...)
			return err
		},
		func(
			ctx context.Context,
			query string,
			args ...any,
		) (clientidentity.Rows, error) {
			return tx.Query(ctx, query, args...)
		},
	); err != nil {
		return nil, fmt.Errorf("acquire production Client identity boundary: %w", err)
	}

	ready = true
	return observation, nil
}

func (o *clientIdentityLockObservation) WaitForWaiters(
	ctx context.Context,
	want int,
) error {
	if o == nil || o.observer == nil || o.holderPID == 0 {
		return errors.New("Client identity lock observation is not initialized")
	}
	if want < 1 {
		return fmt.Errorf("invalid advisory lock waiter count %d", want)
	}
	const query = `
SELECT count(DISTINCT waiting.pid)
FROM pg_locks AS held
JOIN pg_locks AS waiting
  ON waiting.locktype = held.locktype
 AND waiting.database IS NOT DISTINCT FROM held.database
 AND waiting.classid IS NOT DISTINCT FROM held.classid
 AND waiting.objid IS NOT DISTINCT FROM held.objid
 AND waiting.objsubid IS NOT DISTINCT FROM held.objsubid
 AND waiting.mode = held.mode
WHERE held.pid = $1
  AND held.locktype = 'advisory'
  AND held.granted
  AND waiting.pid <> held.pid
  AND NOT waiting.granted
`
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	lastObserved := 0
	for {
		if err := o.observer.QueryRow(ctx, query, o.holderPID).Scan(&lastObserved); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf(
					"wait for %d advisory lock waiters (last observed %d): %w",
					want,
					lastObserved,
					ctx.Err(),
				)
			}
			return fmt.Errorf("query advisory lock waiters: %w", err)
		}
		if lastObserved == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for %d advisory lock waiters (last observed %d): %w",
				want,
				lastObserved,
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (o *clientIdentityLockObservation) Release(ctx context.Context) error {
	if o == nil || o.tx == nil {
		return nil
	}
	tx := o.tx
	o.tx = nil
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	return nil
}

func (o *clientIdentityLockObservation) Close(ctx context.Context) {
	if o == nil {
		return
	}
	_ = o.Release(ctx)
	if o.observer != nil {
		_ = o.observer.Close(ctx)
		o.observer = nil
	}
	if o.holder != nil {
		_ = o.holder.Close(ctx)
		o.holder = nil
	}
}

func seedProspectConversionSource(
	ctx context.Context,
	pool *pgxpool.Pool,
	mspID string,
	actorID string,
	prospectID string,
	pipelineID string,
	openStageID string,
	wonStageID string,
	opportunityID string,
	proposalID string,
	proposalVersionID string,
	pdfSnapshotID string,
	now time.Time,
) error {
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO msp_organizations (
				id, display_id, name, created_by, updated_by
			) VALUES ($1, 'MSP-CROSS-PATH', 'Cross-path Test MSP', $2, $2)`,
			args: []any{mspID, actorID},
		},
		{
			query: `INSERT INTO prospects (
				id, msp_id, display_id, name, email, lifecycle_state,
				version, created_at, created_by, updated_at, updated_by
			) VALUES (
				$1, $2, 'PROSPECT-CROSS-PATH', 'Café Managed Services',
				'prospect@example.test', 'active', 1, $3, $4, $3, $4
			)`,
			args: []any{prospectID, mspID, now, actorID},
		},
		{
			query: `INSERT INTO pipelines (
				id, msp_id, key, name, enabled, version,
				created_at, created_by, updated_at, updated_by
			) VALUES (
				$1, $2, 'cross-path', 'Cross-path', true, 1,
				$3, $4, $3, $4
			)`,
			args: []any{pipelineID, mspID, now, actorID},
		},
		{
			query: `INSERT INTO pipeline_stages (
				id, pipeline_id, msp_id, key, name, position,
				probability, forecast_category, version
			) VALUES
				($1, $2, $3, 'open', 'Open', 1, 25, 'pipeline', 1),
				($4, $2, $3, 'won', 'Won', 2, 100, 'closed_won', 1)`,
			args: []any{openStageID, pipelineID, mspID, wonStageID},
		},
		{
			query: `INSERT INTO opportunities (
				id, msp_id, prospect_id, pipeline_id, stage_id,
				display_id, name, amount_minor, currency,
				lifecycle_state, version, created_at, created_by,
				updated_at, updated_by
			) VALUES (
				$1, $2, $3, $4, $5, 'OPPORTUNITY-CROSS-PATH',
				'Cross-path opportunity', 1000, 'USD',
				'active', 1, $6, $7, $6, $7
			)`,
			args: []any{
				opportunityID, mspID, prospectID, pipelineID,
				openStageID, now, actorID,
			},
		},
		{
			query: `INSERT INTO proposals (
				id, msp_id, prospect_id, opportunity_id, display_id,
				current_version, state, version,
				created_at, created_by, updated_at, updated_by
			) VALUES (
				$1, $2, $3, $4, 'PROPOSAL-CROSS-PATH',
				1, 'accepted', 1, $5, $6, $5, $6
			)`,
			args: []any{proposalID, mspID, prospectID, opportunityID, now, actorID},
		},
		{
			query: `INSERT INTO proposal_versions (
				id, proposal_id, msp_id, version, currency,
				subtotal_minor, tax_minor, total_minor,
				cost_minor, margin_minor, issued_at, issued_by,
				pdf_snapshot_id
			) VALUES (
				$1, $2, $3, 1, 'USD', 1000, 0, 1000,
				0, 1000, $4, $5, $6
			)`,
			args: []any{
				proposalVersionID, proposalID, mspID, now, actorID, pdfSnapshotID,
			},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func countMSPRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	mspID string,
) (int, error) {
	allowed := map[string]bool{
		"audit_ledger": true, "client_organizations": true,
		"contacts": true, "event_outbox": true,
		"opportunity_conversions": true, "projects": true,
	}
	if !allowed[table] {
		return 0, fmt.Errorf("unsupported test table %q", table)
	}
	var count int
	err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM "+table+" WHERE msp_id = $1",
		mspID,
	).Scan(&count)
	return count, err
}

func cleanupClientIdentityCrossPath(
	ctx context.Context,
	pool *pgxpool.Pool,
	mspID string,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	statements := []string{
		"ALTER TABLE audit_ledger DISABLE TRIGGER audit_ledger_append_only",
		"ALTER TABLE proposal_versions DISABLE TRIGGER proposal_versions_immutable",
		"DELETE FROM opportunity_conversions WHERE msp_id = $1",
		"DELETE FROM project_budgets WHERE msp_id = $1",
		"DELETE FROM projects WHERE msp_id = $1",
		"DELETE FROM approvals WHERE msp_id = $1",
		"DELETE FROM proposal_lines WHERE msp_id = $1",
		"DELETE FROM proposal_versions WHERE msp_id = $1",
		"DELETE FROM proposals WHERE msp_id = $1",
		"DELETE FROM opportunities WHERE msp_id = $1",
		"DELETE FROM pipeline_stages WHERE msp_id = $1",
		"DELETE FROM pipelines WHERE msp_id = $1",
		"DELETE FROM contacts WHERE msp_id = $1",
		"DELETE FROM event_outbox WHERE msp_id = $1",
		"DELETE FROM audit_ledger WHERE msp_id = $1",
		"DELETE FROM client_organizations WHERE msp_id = $1",
		"DELETE FROM prospects WHERE msp_id = $1",
		"DELETE FROM msp_organizations WHERE id = $1",
		"ALTER TABLE proposal_versions ENABLE TRIGGER proposal_versions_immutable",
		"ALTER TABLE audit_ledger ENABLE TRIGGER audit_ledger_append_only",
	}
	for _, statement := range statements {
		args := []any(nil)
		if strings.Contains(statement, "$1") {
			args = []any{mspID}
		}
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	return tx.Commit(ctx)
}
