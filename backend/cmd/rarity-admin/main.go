package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/localadminrecovery"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"golang.org/x/term"
)

var errPasswordMismatch = errors.New("password confirmation does not match")

type promptWriter io.Writer
type passwordReader func(promptWriter) ([]byte, []byte, error)
type resetFunc func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error)
type preflightFunc func(context.Context, string, string) (tagging.ClassificationPreflightReport, error)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr,
		readPasswordsFromTTY, resetDatabasePassword, classificationPreflightDatabase); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	lookup func(string) string,
	stdout, stderr io.Writer,
	readPasswords passwordReader,
	reset resetFunc,
	preflight preflightFunc,
) error {
	if len(args) > 0 && args[0] == "calendar-demo-seed" {
		return runCalendarDemoSeed(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "calendar-reconcile" {
		return runCalendarReconcile(ctx, args[1:], lookup, stdout, stderr, reconcileCalendarDatabase)
	}
	if len(args) > 0 && args[0] == "classification-preflight" {
		return runClassificationPreflight(ctx, args[1:], lookup, stdout, stderr, preflight)
	}
	if len(args) < 2 || args[0] != "local-admin" || args[1] != "reset-password" {
		return errors.New("usage: rarity-admin classification-preflight --msp DISPLAY_ID | rarity-admin local-admin reset-password --msp DISPLAY_ID --username USERNAME --reason REASON")
	}
	flags := flag.NewFlagSet("local-admin reset-password", flag.ContinueOnError)
	flags.SetOutput(stderr)
	msp := flags.String("msp", "", "MSP display ID")
	username := flags.String("username", "", "local administrator username")
	reason := flags.String("reason", "", "incident or recovery reason recorded in the audit ledger")
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*msp) == "" ||
		strings.TrimSpace(*username) == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("--msp, --username, and --reason are required; passwords are accepted only from an interactive TTY")
	}
	databaseURL := strings.TrimSpace(lookup("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	password, confirmation, err := readPasswords(promptWriter(stderr))
	if err != nil {
		return fmt.Errorf("read password from TTY: %w", err)
	}
	defer clearBytes(password)
	defer clearBytes(confirmation)
	if len(password) != len(confirmation) ||
		subtle.ConstantTimeCompare(password, confirmation) != 1 {
		return errPasswordMismatch
	}
	result, err := reset(ctx, databaseURL, localadminrecovery.Command{
		MSPDisplayID: *msp,
		Username:     *username,
		Password:     password,
		Reason:       *reason,
	})
	if err != nil {
		return fmt.Errorf("reset local administrator password: %w", err)
	}
	fmt.Fprintf(stdout,
		"local administrator password reset msp=%s username=%s version=%d sessions_revoked=%d\n",
		result.MSPDisplayID, result.Username, result.Version, result.SessionsRevoked)
	return nil
}

func runClassificationPreflight(
	ctx context.Context,
	args []string,
	lookup func(string) string,
	stdout, stderr io.Writer,
	preflight preflightFunc,
) error {
	flags := flag.NewFlagSet("classification-preflight", flag.ContinueOnError)
	flags.SetOutput(stderr)
	msp := flags.String("msp", "", "MSP display ID")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*msp) == "" {
		return errors.New("--msp is required")
	}
	databaseURL := strings.TrimSpace(lookup("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if preflight == nil {
		return errors.New("classification preflight is unavailable")
	}
	report, err := preflight(ctx, databaseURL, strings.TrimSpace(*msp))
	if report.MSPID != "" {
		if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
			return fmt.Errorf("write classification preflight report: %w", encodeErr)
		}
	}
	if err != nil {
		return fmt.Errorf("run classification preflight: %w", err)
	}
	return nil
}

func readPasswordsFromTTY(_ promptWriter) ([]byte, []byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, errors.New("an interactive TTY is required")
	}
	defer tty.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return nil, nil, errors.New("an interactive TTY is required")
	}
	fmt.Fprint(tty, "New local administrator password: ")
	password, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprint(tty, "Confirm new local administrator password: ")
	confirmation, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		clearBytes(password)
		return nil, nil, err
	}
	return password, confirmation, nil
}

func resetDatabasePassword(
	ctx context.Context,
	databaseURL string,
	command localadminrecovery.Command,
) (localadminrecovery.Result, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return localadminrecovery.Result{}, fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	return localadminrecovery.NewService(
		localadminrecovery.NewPostgresRepository(pool),
		time.Now,
		id.New,
	).ResetPassword(ctx, command)
}

func classificationPreflightDatabase(ctx context.Context, databaseURL, mspDisplayID string) (tagging.ClassificationPreflightReport, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return tagging.ClassificationPreflightReport{}, fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	return tagging.NewMigrationService(
		psa.NewClassificationMigrationRepositoryFromPool(pool),
		time.Now,
	).ClassificationPreflight(ctx, mspDisplayID)
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
