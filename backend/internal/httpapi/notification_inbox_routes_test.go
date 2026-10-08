package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

const notificationInboxReadID = "00000000-0000-4000-8000-000000000401"

type notificationInboxActionsStub struct {
	principal authorization.Principal
	cursor    string
	limit     int
	listed    notifications.InboxPage
	unread    int
	marked    notifications.RecipientNotification
	markID    string
	version   int64
	err       error
}

func (stub *notificationInboxActionsStub) List(_ context.Context, principal authorization.Principal, cursor string, limit int) (notifications.InboxPage, error) {
	stub.principal, stub.cursor, stub.limit = principal, cursor, limit
	return stub.listed, stub.err
}

func (stub *notificationInboxActionsStub) UnreadCount(_ context.Context, principal authorization.Principal) (int, error) {
	stub.principal = principal
	return stub.unread, stub.err
}

func (stub *notificationInboxActionsStub) MarkRead(_ context.Context, principal authorization.Principal, notificationID string, expectedVersion int64) (notifications.RecipientNotification, error) {
	stub.principal, stub.markID, stub.version = principal, notificationID, expectedVersion
	return stub.marked, stub.err
}

func notificationInboxPrincipal() authorization.Principal {
	return authorization.Principal{ID: "recipient-id", Scope: scope.Principal{MSPID: "msp-id"}}
}

func notificationInboxRouter(actions InboxActions) http.Handler {
	return NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return notificationInboxPrincipal(), nil
		},
		NotificationInbox: actions,
	})
}

func TestNotificationInboxListUsesTrustedPrincipalAndNotificationIdentity(t *testing.T) {
	createdAt := time.Date(2026, time.August, 15, 10, 30, 0, 0, time.UTC)
	actions := &notificationInboxActionsStub{listed: notifications.InboxPage{
		Notifications: []notifications.RecipientNotification{{
			ID: "notification-id", RecipientID: "recipient-id", DeliveryID: "delivery-id",
			DeduplicationKey: "internal-deduplication-key", Title: "Calendar schedule changed",
			Body: "schedule: calendar item", ActionPath: "/calendar",
			ContentClassification: "internal", CreatedAt: createdAt, Version: 3,
		}},
		NextCursor: "opaque-next",
	}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?cursor=opaque-current", nil)
	request.Header.Set("X-MSP-ID", "attacker-msp")
	request.Header.Set("X-Recipient-ID", "attacker-recipient")
	notificationInboxRouter(actions).ServeHTTP(response, request)

	if response.Code != http.StatusOK || actions.principal.ID != "recipient-id" || actions.principal.Scope.MSPID != "msp-id" || actions.cursor != "opaque-current" || actions.limit != 50 {
		t.Fatalf("status=%d principal=%+v cursor=%q limit=%d body=%s", response.Code, actions.principal, actions.cursor, actions.limit, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"id":"notification-id"`) || !strings.Contains(body, `"next_cursor":"opaque-next"`) || strings.Contains(body, "delivery-id") || strings.Contains(body, "internal-deduplication-key") {
		t.Fatalf("response exposed the wrong inbox identity: %s", body)
	}
}

func TestNotificationInboxListClampsExplicitLimits(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		want      int
	}{
		{name: "minimum", raw: "0", want: 1},
		{name: "maximum", raw: "101", want: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &notificationInboxActionsStub{}
			response := httptest.NewRecorder()
			notificationInboxRouter(actions).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/notifications?limit="+test.raw, nil))
			if response.Code != http.StatusOK || actions.limit != test.want {
				t.Fatalf("status=%d limit=%d want=%d body=%s", response.Code, actions.limit, test.want, response.Body.String())
			}
		})
	}
}

func TestNotificationInboxUnreadCountHasLiteralResponse(t *testing.T) {
	actions := &notificationInboxActionsStub{unread: 2}
	response := httptest.NewRecorder()
	notificationInboxRouter(actions).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread-count", nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"count":2}` || actions.principal.ID != "recipient-id" {
		t.Fatalf("status=%d principal=%+v body=%q", response.Code, actions.principal, response.Body.String())
	}
}

func TestNotificationInboxMarkReadUsesPathNotificationAndIsRepeatable(t *testing.T) {
	readAt := time.Date(2026, time.August, 15, 10, 31, 0, 0, time.UTC)
	actions := &notificationInboxActionsStub{marked: notifications.RecipientNotification{
		ID: notificationInboxReadID, DeliveryID: "delivery-id", RecipientID: "recipient-id",
		Title: "Calendar schedule changed", Body: "schedule: calendar item",
		ActionPath: "/calendar", ContentClassification: "internal",
		CreatedAt: readAt.Add(-time.Minute), ReadAt: &readAt, Version: 4,
	}}
	router := notificationInboxRouter(actions)
	for attempt := 0; attempt < 2; attempt++ {
		response := performNotificationInboxJSON(router, http.MethodPatch, "/api/v1/notifications/"+notificationInboxReadID+"/read", `{"expected_version":3}`)
		if response.Code != http.StatusOK || response.Header().Get("ETag") != `"4"` || !strings.Contains(response.Body.String(), `"id":"`+notificationInboxReadID+`"`) {
			t.Fatalf("attempt=%d status=%d etag=%q body=%s", attempt, response.Code, response.Header().Get("ETag"), response.Body.String())
		}
		if actions.principal.ID != "recipient-id" || actions.markID != notificationInboxReadID || actions.version != 3 {
			t.Fatalf("attempt=%d principal=%+v id=%q version=%d", attempt, actions.principal, actions.markID, actions.version)
		}
	}
}

func TestNotificationInboxMarkReadMapsOwnershipAndVersionErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "cross recipient", err: scope.ErrNotFound, want: http.StatusNotFound},
		{name: "stale version", err: object.ErrVersionConflict, want: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &notificationInboxActionsStub{err: test.err}
			response := performNotificationInboxJSON(notificationInboxRouter(actions), http.MethodPatch, "/api/v1/notifications/"+notificationInboxReadID+"/read", `{"expected_version":3}`)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestNotificationInboxRoutesRequireAuthentication(t *testing.T) {
	router := NewRouter(Dependencies{NotificationInbox: &notificationInboxActionsStub{}})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread-count", nil),
		notificationInboxJSONRequest(http.MethodPatch, "/api/v1/notifications/notification-id/read", `{"expected_version":1}`),
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}

func TestNotificationInboxRejectsMalformedCursorLimitAndBody(t *testing.T) {
	cursorActions := &notificationInboxActionsStub{err: notifications.ErrInvalidInboxRequest}
	cursorResponse := httptest.NewRecorder()
	notificationInboxRouter(cursorActions).ServeHTTP(cursorResponse, httptest.NewRequest(http.MethodGet, "/api/v1/notifications?cursor=%25%25%25", nil))
	if cursorResponse.Code != http.StatusBadRequest {
		t.Fatalf("cursor status=%d body=%s", cursorResponse.Code, cursorResponse.Body.String())
	}

	limitResponse := httptest.NewRecorder()
	notificationInboxRouter(&notificationInboxActionsStub{}).ServeHTTP(limitResponse, httptest.NewRequest(http.MethodGet, "/api/v1/notifications?limit=many", nil))
	if limitResponse.Code != http.StatusBadRequest {
		t.Fatalf("limit status=%d body=%s", limitResponse.Code, limitResponse.Body.String())
	}

	for _, body := range []string{
		`{"expected_version":`,
		`{"expected_version":0}`,
		`{"expected_version":1,"recipient_id":"attacker"}`,
	} {
		response := performNotificationInboxJSON(notificationInboxRouter(&notificationInboxActionsStub{}), http.MethodPatch, "/api/v1/notifications/"+notificationInboxReadID+"/read", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%q status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestNotificationInboxRejectsMalformedNotificationIDBeforeAction(t *testing.T) {
	for _, notificationID := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000", "00000000-0000-4000-8000-00000000000A", "urn:uuid:00000000-0000-4000-8000-000000000001"} {
		actions := &notificationInboxActionsStub{}
		response := performNotificationInboxJSON(notificationInboxRouter(actions), http.MethodPatch, "/api/v1/notifications/"+notificationID+"/read", `{"expected_version":1}`)
		if response.Code != http.StatusBadRequest || actions.markID != "" {
			t.Fatalf("id=%q status=%d mark ID=%q body=%s", notificationID, response.Code, actions.markID, response.Body.String())
		}
	}
}

func performNotificationInboxJSON(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, notificationInboxJSONRequest(method, path, body))
	return response
}

func notificationInboxJSONRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
