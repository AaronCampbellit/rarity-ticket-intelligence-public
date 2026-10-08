# Frontend Notification Center Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an accessible authenticated top-bar notification drawer that consumes the existing principal-owned inbox APIs without duplicating Mentions.

**Architecture:** A focused `features/notifications` module owns strict response parsing, action sanitization, and the drawer state machine. The authenticated `App` mounts the feature inside existing `AppShell` top-bar content, while the shared `Dialog` supplies modal focus containment and restoration. Initial count and open-cycle list/count requests run independently; cursor and mutation responses are applied only when they still belong to the active request generation.

**Tech Stack:** React 19, TypeScript 7, Vitest, Testing Library, axe-core, Lucide React, Vite 8.

## Global Constraints

- Consume only `GET /api/v1/notifications`, `GET /api/v1/notifications/unread-count`, and `PATCH /api/v1/notifications/{id}/read`.
- Do not change backend schema, routes, notification planning, delivery, or policy behavior.
- Do not call Mentions endpoints or add a Mentions affordance.
- Mark read only from the authoritative PATCH response; never decrement optimistically.
- No timer polling; refresh on mount and drawer open.
- Accept only bounded `#/...` hashes or unambiguous root-relative action paths.
- Reuse the existing design-system `Dialog` with `variant="drawer"` for focus trap, Escape, backdrop, and restoration.
- Run every production behavior through a witnessed RED/GREEN TDD cycle.

---

## File Structure

- `frontend/src/features/notifications/types.ts`: browser inbox types and injectable API interface.
- `frontend/src/features/notifications/api.ts`: strict JSON validation and same-origin HTTP requests.
- `frontend/src/features/notifications/action.ts`: bounded internal action sanitizer and default navigator.
- `frontend/src/features/notifications/api.test.ts`: API and sanitizer boundary tests.
- `frontend/src/features/notifications/NotificationCenter.tsx`: unread badge, drawer, pagination, read convergence, and stale-response guards.
- `frontend/src/features/notifications/NotificationCenter.test.tsx`: user-visible behavior, focus, error, race, action, and accessibility tests.
- `frontend/src/features/notifications/notification-center.css`: top-bar trigger, badge, drawer, item, state, and responsive styling.
- `frontend/src/App.tsx`: authenticated shell composition only.
- `frontend/src/App.test.tsx`: authenticated integration and absence from unauthenticated states.

---

### Task 1: Typed inbox API and safe actions

**Files:**
- Create: `frontend/src/features/notifications/types.ts`
- Create: `frontend/src/features/notifications/api.ts`
- Create: `frontend/src/features/notifications/action.ts`
- Create: `frontend/src/features/notifications/api.test.ts`

**Interfaces:**
- Produces: `NotificationItem`, `NotificationPage`, `NotificationCenterAPI`, `createNotificationCenterAPI(fetcher?)`, `notificationCenterAPI`, `safeNotificationAction(value)`, and `navigateNotificationAction(href)`.
- Consumes: `csrfHeaders()` from `frontend/src/api/browserSession.ts`.

- [ ] **Step 1: Write failing API and action tests**

Create literal fixtures with all deployed fields and prove normalization, opaque cursor encoding, unread count, exact mutation body/CSRF, malformed payload rejection, response error codes, and action sanitization:

```tsx
const item = {
  id: "00000000-0000-4000-8000-000000000101",
  title: "Calendar schedule changed",
  body: "schedule: Northwind: On-site visit",
  action_path: "/calendar",
  content_classification: "internal",
  created_at: "2026-08-16T12:00:00Z",
  version: 1,
};

it("loads a validated page with an opaque cursor", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json({
    notifications: [item],
    next_cursor: "cursor + / =",
  }));
  const api = createNotificationCenterAPI(fetcher);
  await expect(api.list("prior + / =", 25)).resolves.toEqual({
    notifications: [{
      id: item.id,
      title: item.title,
      body: item.body,
      actionPath: "/calendar",
      contentClassification: "internal",
      createdAt: "2026-08-16T12:00:00Z",
      version: 1,
    }],
    nextCursor: "cursor + / =",
  });
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/notifications?limit=25&cursor=prior+%2B+%2F+%3D",
    expect.objectContaining({ credentials: "same-origin" }),
  );
});

it("accepts only bounded internal actions", () => {
  expect(safeNotificationAction("/calendar")).toBe("/calendar");
  expect(safeNotificationAction("#/work?workRecordID=record-1")).toBe("#/work?workRecordID=record-1");
  expect(safeNotificationAction("https://attacker.example/calendar")).toBeUndefined();
  expect(safeNotificationAction("//attacker.example/calendar")).toBeUndefined();
  expect(safeNotificationAction("/calendar?token=secret")).toBeUndefined();
});
```

The mark-read test sets `document.cookie = "rarity_csrf=csrf-token"` and requires `PATCH`, `credentials: "same-origin"`, `Content-Type: application/json`, `X-Rarity-CSRF: csrf-token`, and body `{"expected_version":3}`.

- [ ] **Step 2: Run Task 1 tests and verify RED**

Run: `npm test -- --run src/features/notifications/api.test.ts`

Expected: FAIL because the notification modules do not exist.

- [ ] **Step 3: Implement strict types, parser, requests, and action boundary**

Use these public types:

```ts
export type NotificationItem = {
  id: string;
  title: string;
  body: string;
  actionPath: string;
  contentClassification: string;
  createdAt: string;
  readAt?: string;
  version: number;
};

export type NotificationPage = {
  notifications: NotificationItem[];
  nextCursor?: string;
};

export type NotificationCenterAPI = {
  list(cursor?: string, limit?: number, signal?: AbortSignal): Promise<NotificationPage>;
  unreadCount(signal?: AbortSignal): Promise<number>;
  markRead(id: string, expectedVersion: number, signal?: AbortSignal): Promise<NotificationItem>;
};
```

`api.ts` must reject non-record payloads, blank required strings, invalid timestamps, non-safe integers, versions below one, non-array notifications, and invalid optional strings with `NotificationAPIError("invalid_response", 502)`. Build list queries with `URLSearchParams` and default limit `25`. Parse backend error envelopes into `NotificationAPIError(code, status, message)`.

`safeNotificationAction` trims nothing: the value must already equal `value.trim()`, contain no control character, and be at most 512 characters. Accept `#/` hashes directly. For paths, require one leading slash, reject `//`, parse against `window.location.origin`, and require the same origin with empty search and hash before returning `url.pathname`.

- [ ] **Step 4: Run Task 1 tests and verify GREEN**

Run: `npm test -- --run src/features/notifications/api.test.ts`

Expected: PASS with no console errors.

- [ ] **Step 5: Commit Task 1**

```bash
git add frontend/src/features/notifications/types.ts frontend/src/features/notifications/api.ts frontend/src/features/notifications/action.ts frontend/src/features/notifications/api.test.ts
git commit -m "feat: add notification center client contract"
```

---

### Task 2: Accessible drawer state machine

**Files:**
- Create: `frontend/src/features/notifications/NotificationCenter.tsx`
- Create: `frontend/src/features/notifications/NotificationCenter.test.tsx`
- Create: `frontend/src/features/notifications/notification-center.css`
- Modify: `frontend/src/features/notifications/types.ts`

**Interfaces:**
- Consumes: `NotificationCenterAPI`, `notificationCenterAPI`, `safeNotificationAction`, `navigateNotificationAction`, and design-system `Dialog`.
- Produces: `NotificationCenter({ api?, onNavigate? })`.

- [ ] **Step 1: Write failing badge, drawer, and focus tests**

Use a real `NotificationCenter` with a small stateful API double that returns complete `NotificationItem` objects. Name the production breaks: count not loaded, drawer not labelled, focus not restored, or list request not started on open.

```tsx
it("shows the unread count and opens a focus-contained drawer", async () => {
  const api = notificationAPI({ unread: 2, page: page([unreadItem, readItem]) });
  render(<NotificationCenter api={api} />);
  const bell = await screen.findByRole("button", { name: "Notifications, 2 unread" });
  expect(bell).toHaveTextContent("2");
  await userEvent.click(bell);
  expect(await screen.findByRole("dialog", { name: "Notifications" })).toBeVisible();
  expect(screen.getByRole("heading", { name: "Notifications" })).toBeVisible();
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
  expect(bell).toHaveFocus();
});
```

Also require a visible `99+` badge but accessible exact count for `104`, parallel list/count calls on open, newest returned order, and zero Mentions text/requests.

- [ ] **Step 2: Run focused component tests and verify RED**

Run: `npm test -- --run src/features/notifications/NotificationCenter.test.tsx`

Expected: FAIL because `NotificationCenter` does not exist.

- [ ] **Step 3: Implement the minimal badge, dialog, first page, and CSS**

Use `Bell` and `X` from `lucide-react`. Render the close control inside `Dialog` actions so the shared dialog owns focus trapping and restoration. Load unread count on mount with an `AbortController`. On open, run `Promise.allSettled([loadFirstPage(), refreshUnread()])` by starting both promises before awaiting either; show list and count failures independently.

Keep these independent states rather than one combined request object:

```ts
const [open, setOpen] = useState(false);
const [unreadCount, setUnreadCount] = useState(0);
const [items, setItems] = useState<NotificationItem[]>([]);
const [nextCursor, setNextCursor] = useState<string>();
const [listState, setListState] = useState<"idle" | "loading" | "ready" | "error">("idle");
const [loadingMore, setLoadingMore] = useState(false);
const [pageError, setPageError] = useState(false);
const [itemErrors, setItemErrors] = useState<Record<string, string>>({});
```

Style the dialog through `.notification-center` descendants plus the existing `[data-variant="drawer"]` geometry. Preserve a 44px minimum trigger target, high-contrast unread marker, readable wrapping, a maximum desktop width of `28rem`, and full viewport width below `520px`.

- [ ] **Step 4: Run badge/drawer tests and verify GREEN**

Run: `npm test -- --run src/features/notifications/NotificationCenter.test.tsx -t 'unread|drawer|focus|parallel|99'`

Expected: PASS.

- [ ] **Step 5: Write failing pagination, mutation, race, action, and accessibility tests**

Add behavior tests that would fail for each realistic mutation:

- two rapid `Load more` activations produce one request and append one page;
- a failed page retains current items and retries the same cursor;
- `Mark read` sends the displayed version, replaces only from the response, and refreshes count;
- a failed/conflicted mutation leaves the item unread and shows item-scoped retry copy;
- `Open` waits for unread PATCH success before invoking `onNavigate("/calendar")`;
- invalid external action renders no `Open` control;
- a late first-page response from a closed/reopened cycle cannot repaint the newer cycle;
- a late mutation response cannot replace an item that a newer first-page reload superseded;
- the populated open drawer returns `[]` from `findA11yViolations`.

- [ ] **Step 6: Run expanded component tests and verify RED**

Run: `npm test -- --run src/features/notifications/NotificationCenter.test.tsx`

Expected: FAIL on pagination, convergence, or request-generation assertions because only the first-page drawer exists.

- [ ] **Step 7: Implement pagination, mutation convergence, stale guards, and actions**

Use monotonically increasing refs:

```ts
const listGeneration = useRef(0);
const itemGenerations = useRef(new Map<string, number>());
const loadingCursor = useRef<string>();
```

Increment `listGeneration` before every first-page load and on close. Apply a first-page result only if its captured generation still matches. Before applying a mark-read result, require both the item generation and the list generation captured at mutation start to match current values. Store the active pagination cursor in `loadingCursor`; return early if the same cursor is already in flight and clear it in `finally` only when still equal.

Deduplicate appended notifications by ID while preserving first occurrence order. On authoritative read success, replace the exact ID and then call `refreshUnread`. `Open` calls the same mutation path for unread items and invokes `onNavigate` only with the sanitized href after success.

- [ ] **Step 8: Run all Task 2 tests and verify GREEN**

Run: `npm test -- --run src/features/notifications/NotificationCenter.test.tsx src/features/notifications/api.test.ts`

Expected: PASS with no relevant warnings or unhandled promise rejections.

- [ ] **Step 9: Format and commit Task 2**

Run: `npx prettier --write src/features/notifications/*.ts src/features/notifications/*.tsx src/features/notifications/*.css`

```bash
git add frontend/src/features/notifications
git commit -m "feat: add accessible notification drawer"
```

---

### Task 3: Authenticated shell integration and rendered verification

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/app.css` only if existing top-bar layout requires a one-line responsive containment rule.

**Interfaces:**
- Consumes: `NotificationCenter` and the authenticated `principal.id`.
- Produces: exactly one notification center inside authenticated top-bar content and none in loading, login, setup, or break-glass shells.

- [ ] **Step 1: Write failing authenticated integration tests**

Extend the existing fetch fixture with complete notification responses and prove the real authenticated app exposes one bell while unauthenticated application states expose none:

```tsx
expect(await screen.findByRole("button", { name: "Notifications, 1 unread" })).toBeVisible();
expect(screen.getAllByRole("button", { name: /Notifications,/ })).toHaveLength(1);
```

The integration test opens the real drawer, marks its calendar item read, and checks the PATCH request body contains the item version. Keep the action path inert in this App-level test so it does not leave jsdom.

- [ ] **Step 2: Run App integration tests and verify RED**

Run: `npm test -- --run src/App.test.tsx src/AppBoundary.test.tsx -t 'notification|authenticated application boundary'`

Expected: FAIL because authenticated `App` does not mount `NotificationCenter`.

- [ ] **Step 3: Mount the notification center in authenticated top-bar content**

Import directly from `./features/notifications/NotificationCenter` and place:

```tsx
<NotificationCenter key={principal.id} />
```

before the active-client selector inside the existing `topBar` fragment. Do not add a route, navigation manifest entry, capability gate, or Mentions integration.

- [ ] **Step 4: Run App integration tests and verify GREEN**

Run: `npm test -- --run src/App.test.tsx src/AppBoundary.test.tsx -t 'notification|authenticated application boundary'`

Expected: PASS.

- [ ] **Step 5: Run focused and full automated verification**

Run:

```bash
npm test -- --run src/features/notifications/api.test.ts src/features/notifications/NotificationCenter.test.tsx src/App.test.tsx src/AppBoundary.test.tsx --maxWorkers=2
npm test -- --run --maxWorkers=2
npm run build
```

Expected: focused tests pass; all frontend test files pass; TypeScript and Vite production build exit zero. The existing jsdom canvas and Vite chunk-size warnings may remain but no notification-center warning is accepted.

- [ ] **Step 6: Validate the rendered flow with Playwright fallback**

Browser plugin is not available in this session, so use the repository’s installed Playwright. Start the authenticated local/Compose surface that already supplies session fixtures; do not install another browser dependency. Validate this target flow:

`authenticated shell -> bell with unread badge -> open populated drawer -> mark item read -> close with Escape -> focus returns to bell`.

At desktop `1440x900` and mobile `390x844`, verify page identity, nonblank content, no framework overlay, no relevant console errors, drawer containment, readable item wrapping, interaction state, and screenshot evidence. Save screenshots outside the repository.

- [ ] **Step 7: Commit Task 3**

```bash
git add frontend/src/App.tsx frontend/src/App.test.tsx frontend/src/app.css
git commit -m "feat: expose notification center in app shell"
```

- [ ] **Step 8: Final diff and status gate**

Run:

```bash
git diff --check main...HEAD
git status --short
git log --oneline --decorate main..HEAD
```

Expected: no whitespace errors, clean worktree, and only the design, plan, notification module, and authenticated shell integration commits.
