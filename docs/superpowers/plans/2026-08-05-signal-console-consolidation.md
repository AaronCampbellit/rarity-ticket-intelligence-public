# Signal Console Consolidation Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Signal as Rarity's single production shell with record-only tabs, retained page history, useful previews, top-bar AI, optional Kanban views, and no Atlas or synthetic-demo artifacts.

**Architecture:** Separate route history from record workspace state instead of representing both as tabs. Keep route surfaces mounted in a bounded in-memory stage, restrict persisted workspace items to authorized ticket/task/project records, and have feature pages use one preview/direct-open contract. Simplify presentation to Signal plus density, then delete every obsolete direction and demo branch after manifest-driven tests prove the migration.

**Tech Stack:** React 19, TypeScript 5, Vite 8, Vitest 4, Testing Library, axe-core, Lucide React, CSS design tokens, Docker Compose.

## Global Constraints

- Signal is the only shipped product direction.
- Top tabs contain only individual tickets, tasks, and projects.
- Normal product pages preserve useful session history without visible route tabs.
- Ask Rarity sits beside Create and never interrupts active work.
- Work, Sales, and Projects offer consistent optional Kanban views.
- No demo workspace code, build flag, fake tenant data, Atlas implementation, or user-facing raw JSON remains.
- Existing authorization, client isolation, dirty-state protection, responsive navigation, and WCAG A/AA behavior must not regress.
- New behavior is implemented test-first. Each red test must fail for the missing behavior before production code changes.
- Do not use a browser for visual QA until the user explicitly selects one.

---

### Task 1: Restrict workspace state to record tabs

**Files:**
- Modify: `frontend/src/design-system/workspace/types.ts`
- Modify: `frontend/src/design-system/workspace/useWorkspace.tsx`
- Modify: `frontend/src/design-system/workspace/useWorkspace.test.ts`
- Modify: `frontend/src/design-system/workspace/WorkspaceTabs.tsx`

**Interfaces:**
- Consumes: `RouteID`, authorized client IDs, and the existing dirty-source reducer contract.
- Produces:

```ts
export type RecordEntity = "ticket" | "task" | "project";

export type WorkspaceItem = {
  id: string;
  routeID: RouteID;
  recordID: string;
  parentRecordID?: string;
  clientID: string;
  label: string;
  entityType: RecordEntity;
  openedAt: number;
  dirty?: boolean;
  preview?: {
    title: string;
    summary?: string;
    status?: string;
  };
};

openRecord: (item: WorkspaceItem) => void;
```

- `openRecord` deduplicates the record tab, activates it, and clears the matching preview.
- Workspace persistence schema becomes version 4 and rejects route-level or version-3 entries.

- [x] **Step 1: Write failing reducer and persistence tests**

Add tests that name the breaks:

```ts
it("opens only supported record entities and clears their preview", () => {
  const previewed = workspaceReducer(
    initialWorkspaceState,
    workspaceActions.openPreview(ticket),
  );
  expect(
    workspaceReducer(previewed, workspaceActions.openRecord(ticket)),
  ).toEqual({
    tabs: [ticket],
    activeID: ticket.id,
  });
});

it("rejects old route-tab storage instead of restoring it", () => {
  sessionStorage.setItem(
    workspaceStorageKey("principal-1"),
    JSON.stringify({
      version: 3,
      tabs: [{ id: "route:sales", routeID: "sales", kind: "route" }],
    }),
  );
  expect(
    restoreWorkspace("principal-1", new Set(["sales"]), new Set(["client-1"])),
  ).toEqual(initialWorkspaceState);
});

it("serializes entity type and authorized parent context", () => {
  expect(
    JSON.parse(
      serializeWorkspace(
        { tabs: [task], activeID: task.id },
        new Set(["client-1"]),
      ),
    ),
  ).toEqual({
    version: 4,
    tabs: [
      expect.objectContaining({
        entityType: "task",
        parentRecordID: "project-1",
      }),
    ],
    activeID: task.id,
  });
});
```

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace/useWorkspace.test.ts
```

Expected: FAIL because `entityType`, `openRecord`, and schema version 4 do not exist.

- [x] **Step 3: Implement the record-only model**

Replace `kind`, `pinned`, route-item serialization, `toggle-pin`, and
`promote-preview`. Add:

```ts
case "open-record":
  return {
    ...state,
    tabs: uniqueTab(state.tabs, action.item),
    activeID: action.item.id,
    preview: undefined,
  };
```

Validate restored items with an explicit entity set:

```ts
const recordEntities = new Set<RecordEntity>(["ticket", "task", "project"]);
```

Only accept items whose route remains authorized, client remains authorized,
record ID is non-empty and at most 128 characters, and entity type is supported.

- [x] **Step 4: Update record tabs**

Remove Pin rendering and direction-dependent tab modes. Keep activation,
keyboard reordering, dirty indication, and close behavior. Render no navigation
fallback based on route tabs; closing the active record calls its owning route.

- [x] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace
```

Expected: all workspace tests pass.

- [x] **Step 6: Commit**

```bash
git add frontend/src/design-system/workspace
git commit -m "Restrict workspace tabs to records"
```

---

### Task 2: Preserve page history without visible route tabs

**Files:**
- Create: `frontend/src/design-system/workspace/PageHistoryStage.tsx`
- Create: `frontend/src/design-system/workspace/PageHistoryStage.test.tsx`
- Modify: `frontend/src/design-system/workspace/index.ts`
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.test.tsx`

**Interfaces:**
- Consumes: active `RouteID`, `PageFamily`, allowed route IDs, route dirty
  sources, and the active route React node.
- Produces:

```ts
export function PageHistoryStage(props: {
  activeID: RouteID;
  pageFamily: PageFamily;
  allowedRouteIDs: ReadonlySet<RouteID>;
  dirtyRouteIDs: ReadonlySet<string>;
  children: ReactNode;
  limit?: number;
}): ReactNode;
```

- The stage retains up to eight clean, least-recently-used route surfaces.
- Dirty route owners use IDs in the existing `route:${routeID}` format and
  cannot be evicted.

- [x] **Step 1: Write failing page-history tests**

```tsx
it("retains route state without creating route tabs", () => {
  const view = render(<HistoryHarness route="sales" />);
  fireEvent.change(screen.getByLabelText("Opportunity note"), {
    target: { value: "Call client tomorrow" },
  });
  view.rerender(<HistoryHarness route="work" />);
  view.rerender(<HistoryHarness route="sales" />);

  expect(screen.getByLabelText("Opportunity note")).toHaveValue(
    "Call client tomorrow",
  );
  expect(screen.queryByRole("navigation", { name: "Open records" }))
    .not.toBeInTheDocument();
});

it("evicts the least-recent clean route after eight surfaces", () => {
  const view = render(<HistoryHarness route="home" limit={8} />);
  for (const route of nineRoutes.slice(1)) {
    view.rerender(<HistoryHarness route={route} limit={8} />);
  }
  expect(view.container.querySelector('[data-history-route="home"]'))
    .not.toBeInTheDocument();
});
```

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace/PageHistoryStage.test.tsx src/design-system/components/navigation/AppShell.test.tsx
```

Expected: FAIL because `PageHistoryStage` does not exist and AppShell still
creates one route tab per navigation event.

- [x] **Step 3: Implement bounded page history**

Use a ref-backed map:

```ts
type HistoryEntry = {
  page: ReactNode;
  family: PageFamily;
  lastActivated: number;
};
```

On activation, update the current entry. Remove unauthorized entries. When the
map exceeds `limit`, remove clean non-active entries ordered by
`lastActivated`. Render retained pages with stable keys and a unique main
content ID only for the active page.

- [x] **Step 4: Remove route-tab coupling from AppShell**

Delete the effect that dispatches `openTab({ id: "route:..." })`. Replace
`WorkspaceStage` with `PageHistoryStage`. Derive `dirtyRouteIDs` from
`state.dirtySources`, and pass the current navigation IDs as
`allowedRouteIDs`.

- [x] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace/PageHistoryStage.test.tsx src/design-system/components/navigation/AppShell.test.tsx src/App.test.tsx
```

Expected: route state survives navigation, record tabs remain absent until a
record opens, and all focused tests pass.

- [x] **Step 6: Commit**

```bash
git add frontend/src/design-system/workspace frontend/src/design-system/components/navigation/AppShell.tsx frontend/src/design-system/components/navigation/AppShell.test.tsx
git commit -m "Separate page history from record tabs"
```

---

### Task 3: Consolidate the shell on collapsible Signal navigation

**Files:**
- Modify: `frontend/src/design-system/components/navigation/DirectionShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/GroupedSidebar.tsx`
- Modify: `frontend/src/design-system/components/navigation/navigation.css`
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.test.tsx`
- Modify: `frontend/src/design-system/foundations/preferences.ts`
- Modify: `frontend/src/design-system/foundations/preferences.test.ts`
- Modify: `frontend/src/design-system/foundations/presentation.tsx`
- Modify: `frontend/src/design-system/components/navigation/PresentationMenu.tsx`
- Modify: `frontend/src/app.css`

**Interfaces:**
- Produces:

```ts
export type PresentationPreferences = {
  density: "adaptive" | "compact" | "comfortable";
};

export function navigationStorageKey(principalID: string): string;
```

- `GroupedSidebar` receives `principalID`, persists collapsed state, and renders
  `data-collapsed` without changing mobile overlay behavior.

- [x] **Step 1: Write failing Signal-only shell tests**

```tsx
it("renders no queue context, Atlas rail, or direction control", () => {
  const view = renderShell();
  expect(view.container.querySelector(".rti-signal-context")).toBeNull();
  expect(view.container.querySelector(".rti-atlas-rail")).toBeNull();
  expect(screen.queryByLabelText("Design direction")).not.toBeInTheDocument();
});

it("collapses grouped navigation and restores the preference per principal", () => {
  const view = renderShell();
  fireEvent.click(screen.getByRole("button", { name: "Collapse navigation" }));
  expect(view.container.querySelector(".rarity-sidebar"))
    .toHaveAttribute("data-collapsed", "true");
  expect(
    window.localStorage.getItem("rti:navigation:principal-1"),
  ).toBe('{"collapsed":true}');
});
```

Update preference tests to prove legacy direction values are ignored while a
valid density is retained.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/navigation/AppShell.test.tsx src/design-system/foundations/preferences.test.ts
```

Expected: FAIL because Signal still renders queue context, Atlas remains
selectable, and grouped navigation cannot collapse.

- [x] **Step 3: Simplify presentation preferences**

Remove `DesignDirection` and the direction field. Keep resilient density
storage. Change `DirectionPage` to always render `data-layout="console"` and
remove direction reads from page templates and workspace components.

- [x] **Step 4: Implement collapsible grouped navigation**

Move the compact icon behavior into `GroupedSidebar`. Use Lucide icons already
mapped by route. The collapse button must use `ChevronLeft` and `ChevronRight`,
set `aria-expanded`, and retain visible accessible names through link
`aria-label` and `title` attributes.

- [x] **Step 5: Remove queue-context and Atlas shell branches**

Make `DirectionShell` a Signal-only wrapper or inline it into AppShell. Delete
`AtlasRail`, `groupIcons`, queue-context markup, and their styles. Update the
desktop app grid to have only navigation plus workspace.

- [x] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/navigation src/design-system/foundations src/design-system/templates
```

Expected: navigation collapse, density, mobile overlay, focus restoration, and
accessibility tests pass.

- [x] **Step 7: Commit**

```bash
git add frontend/src/design-system frontend/src/app.css
git commit -m "Consolidate on the Signal shell"
```

---

### Task 4: Replace preview Pin with Open and move Ask Rarity to the top bar

**Files:**
- Modify: `frontend/src/design-system/workspace/PreviewPane.tsx`
- Modify: `frontend/src/design-system/workspace/AIRail.tsx`
- Modify: `frontend/src/design-system/workspace/AIRail.test.tsx`
- Modify: `frontend/src/design-system/workspace/workspace.css`
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.test.tsx`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- AppShell adds `primaryActions?: ReactNode` before AI and retains `topBar` for
  client/account actions.
- `PreviewPane` calls `openRecord(item)` and `onOpen(item)` from one Open action.

- [x] **Step 1: Write failing preview and AI placement tests**

```tsx
it("offers one Open action and no Pin action in a record preview", () => {
  renderRecordPreview();
  expect(screen.getByRole("button", {
    name: "Open INC-1048 in workspace",
  })).toBeVisible();
  expect(screen.queryByRole("button", {
    name: /Pin INC-1048/,
  })).not.toBeInTheDocument();
});

it("places Ask Rarity immediately after Create without changing the route", () => {
  renderShell({ primaryActions: <button>Create</button> });
  const actions = within(document.querySelector(".rti-command-bar")!)
    .getAllByRole("button");
  expect(actions.slice(0, 2).map((button) => button.textContent))
    .toEqual(["Create", "Ask Rarity"]);
  fireEvent.click(screen.getByRole("button", { name: "Ask Rarity" }));
  expect(screen.getByRole("complementary", { name: "Rarity AI" }))
    .toBeVisible();
  expect(screen.getByRole("heading", { name: "Work" })).toBeVisible();
});
```

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace/AIRail.test.tsx src/design-system/components/navigation/AppShell.test.tsx
```

Expected: FAIL because Pin remains and Ask Rarity is still a floating
bottom-right launcher.

- [x] **Step 3: Implement the single preview action**

Replace the Pin and duplicate Open buttons with:

```tsx
<button
  type="button"
  aria-label={`Open ${item.label} in workspace`}
  onClick={() => {
    openRecord(item);
    onOpen?.(item);
  }}
>
  <ArrowUpRight size={16} aria-hidden="true" />
  <span>Open</span>
</button>
```

Render typed preview title, status, and summary as authored text. Restore focus
to the opening record on close when its trigger remains mounted.

- [x] **Step 4: Move AI into the command bar**

Pass `GlobalWorkActions` through `primaryActions`. Render in this order:

```tsx
<CommandBar>
  {primaryActions}
  <AIRail api={aiAssistAPI} context={context} />
  <CommandPalette navigation={navigation} />
  {topBar}
</CommandBar>
```

Keep the AI panel fixed and non-modal when open, but make the closed trigger a
normal command-bar control rather than a fixed launcher.

- [x] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace src/design-system/components/navigation
```

Expected: preview and AI interaction tests pass.

- [x] **Step 6: Commit**

```bash
git add frontend/src/App.tsx frontend/src/design-system
git commit -m "Refine previews and top bar AI"
```

---

### Task 5: Add ticket preview and direct-open behavior

**Files:**
- Modify: `frontend/src/design-system/components/data/KanbanBoard.tsx`
- Modify: `frontend/src/design-system/components/data/Worklist.tsx`
- Modify: `frontend/src/design-system/components/data/Worklist.test.tsx`
- Modify: `frontend/src/features/home/HomePage.tsx`
- Modify: `frontend/src/features/home/HomePage.test.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Shared record components add `onOpenFull?: (item: T) => void`.
- Ticket workspace items use:

```ts
{
  id: `ticket:${record.id}`,
  routeID: "work",
  recordID: record.id,
  clientID,
  label: record.displayID,
  entityType: "ticket",
  openedAt: Date.now(),
  preview: {
    title: record.title,
    summary: record.description || "No description provided.",
    status: record.status,
  },
}
```

- [x] **Step 1: Write failing shared interaction tests**

```tsx
it("uses one click for preview and double click for full open", () => {
  const preview = vi.fn();
  const openFull = vi.fn();
  render(
    <Worklist
      {...worklistProps}
      onSelect={preview}
      onOpenFull={openFull}
    />,
  );
  const record = screen.getByRole("button", { name: "INC-1048" });
  fireEvent.click(record);
  expect(preview).toHaveBeenCalledOnce();
  fireEvent.doubleClick(record);
  expect(openFull).toHaveBeenCalledOnce();
});
```

Add Home and Work tests that assert double-click activates one ticket tab,
navigates to Work, closes the preview, and exposes the full detail heading.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/data/Worklist.test.tsx src/features/home/HomePage.test.tsx src/features/work/TechnicianWorklist.test.tsx
```

Expected: FAIL because shared record controls have no full-open callback and
ticket items lack entity metadata.

- [x] **Step 3: Implement shared direct-open controls**

Add `onDoubleClick` and an accessible `Open full record` control to list and
Kanban cards. Stop propagation on the explicit full-open button so it does not
also reopen the preview.

- [x] **Step 4: Wire Home and Work ticket targets**

Use one `ticketWorkspaceItem(record, clientID)` helper in the Work feature.
Home and Work dispatch `openPreview` on single click and `openRecord` on
double-click or explicit open. WorkspaceProvider invokes AppShell's
`onActivateWorkspaceItem`, which updates authorized client scope and navigates
to Work with `workRecordID`.

- [x] **Step 5: Render the full ticket form for an active ticket tab**

When the active record tab is the selected ticket, render `WorkDetail` as the
large record surface without the queue grid. A route-only Work visit retains
the list/preview layout. Failed record loads show the existing retryable
`StatePanel` without closing the tab.

- [x] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/data src/features/home src/features/work src/App.test.tsx
```

Expected: ticket preview, direct-open, deduplication, full form, and route
history tests pass.

- [x] **Step 7: Commit**

```bash
git add frontend/src/App.tsx frontend/src/design-system/components/data frontend/src/features/home frontend/src/features/work
git commit -m "Add full ticket workspace opening"
```

---

### Task 6: Standardize persistent Kanban views and project/task records

**Files:**
- Create: `frontend/src/design-system/foundations/viewPreferences.ts`
- Create: `frontend/src/design-system/foundations/viewPreferences.test.ts`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/sales/SalesWorklists.tsx`
- Modify: `frontend/src/features/sales/SalesWorklists.test.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.test.tsx`
- Modify: `frontend/src/features/projects/ProjectPage.tsx`
- Modify: `frontend/src/features/projects/projects.css`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces:

```ts
export type SupportedWorkView = "list" | "kanban";

export function readViewPreference(
  storage: Storage,
  principalID: string,
  routeID: "work" | "sales" | "project",
): SupportedWorkView;

export function writeViewPreference(
  storage: Storage,
  principalID: string,
  routeID: "work" | "sales" | "project",
  view: SupportedWorkView,
): void;
```

- `ProjectWorklist` receives `principalID`, `selectedRecord`, and uses project
  lifecycle state for Kanban lanes.
- `ProjectPage` receives:

```ts
onPreviewTask?: (taskID: string, title: string) => void;
onOpenTask?: (taskID: string, title: string) => void;
```

- [x] **Step 1: Write failing preference tests**

```ts
it("stores a supported view per principal and route", () => {
  writeViewPreference(localStorage, "principal-1", "project", "kanban");
  expect(
    readViewPreference(localStorage, "principal-1", "project"),
  ).toBe("kanban");
  expect(readViewPreference(localStorage, "principal-2", "project")).toBe(
    "list",
  );
});

it("falls back to list for corrupt or unsupported values", () => {
  localStorage.setItem("rti:view:principal-1:work", '"timeline"');
  expect(readViewPreference(localStorage, "principal-1", "work")).toBe("list");
});
```

- [x] **Step 2: Write failing project interaction tests**

Add tests proving Project defaults to list, can switch to Kanban, persists the
choice, single-clicks a project into preview, double-clicks it into a project
tab, and opens a task from `ProjectPage` into a task tab with
`parentRecordID: "project-1"`.

- [x] **Step 3: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/foundations/viewPreferences.test.ts src/features/work/TechnicianWorklist.test.tsx src/features/sales/SalesWorklists.test.tsx src/features/projects/ProjectWorklist.test.tsx
```

Expected: FAIL because route view preferences and project/task record
interactions do not exist.

- [x] **Step 4: Implement route view preferences**

Use principal- and route-scoped keys:

```ts
`rti:view:${encodeURIComponent(principalID)}:${routeID}`
```

Catch storage errors and always fall back to `list`. Wire Work and Sales to the
same helper, replacing direction-derived defaults.

- [x] **Step 5: Add Project list and Kanban**

Render project summaries through `ViewSwitcher`. Group Kanban lanes as Planned,
Active, At risk/blocked, and Complete using normalized lifecycle state. Both
views use the same filtered `items` array and record interaction helper.

- [x] **Step 6: Add project and task record interactions**

Project items use `entityType: "project"` with `recordID` equal to project ID.
Tasks use `entityType: "task"`, `recordID` equal to task ID, and
`parentRecordID` equal to the loaded project ID. Full project tabs render the
existing complete `ProjectPage` and authorized editor controls. Full task tabs
render the task summary, status, owner, schedule/minutes, time-entry action, and
parent-project context from the loaded project workspace.

- [x] **Step 7: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/foundations src/features/work src/features/sales src/features/projects src/App.test.tsx
```

Expected: Work, Sales, and Projects retain their selected view; project/task
preview and full tabs pass; Proposal remains page-history only.

- [x] **Step 8: Commit**

```bash
git add frontend/src/App.tsx frontend/src/design-system frontend/src/features/work frontend/src/features/sales frontend/src/features/projects
git commit -m "Standardize Kanban and delivery records"
```

---

### Task 7: Remove Atlas and synthetic preview artifacts

**Files:**
- Delete: `frontend/src/preview-data/PreviewDataProvider.tsx`
- Delete: `frontend/src/preview-data/PreviewDataProvider.test.tsx`
- Delete: `frontend/src/preview-data/catalog.ts`
- Delete: `frontend/src/preview-data/preview-data.css`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/features/home/HomePage.tsx`
- Modify: `frontend/src/features/home/HomePage.test.tsx`
- Modify: `frontend/src/features/home/home.css`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.test.tsx`
- Modify: `frontend/src/design-system/testing/noRawJson.test.ts`
- Modify: `frontend/src/design-system/testing/routeCoverage.test.ts`
- Modify: `frontend/src/App.test.tsx`
- Modify: `infrastructure/compose/frontend.Dockerfile`
- Modify: `infrastructure/compose/compose.yaml`
- Modify: `infrastructure/compose/compose_test.go`
- Modify: `.env`

**Interfaces:**
- The frontend has no preview-data provider or demo environment branch.
- The Docker frontend build accepts only `RARITY_BUILD_REVISION`.

- [x] **Step 1: Write failing old-artifact and manifest tests**

Add behavior-level tests that import and render every protected route through
the Signal shell, then assert:

```ts
expect(container.querySelector(".rti-atlas-rail")).toBeNull();
expect(container.querySelector(".rti-signal-context")).toBeNull();
expect(screen.queryByLabelText("Design direction")).not.toBeInTheDocument();
expect(screen.queryByText("Synthetic preview data")).not.toBeInTheDocument();
expect(screen.queryByText("Northstar Managed Services")).not.toBeInTheDocument();
expect(screen.queryByRole("navigation", { name: "Open records" })).toBeNull();
```

The route loop also proves that navigation alone leaves the record-tab bar
absent and each route has one active main landmark.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/testing src/App.test.tsx
```

Expected: FAIL while Atlas, queue context, direction switching, and synthetic
preview behavior remain.

- [x] **Step 3: Remove synthetic preview dependencies**

Delete the provider and catalog. Restore Home and Work to authorized live API
data only. Keep authored loading, empty, and error states. Remove demo imports,
conditionals, labels, and CSS.

- [x] **Step 4: Remove remaining Atlas and direction branches**

Delete Atlas-only Home canvas, CSS selectors, workspace modes, design catalog
examples, and tests. Keep the Signal design-system controls and optional
Kanbans.

- [x] **Step 5: Remove the demo build flag**

Remove:

```dockerfile
ARG VITE_RARITY_SYNTHETIC_PREVIEW=false
ENV VITE_RARITY_SYNTHETIC_PREVIEW=$VITE_RARITY_SYNTHETIC_PREVIEW
```

Remove the matching Compose argument and Go test. Remove the ignored local
`.env` setting so the next image is built without demo content.

- [x] **Step 6: Strengthen no-raw-JSON coverage**

Retain the source and route tests that reject `JSON.stringify` output in
user-facing components, `<pre>` rendering of API bodies, and raw JSON editor
labels. Storage and HTTP serialization remain allowed only in non-rendering
boundaries.

- [x] **Step 7: Run focused tests and verify GREEN**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/testing src/design-system/catalog src/features/home src/features/work src/App.test.tsx
docker run --rm -v "$PWD:/src" -v /var/run/docker.sock:/var/run/docker.sock -w /src/infrastructure/compose golang:1.25-alpine sh -lc 'apk add --no-cache docker-cli docker-cli-compose >/dev/null && GOTOOLCHAIN=auto /usr/local/go/bin/go test -count=1'
```

Expected: all focused frontend and Compose contract tests pass without a demo
flag.

- [x] **Step 8: Commit**

```bash
git add -A frontend/src infrastructure/compose docs/superpowers/specs
git commit -m "Remove prototype direction and demo artifacts"
```

---

### Task 8: Run route-wide regression and production verification

**Files:**
- Modify: `docs/superpowers/plans/2026-08-05-signal-console-consolidation.md`

**Interfaces:**
- Consumes the completed Signal shell, page history, record workspace, live
  feature pages, and Docker build.
- Produces a clean branch with verified commits and a refreshed local preview.

- [x] **Step 1: Run the complete frontend suite**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test
```

Expected: all test files and tests pass. Treat React warnings, unhandled
promises, and accessibility errors as failures to investigate.

- [x] **Step 2: Run the production build**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm run build
```

Expected: TypeScript and Vite complete successfully. Record the existing chunk
size warning separately if it remains non-fatal.

- [x] **Step 3: Run formatting and diff checks**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npx prettier --check src
git diff --check
git status --short
```

Expected: formatting and diff checks pass; only intended plan checkbox updates
are uncommitted.

- [x] **Step 4: Rebuild the local preview without demo data**

Run:

```bash
docker compose --env-file .env -f infrastructure/compose/compose.yaml build rarity-web
docker compose --env-file .env -f infrastructure/compose/compose.yaml up -d rarity-web caddy
```

Expected: frontend and edge services start successfully.

- [x] **Step 5: Verify runtime health and shipped asset contents**

Run:

```bash
curl -fsS http://127.0.0.1:18080/healthz
curl -fsS http://127.0.0.1:18080/readyz
asset=$(curl -fsS http://127.0.0.1:18080/ | sed -n 's/.*src="\\([^"]*\\.js\\)".*/\\1/p')
curl -fsS "http://127.0.0.1:18080$asset" > /tmp/rarity-signal-bundle.js
! rg -n "Northstar Managed Services|Synthetic preview|rti-atlas-rail|Queue context" /tmp/rarity-signal-bundle.js
```

Expected: live and ready return success, and the shipped bundle contains none
of the removed prototype artifacts.

- [x] **Step 6: Request user-selected browser QA if none is selected**

Do not infer a browser choice from ambient UI. Ask the user to explicitly select
the in-app browser or another supported browser before claiming visual
inspection. Automated route, interaction, and accessibility verification may be
reported independently.

- [x] **Step 7: Mark the plan complete and commit**

Change all completed plan checkboxes to `[x]`, then run:

```bash
git add docs/superpowers/plans/2026-08-05-signal-console-consolidation.md
git commit -m "Verify Signal console consolidation"
```

Expected: the worktree is clean and the branch contains the approved
specification, incremental implementation commits, and verification evidence.
