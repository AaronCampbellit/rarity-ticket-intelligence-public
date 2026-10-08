package health

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresCheck struct {
	Pool *pgxpool.Pool
}

func (check PostgresCheck) Database(ctx context.Context) error {
	if check.Pool == nil {
		return errors.New("postgres pool is not configured")
	}
	return check.Pool.Ping(ctx)
}

func (check PostgresCheck) Migrations(ctx context.Context) error {
	if check.Pool == nil {
		return errors.New("postgres pool is not configured")
	}
	var version string
	err := check.Pool.QueryRow(ctx, `
		SELECT value FROM platform_metadata WHERE key = 'schema_contract_version'
	`).Scan(&version)
	if err != nil {
		return fmt.Errorf("read schema contract version: %w", err)
	}
	if version != "1" {
		return fmt.Errorf("unexpected schema contract version %q", version)
	}
	return nil
}
