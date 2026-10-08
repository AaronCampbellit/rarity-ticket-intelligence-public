package integrationhealth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type healthRepository struct {
	signals []StoredSignal
	calls   int
}

func (r *healthRepository) List(_ context.Context, mspID string) ([]StoredSignal, error) {
	r.calls++
	return r.signals, nil
}

func TestServiceBuildsMSPScopedSnapshotFromDurableSignals(t *testing.T) {
	now := time.Date(2026, time.July, 29, 21, 0, 0, 0, time.UTC)
	repository := &healthRepository{signals: []StoredSignal{
		{
			ID: "graph", Kind: "graph", Enabled: true, HealthState: "healthy",
			LastSuccessAt: now.Add(-time.Minute),
		},
		{
			ID: "teams", Kind: "teams", Enabled: true, HealthState: "degraded",
			LastSuccessAt: now.Add(-time.Minute), LastErrorCode: "delivery_failed",
			PendingFailures: 2,
		},
	}}
	service := NewService(repository, func() time.Time { return now })
	snapshot, err := service.Snapshot(context.Background(), SnapshotCommand{
		Principal: authorization.Principal{
			Scope:        scope.Principal{MSPID: "msp"},
			Capabilities: authorization.NewCapabilitySet("integration.read"),
		},
	})
	if err != nil {
		t.Fatalf("Snapshot() error=%v", err)
	}
	if repository.calls != 1 || snapshot.State != Degraded ||
		len(snapshot.Connections) != 2 ||
		snapshot.Connections[0].Reason != "healthy" ||
		snapshot.Connections[1].PendingFailures != 2 ||
		snapshot.Connections[1].LastErrorCode != "delivery_failed" {
		t.Fatalf("snapshot incomplete: %+v", snapshot)
	}
}

func TestServiceAuthorizesBeforeLoadingHealthSignals(t *testing.T) {
	repository := &healthRepository{}
	service := NewService(repository, time.Now)
	_, err := service.Snapshot(context.Background(), SnapshotCommand{
		Principal: authorization.Principal{
			Scope: scope.Principal{MSPID: "msp"},
		},
	})
	if !errors.Is(err, authorization.ErrForbidden) || repository.calls != 0 {
		t.Fatalf("unauthorized snapshot error=%v calls=%d", err, repository.calls)
	}
}
