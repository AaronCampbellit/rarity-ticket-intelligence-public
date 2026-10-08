package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type notificationPreferenceActions struct {
	updated     notifications.UpdatePreferenceCommand
	updateCalls int
}

func (a *notificationPreferenceActions) Get(_ context.Context, principal authorization.Principal) (notifications.RecipientPreference, error) {
	return notifications.RecipientPreference{TechnicianID: principal.ID, EventType: notifications.MentionOccurred, EmailEnabled: true, TimeZone: "UTC", Version: 2, EmailAvailable: true}, nil
}
func (a *notificationPreferenceActions) Update(_ context.Context, command notifications.UpdatePreferenceCommand) (notifications.RecipientPreference, error) {
	a.updateCalls++
	a.updated = command
	return notifications.RecipientPreference{TechnicianID: command.Principal.ID, EventType: notifications.MentionOccurred, EmailEnabled: command.EmailEnabled, TeamsEnabled: command.TeamsEnabled, TimeZone: command.TimeZone, QuietStart: command.QuietStart, QuietEnd: command.QuietEnd, Version: command.ExpectedVersion + 1}, nil
}

func TestMentionNotificationPreferencePatchRequiresEveryExplicitField(t *testing.T) {
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "email omitted", body: `{"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"quiet_end":null,"expected_version":2}`},
		{name: "email null", body: `{"email_enabled":null,"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"quiet_end":null,"expected_version":2}`},
		{name: "time zone null", body: `{"email_enabled":true,"teams_enabled":false,"time_zone":null,"quiet_start":null,"quiet_end":null,"expected_version":2}`},
		{name: "version null", body: `{"email_enabled":true,"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"quiet_end":null,"expected_version":null}`},
		{name: "quiet end omitted", body: `{"email_enabled":true,"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"expected_version":2}`},
		{name: "quiet mismatch", body: `{"email_enabled":true,"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"quiet_end":"06:00","expected_version":2}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &notificationPreferenceActions{}
			handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, NotificationPreferences: actions})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/notification-preferences/mentions", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"validation_failed"`) || actions.updateCalls != 0 {
				t.Fatalf("status=%d body=%s updateCalls=%d", response.Code, response.Body.String(), actions.updateCalls)
			}
		})
	}
}

func TestMentionNotificationPreferencePatchAcceptsExplicitQuietHoursClear(t *testing.T) {
	actions := &notificationPreferenceActions{}
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, NotificationPreferences: actions})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/notification-preferences/mentions", strings.NewReader(`{"email_enabled":true,"teams_enabled":false,"time_zone":"UTC","quiet_start":null,"quiet_end":null,"expected_version":2}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.updateCalls != 1 || actions.updated.QuietStart != "" || actions.updated.QuietEnd != "" {
		t.Fatalf("status=%d body=%s command=%+v calls=%d", response.Code, response.Body.String(), actions.updated, actions.updateCalls)
	}
}

func TestMentionNotificationPreferenceRoutesAreSelfScopedAndStrict(t *testing.T) {
	actions := &notificationPreferenceActions{}
	principal := authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp"}}
	handler := NewRouter(Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return principal, nil }, NotificationPreferences: actions})

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/notification-preferences/mentions", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"technician_id":"tech"`) {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}

	patch := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/notification-preferences/mentions", strings.NewReader(`{"email_enabled":true,"teams_enabled":false,"time_zone":"America/New_York","quiet_start":"22:00","quiet_end":"06:00","expected_version":2}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(patch, request)
	if patch.Code != http.StatusOK || actions.updated.Principal.ID != "tech" || actions.updated.ExpectedVersion != 2 {
		t.Fatalf("PATCH status=%d body=%s command=%+v", patch.Code, patch.Body.String(), actions.updated)
	}

	invalid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPatch, "/api/v1/notification-preferences/mentions", strings.NewReader(`{"email_enabled":true,"teams_enabled":false,"time_zone":"UTC","expected_version":2,"technician_id":"other"}`))
	handler.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown self-scope field status=%d", invalid.Code)
	}
}
