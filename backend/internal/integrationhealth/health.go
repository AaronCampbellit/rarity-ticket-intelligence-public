// Package integrationhealth produces authoritative, explainable health state
// from durable connection signals.
package integrationhealth

import "time"

type State string

const (
	Disabled State = "disabled"
	Healthy  State = "healthy"
	Degraded State = "degraded"
	Failed   State = "failed"
)

type Signal struct {
	Enabled             bool
	ReportedState       State
	ReportedReason      string
	LastSuccessAt       time.Time
	ExpectedInterval    time.Duration
	OldestQueuedAt      time.Time
	CredentialExpiresAt time.Time
	ConsecutiveFailures int
	PendingFailures     int
}

type Result struct {
	State               State         `json:"state"`
	Reason              string        `json:"reason"`
	FreshnessLag        time.Duration `json:"freshness_lag_nanoseconds"`
	QueueDelay          time.Duration `json:"queue_delay_nanoseconds"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	PendingFailures     int           `json:"pending_failures"`
}

func Evaluate(signal Signal, now time.Time) Result {
	result := Result{
		ConsecutiveFailures: signal.ConsecutiveFailures,
		PendingFailures:     signal.PendingFailures,
	}
	if !signal.Enabled {
		result.State, result.Reason = Disabled, "disabled"
		return result
	}
	if signal.ReportedState == Failed {
		result.State, result.Reason = Failed, signal.ReportedReason
		if result.Reason == "" {
			result.Reason = "provider_failed"
		}
		return result
	}
	if signal.ReportedState == Degraded {
		result.State, result.Reason = Degraded, signal.ReportedReason
		if result.Reason == "" {
			result.Reason = "provider_degraded"
		}
		return result
	}
	if !signal.CredentialExpiresAt.IsZero() &&
		!signal.CredentialExpiresAt.After(now) {
		result.State, result.Reason = Failed, "credential_expired"
		return result
	}
	if signal.ConsecutiveFailures >= 3 {
		result.State, result.Reason = Failed, "repeated_failures"
		return result
	}
	if signal.LastSuccessAt.IsZero() {
		result.State, result.Reason = Degraded, "never_succeeded"
		return result
	}
	result.FreshnessLag = now.Sub(signal.LastSuccessAt)
	if signal.ExpectedInterval > 0 &&
		result.FreshnessLag > 2*signal.ExpectedInterval {
		result.State, result.Reason = Degraded, "freshness_lag"
		return result
	}
	if !signal.OldestQueuedAt.IsZero() {
		result.QueueDelay = now.Sub(signal.OldestQueuedAt)
		if result.QueueDelay > 5*time.Minute {
			result.State, result.Reason = Degraded, "queue_delay"
			return result
		}
	}
	if signal.PendingFailures > 0 {
		result.State, result.Reason = Degraded, "pending_failures"
		return result
	}
	result.State, result.Reason = Healthy, "healthy"
	return result
}

type ConnectionSignal struct {
	ID   string
	Kind string
	Signal
}

type ConnectionResult struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	LastErrorCode string `json:"last_error_code,omitempty"`
	Result
}

type Snapshot struct {
	State       State              `json:"state"`
	GeneratedAt time.Time          `json:"generated_at"`
	Connections []ConnectionResult `json:"connections"`
}

func BuildSnapshot(signals []ConnectionSignal, now time.Time) Snapshot {
	snapshot := Snapshot{
		State: Healthy, GeneratedAt: now.UTC(),
		Connections: make([]ConnectionResult, 0, len(signals)),
	}
	for _, signal := range signals {
		result := Evaluate(signal.Signal, now)
		snapshot.Connections = append(snapshot.Connections, ConnectionResult{
			ID: signal.ID, Kind: signal.Kind, Result: result,
		})
		if stateRank(result.State) > stateRank(snapshot.State) {
			snapshot.State = result.State
		}
	}
	return snapshot
}

func stateRank(state State) int {
	switch state {
	case Failed:
		return 3
	case Degraded:
		return 2
	case Healthy:
		return 1
	default:
		return 0
	}
}
