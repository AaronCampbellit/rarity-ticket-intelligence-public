package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
)

type publishNotificationPolicyRequest struct {
	ExpectedVersion    int64                          `json:"expected_version,omitempty"`
	Key                string                         `json:"key"`
	Name               string                         `json:"name"`
	EventType          string                         `json:"event_type"`
	Conditions         notifications.PolicyConditions `json:"conditions,omitempty"`
	Destinations       []notifications.Destination    `json:"destinations"`
	QuietPeriodSeconds int                            `json:"quiet_period_seconds"`
	CriticalBypass     bool                           `json:"critical_bypass"`
	Enabled            bool                           `json:"enabled"`
	Priority           int                            `json:"priority"`
	StableOrder        int                            `json:"stable_order"`
}

type updateMentionPreferenceRequest struct {
	EmailEnabled    json.RawMessage `json:"email_enabled"`
	TeamsEnabled    json.RawMessage `json:"teams_enabled"`
	TimeZone        json.RawMessage `json:"time_zone"`
	QuietStart      json.RawMessage `json:"quiet_start"`
	QuietEnd        json.RawMessage `json:"quiet_end"`
	ExpectedVersion json.RawMessage `json:"expected_version"`
}

func decodeMentionPreferenceUpdate(body updateMentionPreferenceRequest) (notifications.UpdatePreferenceCommand, bool) {
	var command notifications.UpdatePreferenceCommand
	if !decodeRequiredJSON(body.EmailEnabled, &command.EmailEnabled) ||
		!decodeRequiredJSON(body.TeamsEnabled, &command.TeamsEnabled) ||
		!decodeRequiredJSON(body.TimeZone, &command.TimeZone) ||
		!decodeRequiredJSON(body.ExpectedVersion, &command.ExpectedVersion) ||
		strings.TrimSpace(command.TimeZone) == "" || command.ExpectedVersion < 0 {
		return notifications.UpdatePreferenceCommand{}, false
	}
	if len(body.QuietStart) == 0 || len(body.QuietEnd) == 0 {
		return notifications.UpdatePreferenceCommand{}, false
	}
	startNull, endNull := isJSONNull(body.QuietStart), isJSONNull(body.QuietEnd)
	if startNull || endNull {
		return command, startNull && endNull
	}
	if !decodeRequiredJSON(body.QuietStart, &command.QuietStart) || !decodeRequiredJSON(body.QuietEnd, &command.QuietEnd) ||
		!validClockValue(command.QuietStart) || !validClockValue(command.QuietEnd) {
		return notifications.UpdatePreferenceCommand{}, false
	}
	return command, true
}

func decodeRequiredJSON(raw json.RawMessage, target any) bool {
	return len(raw) > 0 && !isJSONNull(raw) && json.Unmarshal(raw, target) == nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func validClockValue(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func (r *Router) registerNotificationRoutes() {
	r.mux.HandleFunc("GET /api/v1/notification-policies", r.listNotificationPolicies)
	r.mux.HandleFunc("POST /api/v1/notification-policies", r.publishNotificationPolicy)
	r.mux.HandleFunc(
		"POST /api/v1/notification-policies/{id}/versions",
		r.publishNotificationPolicy,
	)
	r.mux.HandleFunc("GET /api/v1/notification-preferences/mentions", r.getMentionNotificationPreference)
	r.mux.HandleFunc("PATCH /api/v1/notification-preferences/mentions", r.updateMentionNotificationPreference)
}

func (r *Router) getMentionNotificationPreference(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationPreferences == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "mention notification preferences are unavailable")
		return
	}
	preference, err := r.dependencies.NotificationPreferences.Get(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, preference.Version)
	writeJSON(writer, http.StatusOK, preference)
}

func (r *Router) updateMentionNotificationPreference(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationPreferences == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "mention notification preferences are unavailable")
		return
	}
	var body updateMentionPreferenceRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	command, valid := decodeMentionPreferenceUpdate(body)
	if !valid {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "mention notification preference is invalid")
		return
	}
	command.Principal = principal
	preference, err := r.dependencies.NotificationPreferences.Update(request.Context(), command)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, preference.Version)
	writeJSON(writer, http.StatusOK, preference)
}

func (r *Router) listNotificationPolicies(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationPolicies == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "notification policy management is unavailable")
		return
	}
	found, err := r.dependencies.NotificationPolicies.ListPolicies(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) publishNotificationPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationPolicies == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "notification policy management is unavailable")
		return
	}
	var body publishNotificationPolicyRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	published, err := r.dependencies.NotificationPolicies.PublishPolicy(
		request.Context(),
		notifications.PublishPolicyCommand{
			Principal: principal, Target: targetFor(principal),
			PolicyID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion,
			Key: body.Key, Name: body.Name, EventType: body.EventType,
			Conditions: body.Conditions, Destinations: body.Destinations,
			QuietPeriodSeconds: body.QuietPeriodSeconds,
			CriticalBypass:     body.CriticalBypass, Enabled: body.Enabled,
			Priority: body.Priority, StableOrder: body.StableOrder,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, published.Version)
	writeJSON(writer, http.StatusCreated, published)
}
