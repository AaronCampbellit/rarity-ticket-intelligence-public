import { expect, test, type Page } from "@playwright/test";
import axe from "axe-core";

type Fixture = {
  client_a: string;
  client_b: string;
  task_id: string;
  fallback_task_id: string;
  recurring_task_id: string;
  project_id: string;
  unclassified_id: string;
  network_id: string;
  security_id: string;
};

async function openClassification(page: Page) {
  const disclosure = page
    .locator("summary")
    .filter({ hasText: /^Classification$/ })
    .first();
  await disclosure.focus();
  // Returning to a retained workspace can preserve the expanded disclosure.
  if (
    !(await disclosure
      .locator("..")
      .evaluate((element) => element.hasAttribute("open")))
  ) {
    await disclosure.press("Enter");
  }
  const editor = page.getByRole("region", {
    name: "Classification",
    exact: true,
  });
  await expect(editor).toBeVisible();
  return editor;
}

async function assertNoAxeViolations(page: Page, state: string) {
  await page.addScriptTag({ content: axe.source });
  const violations = await page.evaluate(async () => {
    const result = await (
      window as typeof window & { axe: typeof axe }
    ).axe.run(document.body, {
      runOnly: {
        type: "tag",
        values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"],
      },
    });
    return result.violations.map(({ id, impact, nodes }) => ({
      id,
      impact,
      nodes: nodes.map(({ target, html, failureSummary }) => ({
        target,
        html,
        failureSummary,
      })),
    }));
  });
  expect(violations, `${state} axe violations`).toEqual([]);
}

test("keyboard classification journey mutates shared production-backed state", async ({
  page,
  request,
}) => {
  const fixtureResponse = await request.get("/e2e/fixture");
  expect(fixtureResponse.ok()).toBeTruthy();
  const fixture = (await fixtureResponse.json()) as Fixture;

  await page.goto(
    `/#/project?recordType=task&recordID=${fixture.fallback_task_id}&clientID=${fixture.client_a}&label=Unclassified%20intake%20task`,
  );
  await expect(
    page.getByRole("heading", { name: "Unclassified intake task" }),
  ).toBeVisible();
  const fallbackEditor = await openClassification(page);
  const fallbackSelections = fallbackEditor.getByRole("group", {
    name: "Classification tags selections",
  });
  await expect(
    fallbackSelections.getByText("Unclassified", { exact: true }),
  ).toBeVisible();
  const fallbackPicker = fallbackEditor.getByRole("combobox", {
    name: "Classification tags",
  });
  await fallbackPicker.fill("Network");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await fallbackEditor
    .getByRole("button", { name: "Save classification" })
    .press("Enter");
  await expect(
    page.getByRole("status").filter({ hasText: "Classification saved." }),
  ).toBeVisible();
  await expect(
    fallbackSelections.getByText("Unclassified", { exact: true }),
  ).not.toBeVisible();
  await expect(
    fallbackSelections.getByText("Network", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(async () => {
      const response = await request.get("/e2e/evidence");
      const evidence = (await response.json()) as {
        current_unclassified: number;
        current_meaningful: number;
        fallback_removed_events: number;
        meaningful_added_events: number;
      };
      return {
        currentUnclassified: evidence.current_unclassified,
        currentMeaningful: evidence.current_meaningful,
        fallbackRemoved: evidence.fallback_removed_events > 0,
        meaningfulAdded: evidence.meaningful_added_events > 0,
      };
    })
    .toEqual({
      currentUnclassified: 0,
      currentMeaningful: 1,
      fallbackRemoved: true,
      meaningfulAdded: true,
    });
  await fallbackEditor
    .getByRole("button", { name: "Get AI suggestions" })
    .press("Enter");
  const accept = page.getByRole("button", { name: "Accept" });
  await expect(accept).toBeVisible({ timeout: 20_000 });
  await accept.press("Enter");
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: "AI tag accepted and applied." }),
  ).toBeVisible();

  await page.goto(
    `/#/project?recordType=task&recordID=${fixture.task_id}&clientID=${fixture.client_a}&label=Inherited%20security%20task`,
  );
  await expect(
    page.getByRole("heading", { name: "Inherited security task" }),
  ).toBeVisible();
  const editor = await openClassification(page);
  const picker = editor.getByRole("combobox", {
    name: "Classification tags",
  });
  await expect(picker).toBeVisible();
  await expect(
    page.getByText("Security — inherited from project"),
  ).toBeVisible();
  await assertNoAxeViolations(page, "classification picker");

  await page.goto("/#/classification-settings");
  await expect(
    page.getByRole("heading", { name: "Classification settings" }),
  ).toBeVisible();
  await assertNoAxeViolations(page, "classification settings");
  await page.getByRole("button", { name: "Archive Network" }).press("Enter");
  await page
    .getByRole("combobox", { name: "Replacement tag" })
    .selectOption(fixture.security_id);
  await page
    .getByRole("button", { name: "Preview archive impact" })
    .press("Enter");
  await expect(page.getByText(/^\d+ authorized objects$/)).toBeVisible();
  await page
    .getByRole("textbox", { name: "Reason" })
    .fill("Legacy taxonomy retired");
  await page.getByRole("button", { name: "Confirm archive" }).press("Enter");
  await expect(
    page.getByRole("status").filter({ hasText: "Tag archived." }),
  ).toBeVisible();

  await page.goto("/#/classification-insights");
  await expect(
    page.getByRole("heading", { name: "Classification insights" }),
  ).toBeVisible();
  await expect
    .poll(async () => {
      const response = await request.get("/e2e/evidence");
      const evidence = (await response.json()) as { recurring_ready: number };
      return evidence.recurring_ready;
    })
    .toBe(2);
  await page.getByRole("button", { name: "Run insights" }).press("Enter");
  const recurring = page
    .getByRole("heading", { name: "recurring issues" })
    .locator("..");
  await expect(recurring.getByText(/Security: [2-9]/)).toBeVisible({
    timeout: 20_000,
  });
  await recurring
    .getByRole("button", { name: "Load more evidence" })
    .press("Enter");
  const inheritedEvidence = recurring.getByRole("link", {
    name: "Open task Inherited security task",
  });
  await expect(inheritedEvidence).toBeVisible();
  await assertNoAxeViolations(page, "classification insights");

  await expect
    .poll(
      async () => {
        const evidenceResponse = await request.get("/e2e/evidence");
        const evidence = (await evidenceResponse.json()) as {
          archived: number;
          assignment_events: number;
        };
        return {
          archived: evidence.archived,
          assignmentEvents: evidence.assignment_events > 1,
        };
      },
      { timeout: 20_000 },
    )
    .toEqual({
      archived: 1,
      assignmentEvents: true,
    });

  await inheritedEvidence.click();
  await expect(page).toHaveURL(
    new RegExp(`recordType=task&recordID=${fixture.task_id}`),
  );
  await expect(
    page.getByRole("heading", { name: "Inherited security task" }),
  ).toBeVisible();
  await openClassification(page);
  await expect(
    page.getByText("Security — inherited from project"),
  ).toBeVisible();

  const activeClient = page.getByRole("combobox", { name: "Active client" });
  await activeClient.selectOption(fixture.client_b);
  await expect(activeClient).toHaveValue(fixture.client_b);
});
