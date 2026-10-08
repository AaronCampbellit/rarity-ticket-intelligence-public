package observability

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPMiddlewareEmitsSafeTraceAndMetrics(t *testing.T) {
	logs := new(bytes.Buffer)
	logger := NewLogger(logs, slog.LevelInfo)
	metrics := NewHTTPMetrics()
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Pattern = "GET /v1/clients/{client_id}"
		writer.WriteHeader(http.StatusNoContent)
	})
	handler := HTTPMiddleware(logger, metrics)(next)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/clients/client-secret?token=do-not-log",
		nil,
	)
	request.Header.Set(
		"traceparent",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Header().Get("traceparent") == "" ||
		response.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected trace and request correlation headers")
	}
	for _, secret := range []string{"client-secret", "do-not-log"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("HTTP log leaked request data %q: %s", secret, logs.String())
		}
	}
	for _, expected := range []string{
		`"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`,
		`"route":"GET /v1/clients/{client_id}"`,
		`"status":204`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("HTTP log missing %q: %s", expected, logs.String())
		}
	}

	metricsResponse := httptest.NewRecorder()
	metrics.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsResponse.Body.String()
	for _, expected := range []string{
		`rarity_http_requests_total{method="GET",route="GET /v1/clients/{client_id}",status_class="2xx"} 1`,
		`rarity_http_request_duration_seconds_bucket`,
		`rarity_http_requests_in_flight 0`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
}

func TestHTTPMiddlewareCreatesValidTraceWhenInputIsUntrusted(t *testing.T) {
	handler := HTTPMiddleware(
		NewLogger(new(bytes.Buffer), slog.LevelInfo),
		NewHTTPMetrics(),
	)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("traceparent", "attacker-controlled")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	traceparent := response.Header().Get("traceparent")
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 || len(parts[1]) != 32 || len(parts[2]) != 16 {
		t.Fatalf("expected generated W3C traceparent, got %q", traceparent)
	}
}
