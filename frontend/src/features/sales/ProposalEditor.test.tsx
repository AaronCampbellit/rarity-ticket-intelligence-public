import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ProposalEditor } from "./ProposalEditor";
import type { ProposalWorkspace } from "./types";

afterEach(cleanup);

const proposal: ProposalWorkspace = {
  id: "PROP-81",
  currency: "USD",
  approvalState: "required",
  versions: [
    {
      id: "pv2",
      version: 2,
      state: "draft",
      totalMinor: 5200000,
      marginMinor: 1400000,
      currency: "USD",
      issuedAt: null,
      acceptance: null,
    },
    {
      id: "pv1",
      version: 1,
      state: "issued",
      totalMinor: 4800000,
      marginMinor: 1200000,
      currency: "USD",
      issuedAt: "2026-07-22",
      acceptance: null,
    },
  ],
  lines: [
    {
      id: "line-1",
      type: "fixed_fee",
      description: "Discovery and design",
      quantity: 1,
      unitPriceMinor: 1200000,
      unitCostMinor: 400000,
      discountMinor: 0,
      plannedMinutes: 1200,
    },
  ],
};

describe("ProposalEditor", () => {
  it("keeps issued versions immutable while allowing the draft to change", () => {
    render(<ProposalEditor proposal={proposal} />);

    expect(
      screen.getByRole("button", { name: "Edit version 1" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Edit version 2" }),
    ).toBeEnabled();
    expect(screen.getByText("Internal approval required")).toBeVisible();
  });

  it("supports each approved proposal line type", () => {
    render(<ProposalEditor proposal={proposal} />);
    const selector = screen.getByLabelText("Line type");

    for (const value of [
      "fixed_fee",
      "time_and_materials",
      "product_license",
      "recurring_service",
    ]) {
      fireEvent.change(selector, { target: { value } });
    }

    expect(screen.getByRole("option", { name: "Fixed fee" })).toBeVisible();
    expect(
      screen.getByRole("option", { name: "Time and materials" }),
    ).toBeVisible();
    expect(
      screen.getByRole("option", { name: "Product or license" }),
    ).toBeVisible();
    expect(
      screen.getByRole("option", { name: "Recurring service" }),
    ).toBeVisible();
  });
});
