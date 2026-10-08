package datto

import (
	"context"
	"strings"
	"time"
)

const (
	alertLease               = 5 * time.Minute
	defaultFingerprintWindow = 24 * time.Hour
)

type AlertQueueItem struct {
	ID           string
	ConnectionID string
	MSPID        string
	ClientID     string
	ActorID      string
	Observation  AlertObservation
}

type AlertIncident struct {
	ID          string
	MSPID       string
	ClientID    string
	ActorID     string
	DisplayID   string
	Title       string
	Description string
	Priority    string
}

type AlertQueue interface {
	ClaimAlerts(
		context.Context,
		int,
		time.Time,
		time.Duration,
	) ([]AlertQueueItem, error)
	ResolveQueuedAlert(
		context.Context,
		AlertQueueItem,
		time.Duration,
	) (AlertResolution, error)
	CompleteAlert(
		context.Context,
		AlertQueueItem,
		AlertResolution,
		time.Time,
	) error
	ReleaseAlert(
		context.Context,
		AlertQueueItem,
		time.Time,
		string,
	) error
}

type AlertIncidentWriter interface {
	EnsureIncident(context.Context, AlertIncident) error
}

type AlertWorker struct {
	queue     AlertQueue
	incidents AlertIncidentWriter
	now       func() time.Time
}

type AlertWorkerResult struct {
	Claimed   int
	Created   int
	Updated   int
	Recovered int
	Ignored   int
	Failed    int
}

func NewAlertWorker(
	queue AlertQueue,
	incidents AlertIncidentWriter,
	now func() time.Time,
) *AlertWorker {
	return &AlertWorker{queue: queue, incidents: incidents, now: now}
}

func (w *AlertWorker) RunOnce(
	ctx context.Context,
	limit int,
) (AlertWorkerResult, error) {
	if w.queue == nil || w.incidents == nil || w.now == nil || limit <= 0 {
		return AlertWorkerResult{}, ErrInvalidSync
	}
	now := w.now().UTC()
	items, err := w.queue.ClaimAlerts(ctx, limit, now, alertLease)
	if err != nil {
		return AlertWorkerResult{}, err
	}
	result := AlertWorkerResult{Claimed: len(items)}
	for _, item := range items {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		resolution, err := w.queue.ResolveQueuedAlert(
			ctx, item, defaultFingerprintWindow,
		)
		if err == nil && resolution.Action == AlertCreateIncident {
			err = w.incidents.EnsureIncident(
				ctx, incidentFromAlert(item, resolution.IncidentID),
			)
		}
		if err != nil {
			if releaseErr := w.queue.ReleaseAlert(
				ctx, item, now, "alert_processing_failed",
			); releaseErr != nil {
				return result, releaseErr
			}
			result.Failed++
			continue
		}
		if err := w.queue.CompleteAlert(
			ctx, item, resolution, now,
		); err != nil {
			if releaseErr := w.queue.ReleaseAlert(
				ctx, item, now, "alert_completion_failed",
			); releaseErr != nil {
				return result, releaseErr
			}
			result.Failed++
			continue
		}
		switch resolution.Action {
		case AlertCreateIncident:
			result.Created++
		case AlertUpdateIncident:
			result.Updated++
		case AlertAppendRecovery:
			result.Recovered++
		default:
			result.Ignored++
		}
	}
	return result, nil
}

func incidentFromAlert(
	item AlertQueueItem,
	incidentID string,
) AlertIncident {
	displaySuffix := strings.ToUpper(strings.ReplaceAll(item.ID, "-", ""))
	if len(displaySuffix) > 12 {
		displaySuffix = displaySuffix[:12]
	}
	title := strings.TrimSpace(item.Observation.Title)
	if title == "" {
		title = "Datto RMM alert"
	}
	return AlertIncident{
		ID: incidentID, MSPID: item.MSPID, ClientID: item.ClientID,
		ActorID: item.ActorID, DisplayID: "DATTO-" + displaySuffix,
		Title: title, Description: strings.TrimSpace(item.Observation.Diagnostics),
		Priority: normalizeAlertPriority(item.Observation.Priority),
	}
}

func normalizeAlertPriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "moderate":
		return "normal"
	default:
		return "low"
	}
}
