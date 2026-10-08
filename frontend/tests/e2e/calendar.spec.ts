import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { authenticatePage } from "./authenticatedPage";

test("calendar lenses, privacy and explicit scheduling confirmation", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await authenticatePage(page);
  await page.route("**/api/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "calendar-tech",
        navigation: ["calendar", "work"],
        capabilities: ["calendar.read", "calendar.schedule"],
      },
    }),
  );
  const date = new Date().toISOString().slice(0, 10);
  const event = {
    id: "event",
    projection_id: "projection",
    occurrence_id: "occurrence",
    occurrence_key: "local-occurrence",
    source_revision: 1,
    title: "Northwind on-site visit",
    all_day: false,
    starts_at: `${date}T14:00:00Z`,
    ends_at: `${date}T15:00:00Z`,
    timezone: "UTC",
    event_role: "scheduled_work",
    privacy: "full",
    source: { type: "work_record", id: "ticket-id", client_id: "client-id" },
    health: "on_track",
    scheduling_mode: "fixed_block",
    capabilities: { view_source: true, schedule: true },
  };
  let applied = 0;
  await page.route("**/api/v1/calendar/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/live"))
      return route.fulfill({
        contentType: "text/event-stream",
        body: 'event: cursor\ndata: {"cursor":"one"}\n\n',
      });
    if (path.endsWith("/filter-options"))
      return route.fulfill({ json: { clients: { "client-id": 1 } } });
    if (path.endsWith("/capacity")) return route.fulfill({ json: {} });
    if (path.endsWith("/events"))
      return route.fulfill({
        json: {
          events: [
            event,
            {
              ...event,
              id: "busy",
              occurrence_id: "busy-occurrence",
              privacy: "busy",
              title: "Private client must not render",
            },
          ],
        },
      });
    if (path.endsWith("/proposals"))
      return route.fulfill({
        status: 201,
        json: {
          id: "proposal",
          version: 4,
          state: "pending",
          expires_at: new Date(Date.now() + 600000).toISOString(),
          changes: [
            {
              id: "primary",
              required: true,
              requested: {
                Source: { type: "work_record", id: "ticket-id" },
                StartsAt: `${date}T15:00:00Z`,
                EndsAt: `${date}T16:00:00Z`,
              },
            },
          ],
          conflicts: [],
          blocked_sources: [],
          capacity: [],
          health: [],
          notifications: [],
        },
      });
    if (path.endsWith("/apply")) {
      applied++;
      expect(route.request().postDataJSON()).toMatchObject({
        expected_proposal_version: 4,
        reason: "Customer requested later arrival",
      });
      return route.fulfill({ json: { proposal_id: "proposal" } });
    }
    return route.fulfill({ json: [] });
  });
  await page.route("**/api/v1/views?**", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/#/calendar");
  await expect(
    page.getByRole("heading", { name: "Calendar", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Agenda view" }).click();
  await expect(
    page.getByRole("button", { name: /Northwind on-site visit/ }),
  ).toBeVisible();
  await expect(page.getByText("Private client must not render")).toHaveCount(0);
  await page.screenshot({
    path: "/tmp/rarity-calendar-desktop.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: /Northwind on-site visit/ }).click();
  await page.getByRole("button", { name: "Reschedule" }).click();
  await page.getByRole("button", { name: "Preview schedule change" }).click();
  const confirm = page.getByRole("button", {
    name: "Confirm schedule changes",
  });
  await expect(confirm).toBeDisabled();
  expect(applied).toBe(0);
  await page
    .getByLabel("Reason", { exact: true })
    .fill("Customer requested later arrival");
  await confirm.click();
  await expect(page.getByText(/Schedule changes applied/)).toBeVisible();
  expect(applied).toBe(1);
  await page.addScriptTag({ content: axe.source });
  const violations = await page.evaluate(async () =>
    (
      await (window as typeof window & { axe: typeof axe }).axe.run(
        document.body,
        { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21aa"] } },
      )
    ).violations.map(({ id, nodes }) => ({
      id,
      targets: nodes.map((node) => node.target),
    })),
  );
  expect(violations).toEqual([]);
  expect(errors).toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByText(/larger screen|desktop|wider screen/i).first(),
  ).toBeVisible();
  await page.screenshot({
    path: "/tmp/rarity-calendar-phone.png",
    fullPage: true,
  });
});

test("typed calendar forms persist through the real services and projections", async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const fixture = await (
    await request.get("http://127.0.0.1:18082/e2e/fixture")
  ).json();
  const date = new Date().toISOString().slice(0, 10);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/#/calendar");
  await expect(
    page.getByRole("heading", { name: "Calendar", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Create and manage calendar records" })
    .click();
  await page.getByLabel("Record or setting").selectOption("milestone");
  await page.getByLabel("Record client").selectOption(fixture.client_a);
  await page
    .getByLabel("Project", { exact: true })
    .selectOption(fixture.project_id);
  await expect(page.getByText("Directory choices unavailable")).toHaveCount(0);
  await page
    .getByRole("dialog")
    .getByLabel("Name", { exact: true })
    .fill("Browser calendar cutover");
  await page.getByLabel("Due date").fill(date);
  await page.getByLabel("First day").fill(date);
  await page.getByLabel("Last day (included)").fill(date);
  await page
    .getByRole("button", { name: "Create milestone", exact: true })
    .click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  const milestones = await (
    await request.get(
      `http://127.0.0.1:18082/api/v1/projects/${fixture.project_id}/milestones`,
    )
  ).json();
  expect(milestones).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        name: "Browser calendar cutover",
        all_day: true,
        starts_on: date,
        version: 1,
      }),
    ]),
  );
  await page.getByLabel("Record or setting").selectOption("pto");
  await page.getByLabel("PTO type").selectOption("vacation");
  await page.getByLabel("First day").fill(date);
  await page.getByLabel("Last day (included)").fill(date);
  await page.getByRole("button", { name: "Request PTO", exact: true }).click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  const pto = await (
    await request.get("http://127.0.0.1:18082/api/v1/workforce/pto")
  ).json();
  expect(pto).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        state: "requested",
        starts_on: date,
        all_day: true,
      }),
    ]),
  );
  await page.getByLabel("Record or setting").selectOption("maintenance");
  await page
    .getByRole("dialog")
    .getByLabel("Title", { exact: true })
    .fill("Browser maintenance");
  await page.getByLabel("First day").fill(date);
  await page.getByLabel("Last day (included)").fill(date);
  await page.getByLabel("Add affected resource").selectOption(fixture.client_a);
  await page
    .getByRole("button", { name: "Create maintenance window", exact: true })
    .click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  for (const kind of ["renewal", "license"]) {
    await page.getByLabel("Record or setting").selectOption(kind);
    await page.getByLabel("Record client").selectOption(fixture.client_a);
    await page
      .getByRole("dialog")
      .getByLabel("Title", { exact: true })
      .fill(`Browser ${kind}`);
    const owner = await page
      .getByLabel("Owner", { exact: true })
      .locator("option")
      .last()
      .getAttribute("value");
    await page.getByLabel("Owner", { exact: true }).selectOption(owner!);
    await page.getByLabel("Vendor").fill("Acceptance vendor");
    await page.getByLabel("Effective date").fill(date);
    await page
      .getByLabel("Expiration date")
      .fill(`${Number(date.slice(0, 4)) + 1}${date.slice(4)}`);
    await page
      .getByRole("button", { name: `Create ${kind}`, exact: true })
      .click();
    await expect(
      page.getByText("Changes saved. Calendar projections will refresh."),
    ).toBeVisible();
  }
  await page.getByLabel("Record or setting").selectOption("workforce");
  const technician = await page
    .getByLabel("Technician", { exact: true })
    .locator("option")
    .last()
    .getAttribute("value");
  await page
    .getByLabel("Technician", { exact: true })
    .selectOption(technician!);
  await page.getByLabel("Effective from").fill(date);
  await page.getByRole("button", { name: "Publish working schedule" }).click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  await page.getByLabel("Record or setting").selectOption("policy");
  await page.getByRole("button", { name: "Save conflict policy" }).click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  await page.getByLabel("Record or setting").selectOption("custom");
  await page.getByLabel("Object type").selectOption("project");
  await page.getByLabel("Field key").fill("browser_follow_up");
  await page.getByLabel("Label", { exact: true }).fill("Browser follow up");
  await page.getByLabel("Category", { exact: true }).fill("operations");
  await page.getByRole("button", { name: "Save date definition" }).click();
  await expect(
    page.getByText("Changes saved. Calendar projections will refresh."),
  ).toBeVisible();
  const definitions = await (
    await request.get(
      `http://127.0.0.1:18082/api/v1/objects/project/${fixture.project_id}/custom-date-fields`,
    )
  ).json();
  expect(definitions).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        field_type: "date",
        label: "Browser follow up",
      }),
    ]),
  );
  const response = await request.put(
    `http://127.0.0.1:18082/api/v1/objects/project/${fixture.project_id}/custom-date-values`,
    {
      data: {
        field_id: definitions.find(
          (field: { label: string }) => field.label === "Browser follow up",
        ).id,
        date_value: date,
        expected_version: 0,
        idempotency_key: `browser-${fixture.project_id}`,
      },
    },
  );
  expect(response.ok(), await response.text()).toBe(true);
  await page.getByLabel("Record or setting").selectOption("notifications");
  await page.getByRole("button", { name: "Add preference" }).click();
  await page
    .getByRole("button", { name: "Save notification preferences" })
    .click();
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(
              "http://127.0.0.1:18082/api/v1/calendar/notification-preferences",
            )
          ).json()
        ).version,
    )
    .toBe(1);
  await page.addScriptTag({ content: axe.source });
  const violations = await page.evaluate(async () =>
    (
      await (window as typeof window & { axe: typeof axe }).axe.run(
        document.body,
        { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21aa"] } },
      )
    ).violations.map(({ id, nodes }) => ({
      id,
      targets: nodes.map((node) => node.target),
    })),
  );
  expect(violations).toEqual([]);
  await page.screenshot({
    path: "/tmp/rarity-calendar-admin.png",
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Agenda view" }).click();
  await expect
    .poll(async () => {
      await page.getByRole("button", { name: "Refresh", exact: true }).click();
      return page
        .getByRole("button", { name: /Browser calendar cutover/ })
        .count();
    })
    .toBeGreaterThan(0);
  expect(errors).toEqual([]);
});
