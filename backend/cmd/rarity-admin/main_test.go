package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/localadminrecovery"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
)

func unusedPreflight(context.Context, string, string) (tagging.ClassificationPreflightReport, error) {
	return tagging.ClassificationPreflightReport{}, errors.New("preflight should not be called")
}

func TestRunResetPasswordRequiresTTYConfirmationAndPassesNoSecretFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	readCalls := 0
	var gotURL string
	var gotCommand localadminrecovery.Command
	err := run(
		context.Background(),
		[]string{"local-admin", "reset-password", "--msp", "MSP-001", "--username", "Admin", "--reason", "incident recovery"},
		func(key string) string {
			if key == "DATABASE_URL" {
				return "postgres://database"
			}
			return ""
		},
		&stdout,
		&stderr,
		func(ioWriter promptWriter) ([]byte, []byte, error) {
			readCalls++
			return []byte("correct horse battery staple"), []byte("correct horse battery staple"), nil
		},
		func(_ context.Context, databaseURL string, command localadminrecovery.Command) (localadminrecovery.Result, error) {
			gotURL, gotCommand = databaseURL, command
			return localadminrecovery.Result{
				MSPDisplayID: "MSP-001", Username: "admin", Version: 4, SessionsRevoked: 3,
			}, nil
		},
		unusedPreflight,
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if readCalls != 1 || gotURL != "postgres://database" ||
		gotCommand.MSPDisplayID != "MSP-001" || gotCommand.Username != "Admin" ||
		gotCommand.Reason != "incident recovery" {
		t.Fatalf("calls=%d url=%q command=%+v", readCalls, gotURL, gotCommand)
	}
	if !strings.Contains(stdout.String(), "sessions_revoked=3") ||
		strings.Contains(stdout.String()+stderr.String(), "correct horse") {
		t.Fatalf("unsafe or incomplete output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsPasswordFlagAndMismatchedConfirmation(t *testing.T) {
	lookup := func(string) string { return "postgres://database" }
	resetCalled := false
	reset := func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error) {
		resetCalled = true
		return localadminrecovery.Result{}, nil
	}
	for name, args := range map[string][]string{
		"password flag":  {"local-admin", "reset-password", "--msp", "MSP", "--username", "admin", "--reason", "incident", "--password", "secret"},
		"missing reason": {"local-admin", "reset-password", "--msp", "MSP", "--username", "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			err := run(context.Background(), args, lookup, &bytes.Buffer{}, &bytes.Buffer{},
				func(promptWriter) ([]byte, []byte, error) {
					return []byte("0123456789abcdef"), []byte("0123456789abcdef"), nil
				}, reset, unusedPreflight)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if resetCalled {
		t.Fatal("reset called for rejected flags")
	}

	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"local-admin", "reset-password", "--msp", "MSP", "--username", "admin", "--reason", "incident"},
		lookup,
		&stdout,
		&stderr,
		func(promptWriter) ([]byte, []byte, error) {
			return []byte("0123456789abcdef"), []byte("different-password"), nil
		},
		reset, unusedPreflight,
	)
	if !errors.Is(err, errPasswordMismatch) || resetCalled {
		t.Fatalf("error=%v resetCalled=%v", err, resetCalled)
	}
	if strings.Contains(stdout.String()+stderr.String()+err.Error(), "0123456789abcdef") {
		t.Fatal("password leaked in error output")
	}
}

func TestRunHelpDoesNotRequireDatabaseOrTTY(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"local-admin", "reset-password", "--help"},
		func(string) string { return "" }, &stdout, &stderr,
		func(promptWriter) ([]byte, []byte, error) { t.Fatal("password reader called"); return nil, nil, nil },
		func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error) {
			t.Fatal("reset called")
			return localadminrecovery.Result{}, nil
		}, unusedPreflight)
	if err != nil || !strings.Contains(stderr.String(), "-msp") {
		t.Fatalf("help error=%v output=%q", err, stderr.String())
	}
}

func TestRunClassificationPreflightUsesDatabaseAndEmitsMachineReadableReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotURL, gotMSP string
	preflight := func(_ context.Context, databaseURL, msp string) (tagging.ClassificationPreflightReport, error) {
		gotURL, gotMSP = databaseURL, msp
		return tagging.ClassificationPreflightReport{
			MSPID: "msp-1", MSPDisplayID: "MSP-001", Passed: true,
			Totals:                  tagging.ClassificationObjectTotals{Total: 12, Meaningful: 10, Unclassified: 2},
			Projection:              tagging.ClassificationProjectionState{LagSeconds: 4},
			AIPolicy:                tagging.ClassificationPreflightPolicy{AutomaticApplyThreshold: .95, Version: 1},
			TaskOneBackfillVerified: true,
			VerifiedNoOpAt:          time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		}, nil
	}
	err := run(context.Background(), []string{"classification-preflight", "--msp", "MSP-001"},
		func(key string) string {
			if key == "DATABASE_URL" {
				return "postgres://database"
			}
			return ""
		}, &stdout, &stderr,
		func(promptWriter) ([]byte, []byte, error) { t.Fatal("password reader called"); return nil, nil, nil },
		func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error) {
			t.Fatal("password reset called")
			return localadminrecovery.Result{}, nil
		}, preflight)
	if err != nil || gotURL != "postgres://database" || gotMSP != "MSP-001" {
		t.Fatalf("error=%v url=%q msp=%q stderr=%q", err, gotURL, gotMSP, stderr.String())
	}
	var report tagging.ClassificationPreflightReport
	if json.Unmarshal(stdout.Bytes(), &report) != nil || !report.Passed || report.Totals.Total != 12 {
		t.Fatalf("output=%q report=%+v", stdout.String(), report)
	}
}

func TestRunClassificationPreflightRejectsIncompleteArgumentsAndPropagatesFailure(t *testing.T) {
	lookup := func(key string) string {
		if key == "DATABASE_URL" {
			return "postgres://database"
		}
		return ""
	}
	for name, args := range map[string][]string{
		"missing msp":    {"classification-preflight"},
		"extra argument": {"classification-preflight", "--msp", "MSP-001", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			err := run(context.Background(), args, lookup, &bytes.Buffer{}, &bytes.Buffer{},
				func(promptWriter) ([]byte, []byte, error) { t.Fatal("password reader called"); return nil, nil, nil },
				func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error) {
					t.Fatal("password reset called")
					return localadminrecovery.Result{}, nil
				}, func(context.Context, string, string) (tagging.ClassificationPreflightReport, error) {
					t.Fatal("preflight called")
					return tagging.ClassificationPreflightReport{}, nil
				})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}

	want := errors.New("classification preflight failed")
	err := run(context.Background(), []string{"classification-preflight", "--msp", "MSP-001"}, lookup,
		&bytes.Buffer{}, &bytes.Buffer{},
		func(promptWriter) ([]byte, []byte, error) { t.Fatal("password reader called"); return nil, nil, nil },
		func(context.Context, string, localadminrecovery.Command) (localadminrecovery.Result, error) {
			return localadminrecovery.Result{}, nil
		},
		func(context.Context, string, string) (tagging.ClassificationPreflightReport, error) {
			return tagging.ClassificationPreflightReport{}, want
		})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}
