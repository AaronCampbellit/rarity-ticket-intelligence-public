import { expect, type Page, test } from "@playwright/test";

async function routeBuild(page: Page) {
  await page.route("**/v1/system/build", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ revision: "e2e-shell" }),
    }),
  );
}

async function routeAccess(
  page: Page,
  options: {
    completed?: boolean;
    entraAvailable?: boolean;
    authenticated?: boolean;
  } = {},
) {
  const {
    completed = true,
    entraAvailable = true,
    authenticated = false,
  } = options;

  await page.route("**/api/v1/setup/status", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        completed,
        bootstrap_available: !completed,
        entra_available: entraAvailable,
      }),
    }),
  );
  await page.route("**/api/v1/me", (route) =>
    authenticated
      ? route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            id: "technician-id",
            navigation: ["work", "sales"],
            capabilities: ["work_record.read"],
          }),
        })
      : route.fulfill({ status: 401 }),
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

test.beforeEach(async ({ page }) => {
  await routeBuild(page);
});

test("signed-out Entra users see only the Microsoft login", async ({
  page,
}) => {
  await routeAccess(page);

  await page.goto("/#/sales");

  await expect(
    page.getByRole("link", { name: "Sign in with Microsoft" }),
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/login$/);
  await expect(
    page.getByRole("navigation", { name: "Primary" }),
  ).not.toBeAttached();
  await expect(page.getByLabel("Username")).not.toBeAttached();
  await expect(
    page.getByText("Northwind security modernization"),
  ).not.toBeAttached();
});

test("Entra-free installations show local authentication", async ({ page }) => {
  await routeAccess(page, { entraAvailable: false });

  await page.goto("/#/work");

  await expect(page.getByLabel("Username")).toBeVisible();
  await expect(page.getByLabel("Password")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Sign in with Microsoft" }),
  ).not.toBeAttached();
});

test("incomplete installations show setup without the shell", async ({
  page,
}) => {
  await routeAccess(page, { completed: false, entraAvailable: false });

  await page.goto("/#/sales");

  await expect(
    page.getByRole("heading", {
      name: "Configure this Rarity installation",
    }),
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/setup$/);
  await expect(
    page.getByRole("navigation", { name: "Primary" }),
  ).not.toBeAttached();
});

test("authenticated users enter their permitted workspace", async ({
  page,
}) => {
  await routeAccess(page, { authenticated: true });

  await page.goto("/#/work");

  await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
  await expect(page.getByText("Build e2e-shell")).toBeVisible();
  await expect(page).toHaveURL(/#\/work$/);
});

test("the application logo remains visible at narrow widths", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 700 });
  await routeAccess(page);

  await page.goto("/#/sales");

  const logo = page.locator(".public-access__brand--compact");
  await expect(logo).toBeVisible();
  await expect(logo).toBeInViewport();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
