# Observability

**Status:** HTTP telemetry and capacity source gates implemented; collector/load acceptance pending

OpenTelemetry-compatible traces, structured logs, metrics, health endpoints, and normalized operational events provide end-to-end diagnosis. Correlation follows intake through API, domain transaction, event publication, automation, notification, and webhook delivery.

V1 capacity evidence measures the accepted workload of 50 concurrently active technicians, 1,000 clients, and 5,000 new tickets on a peak day. It exposes p95/p99 read and mutation latency, dashboard/search latency, API acknowledgement, event processing/queue lag, search-index freshness, Graph intake, Datto freshness, storage headroom, backup health, and recovery evidence against the published budgets.

Dashboards serve operators and product administrators. Service-level indicators and objectives are defined for critical journeys. Telemetry itself is capacity-managed, access-controlled, and protected from client-data leakage.

## Implemented telemetry boundary

The API accepts or creates W3C `traceparent` correlation, creates a request/span identity, returns correlation headers, and records safe structured request-completion logs containing method, route pattern, status, duration, trace, span, and request IDs. It never records raw paths, query strings, headers, or bodies in that middleware. Authorization and cookie attributes are redacted by the shared logger.

The internal `/metrics` endpoint exports bounded-cardinality Prometheus counters, duration histograms, and an in-flight gauge by normalized HTTP method, registered route pattern, and status class. Both demo and pilot ingress explicitly return 404 for `/metrics`; an operator collector reaches the API only on its private network. OTLP span export and a deployed collector/dashboard remain environment composition work; the implemented correlation format is OpenTelemetry-compatible but is not represented as exported tracing.

## Capacity evidence gate

`go run ./backend/cmd/rarity-capacity-gate <evidence.json>` strictly decodes a bounded evidence file, computes nearest-rank percentiles, emits a machine-readable report, and fails if any accepted V1 scale, availability, headroom, latency, freshness, or lossless-burst budget is missed. `tests/fixtures/capacity-evidence.json` is schema/command smoke evidence only; it is not a load-test result.

Release acceptance must populate the schema from a realistic isolated run with 50 concurrent technicians, 1,000 Clients, 5,000 peak-day tickets, and a 1,000-event/five-minute burst. Raw samples and the exact revision/environment stay with the release evidence.
