# Phase 0 Delivery-Baseline Backlog

**Status:** Complete; retained as the delivery-baseline record

| ID | Outcome | Acceptance evidence |
|---|---|---|
| P0-01 | Record Milestone 0 approval and baseline ADR | Approval record and ADR exist |
| P0-02 | Establish repository/branch/review conventions | Delivery baseline reviewed |
| P0-03 | Establish CI quality/security/documentation checks | Green baseline run against synthetic repository state |
| P0-04 | Create acceptance environment definition | Reproducible Compose environment design and fixture inventory |
| P0-05 | Create threat model and security gates | Threat model linked to backlog and test requirements |
| P0-06 | Create authorization/client-isolation test charter | Attack matrix maps to fixture suite |
| P0-07 | Select concrete HA, queue/cache, storage compatibility, and mail/Datto operational implementations | HA, Valkey, MinIO, mail, and Datto operating directions accepted; implementation test plans remain |
| P0-08 | Define artifact provenance, SBOM, release, and migration validation policy | CI/release checklist |
| P0-09 | Define observability acceptance signals | Health, backup, queue, sync, security, and trace requirements |

All Phase 0 outcomes have source implementations and constrained-demo evidence.
Reviewed-main CI, signed artifact publication, and the fail-closed release gate
supersede the initial planning-only baseline. Supported-profile release
qualification remains tracked separately and does not reopen Phase 0.
