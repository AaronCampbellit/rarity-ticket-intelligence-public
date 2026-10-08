import { expect, test } from "@playwright/test";

import { authenticatePage } from "./authenticatedPage";
import { installOpportunityToProjectFixtures } from "./operationalFixtures";

test.beforeEach(async ({ page }) => {
  await authenticatePage(page);
});

test("accepted opportunity becomes a budgeted project", async ({ page }) => {
  const fixture = await installOpportunityToProjectFixtures(page);
  await page.goto("/#/sales");
  await expect(
    page.getByRole("heading", { name: "Northwind security modernization" }),
  ).toBeVisible();
  await expect(
    page.getByText("Customer accepted the commercial baseline"),
  ).toBeVisible();
  await expect(page.getByText("accepted-scope.pdf")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Revenue forecast" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Proposals" }).click();
  await expect(
    page.getByText("accepted", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Version 1" })).toBeVisible();

  await page.getByRole("link", { name: "Conversion" }).click();
  await expect(
    page.getByRole("combobox", { name: "Accepted proposal", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Accepted proposal")).toHaveValue("proposal-1");
  await page.getByLabel("Project name").fill("Security modernization");
  await expect(
    page.getByRole("checkbox", { name: "Schedule kickoff" }),
  ).toBeEnabled();
  await expect(
    page.getByRole("checkbox", { name: "Qualify prospect" }),
  ).toBeDisabled();
  await page.getByRole("checkbox", { name: "Schedule kickoff" }).check();
  await page.getByRole("button", { name: "Preview conversion" }).click();
  await expect(
    page.getByRole("checkbox", { name: "Schedule kickoff" }),
  ).toBeChecked();
  await expect(
    page.getByRole("checkbox", { name: "Schedule kickoff" }),
  ).toBeDisabled();
  const conversionSummary = page.getByRole("region", {
    name: "Conversion summary",
  });
  await expect(
    conversionSummary
      .getByText("Original budget")
      .locator("..")
      .getByText("$100,000"),
  ).toBeVisible();
  await expect(
    conversionSummary.getByText("Planned work").locator("..").getByText("40h"),
  ).toBeVisible();
  await page.getByRole("button", { name: "Create project" }).click();

  await expect(
    page.getByText("Project created from the accepted Proposal."),
  ).toBeVisible();
  await expect(
    page.getByRole("button", {
      name: "PRJ-PROP-001 Security modernization",
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Security modernization" }),
  ).toBeVisible();
  await expect(page.getByText("Original commercial baseline")).toBeVisible();
  await expect(
    page
      .getByText("Original commercial baseline")
      .locator("..")
      .getByText("$100,000"),
  ).toBeVisible();
  const deliveryPhase = page
    .getByRole("region", { name: "Delivery plan" })
    .getByRole("listitem")
    .filter({ hasText: "Delivery" });
  await expect(deliveryPhase.getByText(/40h planned/)).toBeVisible();
  const projectTasks = page.getByRole("region", { name: "Project tasks" });
  await expect(projectTasks.getByText("Schedule kickoff")).toBeVisible();
  await expect(projectTasks.getByText("Qualify prospect")).not.toBeVisible();
  expect(fixture.previewBodies).toEqual([
    expect.objectContaining({
      selected_task_ids: ["kickoff"],
      task_versions: { kickoff: 2 },
    }),
  ]);
  expect(fixture.conversionBodies).toEqual([
    expect.objectContaining({
      preview_hash: expect.stringMatching(/^[0-9a-f]{64}$/),
      selected_task_ids: ["kickoff"],
      task_versions: { kickoff: 2 },
    }),
  ]);
  expect(fixture.scopedRequests).toEqual(
    expect.arrayContaining([
      "GET /api/v1/opportunities?limit=100",
      "GET /api/v1/opportunity-forecast",
      "GET /api/v1/opportunities/opportunity-1/activities?limit=100",
      "GET /api/v1/opportunities/opportunity-1/tasks",
      "GET /api/v1/opportunities/opportunity-1/attachments",
      "GET /api/v1/proposals?limit=100",
      "POST /api/v1/opportunities/opportunity-1/conversion-preview",
      "POST /api/v1/opportunities/opportunity-1/convert",
      "GET /api/v1/projects?limit=100",
      "GET /api/v1/projects/project-1",
    ]),
  );
  expect(fixture.failures).toEqual([]);
});
