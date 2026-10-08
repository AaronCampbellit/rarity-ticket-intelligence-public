package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskHistorySchemaSupportsPolymorphicParentMovement(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL task-history verification")
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

	for table, columns := range map[string][]string{
		"tasks": {"parent_type", "parent_id", "parent_task_id", "version"},
		"task_movement_history": {
			"task_id", "from_parent_type", "from_parent_id",
			"to_parent_type", "to_parent_id", "previous_version",
			"accepted_version", "correlation_id",
		},
	} {
		for _, column := range columns {
			var exists bool
			if err := pool.QueryRow(ctx, `
				SELECT EXISTS (
				  SELECT 1 FROM information_schema.columns
				  WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)`, table, column).Scan(&exists); err != nil {
				t.Fatalf("inspect %s.%s: %v", table, column, err)
			}
			if !exists {
				t.Fatalf("missing %s.%s", table, column)
			}
		}
	}
}
