# Accessibility

**Status:** Automated workflow, forced-colors, effective-zoom, and touch audits implemented; physical assistive-technology acceptance pending

Rarity targets WCAG 2.2 AA. All workflows support keyboard and screen-reader use; focus order, labels, headings, landmarks, errors, live updates, contrast, motion preferences, zoom, and touch targets are tested.

Pointer-only drag, color-only status, time-limited interactions without extension, and inaccessible custom controls are not accepted. Automated checks supplement—not replace—manual assistive-technology testing.

The frontend component suite runs `axe-core` WCAG A/AA rules across the Sales, Proposal, Conversion, and Project workflows and verifies a keyboard skip link to the active main landmark. The authenticated design-system browser suite also checks forced-colors and reduced-motion emulation, effective 200%/400% reflow widths, reachable focus, non-color status/error meaning, disabled state, 44 px mobile navigation, and touch-operated dialog actions.

Automation does not replace true browser zoom, physical touch hardware, complete
keyboard traversal, VoiceOver, or NVDA. Those remain manual release evidence.
The required matrix and result fields are recorded in the
[Pilot Acceptance Runbook](../05-infrastructure/pilot-acceptance-runbook.md).
