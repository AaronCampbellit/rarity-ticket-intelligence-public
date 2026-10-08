package integrationhealth

import (
	"context"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type StoredSignal struct {
	ID              string
	Kind            string
	Enabled         bool
	HealthState     string
	LastSuccessAt   time.Time
	LastErrorCode   string
	PendingFailures int
}

type Repository interface {
	List(context.Context, string) ([]StoredSignal, error)
}

type SnapshotCommand struct {
	Principal authorization.Principal
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (s *Service) Snapshot(
	ctx context.Context,
	command SnapshotCommand,
) (Snapshot, error) {
	target := scope.Target{MSPID: command.Principal.Scope.MSPID}
	if err := authorization.Authorize(
		command.Principal, "integration.read", target,
	); err != nil {
		return Snapshot{}, err
	}
	stored, err := s.repository.List(ctx, target.MSPID)
	if err != nil {
		return Snapshot{}, err
	}
	signals := make([]ConnectionSignal, len(stored))
	for index, item := range stored {
		reportedState := State("")
		reportedReason := ""
		switch item.HealthState {
		case "failed":
			reportedState = Failed
			reportedReason = item.LastErrorCode
		case "degraded":
			reportedState = Degraded
			reportedReason = item.LastErrorCode
		}
		signals[index] = ConnectionSignal{
			ID: item.ID, Kind: item.Kind,
			Signal: Signal{
				Enabled: item.Enabled, ReportedState: reportedState,
				ReportedReason:   item.LastErrorCode,
				LastSuccessAt:    item.LastSuccessAt,
				ExpectedInterval: expectedInterval(item.Kind),
				PendingFailures:  item.PendingFailures,
			},
		}
		if reportedReason != "" {
			signals[index].ReportedReason = reportedReason
		}
	}
	snapshot := BuildSnapshot(signals, s.now().UTC())
	for index := range snapshot.Connections {
		snapshot.Connections[index].LastErrorCode = stored[index].LastErrorCode
	}
	return snapshot, nil
}

func expectedInterval(kind string) time.Duration {
	switch kind {
	case "graph", "forwarding":
		return 5 * time.Minute
	case "datto":
		return 15 * time.Minute
	default:
		return 0
	}
}
