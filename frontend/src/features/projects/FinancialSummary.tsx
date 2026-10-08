import { formatMoney } from "./view-model";
import type { FinancialSummaryModel } from "./types";
import "./projects.css";

export function FinancialSummary({
  summary,
}: {
  summary: FinancialSummaryModel;
}) {
  const metrics = [
    ["Original commercial baseline", summary.originalBudgetMinor, true],
    ["Current budget", summary.currentBudgetMinor, true],
    ["Planned labor", summary.plannedLaborMinor, true],
    ["Actual labor", summary.actualLaborMinor, summary.actualLaborComplete],
    ["Cost actuals", summary.costActualsMinor, true],
    ["Committed cost", summary.committedCostMinor, true],
    ["Billable work", summary.billableWorkMinor, true],
    ["Recognized profit", summary.profitMinor, summary.profitAvailable],
  ] as const;
  return (
    <section className="project-panel" aria-labelledby="financial-title">
      <div className="project-panel-heading">
        <div>
          <h2 id="financial-title">Project financials</h2>
          <p>Original baseline, current plan, actuals, and recognized work</p>
        </div>
        <div className="profit-callout">
          <strong>
            {formatMoney(summary.projectedProfitMinor, summary.currency)}
          </strong>
          <span>
            Projected profit
            {summary.profitAvailable
              ? ` · recognized margin ${(
                  summary.marginBasisPoints / 100
                ).toFixed(1)}%`
              : " · recognized margin unavailable"}
          </span>
        </div>
      </div>
      <dl className="financial-grid">
        {metrics.map(([label, value, available]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>
              {available
                ? formatMoney(value, summary.currency)
                : "Rate required"}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
