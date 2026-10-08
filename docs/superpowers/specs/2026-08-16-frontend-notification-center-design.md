# Frontend Notification Center Design

**Status:** Approved for implementation on 2026-08-16.

## Outcome

Expose the durable principal-owned notification inbox in the authenticated application shell. A technician can see the unread count, open a notification drawer without leaving current work, page through notifications, explicitly mark an unread item read, and follow its safe internal action.

The notification center consumes the existing backend contract only:

- `GET /api/v1/notifications?limit=<n>&cursor=<cursor>`;
- `GET /api/v1/notifications/unread-count`;
- `PATCH /api/v1/notifications/{id}/read` with `expected_version`.

No backend schema, routing, delivery, or policy behavior changes in this slice.

## Approaches Considered

### Top-bar drawer — selected

A notification bell in the authenticated top bar opens a right-aligned modal drawer. This keeps the current workspace visible, has enough room for notification bodies and pagination, and supports a complete keyboard/focus contract.

### Compact popover

A popover would be lighter, but it is too constrained for long authorized notification copy, resilient loading/error states, and cursor pagination. It also creates more fragile responsive positioning.

### Dedicated notification route

A full page would scale to richer filtering later, but it would add navigation, route authorization, and workspace-history behavior that the current list/read contract does not need. It would also pull technicians away from the record they are handling.

## Shell and Component Boundaries

Create a focused `features/notifications` module:

- `types.ts` owns the browser notification and page shapes.
- `api.ts` validates server JSON, performs same-origin requests, includes CSRF on the read mutation, and translates non-success responses into the existing browser request error convention.
- `NotificationCenter.tsx` owns unread-count loading, drawer state, cursor pagination, mark-read convergence, focus restoration, and rendering.
- `notification-center.css` owns drawer, badge, list, responsive, and state styling.

The authenticated `App` composition mounts one `NotificationCenter` in the `AppShell` top-bar content. This keeps the generic design-system shell free of network behavior. The component is principal-bound through React identity: `principalID` changes remount/reset the notification state, and unauthenticated application states never mount it.

The top-bar button uses the existing Lucide icon dependency, has an accessible name that includes the unread count, exposes `aria-expanded` and `aria-controls`, and renders a capped visible badge (`99+`) while retaining the exact count in accessible copy.

## Data Flow

On mount, request only the unread count. Opening the drawer starts the first list request and refreshes the unread count in parallel. Independent requests must not form a waterfall.

The first list page replaces prior drawer content. `Load more` sends the opaque `next_cursor` and appends returned items. A request generation guard prevents a late response from a prior principal, prior open cycle, or superseded first-page retry from repainting current state.

Mark-read is explicit. Selecting `Mark read` sends the item’s current `version`. Success replaces that exact item with the authoritative response and refreshes unread count. A conflict or other failure leaves the item unread, retains it in place, and presents a retryable inline error instead of decrementing browser state speculatively.

Selecting `Open` first marks an unread item read. Navigation occurs only after the authoritative read succeeds; already-read items navigate immediately. This avoids losing the read transition during navigation. A dedicated sanitizer accepts only bounded `#/...` application hashes or root-relative paths with no origin, credentials, protocol-relative prefix, query, or fragment confusion. Accepted root-relative paths remain ordinary anchors (including the current backend `/calendar` contract); accepted hashes use the application hash boundary. The notification center does not translate a calendar action into an unrelated route. Invalid or external values render no action.

The center performs no timer polling. It refreshes unread state on initial mount and each open, which keeps the slice bounded and avoids background traffic without a push/event contract.

## Interaction and Accessibility

The drawer is a labelled modal dialog with a backdrop. Opening moves focus to the drawer heading or first actionable item. `Escape`, the close button, and the backdrop close it and restore focus to the bell. Focus is trapped within the open drawer using the repository’s existing dialog pattern.

Notifications are rendered newest-first as returned by the server. Each item exposes title, body, relative timestamp, unread/read state, optional `Open`, and `Mark read` only while unread. Content classification is not displayed as user-facing copy but remains available on the typed item for future policy-sensitive presentation.

States are explicit:

- initial drawer loading;
- empty inbox;
- first-page failure with retry;
- pagination failure that retains loaded items and offers `Try again`;
- mark-read failure scoped to the affected item;
- loading-more progress that prevents duplicate pagination requests.

At mobile widths the drawer occupies the viewport width below no separate route chrome. On desktop it remains a bounded right rail and does not resize the underlying workspace.

## Mentions Boundary

Internal Mentions remain exclusively in the existing Home widget and mention preference flow. The notification center renders only `recipient_notifications`; it does not call mention endpoints, combine mention counts, create a Mentions link, or manufacture duplicate mention rows.

## Testing

API tests cover complete response normalization, cursor encoding, unread count, CSRF/versioned mark-read requests, malformed payload rejection, and non-success responses.

Component tests cover:

- unread badge and accessible count;
- open/close/focus restoration and Escape;
- parallel open refresh and first-page rendering;
- empty, first-page error, and retry states;
- cursor pagination, append behavior, duplicate-load prevention, and retained pagination errors;
- authoritative mark-read replacement and count refresh;
- stale mutation/list response rejection after item, open-cycle, or principal changes;
- safe internal navigation and rejection of external or malformed actions;
- no Mentions endpoint calls or duplicate Mentions affordance;
- detectable WCAG A/AA violations for the open populated drawer.

The focused tests run red before production code. Completion requires the full frontend suite, TypeScript/Vite production build, and rendered desktop/mobile interaction validation with no relevant console errors.

## Acceptance Criteria

- Every authenticated shell exposes one notification bell.
- The bell presents the exact unread count accessibly and a capped visible badge.
- Opening the drawer preserves the active workspace and loads principal-owned notifications.
- Pagination consumes the backend cursor opaquely and never duplicates an in-flight page request.
- Read mutations carry the exact current version and update only from authoritative responses.
- Unsafe action paths are never navigable.
- Drawer focus is contained while open and returns to the bell on close.
- Mentions remain separate and unduplicated.
- Loading, empty, failure, retry, and responsive states are tested and usable.
