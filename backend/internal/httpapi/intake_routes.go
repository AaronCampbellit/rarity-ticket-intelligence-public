package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

const (
	maxDirectIntakeBytes     = 1 << 20
	maxForwardingIntakeBytes = 50 << 20
)

type acceptedIntakeResponse struct {
	ID              string                 `json:"id"`
	Source          intake.Source          `json:"source"`
	ExternalID      string                 `json:"external_id"`
	ReceivedAt      time.Time              `json:"received_at"`
	ProcessingState intake.ProcessingState `json:"processing_state"`
}

func (r *Router) registerIntakeRoutes() {
	r.mux.HandleFunc("POST /api/v1/intake/direct", r.directIntake)
	r.mux.HandleFunc(
		"POST /api/v1/webhooks/inbound/{connection_id}",
		r.inboundWebhook,
	)
	r.mux.HandleFunc(
		"POST /api/v1/intake/forwarding/{connection_id}",
		r.forwardingIntake,
	)
	r.mux.HandleFunc(
		"POST /api/v1/graph/notifications",
		r.graphNotifications,
	)
}

func (r *Router) graphNotifications(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if validationToken := request.URL.Query().Get("validationToken"); validationToken != "" {
		if len(validationToken) > 255 ||
			strings.ContainsAny(validationToken, "\x00\r\n") {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "validation token is invalid")
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(validationToken))
		return
	}
	if r.dependencies.GraphNotifications == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Graph notifications are unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "application/json is required")
		return
	}
	body := http.MaxBytesReader(writer, request.Body, maxDirectIntakeBytes)
	payload, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "payload_too_large", "payload exceeds the Graph notification limit")
			return
		}
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return
	}
	result, err := r.dependencies.GraphNotifications.Accept(
		request.Context(), payload,
	)
	if errors.Is(err, graphintake.ErrInvalidNotification) {
		writeError(request.Context(), writer, http.StatusUnauthorized, "graph_notification_invalid", "Graph notification validation failed")
		return
	}
	if err != nil {
		writeError(request.Context(), writer, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}

func (r *Router) directIntake(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.DirectIntake == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "direct intake is unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "application/json is required")
		return
	}
	body := http.MaxBytesReader(writer, request.Body, maxDirectIntakeBytes)
	payload, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "payload_too_large", "payload exceeds the direct intake limit")
			return
		}
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return
	}
	if !json.Valid(payload) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body must be valid JSON")
		return
	}
	event, err := r.dependencies.DirectIntake.Accept(
		request.Context(),
		intake.SystemCommand{
			Principal: principal, Target: targetFor(principal),
			ExternalID: request.Header.Get("Idempotency-Key"), Payload: payload,
			ActorID: principal.ID, ActorType: "authenticated_principal",
			SourceName: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, 1)
	writeJSON(writer, http.StatusAccepted, acceptedIntake(event))
}

func (r *Router) inboundWebhook(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if r.dependencies.InboundWebhooks == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "inbound webhooks are unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "application/json is required")
		return
	}
	body := http.MaxBytesReader(writer, request.Body, maxDirectIntakeBytes)
	payload, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "payload_too_large", "payload exceeds the inbound webhook limit")
			return
		}
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return
	}
	if !json.Valid(payload) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body must be valid JSON")
		return
	}
	event, err := r.dependencies.InboundWebhooks.Accept(
		request.Context(),
		webhooks.InboundCommand{
			ConnectionID: request.PathValue("connection_id"),
			Timestamp:    request.Header.Get("X-Rarity-Timestamp"),
			EventID:      request.Header.Get("X-Rarity-Event-ID"),
			Signature:    request.Header.Get("X-Rarity-Signature"),
			Body:         payload,
			SourceName:   source(request),
		},
	)
	switch {
	case errors.Is(err, scope.ErrNotFound),
		errors.Is(err, webhooks.ErrInvalidSignature),
		errors.Is(err, webhooks.ErrStaleTimestamp):
		writeError(request.Context(), writer, http.StatusUnauthorized, "webhook_authentication_failed", "webhook authentication failed")
		return
	case errors.Is(err, webhooks.ErrReplay):
		writeError(request.Context(), writer, http.StatusConflict, "webhook_replay", "webhook event was already received")
		return
	case errors.Is(err, intake.ErrInvalidSystemIntake):
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "webhook request is invalid")
		return
	case err != nil:
		writeError(request.Context(), writer, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeETag(writer, 1)
	writeJSON(writer, http.StatusAccepted, acceptedIntake(event))
}

func acceptedIntake(event intake.InboundEvent) acceptedIntakeResponse {
	return acceptedIntakeResponse{
		ID: event.ID, Source: event.Source, ExternalID: event.ExternalID,
		ReceivedAt: event.ReceivedAt, ProcessingState: event.ProcessingState,
	}
}

func (r *Router) forwardingIntake(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ForwardingIntake == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "forwarding intake is unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "message/rfc822" {
		writeError(request.Context(), writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "message/rfc822 is required")
		return
	}
	body := http.MaxBytesReader(writer, request.Body, maxForwardingIntakeBytes)
	rawMIME, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(request.Context(), writer, http.StatusRequestEntityTooLarge, "payload_too_large", "message exceeds the forwarding intake limit")
			return
		}
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "message body is invalid")
		return
	}
	result, err := r.dependencies.ForwardingIntake.Accept(
		request.Context(),
		intake.ForwardingCommand{
			Principal: principal, ConnectionID: request.PathValue("connection_id"),
			Message: intake.ForwardedMessage{
				EnvelopeFrom: request.Header.Get("X-Rarity-Envelope-From"),
				From:         request.Header.Get("X-Rarity-Header-From"),
				To:           request.Header.Get("X-Rarity-Recipient"),
				MessageID:    request.Header.Get("X-Rarity-Message-ID"),
				SizeBytes:    int64(len(rawMIME)),
				Authentication: intake.SenderAuthentication{
					SPF:   strings.EqualFold(request.Header.Get("X-Rarity-SPF"), "pass"),
					DKIM:  strings.EqualFold(request.Header.Get("X-Rarity-DKIM"), "pass"),
					DMARC: strings.EqualFold(request.Header.Get("X-Rarity-DMARC"), "pass"),
				},
			},
			RawMIME: rawMIME, ActorID: principal.ID,
			ActorType: "authenticated_principal", SourceName: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}
