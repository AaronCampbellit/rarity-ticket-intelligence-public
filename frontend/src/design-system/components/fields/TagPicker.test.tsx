import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { findA11yViolations } from "../../testing/renderA11y";
import { TagPicker } from "./TagPicker";

afterEach(cleanup);

const groups = [
  { id: "technology", label: "Technology", description: "" },
  { id: "service", label: "Service", description: "" },
];
const tags = [
  {
    id: "vpn",
    label: "VPN",
    groupId: "technology",
    state: "active" as const,
    synonyms: ["remote access"],
    version: 1,
  },
  {
    id: "billing",
    label: "Billing",
    groupId: "service",
    state: "active" as const,
    synonyms: [],
    version: 1,
  },
  {
    id: "retired",
    label: "Retired",
    groupId: "service",
    state: "archived" as const,
    synonyms: [],
    version: 1,
  },
];

describe("TagPicker", () => {
  it("searches labels and synonyms, selects with the keyboard, and keeps inherited tags", () => {
    const onChange = vi.fn();
    render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={["billing"]}
        inheritedIds={["vpn"]}
        onChange={onChange}
      />,
    );

    const input = screen.getByRole("combobox", { name: "Classification tags" });
    fireEvent.change(input, { target: { value: "remote access" } });
    expect(screen.getByRole("option", { name: /VPN/ })).toBeVisible();
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).toHaveBeenCalledWith(["billing", "vpn"]);
    expect(screen.getByText("Inherited from project")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Remove VPN" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove Billing" }));
    expect(onChange).toHaveBeenLastCalledWith([]);
  });

  it("announces validation and suggestion decisions without exposing internal keys", async () => {
    const decision = vi.fn();
    const view = render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={[]}
        required
        error="Select at least one tag."
        suggestions={[{ id: "suggestion-1", tagId: "vpn", confidence: 0.98 }]}
        onChange={() => {}}
        onSuggestionDecision={decision}
      />,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Select at least one tag.",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Accept suggestion VPN" }),
    );
    expect(decision).toHaveBeenCalledWith("suggestion-1", "accept");
    expect(view.container.textContent).not.toContain("internal_key");
    expect(await findA11yViolations(view.container)).toEqual([]);
  });

  it("surfaces recent and frequent visible tags ahead of grouped results", () => {
    render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={[]}
        recentIds={["billing"]}
        frequentIds={["vpn"]}
        onChange={() => {}}
      />,
    );

    fireEvent.focus(
      screen.getByRole("combobox", { name: "Classification tags" }),
    );
    expect(screen.getByText("Recent")).toBeVisible();
    expect(screen.getByText("Frequent")).toBeVisible();
  });

  it("uses one de-duplicated rendered option order for recent and frequent keyboard selection", () => {
    const onChange = vi.fn();
    render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={[]}
        recentIds={["billing"]}
        frequentIds={["vpn"]}
        onChange={onChange}
      />,
    );

    const input = screen.getByRole("combobox", { name: "Classification tags" });
    fireEvent.focus(input);
    expect(screen.getAllByRole("option", { name: /Billing/ })).toHaveLength(1);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute(
      "aria-activedescendant",
      screen.getByRole("option", { name: /Billing/ }).id,
    );
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).toHaveBeenLastCalledWith(["billing"]);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute(
      "aria-activedescendant",
      screen.getByRole("option", { name: /VPN/ }).id,
    );
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).toHaveBeenLastCalledWith(["vpn"]);
  });

  it("re-announces matching result counts after selection and a new search", async () => {
    render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={[]}
        onChange={() => {}}
      />,
    );
    const input = screen.getByRole("combobox", { name: "Classification tags" });
    fireEvent.focus(input);
    fireEvent.click(screen.getByRole("option", { name: /VPN/ }));
    expect(screen.getByText("VPN selected.")).toBeVisible();
    fireEvent.change(input, { target: { value: "billing" } });
    await waitFor(() =>
      expect(screen.getByText("1 matching tags.")).toBeVisible(),
    );
  });

  it("keeps virtual focus on the combobox, excludes options from Tab order, and scrolls the active long-list option into view", async () => {
    const user = userEvent.setup();
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    const longTags = Array.from({ length: 12 }, (_, index) => ({
      id: `tag-${index}`,
      label: `Technology ${index}`,
      groupId: "technology",
      state: "active" as const,
      synonyms: [],
      version: 1,
    }));
    render(
      <>
        <TagPicker
          label="Classification tags"
          groups={[groups[0]]}
          tags={longTags}
          selectedIds={[]}
          onChange={() => {}}
        />
        <button type="button">After picker</button>
      </>,
    );
    const input = screen.getByRole("combobox", { name: "Classification tags" });
    await user.click(input);
    for (const option of screen.getAllByRole("option")) {
      expect(option).toHaveAttribute("tabindex", "-1");
    }
    for (let index = 0; index < 10; index += 1) {
      fireEvent.keyDown(input, { key: "ArrowDown" });
    }
    expect(scrollIntoView).toHaveBeenCalled();
    await user.tab();
    expect(screen.getByRole("button", { name: "After picker" })).toHaveFocus();
  });

  it("labels a pending AI suggestion accurately before and after its decision", () => {
    const decision = vi.fn();
    render(
      <TagPicker
        label="Classification tags"
        groups={groups}
        tags={tags}
        selectedIds={[]}
        suggestions={[{ id: "suggestion-1", tagId: "vpn" }]}
        onChange={() => {}}
        onSuggestionDecision={decision}
      />,
    );
    expect(screen.getByText("AI suggestion pending review")).toBeVisible();
    expect(
      screen.getByLabelText("VPN. AI suggestion pending review"),
    ).toBeVisible();
    expect(
      screen.queryByText("Applied automatically by AI"),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Accept suggestion VPN" }),
    );
    expect(decision).toHaveBeenCalledWith("suggestion-1", "accept");
    expect(
      screen.queryByText("AI suggestion pending review"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("VPN suggestion accepted.")).toBeVisible();
  });
});
