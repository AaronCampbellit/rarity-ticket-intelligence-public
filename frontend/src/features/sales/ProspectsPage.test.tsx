import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { ProspectsPage } from "./ProspectsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("creates a prospect-linked opportunity without Client context", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/prospects?limit=500") {
        return Response.json([
          {
            id: "prospect-1",
            display_id: "LEAD-1",
            name: "Northwind",
            email: "buyer@example.test",
            version: 1,
            created_at: "2026-07-30T12:00:00Z",
          },
        ]);
      }
      if (url === "/api/v1/pipelines") {
        return Response.json([
          {
            id: "pipeline-1",
            key: "default",
            name: "Default",
            stages: [
              {
                id: "stage-1",
                pipeline_id: "pipeline-1",
                key: "qualified",
                name: "Qualified",
                position: 1,
                probability: 30,
                forecast_category: "weighted",
                required_fields: [],
                allowed_next_stage_ids: [],
                requires_proposal: false,
                requires_approval: false,
              },
            ],
          },
        ]);
      }
      if (url === "/api/v1/opportunities" && init?.method === "POST") {
        return Response.json({ id: "opportunity-1" }, { status: 201 });
      }
      return Response.json({}, { status: 500 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <ProspectsPage
      capabilities={
        new Set(["opportunity.read", "opportunity.create", "prospect.create"])
      }
    />,
  );
  expect((await screen.findAllByText("Northwind")).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByText("Create opportunity from Prospect"));
  fireEvent.change(screen.getAllByLabelText("Display ID").at(-1)!, {
    target: { value: "OPP-1" },
  });
  fireEvent.change(screen.getAllByLabelText("Name").at(-1)!, {
    target: { value: "Managed services" },
  });
  fireEvent.change(screen.getByLabelText("Pipeline"), {
    target: { value: "pipeline-1" },
  });
  fireEvent.change(screen.getByLabelText("Amount"), {
    target: { value: "2500" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Create linked opportunity" }),
  );
  await waitFor(() =>
    expect(
      screen.getByText("Opportunity created from the Prospect."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input) === "/api/v1/opportunities" && init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).not.toHaveProperty("X-Rarity-Client-ID");
  expect(String(mutation?.[1]?.body)).toContain('"prospect_id":"prospect-1"');
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});
