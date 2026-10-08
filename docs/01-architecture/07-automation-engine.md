# Automation Engine

**Status:** Implemented source contract; runtime composition pending

V1 automation changes Rarity data and calls approved external HTTP APIs. It cannot execute arbitrary code, access the database directly, read host files, open remote sessions, or bypass application permissions and workflow rules.

Automations are immutable published versions composed of triggers, nested conditions, typed actions, branches, waits, retry/failure paths, and completion. Credentials live in encrypted Connection objects, never workflow definitions.

Every run records trigger event, automation version, input snapshot, sanitized steps, duration, retries, outcome, errors, and changed objects. Causation IDs, depth limits, idempotency, rate limits, and re-entry policy prevent loops. Failures enter a dead-letter queue with authorized inspect, retry, replay, and dismiss operations.

The implemented source validates nested typed definitions to depth eight, rejects inline credentials and unknown actions, preserves immutable published versions, and executes through a Client/capability-scoped automation principal. Conditions, waits, connection authorization, idempotent replay, sanitized step history, bounded retry, and reasoned dead-letter actions have portable tests.

See [Integration, Automation, and AI Contracts](../04-api/integration-automation-ai-contracts.md).

## Governed classification automation

Classification mutations use typed add/remove-tag actions and require the
automation principal to hold `classification.apply` in the target Client.
Definitions may trigger on the canonical `tag.added` and `tag.removed` events
and evaluate effective tag IDs. Action input contains immutable tag IDs, never
free-form labels. The ordinary association service still resolves merges,
rejects archived tags, and prevents removal of the final meaningful effective
tag.

Classification events carry idempotency, correlation, causation, and depth
metadata. Re-entry is bounded by the published automation's maximum depth and
the platform maximum; an idempotency key makes replay safe. These controls are
mandatory for tag-triggered chains because a tag mutation can otherwise
re-trigger the same definition. Automated intake may deliberately assign the
protected `Unclassified` fallback, but may not create taxonomy entries.
