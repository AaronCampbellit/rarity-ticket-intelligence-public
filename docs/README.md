# Documentation guide

**Updated:** 2026-09-04

Start with the [current execution backlog](09-roadmap/current-execution.md) for
remaining work and the [local development guide](06-development/local-development.md)
for setup and verification.

## Authority and status

- Accepted [architecture decisions](decisions/README.md) define product and safety boundaries.
- Domain, API, security, and operations documents describe their respective contracts.
  A `Draft` label describes editorial/approval status; it does not mean the matching code is absent.
- The [implementation record](09-roadmap/implementation-plan.md) preserves phased delivery
  history. Prior successful evidence applies only to its stated revision and environment.
- The [current execution backlog](09-roadmap/current-execution.md) is the ordered queue
  for implementation and qualification. Update it with results when completing a slice.
- The [release gate](06-development/release-readiness.md) owns release eligibility.
  Local tests, source availability, and historical artifact verification cannot satisfy
  missing candidate-specific deployment, recovery, capacity, or manual acceptance.
- Dated [plans and specifications](superpowers/README.md) retain design rationale and
  original checklists. Unchecked historical steps are not an instruction to recreate
  existing features, replay migrations, commit, publish, or deploy.

## Reading paths

| Purpose | Documents |
| --- | --- |
| Understand the product | [Vision](00-foundation/01-project-vision.md), [platform](01-architecture/01-platform-overview.md), [V1 scope](09-roadmap/v1.md) |
| Develop and verify | [Local development](06-development/local-development.md), [testing](06-development/testing.md), [contributing](../CONTRIBUTING.md) |
| Integrate | [REST APIs](04-api/rest-api.md), [authentication](04-api/authentication.md), [pagination](04-api/pagination.md) |
| Operate and qualify | [Deployment](05-infrastructure/deployment.md), [pilot runbook](05-infrastructure/pilot-acceptance-runbook.md), [release gate](06-development/release-readiness.md) |
| Resume work | [Execution backlog](09-roadmap/current-execution.md), [plan index](superpowers/README.md), [open questions](OPEN-QUESTIONS.md) |

The local [documentation tracker](../docs-site/index.html) is a navigation aid.
Its category badges are document roles, not proof of implementation or release readiness.

The [unified calendar contract](02-platform/unified-calendar.md) covers the browser workspace, source forms, privacy, routes, and operational tooling.
