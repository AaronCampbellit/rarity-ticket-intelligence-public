# Task 8 report — Internal Knowledge publication tool and reason contract

## RED

Command:

```bash
go test ./backend/internal/knowledge ./backend/internal/httpapi ./backend/internal/aiassist/rtitools -run 'Knowledge.*Publish|Publication.*Reason' -count=1
npm --prefix frontend test -- --run frontend/src/features/knowledge/KnowledgePage.test.tsx
```

Observed output:

```text
backend/internal/knowledge/service_test.go:285:39: unknown field Reason in struct literal of type PublishCommand
backend/internal/aiassist/rtitools/knowledge_publish_tool_test.go:83:10: undefined: NewKnowledgePublishTool
backend/internal/aiassist/rtitools/knowledge_publish_tool_test.go:117:63: actions.published.ExpectedClientVersion undefined
backend/internal/httpapi/psa_routes_test.go:2077:21: actions.published.Reason undefined
FAIL github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge [build failed]
FAIL github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi [build failed]
FAIL github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools [build failed]

No test files found, exiting with code 1
filter: frontend/src/features/knowledge/KnowledgePage.test.tsx
```

The Go failures are the intended missing reason/tool contract. The frontend
path is repository-relative, not relative to the `frontend` package root.

## GREEN

Focused command:

```bash
go test ./backend/internal/knowledge ./backend/internal/store/psa ./backend/internal/httpapi ./backend/internal/aiassist/rtitools -run 'Knowledge.*Publish|Publication.*Reason' -count=1
npm --prefix frontend test -- --run src/features/knowledge/KnowledgePage.test.tsx
```

Output:

```text
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools
Test Files  1 passed (1)
Tests  2 passed (2)
```

Affected-package command:

```bash
go test ./backend/internal/knowledge ./backend/internal/store/psa ./backend/internal/httpapi ./backend/internal/aiassist/rtitools -run Knowledge -count=1
npm --prefix frontend test -- --run src/features/knowledge/KnowledgePage.test.tsx
```

Output:

```text
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools
Test Files  1 passed (1)
Tests  2 passed (2)
```

Final regression command:

```bash
go test ./backend/internal/knowledge ./backend/internal/store/psa ./backend/internal/httpapi ./backend/internal/aiassist/rtitools -count=1
npm --prefix frontend test -- --run src/features/knowledge/KnowledgePage.test.tsx
git diff --check
```

Output:

```text
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/knowledge
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi
ok github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist/rtitools
Test Files  1 passed (1)
Tests  2 passed (2)
```

`git diff --check` emitted no output.

## Delivered behavior

- Ordinary publication rejects a blank reason before repository access and
  stores the trimmed reason on the `knowledge.published` audit fact.
- HTTP and the existing Knowledge UI require and submit that reason while
  retaining the draft-state and publish-capability gates.
- `knowledge.publish` has a closed public schema and resolves an active Client
  plus one exact internal non-empty draft. Its preview shows `draft` to
  `published`, the unchanged version, reason, title, and `internal only; no
  external delivery`.
- Confirmation repeats capability, Client activity/version, Article identity,
  draft/body/internal visibility, and version checks before ordinary publish.
  The database transaction locks the active Client at the prepared revision
  before Article/version/audit/outbox writes.

## Files

- `backend/internal/knowledge/service.go` and tests
- `backend/internal/store/psa/knowledge_repository.go` and tests
- `backend/internal/httpapi/knowledge_routes.go` and HTTP test
- `backend/internal/aiassist/rtitools/knowledge_publish_tool.go` and tests
- `frontend/src/features/knowledge/KnowledgePage.tsx` and test

## Self-review

- Strict decoding rejects client-visible, recipient, delivery, internal-ID,
  and unknown-field attempts.
- Empty, published, ambiguous, client-visible, stale, and inactive Client
  cases produce no publication call. The write transaction fences client
  revision before facts are written.
- The adapter has no external delivery, recipient, client-visible, or catalog
  composition capability.

## Concerns

- No live deployment or browser acceptance was performed; this is local
  source/test verification only.
