# Task 7: Strict AI HTTP API

Implemented the `/api/v1` AI management and durable-job HTTP boundary without
runtime composition changes. Management routes require `ai.manage` at MSP
scope; generation and job routes require `ai.assist` at the active Client.

- Provider credentials are accepted only on create/replace, copied to bytes,
  cleared after use where possible, and omitted from every response DTO.
- AI JSON requests are single-object, unknown-field and trailing-value strict,
  JSON content-type checked, and capped at 1 MiB with stable validation errors.
- Versioned provider, policy, and job responses include ETags. Body and
  `If-Match` versions must agree; reasoned mutations require a reason.
- Job submission supplies only the feature and server-derived Work Record
  identity, requires `Idempotency-Key`, and returns `202`, `Location`, and
  `Retry-After`.
- Recommendation decisions remain human-only and always serialize
  `applied:false` and `sent:false`.

Verification:

```text
go test ./backend/internal/httpapi -count=1
go test ./backend/... -run '^$'
git diff --check
```

Task 8 remains responsible for composing the management and job services into
the API process.
