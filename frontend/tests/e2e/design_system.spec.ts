import { expect, test } from "@playwright/test";

import { authenticatePage } from "./authenticatedPage";

test.describe("authenticated Rarity design-system contract", () => {
  test.beforeEach(async ({ page }) => {
    await authenticatePage(page);
  });

  test("shell starts at the viewport top and keeps a continuous sidebar surface", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 730, height: 800 });
    await page.goto("/#/recovery-access");

    const workspace = page.locator(".rarity-workspace");
    const topbar = page.locator(".rarity-topbar");
    await expect(workspace).toBeVisible();
    await expect(topbar).toBeVisible();
    expect((await workspace.boundingBox())?.y).toBe(0);
    expect((await topbar.boundingBox())?.y).toBe(0);

    await page.setViewportSize({ width: 1440, height: 900 });
    const sidebar = page.locator("#rti-primary-sidebar");
    const navigationSurface = await page.evaluate(() => {
      const probe = document.createElement("div");
      probe.style.backgroundColor = "var(--rti-surface-navigation)";
      document.body.append(probe);
      const color = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return color;
    });
    await expect(sidebar).toHaveCSS("background-color", navigationSurface);
    expect((await sidebar.boundingBox())?.height).toBe(900);
    await expect(sidebar.locator("nav")).toHaveCSS("overflow-y", "auto");
  });

  test("catalog reflows without horizontal overflow at supported desktop widths", async ({
    page,
  }) => {
    for (const viewport of [
      { width: 1440, height: 900 },
      { width: 834, height: 900 },
    ]) {
      await page.setViewportSize(viewport);
      await page.goto("/#/design-system");
      await expect(
        page.getByRole("heading", { name: "Rarity design system" }),
      ).toBeVisible();
      const overflows = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      );
      expect(overflows).toBe(false);
    }
  });

  test("shared dialog traps focus, closes on Escape, and restores its trigger", async ({
    page,
  }) => {
    await page.goto("/#/design-system");
    const trigger = page.getByRole("button", { name: "Open dialog" });
    await trigger.focus();
    await trigger.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(trigger).toBeFocused();
  });

  test("reduced motion and forced colors retain named controls", async ({
    page,
  }) => {
    await page.emulateMedia({
      reducedMotion: "reduce",
      forcedColors: "active",
    });
    await page.goto("/#/design-system");
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Open dialog" }),
    ).toBeVisible();
    await expect(page.getByText("healthy", { exact: true })).toBeVisible();
    await expect(page.getByText("Running", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Saving changes" }),
    ).toBeDisabled();
    await expect(page.getByText("WORK-LIST-UNAVAILABLE")).toBeVisible();
  });

  test("desktop-only catalog has an accessible handoff at effective zoom widths", async ({
    page,
  }) => {
    for (const viewport of [
      { width: 720, height: 900 },
      { width: 360, height: 800 },
    ]) {
      await page.setViewportSize(viewport);
      await page.goto("/#/design-system");
      await expect(
        page.getByText("Desktop workspace", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("heading", { name: "Design system" }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Open dialog" }),
      ).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > window.innerWidth,
        ),
      ).toBe(false);
      const returnAction = page.getByRole("button", {
        name: "Return to mobile Home",
      });
      await returnAction.scrollIntoViewIfNeeded();
      await returnAction.focus();
      await expect(returnAction).toBeFocused();
      const bounds = await returnAction.boundingBox();
      expect(bounds?.width).toBeGreaterThanOrEqual(24);
      expect(bounds?.height).toBeGreaterThanOrEqual(24);
    }
  });
});

test.describe("touch-sized authenticated desktop handoff", () => {
  test.beforeEach(async ({ page }) => {
    await authenticatePage(page);
  });

  test.use({
    hasTouch: true,
    isMobile: true,
    viewport: { width: 390, height: 844 },
  });

  test("navigation and handoff return actions work by touch", async ({
    page,
  }) => {
    await page.goto("/#/design-system");
    await expect(
      page.getByText("Desktop workspace", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Design system" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Open dialog" })).toHaveCount(
      0,
    );
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      ),
    ).toBe(false);

    const navigation = page.getByRole("button", { name: "Open navigation" });
    const navigationBounds = await navigation.boundingBox();
    expect(navigationBounds?.width).toBeGreaterThanOrEqual(44);
    expect(navigationBounds?.height).toBeGreaterThanOrEqual(44);
    const sidebar = page.locator("#rti-primary-sidebar");
    await expect
      .poll(async () => {
        const closedSidebar = await sidebar.boundingBox();
        return (closedSidebar?.x ?? 0) + (closedSidebar?.width ?? 0);
      })
      .toBeLessThanOrEqual(0);
    await navigation.tap();
    await expect.poll(async () => (await sidebar.boundingBox())?.x).toBe(0);
    expect((await sidebar.boundingBox())?.width).toBeLessThanOrEqual(390);
    await page.keyboard.press("Escape");
    await expect
      .poll(async () => {
        const closedSidebar = await sidebar.boundingBox();
        return (closedSidebar?.x ?? 0) + (closedSidebar?.width ?? 0);
      })
      .toBeLessThanOrEqual(0);
    await expect(navigation).toBeFocused();

    const returnAction = page.getByRole("button", {
      name: "Return to mobile Home",
    });
    await returnAction.scrollIntoViewIfNeeded();
    const returnBounds = await returnAction.boundingBox();
    expect(returnBounds?.width).toBeGreaterThanOrEqual(44);
    expect(returnBounds?.height).toBeGreaterThanOrEqual(44);
    await returnAction.tap();
    await expect(page).toHaveURL(/#\/home$/);
    await expect(
      page.getByRole("heading", { name: "Your operational home" }),
    ).toBeVisible();
  });
});
