import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Combobox } from "./Combobox";
import { DateRangePicker } from "./DateRangePicker";
import { MultiSelect } from "./MultiSelect";

afterEach(cleanup);

const options = [
  { value: "alex", label: "Alex Morgan" },
  { value: "sam", label: "Sam Rivera" },
  { value: "jamie", label: "Jamie Chen" },
];

describe("interactive fields", () => {
  it("filters and keyboard-selects a combobox option", () => {
    const select = vi.fn();
    render(
      <Combobox
        label="Assignee"
        options={options}
        value=""
        onChange={select}
      />,
    );
    const input = screen.getByRole("combobox", { name: "Assignee" });
    fireEvent.change(input, { target: { value: "sam" } });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(select).toHaveBeenCalledWith("sam");
  });

  it("resets virtual focus when options reorder or become empty", () => {
    const select = vi.fn();
    const view = render(
      <Combobox
        label="Assignee"
        options={options}
        value=""
        onChange={select}
      />,
    );
    const input = screen.getByRole("combobox", { name: "Assignee" });
    fireEvent.focus(input);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute("aria-activedescendant");

    view.rerender(
      <Combobox
        label="Assignee"
        options={[options[2], options[0], options[1]]}
        value=""
        onChange={select}
      />,
    );
    expect(input).not.toHaveAttribute("aria-activedescendant");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(select).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute("aria-activedescendant");
    view.rerender(
      <Combobox label="Assignee" options={[]} value="" onChange={select} />,
    );
    expect(input).not.toHaveAttribute("aria-activedescendant");
    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(input).not.toHaveAttribute("aria-activedescendant");
  });

  it("adds and removes multi-select chips", () => {
    const change = vi.fn();
    const { rerender } = render(
      <MultiSelect
        label="Notify"
        options={options}
        values={["alex"]}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove Alex Morgan" }));
    expect(change).toHaveBeenCalledWith([]);

    rerender(
      <MultiSelect
        label="Notify"
        options={options}
        values={[]}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Choose Notify" }));
    fireEvent.click(screen.getByRole("option", { name: "Sam Rivera" }));
    expect(change).toHaveBeenCalledWith(["sam"]);
  });

  it("reports an invalid date range", () => {
    render(
      <DateRangePicker
        label="Service window"
        start="2026-08-12"
        end="2026-08-10"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "End date must be on or after the start date.",
    );
  });
});
