package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectPersistenceUsesScopedCompositeForeignKeys(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL project isolation verification")
	}
	ctx := context.Background()
	if err := migrateTestDatabase(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)

	rows, err := pool.Query(ctx, `
		SELECT conrelid::regclass::text, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE contype = 'f'
		  AND conrelid IN (
		    'projects'::regclass,
		    'phases'::regclass,
		    'resource_plans'::regclass
		  )`)
	if err != nil {
		t.Fatalf("query project constraints: %v", err)
	}
	defer rows.Close()

	definitions := map[string][]string{}
	for rows.Next() {
		var table, definition string
		if err := rows.Scan(&table, &definition); err != nil {
			t.Fatalf("scan project constraint: %v", err)
		}
		definitions[table] = append(definitions[table], definition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read project constraints: %v", err)
	}

	requireConstraint(t, definitions["projects"], "FOREIGN KEY (client_id, msp_id)")
	requireConstraint(t, definitions["phases"], "FOREIGN KEY (project_id, msp_id, client_id)")
	requireConstraint(t, definitions["resource_plans"], "FOREIGN KEY (project_id, msp_id, client_id)")
	requireConstraint(t, definitions["resource_plans"], "FOREIGN KEY (phase_id, msp_id, client_id)")
}

func requireConstraint(t *testing.T, definitions []string, fragment string) {
	t.Helper()
	for _, definition := range definitions {
		if strings.Contains(definition, fragment) {
			return
		}
	}
	t.Fatalf("constraint containing %q not found in %v", fragment, definitions)
}
