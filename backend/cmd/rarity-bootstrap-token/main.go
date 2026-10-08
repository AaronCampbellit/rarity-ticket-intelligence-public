package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

func main() {
	if err := run(
		context.Background(),
		os.Getenv,
		os.Stdout,
		os.Stderr,
		issueDatabaseToken,
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type issueTokenFunc func(
	context.Context,
	string,
) (string, time.Time, error)

func run(
	ctx context.Context,
	lookup func(string) string,
	stdout, stderr io.Writer,
	issue issueTokenFunc,
) error {
	databaseURL := strings.TrimSpace(lookup("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	publicURL := strings.TrimSpace(lookup("RARITY_PUBLIC_URL"))
	if publicURL == "" {
		return errors.New("RARITY_PUBLIC_URL is required")
	}
	if _, err := setup.BuildBootstrapURL(publicURL, "validation-token"); err != nil {
		return fmt.Errorf("RARITY_PUBLIC_URL: %w", err)
	}
	token, expiresAt, err := issue(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("issue bootstrap token: %w", err)
	}
	setupURL, err := setup.BuildBootstrapURL(publicURL, token)
	if err != nil {
		return fmt.Errorf("build bootstrap URL: %w", err)
	}
	fmt.Fprintln(stdout, setupURL)
	fmt.Fprintln(stderr, "expires_at="+expiresAt.UTC().Format(time.RFC3339))
	return nil
}

func issueDatabaseToken(
	ctx context.Context,
	databaseURL string,
) (string, time.Time, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	token, expiresAt, err := setup.NewService(
		setup.NewPostgresRepository(pool), time.Now, nil,
	).IssueToken(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}
