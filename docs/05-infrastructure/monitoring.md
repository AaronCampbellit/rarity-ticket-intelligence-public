# Monitoring

**Status:** Core HTTP signals implemented; operational collectors and alert acceptance pending

Monitoring covers availability, latency, error rate, saturation, replication, queue/event lag, worker progress, email age, integration health, automation failure, search freshness, backup freshness, certificate expiry, and capacity.

Alerts are actionable, routed, deduplicated, severity-defined, and linked to runbooks. Rarity should surface its own health before technicians discover degradation.

The API currently exposes liveness, database/migration readiness, build revision, private HTTP metrics, trace/request correlation, and structured request completion logs. Integration packages provide explicit freshness/failure states, and recovery tooling produces backup/restore evidence. Public ingress hides metrics.

Remaining environment composition must scrape and retain metrics, export correlated spans, ingest structured logs, evaluate PostgreSQL quorum/replication and pgBackRest state, and route the accepted warning/critical thresholds to named runbooks. A release cannot infer monitoring acceptance from endpoint existence alone.
