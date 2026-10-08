# Rarity Design System Acceptance

Status: automated source and component gates complete; rendered browser and
assistive-technology acceptance pending.

| Contract | Automated evidence | Manual evidence | Status |
|---|---|---|---|
| Public package boundary | `boundaries.test.ts`, `public-api.test.ts` | Not required | pass |
| Components and workflow patterns | Design-system Vitest suite and axe checks | Chrome 150, Aaron/Codex, August 1 2026: desktop catalog visually reviewed | pass |
| Authenticated feature adoption | All 33 route/action-owning feature surfaces import the public design-system boundary; four display-only subviews are composed inside migrated pages; 202 Vitest tests and production build pass | Chrome 150, Aaron/Codex, August 1 2026: final administration, Sales, Proposal, and Conversion routes reviewed with one main landmark and no horizontal overflow | pass |
| Work retry, filters, dialogs, and settings | Work feature Vitest suite | Chrome 150, Aaron/Codex, August 1 2026: unauthenticated narrow Work state reviewed | pass |
| Sales and Projects versioned workflows | Sales and Projects Vitest suites | Desktop and narrow workflow review pending | automated pass |
| Provider-agnostic AI and Ollama | AI settings Vitest suite | August 2 2026: authenticated management API configured local Ollama 0.14.2, discovered models, selected `qwen3:14b`, enabled the policy, and completed a real summary job with a pending-human recommendation; visual GUI review remains pending | runtime pass; visual pending |
| Keyboard traversal and dialog focus | Chrome interaction: dialog trap/Escape/trigger restoration; mobile navigation Escape/trigger restoration | Chrome 150, Aaron/Codex, August 1 2026 | pass |
| Desktop 1440 by 900 | Chrome geometry: 1440 viewport, 1425 document, 168 sidebar, 1257 main | Chrome 150, Aaron/Codex, August 1 2026 | pass |
| Narrow 390 by 844 | Chrome geometry: 390 viewport, 375 document/main, no horizontal overflow; closed sidebar off-screen and open sidebar 320 px | Touch/tablet review pending | automated pass |
| Reduced motion and forced colors | Chromium 151 emulation verifies named status/error text, disabled state, focus, and reduced-motion behavior | Physical high-contrast review pending | automated pass; manual pending |
| Zoom and reflow | Chromium 151 effective-width checks at 720 px and 360 px verify reflow, no horizontal overflow, reachable focus, and minimum targets | True 200% and 400% browser zoom review pending | automated pass; manual pending |
| Touch and tablet targets | Chromium 151 mobile/touch emulation verifies a 44 px navigation target and touch-operated dialog actions | Physical touch/tablet review pending | automated pass; manual pending |
| VoiceOver on macOS | None claimed | Date, browser, operator, and outcome required | pending |
| NVDA on Windows | None claimed | Date, browser, operator, and outcome required | pending |

The design specification must remain approved for implementation, not marked
implemented and accepted, until every required manual row has evidence.

## Chrome findings

- Fixed a cascade conflict where legacy mobile sidebar rules overrode the
  design-system off-canvas navigation and left a clipped rail over narrow pages.
- Rechecked desktop and narrow geometry after the fix.
- Reviewed the final migrated administration, Sales, Proposal, and Conversion
  routes; each rendered one named main landmark without horizontal overflow.
- Chrome reported no console warnings or errors on the catalog.
- The authenticated design-system Playwright matrix passed 5 of 5 checks in
  Chromium 151.0.7922.34 on August 2 2026, including effective-width reflow,
  forced-colors/reduced-motion emulation, and mobile touch emulation.
- True 200/400 percent browser zoom, physical touch hardware, VoiceOver, and
  NVDA remain pending because the connected Chrome control surface did not
  expose those environment controls.
- On August 2, the requested Chrome control connection was unavailable because
  its extension was not connected. No in-app-browser result was substituted for
  the required Chrome, VoiceOver, or NVDA acceptance.
