package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
)

type notificationInboxItemResponse struct {
	ID                    string     `json:"id"`
	Title                 string     `json:"title"`
	Body                  string     `json:"body"`
	ActionPath            string     `json:"action_path"`
	ContentClassification string     `json:"content_classification"`
	CreatedAt             time.Time  `json:"created_at"`
	ReadAt                *time.Time `json:"read_at,omitempty"`
	Version               int64      `json:"version"`
}

type notificationInboxPageResponse struct {
	Notifications []notificationInboxItemResponse `json:"notifications"`
	NextCursor    string                          `json:"next_cursor,omitempty"`
}

type notificationUnreadCountResponse struct {
	Count int `json:"count"`
}

type markNotificationReadRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

func (r *Router) registerNotificationInboxRoutes() {
	r.mux.HandleFunc("GET /api/v1/notifications", r.listNotificationInbox)
	r.mux.HandleFunc("GET /api/v1/notifications/unread-count", r.notificationInboxUnreadCount)
	r.mux.HandleFunc("PATCH /api/v1/notifications/{id}/read", r.markNotificationInboxRead)
}

func (r *Router) listNotificationInbox(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationInbox == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "notification inbox is unavailable")
		return
	}
	limit, valid := notificationInboxLimit(request.URL.Query().Get("limit"))
	if !valid {
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
		return
	}
	page, err := r.dependencies.NotificationInbox.List(request.Context(), principal, request.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	response := notificationInboxPageResponse{
		Notifications: make([]notificationInboxItemResponse, len(page.Notifications)),
		NextCursor:    page.NextCursor,
	}
	for index, item := range page.Notifications {
		response.Notifications[index] = notificationInboxItem(item)
	}
	writeJSON(writer, http.StatusOK, response)
}

func (r *Router) notificationInboxUnreadCount(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationInbox == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "notification inbox is unavailable")
		return
	}
	count, err := r.dependencies.NotificationInbox.UnreadCount(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, notificationUnreadCountResponse{Count: count})
}

func (r *Router) markNotificationInboxRead(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.NotificationInbox == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "notification inbox is unavailable")
		return
	}
	var body markNotificationReadRequest
	if !decodeNotificationInboxBody(writer, request, &body) {
		return
	}
	if body.ExpectedVersion < 1 {
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
		return
	}
	notificationID := strings.TrimSpace(request.PathValue("id"))
	if !internalid.ValidCanonical(notificationID) {
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
		return
	}
	item, err := r.dependencies.NotificationInbox.MarkRead(request.Context(), principal, notificationID, body.ExpectedVersion)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, item.Version)
	writeJSON(writer, http.StatusOK, notificationInboxItem(item))
}

func notificationInboxLimit(raw string) (int, bool) {
	if raw == "" {
		return 50, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if limit < 1 {
		return 1, true
	}
	if limit > 100 {
		return 100, true
	}
	return limit, true
}

func decodeNotificationInboxBody(writer http.ResponseWriter, request *http.Request, target any) bool {
	body := http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(request.Context(), writer, http.StatusBadRequest, "invalid_notification_inbox", "notification inbox request is invalid")
		return false
	}
	return true
}

func notificationInboxItem(item notifications.RecipientNotification) notificationInboxItemResponse {
	return notificationInboxItemResponse{
		ID: item.ID, Title: item.Title, Body: item.Body, ActionPath: item.ActionPath,
		ContentClassification: item.ContentClassification, CreatedAt: item.CreatedAt,
		ReadAt: item.ReadAt, Version: item.Version,
	}
}
