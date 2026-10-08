# Engineering Principles

**Status:** Draft

- Put domain invariants in shared application services used by UI, APIs, automation, and background jobs.
- Default deny at every trust boundary and authorize against object, action, client, and context.
- Prefer explicit schemas, typed actions, deterministic evaluation, idempotency, and immutable published versions.
- Design failure modes, retries, timeouts, dead-letter handling, and recovery before shipping.
- Preserve compatibility through versioned APIs, events, workflows, and migrations.
- Keep secrets out of source, logs, events, URLs, and workflow definitions.
- Test isolation, authorization, recovery, concurrency, performance, and accessibility—not only happy paths.
- Make operational health and decision explanations visible to authorized administrators.
- Record durable choices as ADRs and keep documentation synchronized with behavior.
