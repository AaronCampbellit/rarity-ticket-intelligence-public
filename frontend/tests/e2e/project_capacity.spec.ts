import { expect, test } from "@playwright/test";

import { authenticatePage } from "./authenticatedPage";
import { installProjectCapacityFixtures } from "./operationalFixtures";

test.beforeEach(async ({ page }) => {
  await authenticatePage(page);
});

test("Project capacity distinguishes overbooking from remaining availability", async ({
  page,
}) => {
  const fixture = await installProjectCapacityFixtures(page);
  await page.goto("/#/project");
  await page.getByRole("button", { name: "Open", exact: true }).click();

  await expect(
    page.getByRole("heading", { name: "Resource capacity" }),
  ).toBeVisible();
  await expect(page.getByText("20h over")).toBeVisible();
  await expect(page.getByText("40h free")).toBeVisible();
  await expect(
    page.getByRole("meter", { name: "Marcus Reed capacity" }),
  ).toHaveAttribute("aria-valuenow", "3600");
  expect(
    fixture.scopedRequests.filter(
      (request) => request === "GET /api/v1/projects?limit=100",
    ),
  ).toHaveLength(2);
  expect(
    fixture.scopedRequests.filter(
      (request) => request === "GET /api/v1/projects/project-1",
    ),
  ).toHaveLength(1);
  expect(fixture.failures).toEqual([]);
});
