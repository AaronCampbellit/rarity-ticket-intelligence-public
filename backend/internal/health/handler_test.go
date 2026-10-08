package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCheck struct {
	database  error
	migration error
}

func (f fakeCheck) Database(context.Context) error   { return f.database }
func (f fakeCheck) Migrations(context.Context) error { return f.migration }

func TestReadyzRequiresDatabaseAndCurrentMigration(t *testing.T) {
	handler := NewHandler(fakeCheck{
		database:  errors.New("database unavailable"),
		migration: errors.New("migration unavailable"),
	}, "abc123")
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected readiness failure, got %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatalf("readiness leaked internal failure: %s", response.Body.String())
	}
}

func TestBuildEndpointReportsRevision(t *testing.T) {
	handler := NewHandler(fakeCheck{}, "abc123")
	request := httptest.NewRequest(http.MethodGet, "/v1/system/build", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "abc123") {
		t.Fatalf("expected revision response, got %d %s", response.Code, response.Body.String())
	}
}

func TestMetricsEndpointUsesConfiguredInternalHandler(t *testing.T) {
	metrics := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("rarity_test_metric 1\n"))
	})
	handler := NewHandlerWithMetrics(fakeCheck{}, "abc123", metrics)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "rarity_test_metric 1") {
		t.Fatalf("expected internal metrics response, got %d %s", response.Code, response.Body.String())
	}
}
