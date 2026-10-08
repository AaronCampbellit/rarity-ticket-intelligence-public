# Build Roadmap Status Design

**Status:** Approved visual direction; written specification awaiting review

## Purpose

Replace the obsolete M0/V1/V2/Future roadmap cards in the local documentation
site with an execution-status board that answers three questions immediately:

1. Which implementation phases are complete?
2. Which acceptance and integration work is active now?
3. What requires human review or an external environment before release?

The board must describe evidence honestly. It must not use percentages or imply
that implemented source contracts are production-ready when live acceptance is
still outstanding.

## Selected visual direction

The selected design is **Option 1 — Execution Status Board**.

It preserves the documentation site's existing near-black background, warm
white serif headings, lime accent, compact uppercase labels, restrained borders,
and existing top navigation. The section becomes denser and more operational
than the former product-horizon cards while remaining part of the existing
single-page documentation site.

The section contains:

- a heading and short explanation of the status model;
- a compact status banner stating that source implementation is complete while
  acceptance remains active;
- a primary Phase 0–8 execution board;
- a prioritized next-completion-gates panel; and
- direct links to the canonical implementation plan and relevant acceptance
  documents.

## Status model

Every state uses a text label in addition to color.

- **Complete** means the phase's planned source delivery and currently required
  evidence are recorded as complete.
- **Active acceptance** means the source slice is implemented but one or more
  real-environment, accessibility, operational, or release gates remain.
- **Needs human** means progress requires human judgment, physical interaction,
  credentials, or sign-off.
- **Blocked by environment** means the necessary supported platform, external
  service, production-like topology, or publish pipeline is unavailable.

The initial board classifies Phases 0–5 as **Complete** and Phases 6–8 as
**Active acceptance**. Phase 6–8 cards must explicitly say that their source
slices are implemented so the active state is not mistaken for missing product
code.

The summary states that Phases 0–5 are complete and Phases 6–8 remain in active
acceptance. It includes the evidence update date but no percentage.

## Phase board

The primary board uses nine compact phase rows grouped into two bordered
columns: Phases 0–4 on the left and Phases 5–8 on the right. It becomes one
column at narrow widths. Each phase shows:

- phase number and short title;
- status text and status icon;
- one-sentence delivered scope;
- one-sentence evidence or remaining-gate summary; and
- a link to the canonical plan section or supporting evidence.

The phase titles and classifications are:

| Phase | Title | Board state |
| --- | --- | --- |
| 0 | Foundation approval and delivery baseline | Complete |
| 1 | Secure platform kernel | Complete |
| 2 | Work management | Complete |
| 3 | Workflow, SLA, and technician experience | Complete |
| 4 | Native PSA sales | Complete |
| 5 | Native PSA project delivery | Complete |
| 6 | Intake and integration | Active acceptance |
| 7 | Automation and intelligence | Active acceptance |
| 8 | Production operations and release readiness | Active acceptance |

The titles may be shortened in the rendered cards when necessary, but their
meaning and phase numbers must remain aligned with
`docs/09-roadmap/implementation-plan.md`.

## Next completion gates

The secondary panel follows the phase columns and presents six compact
completion gates in one horizontal row on wide screens. It orders remaining
work by the action needed, not by an invented completion date.

The six gates are:

1. GUI review — needs human;
2. external provider validation — blocked by configured environments;
3. accessibility acceptance covering zoom, high contrast, touch, VoiceOver,
   and NVDA — needs human;
4. supported Ubuntu 24.04 profile validation — blocked by environment;
5. HA, restore, telemetry, and realistic-capacity evidence — blocked by
   production-like infrastructure; and
6. published SBOM, provenance, attestation, and signature acceptance — blocked
   until a candidate and signing infrastructure exist.

A gate can change state as its dependency changes. The board shows only the
current evidence state; it does not preserve status history.

## Source of truth and update behavior

`docs/09-roadmap/implementation-plan.md` remains the canonical narrative and
evidence source. The documentation-site board is a concise manually maintained
projection of that file, not a second planning system.

The rendered board links to the implementation plan and acceptance documents.
It does not fetch Markdown, call an API, or calculate readiness from test output
at runtime. A short note states that Markdown remains the source of truth and
that the board reflects the latest recorded evidence.

When a phase or gate changes, the implementation-plan entry is updated first,
then the board's state, summary counts, detail text, and updated date are changed
in the same coherent revision.

## Interaction and responsive behavior

The board is primarily informational. Its only controls are normal document
links. Links must have visible hover and keyboard-focus states and descriptive
text such as **View phase evidence** rather than an unlabeled arrow.

At wide desktop widths, the phase board uses two equal regions and the
completion-gates panel spans their full width below. At tablet widths, phase
entries remain in two columns only while their content remains readable and
gates reflow to three or two columns. At mobile widths, all content becomes a
single column without horizontal scrolling.

The existing sticky navigation and `#roadmap` anchor remain unchanged.

## Accessibility

- Status is conveyed through text and icon shape, never color alone.
- Heading levels preserve the page hierarchy.
- Phase entries use semantic articles or list items; gate groups use named
  sections and semantic lists.
- Links meet the existing focus-visible treatment and have adequate touch
  targets.
- Text and status labels reflow at 200% zoom without clipping or horizontal
  page scrolling.
- Lime, cyan, amber, muted text, and border treatments must retain sufficient
  contrast against their backgrounds.
- Reduced-motion preferences remain respected; the board requires no motion to
  communicate status.

## Failure and stale-state behavior

Because the board is static HTML, runtime loading and API failure states do not
apply. The relevant failure mode is stale or contradictory documentation.

To reduce that risk:

- no status is inferred from an unverified deployment;
- no percentage or target date is shown;
- active cards distinguish implemented source from incomplete acceptance;
- each remaining gate names the dependency preventing closure; and
- the implementation plan is linked as the authoritative detail.

## Verification

Implementation acceptance requires:

- the old M0/V1/V2/Future cards and percentage bar are absent;
- all Phase 0–8 entries render with the specified initial states;
- the visible summary agrees with the phase and gate entries;
- every phase and gate link resolves to an existing local document or anchor;
- desktop, tablet, mobile, 200% zoom, keyboard, and reduced-motion checks pass;
- status remains understandable in a grayscale or high-contrast review;
- the browser console has no new errors; and
- visual comparison confirms that the result follows Option 1's hierarchy and
  the existing documentation-site design language.

Live human acceptance for VoiceOver, NVDA, physical touch, and platform-specific
high-contrast behavior remains recorded separately from static browser
verification.
