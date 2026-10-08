# Distinct Product Directions Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn Signal Console and Atlas Workspace into structurally different, route-complete operating models and add an explicit synthetic preview dataset for realistic comparison.

**Architecture:** Keep domain logic, permissions, APIs, workspace state, and component primitives shared. Add direction-aware shell regions and page-family composition, then feed both directions the same typed synthetic preview view models only when an explicit local preview flag is enabled.

**Tech Stack:** React 19, TypeScript 7, Vite 8, Vitest, Testing Library, CSS custom properties, Lucide React.

## Global Constraints

- Both directions render every protected route in `routeManifest`.
- Signal is a dense three-pane console with persistent queue context and inspector behavior.
- Atlas is a spatial canvas workspace with an expandable icon rail, prominent tabs, boards, drawers, and AI sidecar.
- Production and normal development never fall back to synthetic data after an API failure.
- Synthetic content is enabled explicitly, visibly labeled, deterministic, and contains no real customer information.
- User-facing raw JSON is prohibited.
- Direction switching preserves route, active client, tabs, preview context, and dirty-state protection.
- Mobile exposes only triage, ticket updates, notes, time entry, approvals, search, and AI chat.
- No new runtime dependency is required.

---

## File structure

- `frontend/src/design-system/foundations/presentation.tsx`: exposes the active direction to nested feature templates without rereading storage.
- `frontend/src/design-system/components/navigation/DirectionShell.tsx`: renders Signal context rail or Atlas icon rail and supplies direction-specific shell landmarks.
- `frontend/src/design-system/components/navigation/AppShell.tsx`: composes the direction shell, shared workspace state, top bar, preview, and AI.
- `frontend/src/design-system/components/navigation/navigation.css`: direction-specific navigation geometry and shell transitions.
- `frontend/src/design-system/workspace/WorkspaceTabs.tsx`: direction-aware tab semantics and visual hierarchy.
- `frontend/src/design-system/workspace/PreviewPane.tsx`: Signal docked inspector and Atlas floating drawer presentation.
- `frontend/src/design-system/workspace/AIRail.tsx`: Signal compact assistant dock and Atlas full-height sidecar.
- `frontend/src/design-system/workspace/workspace.css`: workspace geometry, tabs, previews, AI, and responsive behavior.
- `frontend/src/design-system/templates/DirectionPage.tsx`: shared page-family wrapper that exposes a context rail, view controls, action strip, and canvas regions.
- `frontend/src/design-system/templates/page.css`: Signal and Atlas family-level composition.
- `frontend/src/preview-data/catalog.ts`: deterministic synthetic tenant, records, people, metrics, integrations, sales, projects, billing, and audit summaries.
- `frontend/src/preview-data/PreviewDataProvider.tsx`: explicit preview-mode boundary and typed data access.
- `frontend/src/preview-data/preview-data.css`: visible synthetic-data indicator.
- `frontend/src/features/home/HomePage.tsx`: consumes preview data only through the explicit provider and renders direction-specific operational home composition.
- `frontend/src/features/home/home.css`: visually distinct Signal console and Atlas canvas home layouts.
- `frontend/src/App.tsx`: reads the preview-mode build flag and wraps authenticated content.
- `frontend/src/design-system/testing/routeCoverage.test.ts`: proves all protected routes have direction-aware family coverage.

---

### Task 1: Presentation context and direction shell

**Files:**
- Create: `frontend/src/design-system/foundations/presentation.tsx`
- Create: `frontend/src/design-system/components/navigation/DirectionShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/design-system/components/navigation/navigation.css`
- Modify: `frontend/src/design-system/foundations/tokens.css`
- Modify: `frontend/src/design-system/index.ts`
- Test: `frontend/src/design-system/components/navigation/AppShell.test.tsx`

**Interfaces:**
- Produces: `PresentationProvider`, `usePresentation()`, and `DirectionShell`.
- `usePresentation(): { direction: DesignDirection; density: DensityPreference }`.
- `DirectionShell` consumes navigation, active route, and children; it renders `.rti-signal-context` only for Signal and `.rti-atlas-rail` only for Atlas.

- [ ] **Step 1: Write failing shell-geometry tests**

```tsx
it.each([
  ["signal", "rti-signal-context", "rti-atlas-rail"],
  ["atlas", "rti-atlas-rail", "rti-signal-context"],
] as const)("renders distinct %s shell geometry", (direction, present, absent) => {
  localStorage.setItem(
    "rarity:presentation:principal-1",
    JSON.stringify({ direction, density: "adaptive" }),
  );
  const { container } = renderShell();
  expect(container.querySelector(`.${present}`)).toBeInTheDocument();
  expect(container.querySelector(`.${absent}`)).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run the focused test and confirm failure**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/navigation/AppShell.test.tsx
```

Expected: FAIL because direction-specific shell regions do not exist.

- [ ] **Step 3: Add the presentation context and direction shell**

```tsx
const PresentationContext = createContext<PresentationPreferences | null>(null);

export function PresentationProvider({
  value,
  children,
}: {
  value: PresentationPreferences;
  children: ReactNode;
}) {
  return (
    <PresentationContext.Provider value={value}>
      {children}
    </PresentationContext.Provider>
  );
}

export function usePresentation() {
  const value = useContext(PresentationContext);
  if (!value) throw new Error("usePresentation requires PresentationProvider");
  return value;
}
```

Compose Signal with labeled context navigation and Atlas with a compact icon
rail. Keep all existing navigation actions and permission filtering unchanged.

- [ ] **Step 4: Implement direction-specific shell tokens and geometry**

Signal uses `14.5rem 12.5rem minmax(0, 1fr)` before overlays. Atlas uses
`4.75rem minmax(0, 1fr)`, with an expandable `14rem` rail. Add named CSS
variables for context width, inspector width, Atlas rail width, and direction
motion.

- [ ] **Step 5: Run tests and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/components/navigation/AppShell.test.tsx
git add frontend/src/design-system
git commit -m "Differentiate the direction shells"
```

Expected: focused tests PASS.

### Task 2: Direction-aware multitasking surfaces

**Files:**
- Modify: `frontend/src/design-system/workspace/WorkspaceTabs.tsx`
- Modify: `frontend/src/design-system/workspace/PreviewPane.tsx`
- Modify: `frontend/src/design-system/workspace/AIRail.tsx`
- Modify: `frontend/src/design-system/workspace/workspace.css`
- Test: `frontend/src/design-system/workspace/AIRail.test.tsx`
- Test: `frontend/src/design-system/components/navigation/AppShell.test.tsx`

**Interfaces:**
- Consumes: `usePresentation()` from Task 1.
- Produces: direction-specific `data-mode` attributes on tabs, preview, and AI.
- Preview modes are `"inspector"` for Signal and `"drawer"` for Atlas.
- AI modes are `"dock"` for Signal and `"sidecar"` for Atlas.

- [ ] **Step 1: Write failing behavior and semantics tests**

```tsx
expect(screen.getByLabelText(/preview/i)).toHaveAttribute(
  "data-mode",
  direction === "signal" ? "inspector" : "drawer",
);
expect(screen.getByLabelText("Rarity AI")).toHaveAttribute(
  "data-mode",
  direction === "signal" ? "dock" : "sidecar",
);
```

Also assert that closing either surface returns focus to its trigger.

- [ ] **Step 2: Run the tests and confirm failure**

Run:

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace/AIRail.test.tsx src/design-system/components/navigation/AppShell.test.tsx
```

Expected: FAIL because `data-mode` and focus-return behavior are absent.

- [ ] **Step 3: Implement inspector/drawer and dock/sidecar modes**

Use the shared workspace actions and record context. Do not duplicate state.
Signal preview remains in document flow beside the queue; Atlas preview is a
raised right-edge drawer. Signal AI expands to a compact lower-right dock that
leaves the inspector usable; Atlas AI becomes a full-height sidecar beside the
canvas.

- [ ] **Step 4: Make tabs visibly different**

Signal tabs use compact rectangular task chips below the action strip. Atlas
tabs use a taller browser-like strip with stronger pinned, dirty, and active
surface states. Preserve keyboard reordering and close protection.

- [ ] **Step 5: Run tests and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/workspace src/design-system/components/navigation/AppShell.test.tsx
git add frontend/src/design-system/workspace frontend/src/design-system/components/navigation/AppShell.test.tsx
git commit -m "Differentiate multitasking surfaces"
```

Expected: focused tests PASS.

### Task 3: Page-family composition

**Files:**
- Create: `frontend/src/design-system/templates/DirectionPage.tsx`
- Modify: `frontend/src/design-system/templates/page.css`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/design-system/templates/Page.test.tsx`
- Test: `frontend/src/design-system/testing/routeCoverage.test.ts`

**Interfaces:**
- Consumes: `usePresentation()` and `PageFamily`.
- Produces:

```ts
type DirectionPageProps = {
  family: PageFamily;
  title: string;
  context?: ReactNode;
  actions?: ReactNode;
  viewControls?: ReactNode;
  children: ReactNode;
};
```

- [ ] **Step 1: Write failing family-coverage tests**

```tsx
it.each(["home", "worklist", "record", "settings", "operations", "catalog"])(
  "renders distinct direction composition for %s",
  (family) => {
    const { rerender } = renderDirectionPage("signal", family);
    expect(screen.getByRole("main")).toHaveAttribute("data-layout", "console");
    rerender(renderDirectionPageElement("atlas", family));
    expect(screen.getByRole("main")).toHaveAttribute("data-layout", "canvas");
  },
);
```

Extend route coverage to require every protected route to map to one supported
family in both directions.

- [ ] **Step 2: Run tests and confirm failure**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/templates/Page.test.tsx src/design-system/testing/routeCoverage.test.ts
```

Expected: FAIL because `DirectionPage` is not exported.

- [ ] **Step 3: Implement the family wrapper**

Render one `main` landmark. Signal composes context, sticky actions, dense stage,
and optional inspector slot. Atlas composes canvas header, view controls,
modular stage, and drawer anchor. Direction-specific wrappers must not change
feature permissions or mutation handlers.

- [ ] **Step 4: Apply family-level CSS**

Settings become compact split navigation in Signal and progressive section
cards in Atlas. Worklists and operations receive denser table defaults in
Signal and board/card spacing in Atlas. Record pages receive sticky actions in
Signal and a full workspace canvas in Atlas.

- [ ] **Step 5: Run tests and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/templates src/design-system/testing/routeCoverage.test.ts src/App.test.tsx
git add frontend/src/design-system/templates frontend/src/design-system/index.ts frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "Add direction-aware page composition"
```

Expected: focused tests PASS.

### Task 4: Explicit synthetic preview dataset

**Files:**
- Create: `frontend/src/preview-data/catalog.ts`
- Create: `frontend/src/preview-data/PreviewDataProvider.tsx`
- Create: `frontend/src/preview-data/PreviewDataProvider.test.tsx`
- Create: `frontend/src/preview-data/preview-data.css`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/vite-env.d.ts`

**Interfaces:**
- Produces:

```ts
type PreviewDataCatalog = {
  tenant: PreviewTenant;
  people: PreviewPerson[];
  clients: PreviewClient[];
  work: PreviewWorkRecord[];
  approvals: PreviewApproval[];
  integrationHealth: PreviewIntegrationHealth[];
  sales: PreviewSalesSummary;
  projects: PreviewProjectSummary[];
  audit: PreviewAuditEvent[];
};

function usePreviewData(): {
  enabled: boolean;
  clock: Date;
  catalog?: PreviewDataCatalog;
};
```

- The only activation source is
  `import.meta.env.VITE_RARITY_SYNTHETIC_PREVIEW === "true"`.

- [ ] **Step 1: Write failing explicit-boundary tests**

```tsx
it("does not expose fixtures when preview mode is disabled", () => {
  renderProvider(false);
  expect(screen.queryByText("Synthetic preview data")).not.toBeInTheDocument();
  expect(readPreviewContext().catalog).toBeUndefined();
});

it("exposes stable labeled fixtures when explicitly enabled", () => {
  renderProvider(true);
  expect(screen.getByText("Synthetic preview data")).toBeVisible();
  expect(readPreviewContext().catalog?.tenant.name).toBe(
    "Northstar Managed Services",
  );
  expect(readPreviewContext().catalog?.work).toHaveLength(30);
});
```

- [ ] **Step 2: Run the test and confirm failure**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/preview-data/PreviewDataProvider.test.tsx
```

Expected: FAIL because the provider does not exist.

- [ ] **Step 3: Build the deterministic catalog**

Use stable UUID-shaped IDs and the fixed clock `2026-08-05T14:00:00Z`.
Include four synthetic clients, twelve people, thirty work records, pending and
approved time, integration failures and healthy states, opportunities,
projects, billing review, automation runs, sessions, service keys, webhooks,
directory records, client resources, and audit events. Include critical,
warning, healthy, empty, and overloaded examples.

- [ ] **Step 4: Add the provider and visible indicator**

The provider supplies fixtures only when the explicit flag is true. It does not
intercept `fetch`, catch API failures, or replace rejected requests. Mount a
small persistent shell indicator containing the exact text “Synthetic preview
data”.

- [ ] **Step 5: Run tests and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/preview-data src/App.test.tsx
git add frontend/src/preview-data frontend/src/App.tsx frontend/src/vite-env.d.ts
git commit -m "Add explicit synthetic preview data"
```

Expected: focused tests PASS.

### Task 5: Realistic direction-specific home and work comparison

**Files:**
- Modify: `frontend/src/features/home/HomePage.tsx`
- Modify: `frontend/src/features/home/home.css`
- Modify: `frontend/src/features/home/HomePage.test.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`

**Interfaces:**
- Consumes: `usePreviewData()`, `usePresentation()`, existing work API and
  workspace preview actions.
- Produces: same work dataset rendered as a Signal queue/inspector and an Atlas
  board/canvas, with a shared table/board view switch.

- [ ] **Step 1: Write failing direction and fixture tests**

```tsx
it("renders the Signal operational queue from explicit preview data", () => {
  renderHome({ direction: "signal", preview: true });
  expect(screen.getByLabelText("Signal live queue")).toBeVisible();
  expect(screen.queryByLabelText("Atlas work canvas")).not.toBeInTheDocument();
});

it("renders the Atlas work canvas from the same preview records", () => {
  renderHome({ direction: "atlas", preview: true });
  expect(screen.getByLabelText("Atlas work canvas")).toBeVisible();
  expect(screen.getByText("Password resets failing after policy rollout")).toBeVisible();
});
```

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/features/home/HomePage.test.tsx src/features/work/TechnicianWorklist.test.tsx
```

Expected: FAIL because the distinct regions do not exist.

- [ ] **Step 3: Implement Signal home and worklist**

Use a compact priority queue, SLA strip, selected-row treatment, client/context
rail, and persistent inspector affordance. Keep primary actions visible in a
sticky strip.

- [ ] **Step 4: Implement Atlas home and worklist**

Use a larger canvas with grouped workload cards, a four-lane Kanban summary,
schedule and health modules, drawer previews, and a clearly visible table/board
switch. Drag and drop may update local preview state only; live mutations remain
bound to supported API commands.

- [ ] **Step 5: Run tests and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/features/home src/features/work
git add frontend/src/features/home frontend/src/features/work
git commit -m "Differentiate home and work experiences"
```

Expected: focused tests PASS.

### Task 6: Route-wide visual migration and design-system catalog

**Files:**
- Modify: `frontend/src/app.css`
- Modify: `frontend/src/features/*/*.css`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`
- Modify: `frontend/src/design-system/catalog/catalog.css`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.test.tsx`
- Modify: `frontend/src/design-system/testing/noRawJson.test.ts`
- Modify: `frontend/src/design-system/testing/routeCoverage.test.ts`

**Interfaces:**
- Consumes: direction attributes and page-family composition from Tasks 1–3.
- Produces: route-complete visual differentiation without changing domain APIs.

- [ ] **Step 1: Extend route-wide failing assertions**

Assert all protected route IDs render with:

```ts
expect(main).toHaveAttribute(
  "data-layout",
  direction === "signal" ? "console" : "canvas",
);
```

Catalog tests must find examples of the Signal context rail, Atlas icon rail,
Signal inspector, Atlas drawer, both AI modes, table, Kanban, date picker,
combobox, textarea, guided builders, loading, empty, error, and conflict states.

- [ ] **Step 2: Run coverage and catalog tests**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/testing src/design-system/catalog
```

Expected: FAIL until every family and catalog example is represented.

- [ ] **Step 3: Migrate feature surfaces by family**

Apply direction-aware CSS to sales, projects, operations, automation, billing,
knowledge, organization, authentication administration, setup, Teams, and AI.
Signal reduces decorative cards, tightens rows, and uses pinned regions. Atlas
groups existing sections into raised canvas modules with comfortable spacing
and drawer-compatible edges. Preserve selectors relied on by existing tests.

- [ ] **Step 4: Expand the catalog and raw-JSON guard**

Show both directions side by side for shell, navigation, page families,
multitasking, fields, builders, data views, and feedback. Extend the static raw
JSON scan to all migrated TSX routes and visible labels.

- [ ] **Step 5: Run route coverage and commit**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test -- src/design-system/testing src/design-system/catalog src/App.test.tsx
git add frontend/src
git commit -m "Complete the direction-wide visual migration"
```

Expected: focused tests PASS.

### Task 7: Verification and preview handoff

**Files:**
- Modify only files required by failures discovered during verification.

**Interfaces:**
- Consumes: completed Tasks 1–6.
- Produces: passing test/build evidence and a running local synthetic preview.

- [ ] **Step 1: Format and inspect the diff**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm run format
git diff --check
git status --short
```

Expected: no whitespace errors; only intended files changed.

- [ ] **Step 2: Run the complete frontend suite**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm test
```

Expected: all tests PASS.

- [ ] **Step 3: Run the production build**

```bash
docker run --rm -v "$PWD/frontend:/app" -w /app node:22-bookworm npm run build
```

Expected: TypeScript and Vite build PASS. A bundle-size warning is non-blocking
unless a new chunk materially regresses the existing baseline.

- [ ] **Step 4: Start the explicit synthetic preview**

Set `VITE_RARITY_SYNTHETIC_PREVIEW=true` only in the local preview environment,
rebuild the frontend container, and verify health and readiness with existing
Compose probes. Do not modify tracked production environment defaults.

- [ ] **Step 5: Commit verification fixes**

```bash
git add frontend/src
git commit -m "Verify distinct product directions"
```

Skip the commit if formatting and verification produce no tracked changes.
