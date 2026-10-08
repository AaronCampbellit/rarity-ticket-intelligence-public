# Build Roadmap Status Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the obsolete product-horizon roadmap with the approved Option 1 execution-status board and verify its content, responsiveness, and visual fidelity.

**Architecture:** Keep the documentation site static and make `docs/09-roadmap/implementation-plan.md` the canonical narrative. Render a semantic Phase 0–8 status board and grouped release gates directly in `docs-site/index.html`, style them in the existing token system, and extend the existing documentation validator with stable content-contract checks.

**Tech Stack:** Semantic HTML5, existing vanilla CSS, Node.js standard-library validation, Codex in-app Browser.

## Global Constraints

- Preserve the existing documentation-site navigation, `#roadmap` anchor, design tokens, typography, and near-black/lime visual language.
- Show no percentages, target dates, or claims inferred from an unverified deployment.
- Classify Phases 0–5 as `Complete` and Phases 6–8 as `Active acceptance`.
- State explicitly that Phase 6–8 source slices are implemented while acceptance remains active.
- Convey status with text and icon shape, never color alone.
- Keep `docs/09-roadmap/implementation-plan.md` as the source of truth.
- Do not add runtime APIs, Markdown fetching, new routes, gradients, glass effects, or custom image assets.

---

### Task 1: Roadmap content contract and semantic markup

**Files:**
- Modify: `scripts/validate-docs.mjs`
- Modify: `docs-site/index.html`
- Modify: `docs-site/app.js`

**Interfaces:**
- Consumes: canonical phase titles and current evidence from `docs/09-roadmap/implementation-plan.md`
- Produces: `#roadmap`, nine `.roadmap-phase` articles with `data-state`, two `.roadmap-phase-column` groups, and six gate items with `data-gate-state` for CSS and browser verification

- [ ] **Step 1: Add a failing roadmap contract to the docs validator**

Read `docs-site/index.html` as `siteIndex` and add checks for:

```js
const requiredRoadmapPhases = Array.from(
  { length: 9 },
  (_, phase) => `data-phase="${phase}"`,
);
const requiredRoadmapStates = [
  'data-state="complete"',
  'data-state="active-acceptance"',
  'data-gate-state="needs-human"',
  'data-gate-state="blocked-environment"',
];
const forbiddenRoadmapCopy = [
  "100% complete",
  "Technician platform",
  "Client + intelligence",
  "MSP operating system",
];
```

Fail with a `roadmap status board contract invalid` message when a required
marker is absent or forbidden copy remains.

- [ ] **Step 2: Run the validator and confirm the new contract fails**

Run:

```bash
node scripts/validate-docs.mjs
```

Expected: non-zero exit with `roadmap status board contract invalid`.

- [ ] **Step 3: Replace the old roadmap cards with semantic Option 1 markup**

In `docs-site/index.html`, preserve `section#roadmap` and add:

```html
<div class="roadmap-summary" aria-label="Build status summary">
  <div class="roadmap-summary-state">
    <strong>Source implementation complete · Acceptance in progress</strong>
    <span>Phases 0–5 complete. Phases 6–8 remain in active acceptance.</span>
  </div>
</div>
```

Add Phase 0–8 entries using `article.roadmap-phase`, `data-phase="N"`, and
either `data-state="complete"` or `data-state="active-acceptance"`. Each article
contains its phase number, canonical title, visible status label, delivery
summary, evidence or remaining-gate summary, and a descriptive link to
`../docs/09-roadmap/implementation-plan.md`.

Add six completion-gate items and use these exact state hooks where applicable:

```html
<li data-gate-state="needs-human">
<li data-gate-state="blocked-environment">
```

The six items are GUI review, external provider validation, accessibility
acceptance, supported Ubuntu profile, HA/restore/telemetry/capacity, and signed
release artifacts. Their descriptions retain the detailed human and
environment dependencies.
End the board with a visible note that Markdown remains the source of truth and
the board reflects the latest recorded evidence.

- [ ] **Step 4: Add the approved spec and plan to the site document manifest**

Add these exact paths to `documentPaths` in `docs-site/app.js`:

```text
docs/superpowers/specs/2026-08-02-build-roadmap-status-design.md
docs/superpowers/plans/2026-08-02-build-roadmap-status.md
```

- [ ] **Step 5: Run the validator and confirm the content contract passes**

Run:

```bash
node scripts/validate-docs.mjs
node scripts/validate-markdown-links.mjs
```

Expected: both commands exit zero.

- [ ] **Step 6: Commit the semantic board and contract**

```bash
git add scripts/validate-docs.mjs docs-site/index.html docs-site/app.js
git commit -m "feat: replace roadmap with execution status board"
```

### Task 2: Responsive execution-board styling

**Files:**
- Modify: `docs-site/styles.css`

**Interfaces:**
- Consumes: `.roadmap-summary`, `.roadmap-phase-list`, `.roadmap-phase-column`, `.roadmap-phase`, `.phase-status`, and `.gate-panel` markup from Task 1
- Produces: desktop two-region hierarchy, tablet stacking, mobile single-column reflow, and accessible focus/contrast states

- [ ] **Step 1: Add the desktop Option 1 layout**

Create CSS using existing tokens:

```css
.roadmap-summary {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 220px;
}
.roadmap-phase-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.gate-panel ol {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
}
```

Use `var(--night)`, `var(--panel)`, `var(--panel-2)`, `var(--line)`,
`var(--ink)`, `var(--muted)`, `var(--lime)`, `var(--cyan)`, and `var(--amber)`.
Do not add gradients. Complete, active-acceptance, needs-human, and
blocked-environment styles must retain visible text labels and distinct icon
shapes.

- [ ] **Step 2: Add tablet and mobile reflow**

At `max-width: 1050px`, keep phase entries in two columns and reflow gates to
three columns. At `max-width: 850px`, stack the phase columns and reflow gates
to two columns so a 720-pixel viewport, equivalent to 200% reflow from the
desktop target, retains usable content width. At `max-width: 700px`, make the
summary and gates single-column, reduce padding, allow status labels to wrap,
and prevent horizontal overflow.

- [ ] **Step 3: Preserve accessibility preferences**

Keep link focus on the global `:focus-visible` rule, give roadmap links at least
44px of usable block height where practical, and ensure decorative status marks
are `aria-hidden` in HTML. Add no essential animation; any hover transition
must be disabled inside the existing `prefers-reduced-motion: reduce` query.

- [ ] **Step 4: Run static verification**

Run:

```bash
node scripts/validate-docs.mjs
node scripts/validate-markdown-links.mjs
git diff --check
```

Expected: all commands exit zero.

- [ ] **Step 5: Commit the responsive styling**

```bash
git add docs-site/styles.css
git commit -m "style: add responsive roadmap status layout"
```

### Task 3: Browser acceptance and design QA

**Files:**
- Create: `design-qa.md`
- Modify as needed after visual comparison: `docs-site/index.html`
- Modify as needed after visual comparison: `docs-site/styles.css`

**Interfaces:**
- Consumes: approved Option 1 image and completed local documentation site
- Produces: same-viewport screenshots, interaction evidence, and `design-qa.md` with `final result: passed`

- [ ] **Step 1: Start the local documentation site**

Run:

```bash
python3 -m http.server 18083 --bind 127.0.0.1 --directory docs-site
```

Keep the process running for browser acceptance.

- [ ] **Step 2: Capture the same desktop state as the reference**

Use the Codex in-app Browser at:

```text
http://127.0.0.1:18083/index.html#roadmap
```

Set a 1440×1024 viewport, reload, inspect the console, and save a screenshot of
the complete roadmap section.

- [ ] **Step 3: Compare reference and implementation**

Open the approved Option 1 image:

```text
/Users/aaroncampbell/.codex/generated_images/019fadab-9ee7-7170-b198-c61ddb0f8906/call_GIrBzXcBV5zXpnVBeo0DMdqU.png
```

Open the implementation screenshot at the same time and compare hierarchy,
spacing, typography, card density, border treatment, status readability, and
the right-side completion-gates panel.

- [ ] **Step 4: Exercise responsive and keyboard states**

Verify:

- 1440×1024 desktop;
- tablet width near 900px;
- mobile width near 390px;
- browser zoom at 200%;
- grayscale and forced-colors/high-contrast readability;
- Tab focus through every roadmap link;
- no horizontal page scrolling;
- descriptive links resolve; and
- no new browser-console errors.

- [ ] **Step 5: Record and fix design QA**

Create `design-qa.md` with sections for reference, captured viewport, findings,
fixes, remaining P3 polish, and the exact line:

```text
final result: passed
```

If any P0/P1/P2 issue exists, record it, fix it, recapture, and repeat the
comparison before marking the result passed.

- [ ] **Step 6: Run final verification**

Run:

```bash
node scripts/validate-docs.mjs
node scripts/validate-markdown-links.mjs
git diff --check
git status --short
```

Expected: validators and whitespace check pass; status shows only intentional
roadmap QA changes.

- [ ] **Step 7: Commit verified implementation evidence**

```bash
git add design-qa.md docs-site/index.html docs-site/styles.css
git commit -m "test: record roadmap design acceptance"
```
