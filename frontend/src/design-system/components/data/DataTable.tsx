import type { ReactNode } from "react";

import "./data.css";

export type DataColumn<T> = {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  align?: "start" | "end";
};

export function DataTable<T>({
  caption,
  columns,
  rows,
  getRowID,
}: {
  caption: string;
  columns: DataColumn<T>[];
  rows: T[];
  getRowID: (row: T) => string;
}) {
  return (
    <div className="rti-table-wrap">
      <table className="rti-table">
        <caption>{caption}</caption>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.id} scope="col" data-align={column.align}>
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={getRowID(row)}>
              {columns.map((column) => (
                <td key={column.id} data-align={column.align}>
                  {column.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
