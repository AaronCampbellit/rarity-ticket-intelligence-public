package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

func TestProviderAgnosticAIRuntimeMigrationUpDownContract(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration integration tests")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL administration pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("provider_ai_runtime_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})

	schemaURL := databaseURLWithSearchPath(t, databaseURL, schema)
	if err := store.Migrate(ctx, schemaURL); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}

	database, err := sql.Open("pgx", schemaURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set PostgreSQL migration dialect: %v", err)
	}
	if err := goose.DownToContext(ctx, database, ".", 48); err != nil {
		t.Fatalf("roll back runtime migration for legacy setup: %v", err)
	}

	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("open isolated schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const (
		mspID        = "00000000-0000-0000-0000-000000000101"
		actorID      = "00000000-0000-0000-0000-000000000102"
		connectionID = "00000000-0000-0000-0000-000000000103"
		policyID     = "00000000-0000-0000-0000-000000000104"
		clientID     = "00000000-0000-0000-0000-000000000105"
		workID       = "00000000-0000-0000-0000-000000000106"
		providerID   = "00000000-0000-0000-0000-000000000107"
		modelID      = "00000000-0000-0000-0000-000000000108"
		jobID        = "00000000-0000-0000-0000-000000000109"
		recommendID  = "00000000-0000-0000-0000-000000000110"
		usageID      = "00000000-0000-0000-0000-000000000111"
	)

	execSQL(t, ctx, pool, `
		INSERT INTO msp_organizations (id, display_id, name, created_by, updated_by)
		VALUES ($1, 'provider-ai', 'Provider AI', $2, $2);
		INSERT INTO client_organizations (id, msp_id, display_id, name, created_by, updated_by)
		VALUES ($3, $1, 'client', 'Client', $2, $2);
		INSERT INTO connections (id, msp_id, name, kind, client_scopes, capabilities, data_scopes, created_by, updated_by)
		VALUES ($4, $1, 'legacy', 'external_http', ARRAY[$3::uuid], ARRAY['read'], ARRAY['standard'], $2, $2);
		INSERT INTO ai_policies (
			id, msp_id, enabled, provider, model, provider_connection_id,
			provider_disclosure_accepted_at, provider_disclosure_accepted_by, allowed_features, created_by, updated_by
		) VALUES ($5, $1, true, 'legacy-provider', 'legacy-model', $4, now(), $2, ARRAY['summary'], $2, $2);
	`, mspID, actorID, clientID, connectionID, policyID)
	var legacyCheckCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint constraint_row
		WHERE constraint_row.conrelid = 'ai_policies'::regclass
		  AND constraint_row.contype = 'c'
		  AND (
			pg_get_constraintdef(constraint_row.oid) LIKE '%allowed_features <@%'
			OR pg_get_constraintdef(constraint_row.oid) LIKE '%provider_connection_id IS NOT NULL%'
		  )
	`).Scan(&legacyCheckCount); err != nil {
		t.Fatalf("count legacy policy checks: %v", err)
	}
	if legacyCheckCount != 2 {
		t.Fatalf("legacy policy checks=%d, want 2", legacyCheckCount)
	}

	if err := goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("apply provider AI runtime migration: %v", err)
	}

	var enabled, archivedEnabled bool
	var archivedProvider string
	if err := pool.QueryRow(ctx, `
		SELECT policy.enabled, archive.was_enabled, archive.provider
		FROM ai_policies policy
		JOIN ai_policy_legacy_provider_configurations archive ON archive.policy_id = policy.id
		WHERE policy.id = $1
	`, policyID).Scan(&enabled, &archivedEnabled, &archivedProvider); err != nil {
		t.Fatalf("read migrated legacy policy: %v", err)
	}
	if enabled || !archivedEnabled || archivedProvider != "legacy-provider" {
		t.Fatalf("legacy policy migration enabled=%t archivedEnabled=%t provider=%q", enabled, archivedEnabled, archivedProvider)
	}
	if _, err := pool.Exec(ctx, `UPDATE ai_policies SET allowed_features = ARRAY['unsupported'] WHERE id = $1`, policyID); err == nil {
		t.Fatal("provider runtime migration did not retain allowed-feature validation")
	}

	if err := goose.DownContext(ctx, database, "."); err != nil {
		t.Fatalf("roll back provider AI runtime migration: %v", err)
	}
	var restoredEnabled bool
	var restoredProvider string
	if err := pool.QueryRow(ctx, `SELECT enabled, provider FROM ai_policies WHERE id = $1`, policyID).Scan(&restoredEnabled, &restoredProvider); err != nil {
		t.Fatalf("read restored legacy policy: %v", err)
	}
	if !restoredEnabled || restoredProvider != "legacy-provider" {
		t.Fatalf("legacy policy was not restored enabled=%t provider=%q", restoredEnabled, restoredProvider)
	}

	if err := goose.UpByOneContext(ctx, database, "."); err != nil {
		t.Fatalf("reapply provider AI runtime migration: %v", err)
	}
	execSQL(t, ctx, pool, `
		INSERT INTO work_records (id, msp_id, client_id, display_id, record_type, title, status, priority, created_by, updated_by)
		VALUES ($1, $2, $3, 'work', 'incident', 'Work', 'open', 'normal', $4, $4);
		INSERT INTO ai_provider_connections (
			id, msp_id, name, adapter_type, network_mode, base_url, enabled,
			disclosure_accepted_at, disclosure_accepted_by,
			created_at, created_by, updated_at, updated_by
		) VALUES ($5, $2, 'provider', 'ollama', 'remote', 'https://models.example.test', true,
		          now(), $4, now(), $4, now(), $4);
		INSERT INTO ai_model_profiles (
			id, msp_id, connection_id, provider_model_id, display_name, supported_features,
			context_limit, output_limit, enabled, discovered_at, last_discovered_at, created_at, updated_at
		) VALUES ($6, $2, $5, 'model', 'Model', ARRAY['summary'], 4096, 1024, true, now(), now(), now(), now());
		UPDATE ai_policies
		SET allowed_features = ARRAY['summary'], summary_model_profile_id = $6, enabled = true
		WHERE id = $7;
		INSERT INTO ai_generation_jobs (
			id, msp_id, client_id, work_record_id, requested_by, feature, model_profile_id,
			idempotency_key, created_at, updated_at
		) VALUES ($8, $2, $3, $1, $4, 'summary', $6, 'integration-job', now(), now());
	`, workID, mspID, clientID, actorID, providerID, modelID, policyID, jobID)

	// The execution repository uses this one-statement upsert shape so two
	// concurrent retries return one canonical row without a snapshot race.
	const concurrentKey = "concurrent-idempotency-key"
	type submission struct {
		id       string
		inserted bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan submission, 2)
	var submissions sync.WaitGroup
	for _, id := range []string{"00000000-0000-0000-0000-000000000112", "00000000-0000-0000-0000-000000000113"} {
		submissions.Add(1)
		go func(id string) {
			defer submissions.Done()
			<-start
			var result submission
			result.err = pool.QueryRow(ctx, `
				INSERT INTO ai_generation_jobs (
					id, msp_id, client_id, work_record_id, requested_by, feature, model_profile_id,
					idempotency_key, created_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, 'summary', $6, $7, now(), now())
				ON CONFLICT (idempotency_key) DO UPDATE SET id = ai_generation_jobs.id
				RETURNING id::text, (xmax = 0)
			`, id, mspID, clientID, workID, actorID, modelID, concurrentKey).Scan(&result.id, &result.inserted)
			results <- result
		}(id)
	}
	close(start)
	submissions.Wait()
	close(results)
	var canonical string
	inserted := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent idempotent insert: %v", result.err)
		}
		if canonical == "" {
			canonical = result.id
		}
		if canonical != result.id {
			t.Fatalf("concurrent submissions returned different jobs %q and %q", canonical, result.id)
		}
		if result.inserted {
			inserted++
		}
	}
	if inserted != 1 {
		t.Fatalf("concurrent idempotent submissions inserted=%d, want one", inserted)
	}

	var remoteTimeout int
	if err := pool.QueryRow(ctx, `SELECT timeout_seconds FROM ai_provider_connections WHERE id = $1`, providerID).Scan(&remoteTimeout); err != nil {
		t.Fatalf("read remote default timeout: %v", err)
	}
	if remoteTimeout != 300 {
		t.Fatalf("remote timeout default=%d, want 300", remoteTimeout)
	}
	if _, err := pool.Exec(ctx, `UPDATE ai_model_profiles SET supported_features = ARRAY['reply_draft'] WHERE id = $1`, modelID); err == nil {
		t.Fatal("model feature change unexpectedly left enabled policy and queued job incompatible")
	}
	if _, err := pool.Exec(ctx, `UPDATE ai_provider_connections SET enabled = false WHERE id = $1`, providerID); err == nil {
		t.Fatal("provider disable unexpectedly left enabled policy and queued job incompatible")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ai_provider_connections
		SET base_url = 'https://different-provider.example.test'
		WHERE id = $1
	`, providerID); err == nil {
		t.Fatal("provider retarget unexpectedly reused disclosure accepted for the old endpoint")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ai_provider_connections
		SET disclosure_accepted_at = NULL, disclosure_accepted_by = NULL
		WHERE id = $1
	`, providerID); err == nil {
		t.Fatal("provider disclosure removal unexpectedly left enabled policy and queued job authorized")
	}

	execSQL(t, ctx, pool, `
		INSERT INTO ai_recommendations (
			id, msp_id, client_id, work_record_id, feature, provider, model, prompt_version,
			output_ref, generated_at, generated_by
		) VALUES ($1, $2, $3, $4, 'summary', 'provider', 'model', 'v1', 'output', now(), $5);
		INSERT INTO ai_usage_records (id, recommendation_id, msp_id, provider, model, input_units, output_units, cost_minor, recorded_at)
		VALUES ($6, $1, $2, 'provider', 'model', NULL, NULL, NULL, now());
	`, recommendID, mspID, clientID, workID, actorID, usageID)
	if err := goose.DownContext(ctx, database, "."); err == nil || !strings.Contains(err.Error(), "usage values are unknown") {
		t.Fatalf("expected unknown usage rollback preflight error, got %v", err)
	}
}

func databaseURLWithSearchPath(t *testing.T, databaseURL, schema string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestCompactSQLArgumentsRemapsSparsePlaceholders(t *testing.T) {
	query, args, err := compactSQLArguments(
		"INSERT INTO example (msp_id, policy_id, connection_id) VALUES ($1, $5, $4)",
		[]any{"msp", "actor", "client", "connection", "policy"},
	)
	if err != nil {
		t.Fatalf("compact SQL arguments: %v", err)
	}
	if query != "INSERT INTO example (msp_id, policy_id, connection_id) VALUES ($1, $3, $2)" {
		t.Fatalf("compacted query = %q", query)
	}
	if !reflect.DeepEqual(args, []any{"msp", "connection", "policy"}) {
		t.Fatalf("compacted args = %#v", args)
	}
}

func compactSQLArguments(statement string, args []any) (string, []any, error) {
	indexSet := make(map[int]struct{})
	for _, match := range sqlPlaceholderPattern.FindAllStringSubmatch(statement, -1) {
		index, err := strconv.Atoi(match[1])
		if err != nil || index < 1 || index > len(args) {
			return "", nil, fmt.Errorf("SQL placeholder %q has no argument", match[0])
		}
		indexSet[index] = struct{}{}
	}
	indices := make([]int, 0, len(indexSet))
	for index := range indexSet {
		indices = append(indices, index)
	}
	sort.Ints(indices)

	replacements := make(map[int]int, len(indices))
	compactedArgs := make([]any, 0, len(indices))
	for compactedIndex, originalIndex := range indices {
		replacements[originalIndex] = compactedIndex + 1
		compactedArgs = append(compactedArgs, args[originalIndex-1])
	}
	compactedStatement := sqlPlaceholderPattern.ReplaceAllStringFunc(statement, func(placeholder string) string {
		match := sqlPlaceholderPattern.FindStringSubmatch(placeholder)
		originalIndex, _ := strconv.Atoi(match[1])
		return "$" + strconv.Itoa(replacements[originalIndex])
	})
	return compactedStatement, compactedArgs, nil
}

func execSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	for _, statement := range strings.Split(query, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		statement, statementArgs, err := compactSQLArguments(statement, args)
		if err != nil {
			t.Fatalf("compact setup SQL arguments: %v", err)
		}
		if _, err := pool.Exec(ctx, statement, statementArgs...); err != nil {
			t.Fatalf("execute setup SQL: %v", err)
		}
	}
}

var sqlPlaceholderPattern = regexp.MustCompile(`\$(\d+)`)
