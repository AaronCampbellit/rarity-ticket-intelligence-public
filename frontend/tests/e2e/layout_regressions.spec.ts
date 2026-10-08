import { expect, test } from "@playwright/test";
import { authenticatePage } from "./authenticatedPage";

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await authenticatePage(page);
  await page.route("**/api/v1/work-records?*", (route) =>
    route.fulfill({
      json: [
        {
          ID: "layout-ticket",
          DisplayID: "INC-LAYOUT-1048",
          Type: "incident",
          Title:
            "Remote access unavailable for the Northwind service delivery team",
          Description: "Staff cannot connect.",
          Status: "new",
          Priority: "normal",
          QueueID: "queue-1",
          PrimaryOwnerID: "",
          UpdatedAt: "2026-09-05T12:00:00Z",
          Version: 1,
        },
      ],
    }),
  );
  await page.route("**/api/v1/views?*", (route) => route.fulfill({ json: [] }));
});

test("header controls remain separate and reachable at every breakpoint", async ({
  page,
}) => {
  for (const width of [1440, 1280, 1180, 1024, 834, 761, 760, 390, 360]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/#/home");
    await expect(
      page.getByRole("heading", { name: "Your operational home" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Open page assistant" }),
    ).toBeVisible();
    const failures = await page.locator(".rarity-topbar").evaluate((header) => {
      const controls = [
        ...header.querySelectorAll<HTMLElement>("button, input, select"),
      ].filter((el) => el.checkVisibility());
      return controls.flatMap((el) => {
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(
          r.x + r.width / 2,
          r.y + r.height / 2,
        );
        return r.left < 0 ||
          r.right > innerWidth ||
          r.height < 32 ||
          !el.contains(hit)
          ? [
              {
                label:
                  el.getAttribute("aria-label") || el.textContent || el.tagName,
                rect: { x: r.x, y: r.y, width: r.width, height: r.height },
                hit: hit?.outerHTML.slice(0, 200),
              },
            ]
          : [];
      });
    });
    expect(failures, `header at ${width}px`).toEqual([]);
  }
});

test("Home panels fit inside the workspace instead of being clipped", async ({
  page,
}) => {
  for (const width of [1440, 1280, 1024, 834, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/#/home");
    await expect(page.locator(".rti-home__work")).toBeVisible();
    const clipped = await page.locator(".rti-home__grid").evaluate((grid) =>
      [...grid.querySelectorAll("section")]
        .filter((section) => {
          const r = section.getBoundingClientRect();
          return (
            r.right > innerWidth ||
            r.left < 0 ||
            section.scrollWidth > section.clientWidth + 1
          );
        })
        .map(
          (section) =>
            section.getAttribute("aria-label") ||
            section.textContent?.slice(0, 80),
        ),
    );
    expect(clipped, `Home at ${width}px`).toEqual([]);
  }
});

test("ticket preview and open buttons have independent hit areas", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/#/work");
  const row = page.locator(".work-row-shell").first();
  await expect(row).toBeVisible();
  const preview = row.locator(".work-row"),
    open = row.locator(".work-row-open");
  const p = (await preview.boundingBox())!,
    o = (await open.boundingBox())!;
  expect(p.x + p.width).toBeLessThanOrEqual(o.x);
  await preview.click();
  await expect(
    page.getByRole("complementary", { name: /preview/i }),
  ).toBeVisible();
});

test("Create dialog covers the viewport and spaces its fields", async ({
  page,
}) => {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/#/home");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "Create work",
      exact: true,
    });
    await expect(dialog).toBeVisible();
    const backdrop = (await page.getByTestId("dialog-backdrop").boundingBox())!;
    expect(backdrop).toEqual({ x: 0, y: 0, width, height: 900 });
    const fields = dialog.locator("form > label");
    const boxes = await fields.evaluateAll((elements) =>
      elements.map((el) => {
        const r = el.getBoundingClientRect();
        return { x: r.x, y: r.y, right: r.right, bottom: r.bottom };
      }),
    );
    for (let i = 0; i < boxes.length; i++) {
      expect(boxes[i].x).toBeGreaterThanOrEqual(0);
      expect(boxes[i].right).toBeLessThanOrEqual(width);
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i],
          b = boxes[j];
        expect(
          a.right <= b.x ||
            b.right <= a.x ||
            a.bottom + 8 <= b.y ||
            b.bottom + 8 <= a.y,
        ).toBe(true);
      }
    }
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Create", exact: true }),
    ).toBeFocused();
  }
});
