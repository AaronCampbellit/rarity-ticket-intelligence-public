import { expect, test } from "@playwright/test";

import { authenticatePage } from "./authenticatedPage";
import { installChangeOrderFixtures } from "./operationalFixtures";

test.beforeEach(async ({ page }) => {
  await authenticatePage(page);
});

test("reasoned override can be applied once to the current baseline", async ({
  page,
}) => {
  const fixture = await installChangeOrderFixtures(page);
  await page.goto("/#/project");
  await page.getByRole("button", { name: "Open", exact: true }).click();
  await page
    .getByLabel("Override reason")
    .fill("Customer approval recorded in steering call");
  await page.getByRole("button", { name: "Override approval" }).click();

  await expect(
    page
      .getByRole("list")
      .getByText("Customer approval recorded in steering call", {
        exact: true,
      }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Apply approved Change Order" })
    .click();

  await expect(page.getByText("applied", { exact: true })).toBeVisible();
  await expect(
    page.getByText("$115,000", { exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Apply approved Change Order" }),
  ).not.toBeVisible();
  expect(fixture.overrideBodies).toEqual([
    {
      expected_version: 5,
      reason: "Customer approval recorded in steering call",
    },
  ]);
  expect(fixture.applyBodies).toEqual([{ expected_version: 6 }]);
  expect(fixture.projectDetailRequests).toBe(3);
  expect(fixture.scopedRequests).toEqual(
    expect.arrayContaining([
      "POST /api/v1/change-order-versions/change-order-version-1/override-approval",
      "POST /api/v1/change-order-versions/change-order-version-1/apply",
    ]),
  );
  expect(fixture.failures).toEqual([]);
});
