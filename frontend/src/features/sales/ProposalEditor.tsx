import { useMemo, useState } from "react";

import { Button, Page, Select, StatusBadge } from "../../design-system";
import { formatMoney, marginPercent } from "./view-model";
import type { ProposalLineType, ProposalWorkspace } from "./types";
import "./sales.css";

const lineTypeOptions: Array<{ value: ProposalLineType; label: string }> = [
  { value: "fixed_fee", label: "Fixed fee" },
  { value: "time_and_materials", label: "Time and materials" },
  { value: "product_license", label: "Product or license" },
  { value: "recurring_service", label: "Recurring service" },
];

export function ProposalEditor({ proposal }: { proposal: ProposalWorkspace }) {
  const [lineType, setLineType] = useState<ProposalLineType>("fixed_fee");
  const totals = useMemo(() => {
    let revenue = 0;
    let cost = 0;
    for (const line of proposal.lines) {
      revenue += line.quantity * line.unitPriceMinor - line.discountMinor;
      cost += line.quantity * line.unitCostMinor;
    }
    return { revenue, cost, margin: revenue - cost };
  }, [proposal.lines]);

  return (
    <Page
      className="sales-page"
      eyebrow={proposal.id}
      title="Proposal workspace"
      description="Versions are retained as the commercial record."
      actions={
        <StatusBadge
          tone={proposal.approvalState === "required" ? "warning" : "neutral"}
        >
          {proposal.approvalState === "required"
            ? "Internal approval required"
            : "Approval not required"}
        </StatusBadge>
      }
    >
      <div className="proposal-layout">
        <section className="sales-panel" aria-labelledby="lines-heading">
          <div className="panel-heading">
            <div>
              <h2 id="lines-heading">Current draft</h2>
              <p>Scope, pricing, planned work, and margin</p>
            </div>
            <label className="compact-field">
              <span>Line type</span>
              <Select
                value={lineType}
                onChange={(event) =>
                  setLineType(event.target.value as ProposalLineType)
                }
              >
                {lineTypeOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </label>
          </div>
          <div className="proposal-table-wrap">
            <table className="proposal-table">
              <thead>
                <tr>
                  <th>Scope</th>
                  <th>Type</th>
                  <th>Planned</th>
                  <th>Price</th>
                  <th>Margin</th>
                </tr>
              </thead>
              <tbody>
                {proposal.lines.map((line) => {
                  const revenue =
                    line.quantity * line.unitPriceMinor - line.discountMinor;
                  const margin = revenue - line.quantity * line.unitCostMinor;
                  return (
                    <tr key={line.id}>
                      <td>{line.description}</td>
                      <td>
                        {lineTypeOptions.find(
                          (option) => option.value === line.type,
                        )?.label ?? line.type}
                      </td>
                      <td>{Math.round(line.plannedMinutes / 60)}h</td>
                      <td>{formatMoney(revenue, proposal.currency)}</td>
                      <td>{marginPercent(margin, revenue)}</td>
                    </tr>
                  );
                })}
              </tbody>
              <tfoot>
                <tr>
                  <th colSpan={3}>Draft total</th>
                  <td>{formatMoney(totals.revenue, proposal.currency)}</td>
                  <td>{marginPercent(totals.margin, totals.revenue)}</td>
                </tr>
              </tfoot>
            </table>
          </div>
        </section>

        <aside
          className="sales-panel version-rail"
          aria-labelledby="versions-heading"
        >
          <div className="panel-heading">
            <div>
              <h2 id="versions-heading">Proposal versions</h2>
              <p>Issued versions cannot be edited</p>
            </div>
          </div>
          <ol>
            {proposal.versions.map((version) => (
              <li key={version.id}>
                <div>
                  <strong>Version {version.version}</strong>
                  <span>{version.state}</span>
                </div>
                <p>
                  {formatMoney(version.totalMinor, version.currency)} ·{" "}
                  {marginPercent(version.marginMinor, version.totalMinor)}{" "}
                  margin
                </p>
                {version.acceptance ? (
                  <small>
                    Accepted by {version.acceptance.signerName} ·{" "}
                    {version.acceptance.method}
                  </small>
                ) : null}
                <Button
                  type="button"
                  size="compact"
                  disabled={version.state !== "draft"}
                  aria-label={`Edit version ${version.version}`}
                >
                  {version.state === "draft" ? "Edit draft" : "Immutable"}
                </Button>
              </li>
            ))}
          </ol>
        </aside>
      </div>
    </Page>
  );
}
