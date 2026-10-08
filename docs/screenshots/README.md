# Screenshot provenance and safe preview

These are browser screenshots of the implemented React application, captured with Chromium through Playwright CLI at **1440 pixels wide**. They are not design mockups or screenshots of a live customer environment.

| Image | Viewport | Journey |
| --- | --- | --- |
| [technician-work.png](technician-work.png) | 1440 × 1050 | Assigned/unassigned worklist with the selected ticket preview. |
| [project-delivery.png](project-delivery.png) | 1440 × 1720 | Project workspace with phases, tasks, capacity, costs, and financial baselines. |
| [calendar.png](calendar.png) | 1440 × 1280 | Agenda lens with scheduled work and a redacted Busy commitment. |

All displayed names, clients, tickets, amounts, and commitments are synthetic. [preview.mjs](preview.mjs) supplies local HTTP responses to the unmodified frontend, following its API wire shapes and existing [browser fixtures](../../frontend/tests/e2e/operationalFixtures.ts). The build indicator says `synthetic-preview`. Calendar commitments use the current UTC date so they remain visible when the preview is opened later.

## Run the preview

From the repository root, with Node.js 22.12+ and npm:

```bash
npm --prefix frontend ci
node docs/screenshots/preview.mjs
```

The UI listens at **http://127.0.0.1:41732**, and the synthetic fixture listens at **127.0.0.1:41731**. Both bind only to loopback. No credentials, database, application backend, real customer records, provider calls, or persistence are used. The fixture never forwards requests. It provides a synthetic session-refresh heartbeat without creating a real session; other writes are rejected.

Alternate ports:

```bash
RTI_PREVIEW_API_PORT=42731 RTI_PREVIEW_UI_PORT=42732 node docs/screenshots/preview.mjs
```

Use **Work**, **Projects**, and **Calendar** in the sidebar. Ticket search, selection, List/Kanban switching, project opening, and calendar lenses work. Preview changes are read-only: creation, workflow edits, timers, saved-view writes, and schedule changes are not supported. The production frontend may offer those controls, but the fixture returns an error if they are used. Only the illustrated read journeys are populated; other routes can return `preview_route_unavailable`.

Press `Ctrl+C` in the preview terminal to stop its Vite and fixture servers. Keep this preview local; it does not implement application authentication or authorization.

## Reproduce the images

Use your installed browser automation tools, or open the pages manually at the viewport sizes above:

1. Open `http://127.0.0.1:41732/#/work`. Wait for the four synthetic tickets and the selected-ticket panel. Capture the viewport as `technician-work.png`.
2. Open `http://127.0.0.1:41732/#/project`. Click **Open** beside **PRJ-204**. Wait for the delivery plan and financials, then capture the viewport as `project-delivery.png`.
3. Close the open project tab, navigate to **Calendar**, and select **Agenda**. Leave event drawers closed and capture the viewport as `calendar.png`.

The screenshots establish rendered UI behavior with synthetic responses. They do not establish server permissions, client isolation, database persistence, scheduling application, provider connectivity, production performance, or deployed accessibility acceptance. For real-service and PostgreSQL verification, use the [development guide](../06-development/local-development.md) and the [calendar browser tests](../../frontend/tests/e2e/calendar.spec.ts).
