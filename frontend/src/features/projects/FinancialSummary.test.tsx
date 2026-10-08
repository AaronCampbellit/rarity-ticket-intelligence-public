import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { FinancialSummary } from "./FinancialSummary";

it("does not present unrated labor or recognized profit as zero", () => {
  render(
    <FinancialSummary
      summary={{
        currency: "USD",
        originalBudgetMinor: 20000,
        currentBudgetMinor: 20000,
        plannedLaborMinor: 7000,
        actualLaborMinor: 0,
        actualLaborComplete: false,
        costActualsMinor: 2000,
        committedCostMinor: 1000,
        billableWorkMinor: 16000,
        profitMinor: 0,
        profitAvailable: false,
        projectedProfitMinor: 10000,
        marginBasisPoints: 0,
      }}
    />,
  );
  expect(screen.getAllByText("Rate required")).toHaveLength(2);
});
