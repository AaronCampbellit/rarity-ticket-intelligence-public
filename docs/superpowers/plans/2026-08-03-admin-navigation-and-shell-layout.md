# Admin Navigation and Shell Layout Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat Administration navigation with an accessible, categorized Admin disclosure and fix the sidebar and narrow-layout shell defects.

**Architecture:** Keep route ownership in `navigation.ts` by assigning administration routes to explicit Admin subgroups. `GroupedSidebar` owns disclosure state and nested rendering, while the existing `AppShell` continues to own the mobile drawer. CSS gives the sidebar one continuous scrollable surface and explicitly places the workspace in grid row one at the narrow breakpoint.

**Tech Stack:** React 19, TypeScript 7, CSS, Vitest, Testing Library, Playwright 1.62.

## Global Constraints

- Preserve the existing uncommitted local-administrator guard changes; do not stage or revert them.
- Do not use subagents or delegation; execute inline with `superpowers:executing-plans`.
- Admin is collapsed by default and expands automatically whenever an enclosed route is active.
- Keep AI assist, AI settings, integration health, Datto reconciliation, and forwarding intake outside Admin.
- Keep every nested destination as a normal link with visible focus and `aria-current="page"`.
- Close the narrow navigation drawer after selecting a nested link.
- At widths below 760 pixels, the workspace begins at viewport row one.
- Do not add dependencies.

---

### Task 1: Encode Admin subgroup ownership

**Files:**
- Modify: `frontend/src/design-system/components/navigation/GroupedSidebar.tsx`
- Modify: `frontend/src/navigation.ts`
- Create: `frontend/src/navigation.test.ts`

**Interfaces:**
- Produces: `NavigationGroup` values `admin-access`, `admin-platform`, `admin-organization`, and `admin-integrations`.
- Produces: `navigationGroup(page: Page): NavigationGroup`, retaining the existing public signature.
- Consumes: `App.tsx` continues passing `group: navigationGroup(item.page)` without modification.

- [ ] **Step 1: Write the failing route-ownership test**

Create `frontend/src/navigation.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { navigationGroup } from "./navigation";

describe("navigationGroup", () => {
  it.each([
    ["sessions", "admin-access"],
    ["recovery-access", "admin-access"],
    ["role-settings", "admin-access"],
    ["audit", "admin-access"],
    ["setup", "admin-platform"],
    ["service-keys", "admin-platform"],
    ["billing", "admin-platform"],
    ["knowledge", "admin-organization"],
    ["directory-settings", "admin-organization"],
    ["client-resources", "admin-organization"],
    ["teams-settings", "admin-integrations"],
    ["datto-settings", "admin-integrations"],
    ["graph-settings", "admin-integrations"],
    ["webhooks", "admin-integrations"],
    ["automation", "admin-integrations"],
  ] as const)("places %s in %s", (page, group) => {
    expect(navigationGroup(page)).toBe(group);
  });

  it.each([
    ["ai-assist", "operations"],
    ["ai-settings", "operations"],
    ["operations", "operations"],
    ["datto-reconciliation", "operations"],
    ["forwarding-settings", "operations"],
  ] as const)("keeps %s in %s", (page, group) => {
    expect(navigationGroup(page)).toBe(group);
  });
});
```

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
npm --prefix frontend test -- --run src/navigation.test.ts
```

Expected: FAIL because current administration routes return `administration` and provider routes return `operations`.

- [ ] **Step 3: Add explicit Admin groups**

In `GroupedSidebar.tsx`, replace the final `NavigationGroup` member with:

```ts
  | "admin-access"
  | "admin-platform"
  | "admin-organization"
  | "admin-integrations";
```

In `navigation.ts`, map the exact route sets before the current Operations branch:

```ts
  if (["sessions", "recovery-access", "role-settings", "audit"].includes(page)) {
    return "admin-access";
  }
  if (["setup", "service-keys", "billing"].includes(page)) {
    return "admin-platform";
  }
  if (["knowledge", "directory-settings", "client-resources"].includes(page)) {
    return "admin-organization";
  }
  if (
    [
      "teams-settings",
      "datto-settings",
      "graph-settings",
      "webhooks",
      "automation",
    ].includes(page)
  ) {
    return "admin-integrations";
  }
```

Remove those routes from the existing Operations branch. Map the development-only `design-system` route to `admin-platform`.

- [ ] **Step 4: Run the route test and verify GREEN**

Run:

```bash
npm --prefix frontend test -- --run src/navigation.test.ts
```

Expected: 20 parameterized cases pass.

- [ ] **Step 5: Commit the route model**

```bash
git add frontend/src/navigation.ts frontend/src/navigation.test.ts frontend/src/design-system/components/navigation/GroupedSidebar.tsx
git commit -m "refactor(ui): categorize admin navigation routes"
```

### Task 2: Render the accessible Admin disclosure

**Files:**
- Modify: `frontend/src/design-system/components/navigation/GroupedSidebar.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.test.tsx`
- Modify: `frontend/src/design-system/components/navigation/navigation.css`

**Interfaces:**
- Consumes: the four Admin `NavigationGroup` values from Task 1.
- Produces: a button named `Admin` with `aria-expanded` and `aria-controls="rti-admin-navigation"`.
- Produces: nested subgroup headings `Access`, `Platform`, `Organization`, and `Integrations`.

- [ ] **Step 1: Replace the administration fixture and write failing disclosure tests**

Expand the `navigation` fixture in `AppShell.test.tsx` with one item from every Admin subgroup. Replace the Administration-heading assertion with:

```ts
const admin = screen.getByRole("button", { name: "Admin" });
expect(admin).toHaveAttribute("aria-expanded", "false");
expect(screen.queryByRole("link", { name: "Audit" })).not.toBeInTheDocument();

fireEvent.click(admin);
expect(admin).toHaveAttribute("aria-expanded", "true");
expect(screen.getByRole("heading", { name: "Access" })).toBeVisible();
expect(screen.getByRole("heading", { name: "Platform" })).toBeVisible();
expect(screen.getByRole("heading", { name: "Organization" })).toBeVisible();
expect(screen.getByRole("heading", { name: "Integrations" })).toBeVisible();
expect(screen.getByRole("link", { name: "Audit" })).toBeVisible();
```

Add a second test rendering `activeID="audit"` and assert that Admin begins expanded and Audit has `aria-current="page"`.

- [ ] **Step 2: Run the component test and verify RED**

Run:

```bash
npm --prefix frontend test -- --run src/design-system/components/navigation/AppShell.test.tsx
```

Expected: FAIL because Admin is not a button and there is no disclosure region.

- [ ] **Step 3: Implement disclosure rendering**

In `GroupedSidebar.tsx`:

- import `ChevronDown` from `lucide-react`;
- import `useEffect` and `useState`;
- keep the four non-Admin groups in the top-level `groupOrder`;
- define `adminGroupOrder` and labels for the four nested groups;
- compute `adminItems` and `activeAdmin`;
- initialize `adminOpen` from `activeAdmin`;
- use an effect to open Admin whenever `activeAdmin` becomes true;
- render the Admin button and controlled region after the non-Admin groups.

The button contract is:

```tsx
<button
  type="button"
  className="rti-admin-navigation__trigger"
  aria-expanded={adminOpen}
  aria-controls="rti-admin-navigation"
  onClick={() => setAdminOpen((current) => !current)}
>
  <span>Admin</span>
  <ChevronDown size={16} aria-hidden="true" />
</button>
```

The controlled region must use `id="rti-admin-navigation"`, render only while open, group links under `<h3>` labels, preserve the existing active-link logic, and call both `item.onSelect?.()` and `onNavigate()` when selected.

- [ ] **Step 4: Add disclosure styles**

In `navigation.css`, add:

```css
.rti-admin-navigation__trigger {
  display: flex;
  width: 100%;
  min-height: 44px;
  align-items: center;
  justify-content: space-between;
  padding: 11px 13px;
  color: var(--rti-text-inverse);
  background: transparent;
  border: 0;
  border-radius: var(--rti-radius-control);
  font: inherit;
  font-weight: 700;
  text-align: left;
}

.rti-admin-navigation__trigger:hover {
  background: var(--rti-surface-navigation-hover);
}

.rti-admin-navigation__trigger svg {
  transition: transform var(--rti-motion-standard);
}

.rti-admin-navigation__trigger[aria-expanded="true"] svg {
  transform: rotate(180deg);
}

.rti-admin-navigation__content {
  display: grid;
  gap: var(--rti-space-2);
  padding-left: var(--rti-space-2);
}

.rti-admin-navigation__content h3 {
  margin: var(--rti-space-2) var(--rti-space-3) 0;
  color: #92979f;
  font-size: 0.625rem;
  letter-spacing: 0.09em;
  text-transform: uppercase;
}
```

- [ ] **Step 5: Run component tests and verify GREEN**

Run:

```bash
npm --prefix frontend test -- --run src/design-system/components/navigation/AppShell.test.tsx
```

Expected: disclosure, active-route, drawer Escape, and accessibility tests pass.

- [ ] **Step 6: Commit the disclosure**

```bash
git add frontend/src/design-system/components/navigation/GroupedSidebar.tsx frontend/src/design-system/components/navigation/AppShell.test.tsx frontend/src/design-system/components/navigation/navigation.css
git commit -m "feat(ui): collapse administration into admin navigation"
```

### Task 3: Repair sidebar continuity and narrow shell placement

**Files:**
- Modify: `frontend/src/app.css`
- Modify: `frontend/tests/e2e/design_system.spec.ts`

**Interfaces:**
- Consumes: `.rarity-sidebar`, `.rarity-sidebar nav`, `.rarity-workspace`, and the 760-pixel breakpoint.
- Produces: a full-height dark sidebar with an independently scrolling navigation region.
- Produces: workspace `top === 0` at a 730-by-800 viewport.

- [ ] **Step 1: Add failing rendered layout assertions**

In `design_system.spec.ts`, add a test that sets `{ width: 730, height: 800 }`, opens `/#/recovery-access`, and asserts:

```ts
const metrics = await page.evaluate(() => {
  const workspace = document.querySelector(".rarity-workspace")!.getBoundingClientRect();
  const topbar = document.querySelector(".rarity-topbar")!.getBoundingClientRect();
  return { workspaceTop: workspace.top, topbarTop: topbar.top };
});
expect(metrics).toEqual({ workspaceTop: 0, topbarTop: 0 });
```

At `{ width: 1440, height: 900 }`, open the navigation route with the longest Admin contents, expand Admin, and assert:

```ts
const sidebar = page.locator("#rti-primary-sidebar");
await expect(sidebar).toHaveCSS("background-color", "rgb(9, 9, 10)");
expect((await sidebar.boundingBox())?.height).toBe(900);
await expect(sidebar.locator("nav")).toHaveCSS("overflow-y", "auto");
```

- [ ] **Step 2: Run the focused Playwright test and verify RED**

Run:

```bash
npm --prefix frontend exec playwright test tests/e2e/design_system.spec.ts
```

Expected: FAIL with `workspaceTop: 800` at 730-by-800 and `overflow-y: visible` on the sidebar navigation.

- [ ] **Step 3: Implement the shell CSS repair**

In `app.css`, make the navigation region scroll within the full-height sidebar:

```css
.rarity-sidebar {
  overflow: hidden;
}

.rarity-sidebar nav {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}
```

Inside `@media (max-width: 760px)`, explicitly place the in-flow workspace:

```css
.rarity-workspace {
  grid-column: 1;
  grid-row: 1;
}
```

- [ ] **Step 4: Run the focused Playwright test and verify GREEN**

Run:

```bash
npm --prefix frontend exec playwright test tests/e2e/design_system.spec.ts
```

Expected: the workspace/top bar start at zero, the sidebar remains dark for 900 pixels, its navigation scrolls, the drawer opens/closes, and touch tests pass.

- [ ] **Step 5: Run the complete frontend gate**

Run:

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
npm --prefix frontend exec playwright test
git diff --check
```

Expected: 54 or more Vitest files pass, the Vite production build succeeds, all configured Playwright tests pass, and `git diff --check` prints no errors.

- [ ] **Step 6: Perform Browser-plugin rendered QA**

Using the in-app Browser:

1. Open `http://127.0.0.1:4173/#/recovery-access`.
2. At desktop width, verify the dark surface remains continuous while Admin expands and scrolls.
3. Verify Admin is collapsed on a non-Admin route and automatically open on Local administrators.
4. Select an Admin nested link and verify the active route/link.
5. At 730-by-800, verify `.rarity-workspace` and `.rarity-topbar` both have `top: 0`.
6. At 390-by-844, open the drawer, expand Admin, select a nested link, and verify the drawer closes.
7. Capture desktop and narrow screenshots and confirm no relevant console warnings or errors.

- [ ] **Step 7: Commit the layout repair**

```bash
git add frontend/src/app.css frontend/tests/e2e/design_system.spec.ts
git commit -m "fix(ui): keep sidebar and narrow shell aligned"
```

### Task 4: Validate documentation and final scope

**Files:**
- Modify only if validation requires it: `docs-site/app.js`

**Interfaces:**
- Consumes: the approved design and completed Tasks 1–3.
- Produces: evidence that documentation, code, and rendered behavior agree.

- [ ] **Step 1: Run documentation and diff validation**

```bash
node --check docs-site/app.js
node scripts/validate-docs.mjs
node scripts/validate-integration-automation-docs.mjs
git diff --check
```

Expected: 164 or more site documents are present, integration/automation contracts validate, and no whitespace errors are reported.

- [ ] **Step 2: Audit repository scope**

```bash
git status --short
git diff --stat
git log -4 --oneline
```

Confirm the existing local-administrator guard changes remain present and were not accidentally included in sidebar commits. Confirm no deployment, provider configuration, administrator reset, or production mutation occurred.

- [ ] **Step 3: Report acceptance boundaries**

Report separately:

- portable Vitest/build result;
- Playwright rendered result;
- in-app Browser desktop/narrow result;
- commit state;
- demo deployment state;
- first-install bootstrap redesign state.
