# Frontend

React/TypeScript technician, sales, project delivery, and administration application.
The browser uses the supported Go APIs and shared components in `src/design-system`.
Feature modules live in `src/features`; `src/App.tsx` composes authentication and routes.

From this directory, use `npm ci`, `npm test`, and `npm run build`. `npm run dev`
starts Vite; API proxying is enabled with `CLASSIFICATION_ACCEPTANCE_API` for the
local acceptance harness. See [local development](../docs/06-development/local-development.md)
for application setup, PostgreSQL and Playwright prerequisites, and port usage.

The [execution backlog](../docs/09-roadmap/current-execution.md) records source completion,
local verification and remaining acceptance, including the unified calendar. A built feature is not
proof of deployed or manual accessibility acceptance.
