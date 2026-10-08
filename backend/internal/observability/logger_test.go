package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsConfiguredSecretValues(t *testing.T) {
	output := new(bytes.Buffer)
	logger := NewLogger(output, slog.LevelInfo)
	logger.Info("config loaded", "database_url", "postgres://rarity:secret@db/rarity")

	if strings.Contains(output.String(), "secret") {
		t.Fatalf("logger leaked a secret: %s", output.String())
	}
	if !strings.Contains(output.String(), "[REDACTED]") {
		t.Fatalf("logger did not mark a redacted value: %s", output.String())
	}
}

func TestLoggerRedactsAuthorizationAndCookieValues(t *testing.T) {
	output := new(bytes.Buffer)
	logger := NewLogger(output, slog.LevelInfo)
	logger.Info(
		"request metadata",
		"authorization", "Bearer secret-token",
		"cookie", "session=secret-cookie",
	)

	for _, secret := range []string{"secret-token", "secret-cookie"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("logger leaked %q: %s", secret, output.String())
		}
	}
}

func TestLoggerNeverEmitsMentionContentOrTokenAdjacentText(t *testing.T) {
	output := new(bytes.Buffer)
	logger := NewLogger(output, slog.LevelInfo)
	logger.Info(
		"mention operation",
		"body", "private body value",
		"preview", "private preview value",
		"token_adjacent_text", "private adjacent value",
		"source_body", "private source value",
		"safe_error_code", "access_revoked",
	)
	for _, forbidden := range []string{
		"private body value", "private preview value",
		"private adjacent value", "private source value",
	} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("logger leaked mention content %q: %s", forbidden, output.String())
		}
	}
	if !strings.Contains(output.String(), "access_revoked") {
		t.Fatalf("logger removed safe operational evidence: %s", output.String())
	}
}

func TestMentionTelemetryEmitsOnlyBoundedCounterDimensions(t *testing.T) {
	output := new(bytes.Buffer)
	telemetry := NewMentionTelemetry(NewLogger(output, slog.LevelInfo))
	telemetry.Count(MentionMetric{
		Name: "suppression", ParentType: "project", Outcome: "access_revoked",
		MSPID: "msp-1", ClientID: "client-1", ObjectID: "project-1",
	})
	if got := telemetry.Value("suppression", "project", "access_revoked"); got != 1 {
		t.Fatalf("counter=%d want 1", got)
	}
	for _, expected := range []string{"mention metric", "suppression", "project", "access_revoked"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("telemetry missing %q: %s", expected, output.String())
		}
	}
}
