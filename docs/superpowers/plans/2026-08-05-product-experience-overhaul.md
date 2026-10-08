# Product Experience Overhaul Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver two complete, switchable dark-mode product directions and migrate every Rarity frontend route to a polished, role-aware, multitasking operational workspace with no raw JSON exposed to users.

**Architecture:** A typed route manifest becomes the source of truth for navigation and page metadata. Shared shell, workspace, field, data, overlay, and configuration-builder components consume semantic direction and density tokens; feature pages keep their existing API and authorization behavior while migrating presentation. A route/state coverage ledger and rendered smoke harness prevent legacy surfaces from escaping migration.

**Tech Stack:** React 19, TypeScript 7, Vite 8, Vitest, Testing Library, Playwright, axe-core, Lucide React, CSS custom properties.

## Global Constraints

- Both directions use the same routes, state, data, permissions, and component APIs.
- Rarity red remains the primary brand and action color in both directions.
- No user-facing surface may expose, request, edit, or describe raw JSON.
- Do not add dependencies without explicit user permission.
- Preserve every recognized hash, permission boundary, capability check, and API payload contract.
- Use adaptive compact density for queues/tables/audit/operations and comfortable density for forms/setup/record detail, with a global override.
- Phones support Home, search, triage, ticket updates, notes, time entry, approvals, and AI chat; full functionality begins at tablet widths.
- Every route must render in Signal Console and Atlas Workspace, and every pointer interaction must have a keyboard alternative.

---

### Task 1: Typed route manifest and migration ledger

**Files:**
- Create: `frontend/src/app/routes.ts`
- Create: `frontend/src/app/routes.test.ts`
- Create: `frontend/src/design-system/testing/routeCoverage.ts`
- Create: `frontend/src/design-system/testing/routeCoverage.test.ts`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces: `RouteID`, `RouteDefinition`, `routeManifest`, `routeForHash()`, `navigationRoutes()`, and `assertRouteCoverage()`.
- Consumes: current `Page`, `navigation`, `navigationGroup()`, and `pageFromHash()` behavior.

- [ ] **Step 1: Write route-manifest contract tests**

```ts
import { describe, expect, it } from "vitest";
import { routeManifest, routeForHash } from "./routes";

describe("routeManifest", () => {
  it("keeps every current deep link and adds home", () => {
    expect(routeForHash("#/service-desk-settings").id).toBe(
      "service-desk-settings",
    );
    expect(routeForHash("#/home").id).toBe("home");
    expect(new Set(routeManifest.map((route) => route.id)).size).toBe(
      routeManifest.length,
    );
  });

  it("declares role, density, family, and mobile behavior", () => {
    for (const route of routeManifest) {
      expect(route.roles.length).toBeGreaterThan(0);
      expect(["compact", "comfortable"]).toContain(route.density);
      expect(route.family).toBeTruthy();
      expect(typeof route.mobile).toBe("boolean");
    }
  });
});
```

- [ ] **Step 2: Run the tests and verify failure**

Run: `cd frontend && npm test -- src/app/routes.test.ts`

Expected: FAIL because `frontend/src/app/routes.ts` does not exist.

- [ ] **Step 3: Implement the route manifest and compatibility exports**

```ts
export type RouteID =
  | "home"
  | "work"
  | "sales"
  | "proposal"
  | "conversion"
  | "project"
  | "ai-settings"
  | "ai-assist"
  | "teams-settings"
  | "sessions"
  | "recovery-access"
  | "role-settings"
  | "audit"
  | "service-desk-settings"
  | "setup"
  | "operations"
  | "automation"
  | "service-keys"
  | "webhooks"
  | "datto-reconciliation"
  | "datto-settings"
  | "forwarding-settings"
  | "graph-settings"
  | "knowledge"
  | "billing"
  | "directory-settings"
  | "client-resources"
  | "pipeline-settings"
  | "prospects"
  | "design-system"
  | "login"
  | "break-glass";

export type RouteDefinition = {
  id: RouteID;
  label: string;
  group: "home" | "work" | "sales" | "delivery" | "clients" | "knowledge" | "operations" | "admin";
  family: "home" | "worklist" | "record" | "settings" | "operations" | "public" | "catalog";
  roles: Array<"technician" | "admin" | "sales" | "delivery">;
  density: "compact" | "comfortable";
  mobile: boolean;
  legacyHashes: string[];
};
```

Implement every route from the approved spec, preserve `navigation.ts`
compatibility exports, and make `#/home` the authenticated fallback.

- [ ] **Step 4: Add a route coverage assertion**

```ts
export function assertRouteCoverage(
  renderedRoutes: ReadonlySet<string>,
): string[] {
  return routeManifest
    .filter((route) => !["login", "break-glass"].includes(route.id))
    .filter((route) => !renderedRoutes.has(route.id))
    .map((route) => route.id);
}
```

The test supplies every route used by `AuthenticatedWorkspace` and expects an
empty array. It fails whenever the manifest and renderer drift.

- [ ] **Step 5: Run focused and navigation tests**

Run: `cd frontend && npm test -- src/app/routes.test.ts src/design-system/testing/routeCoverage.test.ts src/navigation.test.ts src/App.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/app frontend/src/navigation.ts frontend/src/App.tsx frontend/src/design-system/testing
git commit -m "Add typed product route manifest"
```

### Task 2: Direction, density, elevation, and motion foundations

**Files:**
- Modify: `frontend/src/design-system/foundations/tokens.css`
- Create: `frontend/src/design-system/foundations/preferences.ts`
- Create: `frontend/src/design-system/foundations/preferences.test.ts`
- Modify: `frontend/src/design-system/foundations/reset.css`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Modify: `frontend/src/design-system/catalog/catalog.css`
- Modify: `frontend/src/design-system/testing/boundaries.test.ts`

**Interfaces:**
- Produces: `DesignDirection = "signal" | "atlas"`, `DensityPreference = "adaptive" | "compact" | "comfortable"`, `readPresentationPreferences()`, and `writePresentationPreferences()`.
- Consumes: semantic `--rti-*` variables and existing `[data-density]` behavior.

- [ ] **Step 1: Write preference and token boundary tests**

```ts
it("defaults to signal and adaptive without stored values", () => {
  expect(readPresentationPreferences(new MapStorage())).toEqual({
    direction: "signal",
    density: "adaptive",
  });
});

it("contains no feature-specific direction colors", () => {
  expect(featureCSS).not.toMatch(/data-direction/);
});
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd frontend && npm test -- src/design-system/foundations/preferences.test.ts src/design-system/testing/boundaries.test.ts`

Expected: FAIL because preferences do not exist and direction tokens are
missing.

- [ ] **Step 3: Implement presentation preferences**

Store only the two whitelisted string values under
`rti:presentation-preferences`. Invalid or unavailable storage falls back to
Signal Console and adaptive density.

- [ ] **Step 4: Build the two semantic token ladders**

Add base semantic variables for canvas, navigation, header, panel, raised,
inset, control, overlay, selected, hover, scrim, four elevation levels, pane
widths, control typography, blur, easing, and duration. Override only semantic
variables under:

```css
[data-direction="signal"] { /* crisp neutral graphite */ }
[data-direction="atlas"] { /* softer cool charcoal and stronger depth */ }
```

Keep all status contrast at WCAG AA and remove the light top-bar background
currently hard-coded in `app.css`.

- [ ] **Step 5: Expand the catalog foundation section**

Render both directions, adaptive/compact/comfortable density, elevation,
surface, typography, motion, focus, and semantic-status samples.

- [ ] **Step 6: Run tests and build**

Run: `cd frontend && npm test -- src/design-system && npm run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/design-system
git commit -m "Add dual product direction foundations"
```

### Task 3: Unified product shell and preference switch

**Files:**
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/GroupedSidebar.tsx`
- Modify: `frontend/src/design-system/components/navigation/TopBar.tsx`
- Modify: `frontend/src/design-system/components/navigation/navigation.css`
- Create: `frontend/src/design-system/components/navigation/CommandBar.tsx`
- Create: `frontend/src/design-system/components/navigation/PresentationMenu.tsx`
- Create: `frontend/src/design-system/components/navigation/ProductNav.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/app.css`

**Interfaces:**
- Produces: `AppShell` regions for navigation, command bar, tabs, canvas,
  preview, and AI; `PresentationMenu` props `{ direction, density, onChange }`.
- Consumes: `routeManifest`, presentation preferences, current client selector,
  sign-out, and global create.

- [ ] **Step 1: Write shell behavior tests**

Test:

- Signal is the default `data-direction`.
- The admin-only presentation menu switches to Atlas and persists it.
- Navigation groups come from the route manifest.
- Escape closes mobile navigation and restores focus.
- The workspace starts at the top of a 390px viewport.
- Non-admin users do not see the direction control.

- [ ] **Step 2: Run the shell tests and verify failure**

Run: `cd frontend && npm test -- src/design-system/components/navigation/AppShell.test.tsx`

Expected: FAIL on missing direction, density, and command-bar behavior.

- [ ] **Step 3: Implement the six-region shell**

Keep one DOM structure for both directions. Set `data-direction`,
`data-density`, `data-page-family`, and `data-preview-open` on the app root.
Move client scope, create, user, and preference controls into `CommandBar`.

- [ ] **Step 4: Rebuild navigation from the manifest**

Expose Home, Work, Sales, Delivery, Clients, Knowledge, and Operations as
product areas. Keep Admin as one expandable tree with Access, Platform, Service
configuration, Integrations, Automation, and AI subgroups. Preserve active
route and narrow-drawer behavior.

- [ ] **Step 5: Implement responsive shell behavior**

At wide desktop, navigation, canvas, preview, and AI can coexist. At laptop,
secondary rails are mutually collapsible. Tablet uses overlay navigation.
Phone shows only routes with `mobile: true`.

- [ ] **Step 6: Run shell, app, and accessibility tests**

Run: `cd frontend && npm test -- src/design-system/components/navigation src/App.test.tsx src/AppBoundary.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/design-system/components/navigation frontend/src/App.tsx frontend/src/app.css
git commit -m "Build unified role-aware product shell"
```

### Task 4: Workspace tabs, previews, command palette, and persistent AI rail

**Files:**
- Create: `frontend/src/design-system/workspace/types.ts`
- Create: `frontend/src/design-system/workspace/useWorkspace.ts`
- Create: `frontend/src/design-system/workspace/useWorkspace.test.ts`
- Create: `frontend/src/design-system/workspace/WorkspaceTabs.tsx`
- Create: `frontend/src/design-system/workspace/PreviewPane.tsx`
- Create: `frontend/src/design-system/workspace/CommandPalette.tsx`
- Create: `frontend/src/design-system/workspace/workspace.css`
- Modify: `frontend/src/features/ai/AIAssistPanel.tsx`
- Modify: `frontend/src/features/ai/ai.css`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces: `WorkspaceItem`, `WorkspaceContext`, `openPreview()`,
  `promotePreview()`, `openTab()`, `closeTab()`, `markDirty()`, and
  `WorkspaceProvider`.
- Consumes: route IDs, client ID, record IDs, and the existing AI API.

- [ ] **Step 1: Write workspace reducer tests**

```ts
it("promotes a preview without duplicating an existing tab", () => {
  const state = reduce(initial, openPreview(ticket));
  const promoted = reduce(state, promotePreview());
  expect(promoted.tabs).toEqual([ticket]);
  expect(promoted.preview).toBeUndefined();
});

it("refuses to close dirty tabs without confirmation", () => {
  expect(reduce(dirtyState, closeTab("ticket-1")).closeRequest).toEqual({
    id: "ticket-1",
  });
});
```

- [ ] **Step 2: Run and verify failure**

Run: `cd frontend && npm test -- src/design-system/workspace/useWorkspace.test.ts`

Expected: FAIL because workspace state does not exist.

- [ ] **Step 3: Implement safe workspace persistence**

Persist only route ID, record ID, client ID, label, and timestamps. Never
persist form values, secrets, issued tokens, or API response bodies.

- [ ] **Step 4: Build tabs, preview, and command palette**

Tabs support close, pin, dirty state, keyboard reordering, overflow, and
restore. Preview supports resize, close, pin, and promote. The command palette
opens with `Ctrl/Cmd+K`, searches routes/actions, and restores focus on close.

- [ ] **Step 5: Convert AI assist into the persistent shell rail**

Keep the existing job, history, cancellation, retry, and decision behavior.
Add explicit context attachments for active client/record/page. AI proposed
actions remain drafts until the user confirms them.

- [ ] **Step 6: Run workspace and AI tests**

Run: `cd frontend && npm test -- src/design-system/workspace src/features/ai/AIAssistPanel.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/design-system/workspace frontend/src/features/ai frontend/src/design-system/index.ts frontend/src/App.tsx
git commit -m "Add persistent multitasking workspace"
```

### Task 5: Role-aware Home workspace

**Files:**
- Create: `frontend/src/features/home/HomePage.tsx`
- Create: `frontend/src/features/home/HomePage.test.tsx`
- Create: `frontend/src/features/home/home.css`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/app/routes.ts`

**Interfaces:**
- Produces: `HomePage` props `{ capabilities, clientID, onNavigate, onOpenPreview }`.
- Consumes: allowed routes, capabilities, active client, and existing work,
  operations, sales, and project API helpers where available.

- [ ] **Step 1: Write role-aware Home tests**

Test technician prioritization, administrator health content, mixed-role
unified ordering, no inaccessible links, empty states, and phone availability.

- [ ] **Step 2: Run and verify failure**

Run: `cd frontend && npm test -- src/features/home/HomePage.test.tsx`

Expected: FAIL because Home does not exist.

- [ ] **Step 3: Implement prioritized Home sections**

Use open bands and shared worklist rows for Assigned work, SLA risk, Schedule,
Approvals, Queue health, Platform health, Security events, Recent records, and
Resume work. Render only sections supported by capabilities and available data.

- [ ] **Step 4: Connect interactions**

Filters navigate to the underlying route; record rows open previews; resumable
items activate workspace tabs; loading and partial failures remain regional.

- [ ] **Step 5: Run tests and build**

Run: `cd frontend && npm test -- src/features/home/HomePage.test.tsx src/App.test.tsx && npm run build`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/features/home frontend/src/App.tsx frontend/src/app/routes.ts
git commit -m "Add role-aware operational home"
```

### Task 6: Modern fields, menus, dates, and overlays

**Files:**
- Modify: `frontend/src/design-system/components/fields/Field.tsx`
- Modify: `frontend/src/design-system/components/fields/TextInput.tsx`
- Modify: `frontend/src/design-system/components/fields/Textarea.tsx`
- Modify: `frontend/src/design-system/components/fields/Select.tsx`
- Modify: `frontend/src/design-system/components/fields/fields.css`
- Create: `frontend/src/design-system/components/fields/Combobox.tsx`
- Create: `frontend/src/design-system/components/fields/MultiSelect.tsx`
- Create: `frontend/src/design-system/components/fields/DatePicker.tsx`
- Create: `frontend/src/design-system/components/fields/DateRangePicker.tsx`
- Create: `frontend/src/design-system/components/fields/TimeInput.tsx`
- Create: `frontend/src/design-system/components/fields/SearchInput.tsx`
- Create: `frontend/src/design-system/components/overlays/Menu.tsx`
- Create: `frontend/src/design-system/components/overlays/Popover.tsx`
- Create: `frontend/src/design-system/components/overlays/Tooltip.tsx`
- Create: `frontend/src/design-system/components/overlays/overlays.css`
- Modify: `frontend/src/design-system/components/containers/Dialog.tsx`
- Modify: `frontend/src/design-system/components/containers/Drawer.tsx`
- Modify: `frontend/src/design-system/index.ts`

**Interfaces:**
- Produces: accessible typed field and overlay primitives without external
  dependencies.
- Consumes: current `Field`, dialog focus management, tokens, and form
  semantics.

- [ ] **Step 1: Write interaction and accessibility tests**

Cover searchable select, multi-selection, date typing/calendar selection,
range errors, Escape, click outside, arrow keys, focus return, disabled
reasons, and axe results.

- [ ] **Step 2: Run and verify failure**

Run: `cd frontend && npm test -- src/design-system/components/fields src/design-system/components/overlays`

Expected: FAIL for missing controls and outdated styles.

- [ ] **Step 3: Implement modern field anatomy**

Use visible labels, quiet filled control surfaces, 40px default height, 32px
compact visual height with preserved 44px touch target, inline help/error,
strong focus, clear buttons, optional leading/trailing icons, and deliberate
control typography.

- [ ] **Step 4: Implement overlays**

Menus, popovers, and tooltips use portal-free positioned layers where possible,
collision-aware alignment, keyboard navigation, outside dismissal, focus
return, and reduced-motion transitions.

- [ ] **Step 5: Update the design-system catalog**

Show every state for inputs, textareas, select, combobox, multi-select, dates,
menus, popovers, dialog, and drawer in both directions and all densities.

- [ ] **Step 6: Run component tests and build**

Run: `cd frontend && npm test -- src/design-system/components && npm run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/design-system
git commit -m "Modernize fields and interactive overlays"
```

### Task 7: Shared worklist, Kanban, timeline, and record templates

**Files:**
- Modify: `frontend/src/design-system/components/data/Worklist.tsx`
- Modify: `frontend/src/design-system/components/data/DataTable.tsx`
- Modify: `frontend/src/design-system/components/data/FilterBar.tsx`
- Modify: `frontend/src/design-system/components/data/data.css`
- Create: `frontend/src/design-system/components/data/ViewSwitcher.tsx`
- Create: `frontend/src/design-system/components/data/SavedViews.tsx`
- Create: `frontend/src/design-system/components/data/KanbanBoard.tsx`
- Create: `frontend/src/design-system/components/data/Timeline.tsx`
- Create: `frontend/src/design-system/components/data/KeyValueList.tsx`
- Create: `frontend/src/design-system/templates/RecordWorkspace.tsx`
- Modify: `frontend/src/design-system/templates/Page.tsx`
- Modify: `frontend/src/design-system/templates/page.css`
- Modify: `frontend/src/design-system/index.ts`

**Interfaces:**
- Produces: generic `ViewDefinition<T>`, `KanbanColumn<T>`,
  `RecordWorkspace`, and shared selection/preview callbacks.
- Consumes: existing `Worklist`, `DataTable`, `Page`, filters, pagination, and
  workspace preview actions.

- [ ] **Step 1: Write template and view tests**

Cover shared filtering across table/Kanban, keyboard card movement, transition
confirmation, preview callbacks, mobile list alternatives, saved-view naming,
and record secondary-pane collapse.

- [ ] **Step 2: Run and verify failure**

Run: `cd frontend && npm test -- src/design-system/components/data src/design-system/templates`

Expected: FAIL because shared views and record workspace do not exist.

- [ ] **Step 3: Implement worklist view state**

Keep filters, sort, pagination, and selection independent of view. Kanban drag
requests a transition preview and requires explicit confirmation when a reason
or permission check applies. Provide move buttons and keyboard movement.

- [ ] **Step 4: Implement record workspace**

Create identity header, activity surface, contextual pane, anchored composer,
save/conflict status, and responsive full-height context sheet.

- [ ] **Step 5: Run tests and build**

Run: `cd frontend && npm test -- src/design-system/components/data src/design-system/templates && npm run build`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/design-system
git commit -m "Add operational worklist and record templates"
```

### Task 8: Structured configuration builders and no-raw-JSON enforcement

**Files:**
- Create: `frontend/src/design-system/components/builders/WorkflowBuilder.tsx`
- Create: `frontend/src/design-system/components/builders/ConditionBuilder.tsx`
- Create: `frontend/src/design-system/components/builders/ScopeBuilder.tsx`
- Create: `frontend/src/design-system/components/builders/ConnectionWizard.tsx`
- Create: `frontend/src/design-system/components/builders/builders.css`
- Create: `frontend/src/design-system/components/builders/builders.test.tsx`
- Create: `frontend/src/design-system/testing/noRawJSON.test.ts`
- Modify: `frontend/src/features/automation/AutomationPage.tsx`
- Modify: `frontend/src/features/setup/SetupPage.tsx`
- Modify: `frontend/src/features/auth/ServiceKeysPage.tsx`
- Modify: `frontend/src/features/operations/DattoReconciliationPage.tsx`
- Modify: `frontend/src/design-system/index.ts`

**Interfaces:**
- Produces: typed visual builders that serialize to unchanged API payloads.
- Consumes: current automation `steps`, setup `intake/object_storage/backups`,
  key `capabilities/data_scopes`, and reconciliation values.

- [ ] **Step 1: Write failing builder serialization tests**

```ts
it("serializes workflow cards to the existing steps payload", async () => {
  await user.click(screen.getByRole("button", { name: "Add action" }));
  await user.selectOptions(screen.getByLabelText("Action"), "notify");
  expect(onChange).toHaveBeenLastCalledWith([
    { type: "notify", parameters: {} },
  ]);
});
```

Also assert that rendered production source contains no labels matching
`Typed steps (JSON)`, `(JSON)`, `Verify scope, JSON`, or raw object
serialization.

- [ ] **Step 2: Run and verify failure**

Run: `cd frontend && npm test -- src/design-system/components/builders src/design-system/testing/noRawJSON.test.ts`

Expected: FAIL on current Automation, Setup, Service Keys, and reconciliation
UI.

- [ ] **Step 3: Implement builders**

Workflow Builder provides trigger, condition, branch, action, retry, ordering,
duplicate, remove, drag, keyboard move, and readable flow summary. Scope
Builder provides grouped searchable permissions. Connection Wizard provides
typed steps, readiness, test evidence, and recovery. Condition Builder provides
field/operator/value rows with nested all/any groups.

- [ ] **Step 4: Replace each known exposure**

Keep API request types unchanged. Parse server values into builder models and
serialize builder state at submit time. Render reconciliation objects as
`KeyValueList` and differences, never with `JSON.stringify`.

- [ ] **Step 5: Run feature and enforcement tests**

Run: `cd frontend && npm test -- src/features/automation src/features/setup src/features/auth/ServiceKeysPage.test.tsx src/features/operations/DattoReconciliationPage.test.tsx src/design-system/testing/noRawJSON.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/design-system frontend/src/features/automation frontend/src/features/setup frontend/src/features/auth/ServiceKeysPage.tsx frontend/src/features/operations/DattoReconciliationPage.tsx
git commit -m "Replace raw configuration with visual builders"
```

### Task 9: Migrate Work, Sales, Proposals, Conversion, and Prospects

**Files:**
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/GlobalWorkActions.tsx`
- Modify: `frontend/src/features/work/ServiceDeskSettingsPage.tsx`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/features/sales/OpportunityPage.tsx`
- Modify: `frontend/src/features/sales/SalesWorklists.tsx`
- Modify: `frontend/src/features/sales/PipelinePage.tsx`
- Modify: `frontend/src/features/sales/PipelineSettingsPage.tsx`
- Modify: `frontend/src/features/sales/ProspectsPage.tsx`
- Modify: `frontend/src/features/sales/ProposalEditor.tsx`
- Modify: `frontend/src/features/sales/ForecastView.tsx`
- Modify: `frontend/src/features/sales/sales.css`
- Modify: `frontend/src/features/projects/ConversionPreview.tsx`
- Modify: `frontend/src/features/projects/LiveConversionPage.tsx`
- Modify: relevant tests under the same feature folders

**Interfaces:**
- Produces: migrated worklist and record pages with shared views and previews.
- Consumes: existing APIs, capability checks, business mutations, and Tasks
  3–7 components.

- [ ] **Step 1: Extend tests for shared page behavior**

Assert page template, adaptive compact density, view switch, preview,
open-in-tab, modern field primitives, loading/empty/error states, and current
business mutations for every listed page.

- [ ] **Step 2: Run focused tests and verify new assertions fail**

Run: `cd frontend && npm test -- src/features/work src/features/sales src/features/projects/LiveConversionPage.test.tsx src/features/projects/ConversionPreview.test.tsx`

Expected: FAIL on missing shared templates and interactions.

- [ ] **Step 3: Migrate work and sales worklists**

Use `Page`, `Worklist`, `ViewSwitcher`, `KanbanBoard`, filters, shared
pagination, and workspace preview callbacks. Preserve existing fetch and
mutation functions.

- [ ] **Step 4: Migrate record and editor forms**

Use `RecordWorkspace`, modern fields, sticky actions, activity/context panes,
and visible pending/conflict/error states.

- [ ] **Step 5: Remove obsolete feature CSS**

Delete feature-local button, input, panel, table, overlay, and status rules now
owned by the design system. Keep only domain-specific layout rules.

- [ ] **Step 6: Run tests and build**

Run: `cd frontend && npm test -- src/features/work src/features/sales src/features/projects && npm run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/work frontend/src/features/sales frontend/src/features/projects
git commit -m "Migrate service and sales workspaces"
```

### Task 10: Migrate Projects, Knowledge, Billing, Clients, and Resources

**Files:**
- Modify: all components and tests under `frontend/src/features/projects`
- Modify: all components and tests under `frontend/src/features/knowledge`
- Modify: all components and tests under `frontend/src/features/billing`
- Modify: all components and tests under `frontend/src/features/organization`

**Interfaces:**
- Produces: migrated delivery and organization workspaces.
- Consumes: existing APIs, financial calculations, permissions, project
  transition logic, shared worklists, record workspaces, and builders.

- [ ] **Step 1: Add migration assertions to feature tests**

Cover project list and detail, capacity, financials, change orders, knowledge
list/editor, billing recognition, directory configuration, and client
resources. Assert preview/tab, shared fields, page states, and preserved
mutations.

- [ ] **Step 2: Run and verify new assertions fail**

Run: `cd frontend && npm test -- src/features/projects src/features/knowledge src/features/billing src/features/organization`

Expected: FAIL on legacy compositions.

- [ ] **Step 3: Migrate worklists and dashboards**

Use shared table/list/Kanban/timeline views where the data supports them. Keep
financial tables compact and tabular. Keep capacity and billing drilldowns tied
to source records.

- [ ] **Step 4: Migrate record workspaces and forms**

Use shared record/activity/context patterns and modern fields. Preserve
approval, override, conversion, publish, and recognition evidence.

- [ ] **Step 5: Run tests and build**

Run: `cd frontend && npm test -- src/features/projects src/features/knowledge src/features/billing src/features/organization && npm run build`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/features/projects frontend/src/features/knowledge frontend/src/features/billing frontend/src/features/organization
git commit -m "Migrate delivery and organization workspaces"
```

### Task 11: Migrate Operations, Integrations, Automation, AI, and Admin

**Files:**
- Modify: all components and tests under `frontend/src/features/operations`
- Modify: all components and tests under `frontend/src/features/automation`
- Modify: all components and tests under `frontend/src/features/ai`
- Modify: all components and tests under `frontend/src/features/teams`
- Modify: all components and tests under `frontend/src/features/auth`
- Modify: all components and tests under `frontend/src/features/setup`

**Interfaces:**
- Produces: migrated administrative and operational surfaces with structured
  configuration.
- Consumes: existing security boundaries, APIs, durable jobs, connection
  evidence, shared builders, fields, worklists, and system states.

- [ ] **Step 1: Add migration assertions**

Cover every page listed in Task 1, including login, break-glass, setup-required,
application error, session expiry, loading, empty, permission, and server error.
Assert no raw JSON, shared page families, structured settings, modern overlays,
and preserved security semantics.

- [ ] **Step 2: Run and verify new assertions fail**

Run: `cd frontend && npm test -- src/features/operations src/features/automation src/features/ai src/features/teams src/features/auth src/features/setup`

Expected: FAIL on legacy compositions and raw configuration.

- [ ] **Step 3: Migrate operational worklists and health dashboards**

Use shared worklists, timelines, progress, preview, retry, and job patterns.
Keep errors regional and preserve server evidence.

- [ ] **Step 4: Migrate settings and access pages**

Use local navigation, field groups, structured builders, sticky actions,
connection test evidence, reason-required actions, and readable audit facts.
Never reveal stored secrets.

- [ ] **Step 5: Migrate public and boundary surfaces**

Apply both direction foundations to login, recovery, setup-required, loading,
and global error pages while keeping their focused single-task composition.

- [ ] **Step 6: Run tests and build**

Run: `cd frontend && npm test -- src/features/operations src/features/automation src/features/ai src/features/teams src/features/auth src/features/setup src/AppBoundary.test.tsx && npm run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/operations frontend/src/features/automation frontend/src/features/ai frontend/src/features/teams frontend/src/features/auth frontend/src/features/setup frontend/src/AppBoundary.test.tsx
git commit -m "Migrate operations and administration"
```

### Task 12: Route/state visual ledger and final verification

**Files:**
- Create: `frontend/tests/product-experience.spec.ts`
- Create: `frontend/tests/helpers/productExperience.ts`
- Create: `frontend/tests/product-experience-ledger.md`
- Modify: `frontend/package.json`
- Modify: `docs/07-ui-ux/design-system.md`
- Modify: `docs/07-ui-ux/design-system-acceptance.md`

**Interfaces:**
- Produces: deterministic screenshot and interaction coverage for both
  directions and all routes/states.
- Consumes: route manifest, app test access, Playwright, axe-core, and the
  completed product.

- [ ] **Step 1: Write the route/state smoke specification**

For each manifest route, render Signal and Atlas at 1440×1024. Add applicable
1280×720, 834×1194, and 390×844 cases. Open and capture menus, date pickers,
dialogs, preview, tabs, AI rail, and populated/empty/error/loading states.

- [ ] **Step 2: Add automated acceptance assertions**

Assert:

- no horizontal document overflow
- one visible `main` and one page heading
- no console error or React warning
- no raw-JSON labels or serialized object blocks
- visible focus on keyboard traversal
- successful axe scan for serious/critical violations
- mobile routes match the manifest

- [ ] **Step 3: Run unit, build, and browser verification**

Run:

```bash
cd frontend
npm test
npm run build
npx playwright test tests/product-experience.spec.ts
```

Expected: all commands PASS.

- [ ] **Step 4: Inspect every accepted screenshot**

Compare Signal and Atlas at matching states. Record copy, hierarchy, typography,
palette, elevation, density, icon, interaction, responsive, and JSON-contract
results in `product-experience-ledger.md`. Any uncovered route or legacy
surface fails the ledger.

- [ ] **Step 5: Repair every ledger failure and rerun**

Repeat the focused test and matching screenshot until the ledger contains no
P0, P1, or P2 findings and no page remains on legacy styling.

- [ ] **Step 6: Update product documentation**

Document direction switching, adaptive density, route families, workspace
tabs, preview behavior, AI context, structured configuration, phone scope, and
the route/state verification matrix.

- [ ] **Step 7: Commit**

```bash
git add frontend/tests frontend/package.json docs/07-ui-ux
git commit -m "Verify complete dual-direction migration"
```
