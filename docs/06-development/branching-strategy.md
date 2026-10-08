# Branching Strategy

**Status:** Accepted V1 starting direction

Use protected `main` and short-lived focused branches. Changes are reviewed, tested, and merged through traceable pull requests under [ADR-0034](../decisions/ADR-0034-github-reviewed-artifact-delivery.md). Long-lived environment branches and unreviewed direct production changes are avoided. Required checks, current-branch or merge-queue enforcement, stale-review dismissal, and force-push/deletion denial are repository rules, not conventions.

Release tags are immutable and map to migrations, schemas, documentation, release notes, and the already-produced API/frontend OCI digests. Tags never cause a rebuild and are not substitutes for digest identity.
