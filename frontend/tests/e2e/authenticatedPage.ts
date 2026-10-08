import type { Page } from "@playwright/test";

const navigation = [
  "work",
  "sales",
  "proposal",
  "conversion",
  "project",
  "ai-settings",
  "ai-assist",
  "teams-settings",
  "sessions",
  "recovery-access",
  "role-settings",
  "audit",
  "service-desk-settings",
  "classification-settings",
  "setup",
  "operations",
  "automation",
  "service-keys",
  "webhooks",
  "datto-reconciliation",
  "datto-settings",
  "forwarding-settings",
  "graph-settings",
  "knowledge",
  "billing",
  "directory-settings",
  "client-resources",
  "pipeline-settings",
  "prospects",
];

export async function authenticatePage(page: Page) {
  await page.route("**/v1/system/build", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ revision: "e2e" }),
    }),
  );
  await page.route("**/api/v1/setup/status", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        completed: true,
        bootstrap_available: false,
        entra_available: true,
      }),
    }),
  );
  await page.route("**/api/v1/me", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        id: "e2e-platform-administrator",
        navigation,
        capabilities: ["*"],
      }),
    }),
  );
  await page.route("**/api/v1/directory", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        clients: [
          {
            id: "client-id",
            display_id: "CLIENT-001",
            name: "Northwind Legal",
          },
        ],
        departments: [],
        teams: [],
        queues: [],
      }),
    }),
  );
}
