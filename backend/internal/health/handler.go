package health

import (
	"context"
	"encoding/json"
	"net/http"
)

type Check interface {
	Database(context.Context) error
	Migrations(context.Context) error
}

func NewHandler(check Check, revision string) http.Handler {
	return NewHandlerWithMetrics(check, revision, nil)
}

func NewHandlerWithMetrics(check Check, revision string, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, request *http.Request) {
		if check.Database(request.Context()) != nil || check.Migrations(request.Context()) != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready", "revision": revision})
	})
	mux.HandleFunc("GET /v1/system/build", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"revision": revision})
	})
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	return mux
}

func writeJSON(writer http.ResponseWriter, status int, body map[string]string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
