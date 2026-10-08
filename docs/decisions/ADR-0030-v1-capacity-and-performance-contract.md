# ADR-0030: V1 capacity and performance contract

**Status:** Accepted\
**Date:** 2026-07-27

## Decision

Rarity V1 acceptance proves a supported workload of 50 concurrently active technicians, 1,000 client organizations, and 5,000 new tickets on a peak day. The service objective is 99.9% monthly availability, excluding pre-announced maintenance.

## Service budgets

| Area | V1 target |
| --- | --- |
| Interactive reads | p95 at or below 500 ms; p99 at or below 1.5 seconds |
| Work-record mutations | p95 at or below 1 second; p99 at or below 2 seconds, excluding large transfers and external waits |
| Default technician dashboards | p95 at or below 2 seconds |
| Permission-filtered global search | p95 at or below 1.5 seconds |
| API/webhook durable acknowledgement | p95 at or below 500 ms |
| Inbound-event processing | 95% within 60 seconds; warning at 2 minutes; critical at 10 minutes |
| Inbound burst | 1,000 events in five minutes with no loss of durable events |
| Search indexing | records searchable within 60 seconds |
| Datto freshness | normal 15-minute schedule; warning at 30 minutes; critical at 60 minutes |
| Graph email | normal retrieval/threading within 60 seconds; five-minute delta reconciliation remains the recovery path |

## Capacity and recovery

Storage planning forecasts seven years of retained email and attachments using observed intake volume and keeps at least 30% headroom. Capacity warns at 70% and is critical at 85%; Rarity never silently drops retained data to solve capacity pressure. The accepted database failover and full-disaster recovery objectives remain unchanged.

## Verification

Release acceptance uses realistic synthetic tenants, work, assets, alerts, attachments, and noisy RMM bursts. Each budget is measured and visible in the health dashboard; a release cannot claim capacity acceptance without evidence against this workload.
