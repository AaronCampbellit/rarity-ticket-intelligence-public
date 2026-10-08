import { expect, test } from "@playwright/test";

import { authenticatePage } from "./authenticatedPage";

test.describe("classification administration workspace", () => {
  test.beforeEach(async ({ page }) => {
    await authenticatePage(page);
    await page.route("**/api/v1/tag-groups", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify([
          {
            id: "technology",
            label: "Technology",
            description: "",
            position: 1,
            state: "active",
            system_managed: false,
            version: 1,
          },
        ]),
      }),
    );
    await page.route("**/api/v1/tags", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify([
          {
            id: "vpn",
            label: "VPN",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]),
      }),
    );
  });

  test("reflows the administration catalog without horizontal overflow", async ({
    page,
  }) => {
    await page.goto("/#/classification-settings");
    await expect(
      page.getByRole("heading", { name: "Classification settings" }),
    ).toBeVisible();
    for (const width of [390, 834, 1280, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await expect(page.locator(".classification-settings-page")).toBeVisible();
      await expect(page.getByText("Desktop workspace")).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > window.innerWidth,
        ),
      ).toBe(false);
    }
  });
});
