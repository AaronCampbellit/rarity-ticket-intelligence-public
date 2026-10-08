package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunPrintsAbsoluteRecoverySetupURL(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	lookup := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://database.example/rarity"
		case "RARITY_PUBLIC_URL":
			return "https://rarity.example/support/rti"
		default:
			return ""
		}
	}
	issue := func(context.Context, string) (string, time.Time, error) {
		return "bootstrap-secret",
			time.Date(2026, 8, 3, 16, 15, 0, 0, time.UTC),
			nil
	}

	if err := run(
		context.Background(), lookup, stdout, stderr, issue,
	); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(stdout.String()),
		"https://rarity.example/support/rti/#/setup?bootstrap_token=bootstrap-secret"; got != want {
		t.Fatalf("stdout=%q want=%q", got, want)
	}
	if got, want := strings.TrimSpace(stderr.String()),
		"expires_at=2026-08-03T16:15:00Z"; got != want {
		t.Fatalf("stderr=%q want=%q", got, want)
	}
}

func TestRunRejectsMissingPublicURLBeforeIssuing(t *testing.T) {
	calls := 0
	lookup := func(key string) string {
		if key == "DATABASE_URL" {
			return "postgres://database.example/rarity"
		}
		return ""
	}
	issue := func(context.Context, string) (string, time.Time, error) {
		calls++
		return "bootstrap-secret", time.Now(), nil
	}

	err := run(
		context.Background(), lookup, new(bytes.Buffer), new(bytes.Buffer), issue,
	)
	if err == nil || calls != 0 {
		t.Fatalf("error=%v issue calls=%d", err, calls)
	}
}
