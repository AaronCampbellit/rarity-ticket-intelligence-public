# Rarity Product Design System Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task by task.

**Goal:** Build the authenticated Rarity product design system, migrate the application incrementally, and preserve every existing workflow and authorization contract while replacing duplicated presentation and interaction code.

**Architecture:** The design system lives inside `frontend/src/design-system` with one public entry point. Foundations define semantic CSS contracts; components own presentation and accessible interaction; patterns compose Rarity-specific workflows without fetching data or inferring authorization; templates arrange pages. Existing feature modules remain the owners of API calls, state, capabilities, and business decisions.

**Tech Stack:** React 19, TypeScript 7, CSS custom properties, Lucide React icons, Vitest, Testing Library, axe-core, and Playwright.

## Global Constraints

- The approved source of truth is `docs/07-ui-ux/design-system.md`.
- The first release covers only the authenticated Rarity product application. Documentation, marketing, and external portals are excluded.
- Preserve the current Rarity visual language: dark navigation, white workspace, red primary action, restrained radii, compact operational density, and native form controls.
- Components do not fetch feature data, inspect session state, or infer permissions. Features pass explicit state and callbacks.
- Never hide an unauthorized action when its presence teaches the user about the workflow. Render it disabled with an explanation unless revealing it would expose protected information.
- Keep native controls unless a product requirement needs custom interaction.
- The sole icon source is `lucide-react`. Icons supplement visible labels; icon-only actions require an accessible name and tooltip.
- The catalog is development-only. A production-mode build must never expose the route, including when `VITE_ENABLE_DESIGN_SYSTEM_CATALOG=true` is present.
- Keep all current hash routes and API contracts intact during migration.
- Use test-driven slices: add the failing behavior/accessibility test, run it, implement the smallest public API, rerun the focused test, then run the frontend suite and build at each phase boundary.
- Do not remove feature CSS until the migrated feature passes unit, axe, responsive browser, and representative Playwright checks.
- Do not claim manual VoiceOver, NVDA, forced-colors, or 400% zoom acceptance from automation.
- Git checkpoints are deferred. Do not stage, commit, push, or rewrite history until the user explicitly reauthorizes Git.

## Phase 1 — Foundations and Catalog

### Task 1: Establish the package boundary and public API guard

**Files:**

- Create: `frontend/src/design-system/index.ts`
- Create: `frontend/src/design-system/testing/public-api.test.ts`
- Create: `frontend/src/design-system/testing/renderA11y.ts`
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`

**Step 1: Add the failing public API test**

Create `frontend/src/design-system/testing/public-api.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import * as designSystem from "../index";

describe("design-system public API", () => {
  it("exposes a stable package identity through one entry point", () => {
    expect(designSystem.DESIGN_SYSTEM_NAME).toBe("Rarity");
  });
});
```

Run:

```bash
cd frontend
npm test -- src/design-system/testing/public-api.test.ts
```

Expected: FAIL because `../index` and its exports do not exist.

**Step 2: Install the icon dependency**

Run:

```bash
cd frontend
npm install lucide-react@1.28.0
```

Expected: `package.json` and `package-lock.json` add `lucide-react`; no unrelated dependency upgrades.

**Step 3: Create the public entry point**

Create `frontend/src/design-system/index.ts` with the stable package identity:

```ts
export const DESIGN_SYSTEM_NAME = "Rarity";
```

Do not add wildcard exports. Every later public component is added explicitly here after its own behavior test has failed.

**Step 4: Add the shared axe helper**

Create `frontend/src/design-system/testing/renderA11y.ts`:

```ts
import type { RenderResult } from "@testing-library/react";
import axe from "axe-core";

export async function expectNoA11yViolations(container: RenderResult["container"]) {
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  return result.violations;
}
```

The color-contrast exception is temporary because jsdom cannot resolve the CSS token cascade. Browser acceptance covers actual contrast.

**Step 5: Verify the package boundary**

Run:

```bash
cd frontend
npm test -- src/design-system/testing/public-api.test.ts
npm run build
```

Expected: PASS. Later tasks extend this test only after the corresponding component tests are green.

### Task 2: Introduce semantic tokens without changing rendered behavior

**Files:**

- Create: `frontend/src/design-system/foundations/tokens.css`
- Create: `frontend/src/design-system/foundations/reset.css`
- Create: `frontend/src/design-system/foundations/tokens.test.ts`
- Create: `frontend/src/vite-env.d.ts`
- Modify: `frontend/src/main.tsx`
- Modify: `frontend/src/app.css`

**Step 1: Add a failing token-contract test**

Create `frontend/src/design-system/foundations/tokens.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import "./tokens.css";

describe("semantic token contract", () => {
  it.each([
    "--rti-surface-canvas",
    "--rti-surface-panel",
    "--rti-text-primary",
    "--rti-text-muted",
    "--rti-border-default",
    "--rti-action-primary",
    "--rti-action-danger",
    "--rti-status-success",
    "--rti-status-warning",
    "--rti-status-danger",
    "--rti-focus-ring",
    "--rti-shadow-overlay",
  ])("defines %s", (token) => {
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(token),
    ).not.toBe("");
  });

  it("exposes compact and comfortable density", () => {
    document.documentElement.dataset.density = "compact";
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--rti-density-control-height",
      ),
    ).toContain("--rti-control-height-compact");

    document.documentElement.dataset.density = "comfortable";
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--rti-density-control-height",
      ),
    ).toContain("--rti-control-height-default");
  });
});
```

Run `npm test -- src/design-system/foundations/tokens.test.ts`.

Expected: FAIL because `tokens.css` does not exist.

**Step 2: Define foundations**

Implement the semantic groups from the specification:

- surfaces: canvas, panel, inset, overlay, navigation
- text: primary, secondary, muted, inverse, link, danger
- borders: subtle, default, strong, focus, danger
- actions: primary, primary-hover, secondary, danger, disabled
- statuses: neutral, info, success, warning, danger
- typography: page title, section title, body, compact, metadata, code
- spacing: `--rti-space-1` through `--rti-space-8`
- geometry: control heights, radii, content widths
- focus, overlay shadow, transition duration

Map the current literals (`#09090a`, `#bd0f17`, `#fff`, `#202225`) to semantic tokens so the first import causes no intended visual redesign.

In `reset.css`, move only global normalization: box sizing, body margin/minimums, inherited form font, link reset, screen-reader utility, and reduced-motion override.

Create `frontend/src/vite-env.d.ts` with:

```ts
/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_ENABLE_DESIGN_SYSTEM_CATALOG?: string;
}
```

**Step 3: Load foundations first**

At the top of `frontend/src/main.tsx` import:

```ts
import "./design-system/foundations/tokens.css";
import "./design-system/foundations/reset.css";
```

Replace matching top-level literals in `app.css` with semantic variables. Do not migrate feature CSS in this task.

**Step 4: Verify**

Run:

```bash
cd frontend
npm test -- src/design-system/foundations/tokens.test.ts src/App.test.tsx
npm run build
```

Expected: PASS. Existing App behavior and layout remain intact.

### Task 3: Build the development-only catalog shell

**Files:**

- Create: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Create: `frontend/src/design-system/catalog/catalog.css`
- Create: `frontend/src/design-system/catalog/DesignSystemCatalog.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/vite-env.d.ts`

**Step 1: Add failing route-gate tests**

Test these contracts:

```ts
it("renders the catalog when explicitly enabled", () => {
  window.location.hash = "#/design-system";
  render(<App build={{ revision: "test" }} catalogEnabled />);
  expect(screen.getByRole("heading", { name: "Rarity design system" })).toBeVisible();
});

it("falls back to Sales when the catalog is disabled", () => {
  window.location.hash = "#/design-system";
  render(<App build={{ revision: "test" }} catalogEnabled={false} />);
  expect(screen.queryByRole("heading", { name: "Rarity design system" })).not.toBeInTheDocument();
});
```

Run `npm test -- src/App.test.tsx`.

Expected: FAIL because `catalogEnabled` and the route do not exist.

**Step 2: Add the explicit gate**

Add `catalogEnabled?: boolean` to `App`, defaulting to:

```ts
import.meta.env.DEV ||
(import.meta.env.MODE !== "production" &&
  import.meta.env.VITE_ENABLE_DESIGN_SYSTEM_CATALOG === "true")
```

Only include `"design-system"` in `Page`, route parsing, and navigation when the gate is true. Do not add it to server-supplied principal navigation.

Declare `VITE_ENABLE_DESIGN_SYSTEM_CATALOG` in `vite-env.d.ts`.

**Step 3: Implement the catalog shell**

The catalog must provide:

- a semantic `<main id="main-content">`
- section navigation for Foundations, Components, Patterns, Templates, and States
- density switch using `data-density`
- narrow and desktop preview frames
- realistic Rarity labels and long-text examples
- a keyboard/accessibility note beside each custom interaction

Do not make catalog components perform API calls.

**Step 4: Verify**

Run:

```bash
cd frontend
npm test -- src/design-system/catalog/DesignSystemCatalog.test.tsx src/App.test.tsx
VITE_ENABLE_DESIGN_SYSTEM_CATALOG=true npm run build
```

Expected: PASS. Because this is a production-mode Vite build, inspect `dist` and confirm the navigation does not expose a catalog link even though the environment flag was set.

## Phase 2 — Actions, Fields, and Feedback

### Task 4: Implement the action primitives

**Files:**

- Create: `frontend/src/design-system/components/actions/Button.tsx`
- Create: `frontend/src/design-system/components/actions/IconButton.tsx`
- Create: `frontend/src/design-system/components/actions/ButtonGroup.tsx`
- Create: `frontend/src/design-system/components/actions/actions.css`
- Create: `frontend/src/design-system/components/actions/Button.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type ButtonIntent = "primary" | "secondary" | "tertiary" | "danger" | "link";
type ButtonSize = "compact" | "default";

type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  intent?: ButtonIntent;
  size?: ButtonSize;
  loading?: boolean;
  loadingLabel?: string;
  leadingIcon?: React.ComponentType<{ "aria-hidden"?: boolean }>;
};
```

`IconButton` requires `aria-label` and a visible tooltip. `ButtonGroup` preserves DOM order and permits wrapping; it never changes action priority at a breakpoint.

**TDD cases:**

- default button has `type="button"`
- submit type passes through
- loading makes the control disabled, retains its width, and announces `loadingLabel`
- disabled controls expose `aria-describedby` when an explanation ID is supplied
- icon-only button has an accessible name
- focus-visible uses `--rti-focus-ring`
- danger is not the default intent
- axe reports no violations

Run the failing focused test, implement, then run:

```bash
npm test -- src/design-system/components/actions/Button.test.tsx
```

Expected: PASS.

### Task 5: Implement form and secret-field primitives

**Files:**

- Create: `frontend/src/design-system/components/fields/Field.tsx`
- Create: `frontend/src/design-system/components/fields/TextInput.tsx`
- Create: `frontend/src/design-system/components/fields/Select.tsx`
- Create: `frontend/src/design-system/components/fields/Textarea.tsx`
- Create: `frontend/src/design-system/components/fields/Switch.tsx`
- Create: `frontend/src/design-system/components/fields/WriteOnlySecretField.tsx`
- Create: `frontend/src/design-system/components/fields/FormActions.tsx`
- Create: `frontend/src/design-system/components/fields/ValidationSummary.tsx`
- Create: `frontend/src/design-system/components/fields/fields.css`
- Create: `frontend/src/design-system/components/fields/Field.test.tsx`
- Create: `frontend/src/design-system/components/fields/WriteOnlySecretField.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type FieldProps = {
  id: string;
  label: string;
  hint?: React.ReactNode;
  error?: React.ReactNode;
  required?: boolean;
  children: React.ReactElement<{ id?: string; "aria-describedby"?: string; "aria-invalid"?: boolean }>;
};

type WriteOnlySecretFieldProps = {
  id: string;
  label: string;
  configured: boolean;
  configuredLabel?: string;
  replacementLabel?: string;
  name: string;
  required?: boolean;
  error?: string;
};
```

**Behavior contracts:**

- `Field` joins hint and error IDs without overwriting an input's existing description.
- Errors set `aria-invalid` and are associated with the control.
- Required is conveyed in text and semantics.
- `WriteOnlySecretField` never receives, renders, logs, or places an existing secret in the DOM.
- A configured secret displays only “Credential configured” and an empty replacement input.
- Password reveal changes only the local input type and has an explicit accessible label.
- `Switch` uses a native checkbox and visible state text; it does not use a custom ARIA switch unless native semantics become insufficient.
- `ValidationSummary` links errors to field IDs and receives focus only after a failed submit.

Add failing tests for each contract before implementation. Verify:

```bash
npm test -- src/design-system/components/fields
npm run build
```

Expected: PASS.

### Task 6: Implement status, notice, and shared-state components

**Files:**

- Create: `frontend/src/design-system/components/feedback/StatusBadge.tsx`
- Create: `frontend/src/design-system/components/feedback/Notice.tsx`
- Create: `frontend/src/design-system/components/feedback/StatePanel.tsx`
- Create: `frontend/src/design-system/components/feedback/ToastRegion.tsx`
- Create: `frontend/src/design-system/components/feedback/feedback.css`
- Create: `frontend/src/design-system/components/feedback/feedback.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Modify: `frontend/src/main.tsx`

**Public API:**

```ts
type FeedbackTone = "neutral" | "info" | "success" | "warning" | "danger";

type StatePanelProps = {
  state: "loading" | "empty" | "error" | "permission" | "conflict";
  title: string;
  description?: React.ReactNode;
  action?: React.ReactNode;
  details?: React.ReactNode;
};
```

**Behavior contracts:**

- loading uses `role="status"` and readable text, not animation alone
- errors use `role="alert"` only for new blocking failures
- empty states do not use alert semantics
- error states support retry and optional safe detail text
- raw stack, SQL, provider, and network errors are never rendered by the component
- toasts queue in one `aria-live="polite"` region and remain available long enough to read
- badges include visible text; color is never the sole state signal
- initial environment failure in `main.tsx` uses a full-page `StatePanel` with “Retry” that reloads the document

Verify:

```bash
npm test -- src/design-system/components/feedback frontend/src/App.test.tsx
npm run build
```

Expected: PASS.

## Phase 3 — Application Shell and Containers

### Task 7: Extract the authenticated application shell

**Files:**

- Create: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Create: `frontend/src/design-system/components/navigation/GroupedSidebar.tsx`
- Create: `frontend/src/design-system/components/navigation/TopBar.tsx`
- Create: `frontend/src/design-system/components/navigation/navigation.css`
- Create: `frontend/src/design-system/components/navigation/AppShell.test.tsx`
- Create: `frontend/src/navigation.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/app.css`
- Modify: `frontend/src/design-system/index.ts`

**Public API:**

```ts
type NavigationItem = {
  id: string;
  label: string;
  href: string;
  group: "work" | "sales" | "delivery" | "operations" | "administration";
  allowed: boolean;
};

type AppShellProps = {
  brand: React.ReactNode;
  navigation: NavigationItem[];
  activeID: string;
  topBar: React.ReactNode;
  buildLabel: string;
  children: React.ReactNode;
};
```

**Step 1: Characterize the existing shell**

Extend `App.test.tsx` before extraction:

- active route retains `aria-current="page"`
- disallowed server navigation is absent after authenticated bootstrap
- skip link reaches the page's single `main#main-content`
- global create, client selector, and sign-out remain keyboard reachable
- route hash behavior is unchanged

**Step 2: Group navigation data**

Move route labels and groups to `frontend/src/navigation.ts`. Preserve existing route IDs. The mobile shell uses a button with `aria-expanded`, focus restoration, Escape close, and backdrop dismissal.

**Step 3: Extract presentation only**

Keep session loading, directory fetch, capabilities, selected client, logout, notices, and route state in `App.tsx`. Pass already-filtered navigation and event handlers into shell components.

**Step 4: Verify**

```bash
npm test -- src/design-system/components/navigation/AppShell.test.tsx src/App.test.tsx
npm run build
```

Expected: PASS with all existing App tests unchanged except selectors made semantic.

### Task 8: Build page and pane containers

**Files:**

- Create: `frontend/src/design-system/templates/Page.tsx`
- Create: `frontend/src/design-system/templates/page.css`
- Create: `frontend/src/design-system/components/containers/Panel.tsx`
- Create: `frontend/src/design-system/components/containers/Disclosure.tsx`
- Create: `frontend/src/design-system/components/containers/Dialog.tsx`
- Create: `frontend/src/design-system/components/containers/Drawer.tsx`
- Create: `frontend/src/design-system/components/containers/containers.css`
- Create: `frontend/src/design-system/components/containers/containers.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type PageProps = {
  eyebrow?: string;
  title: string;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  tabs?: React.ReactNode;
  children: React.ReactNode;
};

type DialogProps = {
  open: boolean;
  title: string;
  description?: string;
  initialFocusRef?: React.RefObject<HTMLElement | null>;
  onClose: () => void;
  children: React.ReactNode;
  actions?: React.ReactNode;
};
```

**Behavior contracts:**

- page renders one `main#main-content` and associates its heading
- actions wrap without changing order
- Panel accepts `section` or `aside` semantics and an explicit label
- Disclosure uses native `details/summary`
- Dialog traps focus, closes on Escape when dismissible, restores focus, and blocks background pointer/keyboard interaction
- destructive dialogs are never backdrop-dismissible
- Drawer shares focus behavior and becomes full-width at the narrow breakpoint

Add tests for focus entry, Tab wrapping, Escape, restoration, and axe. Verify focused tests and build.

## Phase 4 — Data and Rarity Workflow Patterns

### Task 9: Build table, worklist, filters, and saved-view primitives

**Files:**

- Create: `frontend/src/design-system/components/data/DataTable.tsx`
- Create: `frontend/src/design-system/components/data/Worklist.tsx`
- Create: `frontend/src/design-system/components/data/FilterBar.tsx`
- Create: `frontend/src/design-system/components/data/Pagination.tsx`
- Create: `frontend/src/design-system/components/data/data.css`
- Create: `frontend/src/design-system/components/data/Worklist.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type WorklistProps<T> = {
  items: T[];
  getID: (item: T) => string;
  selectedID?: string;
  getLabel: (item: T) => string;
  renderItem: (item: T, selected: boolean) => React.ReactNode;
  renderDetail: (item: T) => React.ReactNode;
  empty: React.ReactNode;
  onSelect: (item: T) => void;
};
```

**Behavior contracts:**

- semantic table remains a real `<table>` with caption or accessible name
- selected worklist item uses `aria-current`, not a false selected-tab model
- keyboard activation uses native buttons/links
- detail heading receives focus after selection only on narrow list-to-detail navigation
- filters always have visible labels
- active filter count and reset action are available
- pagination describes current range and disables impossible navigation
- saved-view composition accepts state and callbacks but performs no persistence itself
- loading, empty, error, permission, and conflict render through `StatePanel`

Test long labels, zero rows, one row, keyboard selection, narrow detail transition, and axe.

### Task 10: Implement scoped, reason-required, and conflict patterns

**Files:**

- Create: `frontend/src/design-system/patterns/ScopedAction.tsx`
- Create: `frontend/src/design-system/patterns/ReasonRequiredDialog.tsx`
- Create: `frontend/src/design-system/patterns/VersionedEditor.tsx`
- Create: `frontend/src/design-system/patterns/ConflictRecovery.tsx`
- Create: `frontend/src/design-system/patterns/patterns.css`
- Create: `frontend/src/design-system/patterns/action-patterns.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type ScopeLabel = {
  organization: string;
  client?: string;
  record?: string;
};

type ScopedActionProps = {
  scope: ScopeLabel;
  permitted: boolean;
  disabledReason?: string;
  children: React.ReactElement;
};

type ConflictRecoveryProps = {
  expectedVersion: number;
  currentVersion: number;
  onReload: () => void;
  onReview: () => void;
};
```

**Behavior contracts:**

- scope is visible beside consequential actions
- `permitted=false` disables the child action and associates the explanation
- reason cannot be whitespace
- confirmation restates target, action, and scope
- expected version is passed back unchanged to the feature callback
- conflict state never silently retries a mutation
- reload and review choices are explicit
- override intent and evidence are visually distinct from ordinary approval

Tests must assert callback payloads, disabled explanations, reason validation, focus restoration, and conflict choices.

### Task 11: Implement durable-job, connection, approval, and audit patterns

**Files:**

- Create: `frontend/src/design-system/patterns/DurableJobProgress.tsx`
- Create: `frontend/src/design-system/patterns/ConnectionCard.tsx`
- Create: `frontend/src/design-system/patterns/ApprovalDecision.tsx`
- Create: `frontend/src/design-system/patterns/AuditEvidence.tsx`
- Create: `frontend/src/design-system/patterns/operational-patterns.test.tsx`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Public API:**

```ts
type DurableJobState = "queued" | "running" | "succeeded" | "failed" | "cancelled";

type ConnectionCardProps = {
  name: string;
  providerType: string;
  endpoint?: string;
  enabled: boolean;
  health: "unknown" | "healthy" | "degraded" | "failed";
  credentialConfigured: boolean;
  lastCheckedAt?: string;
  actions: React.ReactNode;
};
```

**Behavior contracts:**

- job state has text, timestamps, and optional progress; animation is nonessential
- cancel/retry availability is passed explicitly
- connection UI is provider-agnostic and supports hosted endpoints and local endpoints such as Ollama
- credentials are represented only by configured/not-configured state
- test/discover/enable/replace actions remain separate
- approval records actor, decision, reason, timestamp, and override marker
- audit evidence uses a definition list or table and preserves correlation/version IDs
- safe error text may include a stable error code but never raw provider response bodies

Verify tests and add every state to the catalog.

## Phase 5 — Incremental Feature Migration

### Task 12: Migrate Work as the reference feature

**Files:**

- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/work/GlobalWorkActions.tsx`
- Modify: `frontend/src/features/work/GlobalWorkActions.test.tsx`
- Modify: `frontend/src/features/work/ServiceDeskSettingsPage.tsx`
- Modify: `frontend/src/features/work/ServiceDeskSettingsPage.test.tsx`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/app.css`
- Modify: `frontend/tests/e2e/runnable_foundation.spec.ts`

**Migration order:**

1. Characterize current fetch payloads, claim behavior, filters, saved views, comments, time, attachment, status, and priority mutations.
2. Replace page header with `Page`.
3. Replace filters and list-detail structure with `FilterBar` and `Worklist`.
4. Replace buttons and fields without changing form names or API calls.
5. Replace idle/loading/error text with `StatePanel`, including a real retry callback for load failure.
6. Replace save-view modal with `Dialog`.
7. Wrap claim/status/priority mutations in `ScopedAction`; use `ReasonRequiredDialog` where the existing API requires a reason.
8. Replace global create modal with `Dialog` while preserving the active-client contract.
9. Migrate Service Desk settings panels and form actions.
10. Remove only selectors no longer referenced by Work markup.

**Acceptance tests:**

- authenticated load, selection, claim, status, priority, public/internal comment, time, attachment, filter, and save-view tests pass
- unauthenticated state explains sign-in and contains no false error alert
- load error offers retry and retry calls the list API again
- dialog focus returns to Create/Save View triggers
- axe passes for idle, ready, error, and dialog-open states
- narrow viewport supports list-to-detail navigation without horizontal overflow

Run:

```bash
cd frontend
npm test -- src/features/work src/design-system
npm run build
npx playwright test tests/e2e/runnable_foundation.spec.ts
```

Expected: PASS. If Playwright lacks the required local server/runtime, record that acceptance as pending rather than substituting a unit test.

### Task 13: Migrate Sales and Projects

**Files:**

- Modify: `frontend/src/features/sales/*.tsx`
- Modify: `frontend/src/features/sales/*.test.tsx`
- Modify: `frontend/src/features/sales/sales.css`
- Modify: `frontend/src/features/projects/*.tsx`
- Modify: `frontend/src/features/projects/*.test.tsx`
- Modify: `frontend/src/features/projects/projects.css`
- Modify: `frontend/tests/e2e/opportunity_to_project.spec.ts`
- Modify: `frontend/tests/e2e/change_order.spec.ts`
- Modify: `frontend/tests/e2e/project_capacity.spec.ts`

**Migration order:**

1. Worklists and empty/error states.
2. Opportunity and Project record page headers, metadata, panels, timelines, and action bars.
3. Proposal tables and version rail through `DataTable` and `VersionedEditor`.
4. Opportunity transition and conversion through `ScopedAction`.
5. Change Order approval/override through `ApprovalDecision` and `ReasonRequiredDialog`.
6. Version conflicts through `ConflictRecovery`.
7. Pipeline and capacity views after core record workflows pass.
8. Remove only unreferenced Sales/Projects selectors.

**Acceptance contracts:**

- capability gates and disabled explanations remain unchanged
- proposal/conversion/change-order version values reach existing API callbacks unchanged
- conversion remains previewable and explicit
- override reason and immutable evidence remain visible
- tables retain headers and accessible names at narrow widths
- existing opportunity-to-project and change-order Playwright workflows pass

Run:

```bash
npm test -- src/features/sales src/features/projects src/design-system
npm run build
npx playwright test tests/e2e/opportunity_to_project.spec.ts tests/e2e/change_order.spec.ts tests/e2e/project_capacity.spec.ts
```

Expected: PASS or an explicitly recorded runtime-only Playwright gate.

### Task 14: Migrate integrations, automation, AI, and administration

**Files:**

- Modify: `frontend/src/features/ai/*.tsx`
- Modify: `frontend/src/features/automation/*.tsx`
- Modify: `frontend/src/features/operations/*.tsx`
- Modify: `frontend/src/features/teams/*.tsx`
- Modify: `frontend/src/features/auth/*.tsx`
- Modify: `frontend/src/features/billing/*.tsx`
- Modify: `frontend/src/features/knowledge/*.tsx`
- Modify: `frontend/src/features/organization/*.tsx`
- Modify: `frontend/src/features/setup/*.tsx`
- Modify: corresponding feature tests and CSS files

**Migration order:**

1. AI provider connections using `ConnectionCard` and `WriteOnlySecretField`.
2. AI/automation long-running actions using `DurableJobProgress`.
3. Teams, Datto, Graph, forwarding, and webhook connections using the same provider-agnostic connection pattern.
4. Operations health using status components and retryable `StatePanel`.
5. Sessions, recovery, roles, service keys, and audit using scoped/destructive confirmations and `AuditEvidence`.
6. Billing, knowledge, directory, client resources, pipeline settings, and setup using shared pages, fields, panels, and state components.

**Required AI and connection tests:**

- provider type is data, not a visual or behavior branch in the design-system component
- an Ollama connection can display `http://host.docker.internal:11434` without requiring a secret
- hosted connections can require a write-only replacement secret
- no test fixture passes an existing credential value into React props
- raw response limit text uses the configured 5 MiB default where surfaced
- test connection, discover models, enable, disable, edit, and replace credential are distinguishable actions
- long-running model discovery and generation can remain queued/running without a client timeout being presented as final job failure

Run:

```bash
npm test -- src/features src/design-system
npm run build
```

Expected: PASS.

## Phase 6 — Acceptance and Legacy Removal

### Task 15: Add browser acceptance coverage for the design-system contract

**Files:**

- Create: `frontend/tests/e2e/design_system.spec.ts`
- Modify: `frontend/playwright.config.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Create: `docs/07-ui-ux/design-system-acceptance.md`

**Automated browser matrix:**

- Chromium desktop at 1440×900
- Chromium narrow at 390×844
- 200% emulated zoom/reflow
- reduced-motion media
- forced-colors media where Chromium support is reliable
- keyboard-only catalog traversal
- dialog focus trap/restoration
- shell mobile navigation
- Work list-to-detail
- long labels and validation errors
- loading, empty, error, permission, conflict, and durable-job states

Use Playwright assertions for geometry, accessible names, focus, overflow, and state—not screenshot approval alone. Store screenshots only as supporting evidence at stable viewports.

Create `design-system-acceptance.md` with a table:

```md
| Contract | Automated evidence | Manual evidence | Status |
|---|---|---|---|
| Keyboard traversal | Playwright test name | Date/browser/operator | pending |
```

Leave VoiceOver, NVDA, touch/tablet, 400% zoom, and human visual review pending until performed. Never mark them passed from code inspection.

Run:

```bash
cd frontend
npx playwright test tests/e2e/design_system.spec.ts
```

Expected: PASS in a configured local frontend environment.

### Task 16: Remove legacy duplication and enforce boundaries

**Files:**

- Modify: `frontend/src/app.css`
- Modify: `frontend/src/features/*/*.css`
- Create: `frontend/src/design-system/testing/boundaries.test.ts`
- Modify: `docs/07-ui-ux/design-system.md`
- Modify: `docs/07-ui-ux/design-system-acceptance.md`

**Step 1: Add the boundary test**

The test recursively reads `frontend/src/features` and fails when it finds:

- imports from `design-system/components/**`, `patterns/**`, `templates/**`, or `foundations/**` instead of `design-system`
- new raw hex colors outside approved demo/chart data
- new feature-local `.settings-card`, `.panel-heading`, `.page-heading`, or generic button styling
- inline SVG or emoji used as product icons

Allow an explicit, reviewed exception list in the test for data-visualization colors only.

**Step 2: Prove CSS selectors are unused before removal**

For each feature family:

```bash
rg -n 'className=.*legacy-selector|className="legacy-selector"' frontend/src
```

Expected: no references before deleting the selector. Do not bulk-delete by class-name similarity.

**Step 3: Run the full frontend gate**

```bash
cd frontend
npm test
npm run build
npx playwright test
```

Expected: PASS, except documented external-runtime tests that cannot start locally.

**Step 4: Perform manual acceptance**

Record date, browser/assistive technology, operator, and outcome for:

- keyboard-only traversal
- VoiceOver on macOS
- NVDA on Windows
- 200% and 400% zoom/reflow
- touch/tablet targets
- forced colors/high contrast
- authenticated visual review at desktop and narrow widths

**Step 5: Close the specification**

Only when automated gates pass and required manual rows have evidence:

- change the specification status from `Design specification in review` to `Implemented and accepted`
- record any deferred component or dark-mode work as a named follow-up, not an implicit gap
- inventory remaining raw feature controls and confirm every exception is intentional

## Final Verification Gate

Run from the repository root:

```bash
cd frontend
npm test
npm run build
npx playwright test
cd ..
git diff --check
git status --short
```

Expected:

- Vitest passes.
- TypeScript and Vite production build pass.
- Playwright passes in the configured runtime; any unavailable external service is named precisely.
- `git diff --check` reports no whitespace errors.
- `git status --short` is reviewed only to distinguish this plan's files from the pre-existing uncommitted backend, infrastructure, documentation, and provider-runtime work.
- No Git staging, commit, push, or deployment occurs.

## Execution Checkpoints

After each phase, record:

1. focused tests run and result
2. full frontend test/build result
3. rendered workflows checked
4. manual acceptance still pending
5. pre-existing dirty files preserved

Continue automatically through routine implementation and test failures. Pause only for a security-sensitive contract decision, destructive change, materially changed scope, missing external authority, or a visual choice not resolved by `docs/07-ui-ux/design-system.md`.
