package health

import (
	"context"
	"os"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
)

func TestPostgresCheckReportsMigratedDatabaseReady(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	if err := store.Migrate(context.Background(), databaseURL); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	pool, err := store.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	t.Cleanup(pool.Close)

	check := PostgresCheck{Pool: pool}
	if err := check.Database(context.Background()); err != nil {
		t.Fatalf("database readiness: %v", err)
	}
	if err := check.Migrations(context.Background()); err != nil {
		t.Fatalf("migration readiness: %v", err)
	}
}
