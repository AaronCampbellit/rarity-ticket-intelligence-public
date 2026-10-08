# Testing

**Status:** Implemented test layers; deployed and manual qualification remain separate

See [local development](local-development.md) for exact commands and prerequisites.
Portable tests skip database-dependent cases without `TEST_DATABASE_URL`. The
acceptance runner uses an isolated database per test-file group. Browser tests
include both HTTP-fixture journeys and a production-backed classification harness.
CI additionally runs race detection, static analysis, dependency and image scans.
Keep these evidence types distinct when reporting results.

The test strategy includes unit, domain contract, integration, API compatibility, authorization matrix, client-isolation, event/idempotency, workflow, automation, browser, accessibility, performance, migration, backup/restore, failover, and security tests.

Critical invariants receive negative tests. Production-like recovery and HA exercises are scheduled. Flaky tests are defects, not permanently ignored gates.
