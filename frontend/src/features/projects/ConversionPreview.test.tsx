import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConversionPreview } from "./ConversionPreview";
import type { ConversionPreviewModel } from "./types";

afterEach(cleanup);

const preview: ConversionPreviewModel = {
  hash: "preview-hash",
  opportunityID: "OPP-1042",
  proposalVersionID: "pv2",
  client: {
    action: "match",
    clientID: "client-id",
    name: "Northwind Legal",
  },
  projectDisplayID: "PRJ-204",
  projectName: "Security modernization",
  currency: "USD",
  originalBudgetMinor: 4800000,
  plannedMinutes: 7200,
  phases: [
    {
      position: 1,
      name: "Discovery",
      proposalLineIDs: ["line-1"],
      plannedMinutes: 1200,
      budgetMinor: 1200000,
    },
    {
      position: 2,
      name: "Delivery",
      proposalLineIDs: ["line-2"],
      plannedMinutes: 6000,
      budgetMinor: 3600000,
    },
  ],
  tasks: [
    { id: "task-1", title: "Schedule kickoff", version: 2, completed: false },
    {
      id: "task-2",
      title: "Capture requirements",
      version: 4,
      completed: false,
    },
  ],
};

describe("ConversionPreview", () => {
  it("shows every reviewed task as selected and immutable", () => {
    render(<ConversionPreview preview={preview} onConvert={vi.fn()} />);

    expect(
      screen.getByRole("checkbox", { name: "Schedule kickoff" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Schedule kickoff" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: "Capture requirements" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Capture requirements" }),
    ).toBeDisabled();
  });

  it("submits the exact reviewed task IDs and versions with the hash", () => {
    const onConvert = vi.fn();
    render(<ConversionPreview preview={preview} onConvert={onConvert} />);
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    expect(onConvert).toHaveBeenCalledWith({
      previewHash: "preview-hash",
      selectedTaskIDs: ["task-1", "task-2"],
      taskVersions: { "task-1": 2, "task-2": 4 },
    });
  });
});
