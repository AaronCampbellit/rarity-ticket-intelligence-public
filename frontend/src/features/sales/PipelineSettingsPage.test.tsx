import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { PipelineSettingsPage } from "./PipelineSettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("starts new pipelines with a conversion-ready closed won stage", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json([])),
  );

  render(<PipelineSettingsPage capabilities={new Set(["pipeline.create"])} />);

  const closedWon = await screen.findByRole("group", { name: "Stage 3" });
  expect(within(closedWon).getByRole("textbox", { name: "Name" })).toHaveValue(
    "Closed won",
  );
  expect(within(closedWon).getByRole("textbox", { name: "Key" })).toHaveValue(
    "closed-won",
  );
  expect(
    within(closedWon).getByRole("spinbutton", { name: "Probability" }),
  ).toHaveValue(100);
  expect(
    within(closedWon).getByRole("combobox", { name: "Forecast category" }),
  ).toHaveValue("closed_won");
});
