package datto

import (
	"context"
	"testing"
	"time"
)

type alertIndex struct {
	byExternal    map[string]string
	byFingerprint map[string]string
	since         time.Time
}

func (i *alertIndex) OpenIncidentByExternalID(_ context.Context, value string) (string, bool) {
	result, ok := i.byExternal[value]
	return result, ok
}

func (i *alertIndex) OpenIncidentByFingerprint(
	_ context.Context,
	value string,
	since time.Time,
) (string, bool) {
	i.since = since
	result, ok := i.byFingerprint[value]
	return result, ok
}

func TestResolveAlertUsesExternalIdentityThenFingerprintWindow(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	index := &alertIndex{
		byExternal:    map[string]string{"alert-id": "incident-exact"},
		byFingerprint: map[string]string{"device|check|failure": "incident-similar"},
	}
	exact := ResolveAlert(context.Background(), index, AlertObservation{
		ExternalID: "alert-id", Fingerprint: "device|check|failure",
		State: AlertActive, ObservedAt: now,
	}, 24*time.Hour)
	if exact.Action != AlertUpdateIncident || exact.IncidentID != "incident-exact" {
		t.Fatalf("external identity was not authoritative: %+v", exact)
	}

	similar := ResolveAlert(context.Background(), index, AlertObservation{
		ExternalID: "new-alert-id", Fingerprint: "device|check|failure",
		State: AlertActive, ObservedAt: now,
	}, 0)
	if similar.Action != AlertUpdateIncident || similar.IncidentID != "incident-similar" ||
		index.since != now.Add(-24*time.Hour) {
		t.Fatalf("fingerprint deduplication invalid: result=%+v since=%v", similar, index.since)
	}
}

func TestClearedAlertAppendsRecoveryButNeverResolvesIncident(t *testing.T) {
	now := time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC)
	index := &alertIndex{byExternal: map[string]string{"alert-id": "incident-id"}}
	result := ResolveAlert(context.Background(), index, AlertObservation{
		ExternalID: "alert-id", State: AlertCleared, ObservedAt: now,
	}, 24*time.Hour)
	if result.Action != AlertAppendRecovery || result.IncidentID != "incident-id" ||
		result.ResolveIncident {
		t.Fatalf("cleared alert changed incident lifecycle: %+v", result)
	}
}

func TestUnmatchedActiveAlertCreatesIncident(t *testing.T) {
	result := ResolveAlert(context.Background(), &alertIndex{}, AlertObservation{
		ExternalID: "alert-id", Fingerprint: "fingerprint",
		State: AlertActive, ObservedAt: time.Now(),
	}, 24*time.Hour)
	if result.Action != AlertCreateIncident {
		t.Fatalf("unmatched alert action = %+v", result)
	}
}
