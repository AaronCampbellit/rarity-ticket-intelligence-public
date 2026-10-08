package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateCreatesPlatformMetadata(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}

	if err := Migrate(context.Background(), databaseURL); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect to migrated database: %v", err)
	}
	t.Cleanup(pool.Close)

	var exists bool
	err = pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'platform_metadata'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("query platform metadata table: %v", err)
	}
	if !exists {
		t.Fatal("expected platform_metadata table")
	}
}
