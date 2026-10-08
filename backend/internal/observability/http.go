package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type traceContextKey struct{}

type TraceContext struct {
	TraceID   string
	SpanID    string
	RequestID string
}

func TraceFromContext(ctx context.Context) (TraceContext, bool) {
	trace, ok := ctx.Value(traceContextKey{}).(TraceContext)
	return trace, ok
}

type metricKey struct {
	Method      string
	Route       string
	StatusClass string
}

type metricValue struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

type HTTPMetrics struct {
	mu       sync.RWMutex
	values   map[metricKey]*metricValue
	inFlight atomic.Int64
}

var durationBuckets = []float64{0.1, 0.25, 0.5, 1, 1.5, 2, 5, 10}

func NewHTTPMetrics() *HTTPMetrics {
	return &HTTPMetrics{values: make(map[metricKey]*metricValue)}
}

func (metrics *HTTPMetrics) Observe(method, route string, status int, duration time.Duration) {
	if metrics == nil {
		return
	}
	key := metricKey{
		Method:      normalizedMethod(method),
		Route:       normalizedRoute(route),
		StatusClass: fmt.Sprintf("%dxx", status/100),
	}
	seconds := duration.Seconds()

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	value := metrics.values[key]
	if value == nil {
		value = &metricValue{Buckets: make([]uint64, len(durationBuckets))}
		metrics.values[key] = value
	}
	value.Count++
	value.Sum += seconds
	for index, upperBound := range durationBuckets {
		if seconds <= upperBound {
			value.Buckets[index]++
		}
	}
}

func (metrics *HTTPMetrics) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(writer, "# HELP rarity_http_requests_total Completed HTTP requests.")
	fmt.Fprintln(writer, "# TYPE rarity_http_requests_total counter")
	fmt.Fprintln(writer, "# HELP rarity_http_request_duration_seconds HTTP request duration.")
	fmt.Fprintln(writer, "# TYPE rarity_http_request_duration_seconds histogram")
	fmt.Fprintln(writer, "# HELP rarity_http_requests_in_flight HTTP requests currently executing.")
	fmt.Fprintln(writer, "# TYPE rarity_http_requests_in_flight gauge")
	fmt.Fprintf(writer, "rarity_http_requests_in_flight %d\n", metrics.inFlight.Load())

	metrics.mu.RLock()
	keys := make([]metricKey, 0, len(metrics.values))
	for key := range metrics.values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := keys[i].Method + keys[i].Route + keys[i].StatusClass
		right := keys[j].Method + keys[j].Route + keys[j].StatusClass
		return left < right
	})
	snapshots := make(map[metricKey]metricValue, len(keys))
	for _, key := range keys {
		value := metrics.values[key]
		snapshots[key] = metricValue{
			Count:   value.Count,
			Sum:     value.Sum,
			Buckets: append([]uint64(nil), value.Buckets...),
		}
	}
	metrics.mu.RUnlock()

	for _, key := range keys {
		value := snapshots[key]
		labels := metricLabels(key)
		fmt.Fprintf(writer, "rarity_http_requests_total{%s} %d\n", labels, value.Count)
		for index, upperBound := range durationBuckets {
			fmt.Fprintf(
				writer,
				"rarity_http_request_duration_seconds_bucket{%s,le=%q} %d\n",
				labels,
				strconv.FormatFloat(upperBound, 'f', -1, 64),
				value.Buckets[index],
			)
		}
		fmt.Fprintf(
			writer,
			"rarity_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n",
			labels,
			value.Count,
		)
		fmt.Fprintf(writer, "rarity_http_request_duration_seconds_sum{%s} %g\n", labels, value.Sum)
		fmt.Fprintf(writer, "rarity_http_request_duration_seconds_count{%s} %d\n", labels, value.Count)
	}
}

func HTTPMiddleware(logger *slog.Logger, metrics *HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			started := time.Now()
			if metrics != nil {
				metrics.inFlight.Add(1)
				defer metrics.inFlight.Add(-1)
			}

			traceID := incomingTraceID(request.Header.Get("traceparent"))
			if traceID == "" {
				traceID = randomHex(16)
			}
			spanID := randomHex(8)
			requestID := randomHex(16)
			trace := TraceContext{TraceID: traceID, SpanID: spanID, RequestID: requestID}
			request = request.WithContext(context.WithValue(request.Context(), traceContextKey{}, trace))
			writer.Header().Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
			writer.Header().Set("X-Request-ID", requestID)

			captured := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
			next.ServeHTTP(captured, request)
			route := normalizedRoute(request.Pattern)
			duration := time.Since(started)
			metrics.Observe(request.Method, route, captured.status, duration)
			if logger != nil {
				logger.InfoContext(
					request.Context(),
					"http request completed",
					"method", normalizedMethod(request.Method),
					"route", route,
					"status", captured.status,
					"duration_ms", duration.Milliseconds(),
					"trace_id", traceID,
					"span_id", spanID,
					"request_id", requestID,
				)
			}
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func incomingTraceID(value string) string {
	parts := strings.Split(value, "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 {
		return ""
	}
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return ""
	}
	if _, err := hex.DecodeString(parts[1] + parts[2] + parts[3]); err != nil {
		return ""
	}
	return strings.ToLower(parts[1])
}

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		panic("cryptographic random source unavailable")
	}
	return hex.EncodeToString(value)
}

func normalizedMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func normalizedRoute(route string) string {
	if strings.TrimSpace(route) == "" {
		return "unmatched"
	}
	return route
}

func metricLabels(key metricKey) string {
	return fmt.Sprintf(
		`method=%q,route=%q,status_class=%q`,
		escapeLabel(key.Method),
		escapeLabel(key.Route),
		escapeLabel(key.StatusClass),
	)
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
