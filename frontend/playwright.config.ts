import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  webServer: [
    {
      command:
        "cd .. && CLASSIFICATION_E2E_SERVE=1 CALENDAR_E2E_SERVE=1 TEST_DATABASE_URL=${TEST_DATABASE_URL:-postgres://postgres:postgres@127.0.0.1:55432/rarity_test?sslmode=disable} go test ./backend/internal/store/psa -run '^TestClassificationBrowserAcceptanceServer$' -count=1 -v",
      url: "http://127.0.0.1:18082/e2e/health",
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command:
        "CLASSIFICATION_ACCEPTANCE_API=http://127.0.0.1:18082 npm run dev -- --host 127.0.0.1 --port 18081",
      url: "http://127.0.0.1:18081",
      reuseExistingServer: true,
    },
  ],
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://127.0.0.1:18081",
    trace: "retain-on-failure",
  },
});
