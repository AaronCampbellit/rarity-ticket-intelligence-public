import { formatMoney } from "./view-model";
import type { ForecastRow } from "./types";
import "./sales.css";

export function ForecastView({ rows }: { rows: ForecastRow[] }) {
  return (
    <section className="sales-panel" aria-labelledby="forecast-title">
      <div className="panel-heading">
        <div>
          <h2 id="forecast-title">Revenue forecast</h2>
          <p>Pipeline, probability-weighted, and committed totals</p>
        </div>
      </div>
      <div className="proposal-table-wrap">
        <table className="proposal-table">
          <thead>
            <tr>
              <th>Owner</th>
              <th>Pipeline</th>
              <th>Weighted</th>
              <th>Committed</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.ownerID}>
                <th scope="row">{row.ownerName}</th>
                <td>{formatMoney(row.pipelineMinor, row.currency)}</td>
                <td>{formatMoney(row.weightedMinor, row.currency)}</td>
                <td>{formatMoney(row.committedMinor, row.currency)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
