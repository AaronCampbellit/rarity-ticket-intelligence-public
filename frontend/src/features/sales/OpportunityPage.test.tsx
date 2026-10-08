import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OpportunityPage } from "./OpportunityPage";
import type { OpportunityWorkspace } from "./types";

afterEach(cleanup);

const opportunity: OpportunityWorkspace = {
  id: "OPP-1042",
  name: "Northwind security modernization",
  clientName: "Northwind Legal",
  ownerName: "Priya Shah",
  teamName: "Advisory",
  stage: {
    id: "discovery",
    name: "Discovery",
    probability: 40,
    ageDays: 12,
  },
  nextStage: {
    id: "proposal",
    name: "Proposal",
    probability: 65,
    ageDays: 0,
    requiresProposal: true,
  },
  expectedValueMinor: 4800000,
  currency: "USD",
  expectedCloseOn: "2026-08-28",
  version: 7,
  activities: [
    { id: "a1", at: "Today, 9:42 AM", text: "Discovery call completed." },
  ],
  tasks: [
    { id: "t1", title: "Confirm licensing quantities", completed: false },
  ],
  proposalVersions: [],
};

describe("OpportunityPage", () => {
  it("blocks a gated transition and focuses the first missing field", async () => {
    render(
      <OpportunityPage opportunity={opportunity} onTransition={vi.fn()} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Move to Proposal" }));

    expect(
      await screen.findByText("A proposal is required for this stage."),
    ).toBeVisible();
    expect(screen.getByLabelText("Proposal")).toHaveFocus();
  });

  it("preserves entered values when a transition conflicts", async () => {
    const conflicted = {
      ...opportunity,
      proposalVersions: [
        {
          id: "pv1",
          version: 1,
          state: "issued" as const,
          totalMinor: 4800000,
          marginMinor: 1200000,
          currency: "USD",
          issuedAt: "2026-07-29",
          acceptance: null,
        },
      ],
    };
    const onTransition = vi
      .fn()
      .mockRejectedValue(new Error("version_conflict"));
    render(
      <OpportunityPage opportunity={conflicted} onTransition={onTransition} />,
    );
    const note = screen.getByLabelText("Opportunity note");
    fireEvent.change(note, { target: { value: "Keep this customer context" } });
    fireEvent.click(screen.getByRole("button", { name: "Move to Proposal" }));

    await waitFor(() =>
      expect(
        screen.getByText(
          "This opportunity changed. Review the latest version and retry.",
        ),
      ).toBeVisible(),
    );
    expect(note).toHaveValue("Keep this customer context");
  });
});
