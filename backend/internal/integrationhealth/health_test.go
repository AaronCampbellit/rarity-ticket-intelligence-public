package integrationhealth

import (
	"testing"
	"time"
)

func TestEvaluateReportsFreshnessQueueAndCredentialFailuresAuthoritatively(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	cases := []struct {
		name   string
		input  Signal
		state  State
		reason string
	}{
		{name: "healthy", input: Signal{
			Enabled: true, LastSuccessAt: now.Add(-time.Minute),
			ExpectedInterval: 5 * time.Minute, CredentialExpiresAt: now.Add(time.Hour),
		}, state: Healthy, reason: "healthy"},
		{name: "stale", input: Signal{
			Enabled: true, LastSuccessAt: now.Add(-11 * time.Minute),
			ExpectedInterval: 5 * time.Minute, CredentialExpiresAt: now.Add(time.Hour),
		}, state: Degraded, reason: "freshness_lag"},
		{name: "queue", input: Signal{
			Enabled: true, LastSuccessAt: now, ExpectedInterval: 5 * time.Minute,
			OldestQueuedAt: now.Add(-6 * time.Minute), CredentialExpiresAt: now.Add(time.Hour),
		}, state: Degraded, reason: "queue_delay"},
		{name: "credential", input: Signal{
			Enabled: true, LastSuccessAt: now, ExpectedInterval: 5 * time.Minute,
			CredentialExpiresAt: now,
		}, state: Failed, reason: "credential_expired"},
		{name: "failures", input: Signal{
			Enabled: true, LastSuccessAt: now, ExpectedInterval: 5 * time.Minute,
			CredentialExpiresAt: now.Add(time.Hour), ConsecutiveFailures: 3,
		}, state: Failed, reason: "repeated_failures"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := Evaluate(test.input, now)
			if result.State != test.state || result.Reason != test.reason {
				t.Fatalf("Evaluate() = %+v", result)
			}
		})
	}
}

func TestSnapshotPreservesPerConnectionEvidenceAndWorstState(t *testing.T) {
	now := time.Date(2026, time.July, 29, 23, 30, 0, 0, time.UTC)
	snapshot := BuildSnapshot([]ConnectionSignal{
		{ID: "graph", Kind: "graph", Signal: Signal{
			Enabled: true, LastSuccessAt: now, ExpectedInterval: 5 * time.Minute,
			CredentialExpiresAt: now.Add(time.Hour),
		}},
		{ID: "teams", Kind: "teams", Signal: Signal{
			Enabled: true, LastSuccessAt: now, ExpectedInterval: time.Minute,
			CredentialExpiresAt: now, PendingFailures: 4,
		}},
	}, now)
	if snapshot.State != Failed || len(snapshot.Connections) != 2 ||
		snapshot.Connections[1].PendingFailures != 4 {
		t.Fatalf("BuildSnapshot() hid connection evidence: %+v", snapshot)
	}
}
