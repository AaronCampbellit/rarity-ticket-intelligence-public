import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DataTable } from "./DataTable";
import { FilterBar } from "./FilterBar";
import { Pagination } from "./Pagination";
import { Worklist } from "./Worklist";

afterEach(cleanup);

const records = [
  { id: "one", label: "INC-10482 VPN unavailable" },
  { id: "two", label: "REQ-10483 New starter setup" },
];

describe("Worklist", () => {
  it("selects native controls and identifies the current record", () => {
    const select = vi.fn();
    render(
      <Worklist
        items={records}
        getID={(record) => record.id}
        selectedID="one"
        getLabel={(record) => record.label}
        renderItem={(record) => <span>{record.label}</span>}
        renderDetail={(record) => <h2>{record.label}</h2>}
        empty={<p>No work found.</p>}
        onSelect={select}
      />,
    );

    expect(
      screen.getByRole("button", { name: records[0].label }),
    ).toHaveAttribute("aria-current", "true");
    fireEvent.click(screen.getByRole("button", { name: records[1].label }));
    expect(select).toHaveBeenCalledWith(records[1]);
  });

  it("renders its supplied empty state", () => {
    render(
      <Worklist
        items={[]}
        getID={(record: { id: string }) => record.id}
        getLabel={() => ""}
        renderItem={() => null}
        renderDetail={() => null}
        empty={<p>No work found.</p>}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText("No work found.")).toBeVisible();
  });

  it("uses one click for preview and double click for full open", () => {
    const preview = vi.fn();
    const openFull = vi.fn();
    render(
      <Worklist
        items={records}
        getID={(record) => record.id}
        getLabel={(record) => record.label}
        renderItem={(record) => <span>{record.label}</span>}
        renderDetail={() => null}
        empty={null}
        onSelect={preview}
        onOpenFull={openFull}
      />,
    );

    const record = screen.getByRole("button", { name: records[0].label });
    fireEvent.click(record);
    expect(preview).toHaveBeenCalledOnce();
    fireEvent.doubleClick(record);
    expect(openFull).toHaveBeenCalledWith(records[0]);
    fireEvent.click(
      screen.getByRole("button", {
        name: `Open full record ${records[0].label}`,
      }),
    );
    expect(openFull).toHaveBeenCalledTimes(2);
    expect(preview).toHaveBeenCalledOnce();
  });
});

describe("data controls", () => {
  it("keeps a semantic table and labeled caption", () => {
    render(
      <DataTable
        caption="Connection health"
        columns={[{ id: "name", header: "Name", cell: (row) => row.name }]}
        rows={[{ id: "one", name: "Local Ollama" }]}
        getRowID={(row) => row.id}
      />,
    );
    expect(
      screen.getByRole("table", { name: "Connection health" }),
    ).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeVisible();
  });

  it("shows active filter count and resets filters", () => {
    const reset = vi.fn();
    render(
      <FilterBar activeCount={2} onReset={reset}>
        <label>
          Status{" "}
          <select>
            <option>Open</option>
          </select>
        </label>
      </FilterBar>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Reset 2 filters" }));
    expect(reset).toHaveBeenCalledOnce();
  });

  it("describes the range and disables impossible pagination", () => {
    render(
      <Pagination
        start={1}
        end={20}
        total={20}
        onPrevious={() => {}}
        onNext={() => {}}
      />,
    );
    expect(screen.getByText("1–20 of 20")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Previous page" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });
});
