package datto

import (
	"context"
	"strings"
	"time"
)

type AlertState string

const (
	AlertActive  AlertState = "active"
	AlertCleared AlertState = "cleared"
)

type AlertObservation struct {
	ExternalID       string
	ExternalDeviceID string
	SiteID           string
	DeviceName       string
	SiteName         string
	Priority         string
	Title            string
	Diagnostics      string
	Fingerprint      string
	State            AlertState
	ObservedAt       time.Time
	SourcePayloadRef string
}

type AlertIndex interface {
	OpenIncidentByExternalID(context.Context, string) (string, bool)
	OpenIncidentByFingerprint(context.Context, string, time.Time) (string, bool)
}

type AlertAction string

const (
	AlertNoAction       AlertAction = "none"
	AlertCreateIncident AlertAction = "create_incident"
	AlertUpdateIncident AlertAction = "update_incident"
	AlertAppendRecovery AlertAction = "append_recovery"
)

type AlertResolution struct {
	Action          AlertAction
	IncidentID      string
	ResolveIncident bool
}

func ResolveAlert(
	ctx context.Context,
	index AlertIndex,
	observation AlertObservation,
	fingerprintWindow time.Duration,
) AlertResolution {
	if index == nil || strings.TrimSpace(observation.ExternalID) == "" ||
		observation.ObservedAt.IsZero() {
		return AlertResolution{Action: AlertNoAction}
	}
	if incidentID, ok := index.OpenIncidentByExternalID(ctx, observation.ExternalID); ok {
		if observation.State == AlertCleared {
			return AlertResolution{
				Action: AlertAppendRecovery, IncidentID: incidentID,
				ResolveIncident: false,
			}
		}
		return AlertResolution{Action: AlertUpdateIncident, IncidentID: incidentID}
	}
	if observation.State == AlertCleared {
		return AlertResolution{Action: AlertNoAction}
	}
	if fingerprintWindow <= 0 {
		fingerprintWindow = 24 * time.Hour
	}
	if fingerprint := strings.TrimSpace(observation.Fingerprint); fingerprint != "" {
		if incidentID, ok := index.OpenIncidentByFingerprint(
			ctx,
			fingerprint,
			observation.ObservedAt.Add(-fingerprintWindow),
		); ok {
			return AlertResolution{Action: AlertUpdateIncident, IncidentID: incidentID}
		}
	}
	return AlertResolution{Action: AlertCreateIncident}
}
