import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { findA11yViolations } from "../../design-system/testing/renderA11y";
import { MentionPicker } from "./MentionPicker";
import type { MentionAPI, MentionCandidate, MentionContext } from "./types";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const context: MentionContext = {
  clientId: "client-1",
  parentType: "work_record",
  parentId: "work-1",
  sourceKind: "comment",
};

const peopleAndTeams: MentionCandidate[] = [
  { targetType: "staff", id: "staff-2", label: "Mira", version: 1 },
  {
    targetType: "team",
    id: "team-1",
    label: "NOC",
    eligibleCount: 2,
    excludedCount: 0,
    eligibleMemberIds: ["staff-2", "staff-3"],
    version: 4,
  },
];

function apiReturning(values: MentionCandidate[]): MentionAPI {
  return {
    candidates: vi.fn().mockResolvedValue(values),
    save: vi.fn(),
  };
}

describe("MentionPicker", () => {
  it("debounces search, groups People and Teams, caps results, and selects by keyboard", async () => {
    vi.useFakeTimers();
    const api = apiReturning([
      ...peopleAndTeams,
      ...Array.from({ length: 60 }, (_, index) => ({
        targetType: "staff" as const,
        id: `staff-${index + 10}`,
        label: `Person ${index}`,
        version: 1,
      })),
    ]);
    const selected = vi.fn();
    render(<MentionPicker context={context} api={api} onSelect={selected} />);
    const input = screen.getByRole("combobox", {
      name: "Mention a person or team",
    });
    fireEvent.change(input, { target: { value: "mi" } });
    expect(api.candidates).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTimeAsync(150));
    expect(screen.getByRole("option", { name: /Mira/ })).toBeVisible();

    expect(screen.getByRole("group", { name: "People" })).toBeVisible();
    expect(screen.getByRole("group", { name: "Teams" })).toBeVisible();
    expect(screen.getAllByRole("option")).toHaveLength(50);
    expect(
      screen.getAllByRole("option").every((option) => option.tabIndex === -1),
    ).toBe(true);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(selected).toHaveBeenCalledWith(peopleAndTeams[0], undefined);
  });

  it("aborts superseded searches and ignores stale responses", async () => {
    vi.useFakeTimers();
    const pending = new Map<string, (value: MentionCandidate[]) => void>();
    const signals: AbortSignal[] = [];
    const api: MentionAPI = {
      candidates: vi.fn(
        (_: MentionContext, query: string, signal?: AbortSignal) => {
          if (signal) signals.push(signal);
          return new Promise<MentionCandidate[]>((resolve) =>
            pending.set(query, resolve),
          );
        },
      ),
      save: vi.fn(),
    };
    render(<MentionPicker context={context} api={api} onSelect={() => {}} />);
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "m" } });
    await act(async () => vi.advanceTimersByTimeAsync(150));
    fireEvent.change(input, { target: { value: "mi" } });
    await act(async () => vi.advanceTimersByTimeAsync(150));
    expect(signals[0].aborted).toBe(true);

    pending.get("mi")?.([
      { targetType: "staff", id: "new", label: "Mira", version: 1 },
    ]);
    await act(async () => Promise.resolve());
    expect(screen.getByRole("option", { name: /Mira/ })).toBeVisible();
    pending.get("m")?.([
      { targetType: "staff", id: "old", label: "Morgan", version: 1 },
    ]);
    await Promise.resolve();
    expect(
      screen.queryByRole("option", { name: /Morgan/ }),
    ).not.toBeInTheDocument();
  });

  it("removes prior-query candidates before the replacement debounce starts", async () => {
    vi.useFakeTimers();
    const selected = vi.fn();
    const pending: Array<(values: MentionCandidate[]) => void> = [];
    const api: MentionAPI = {
      candidates: vi.fn(
        () =>
          new Promise<MentionCandidate[]>((resolve) => pending.push(resolve)),
      ),
      save: vi.fn(),
    };
    render(
      <MentionPicker
        context={context}
        api={api}
        initialQuery="mi"
        onSelect={selected}
      />,
    );
    const input = screen.getByRole("combobox");
    await act(async () => vi.advanceTimersByTimeAsync(150));
    pending.shift()?.([peopleAndTeams[0]]);
    await act(async () => Promise.resolve());
    const staleOption = screen.getByRole("option", { name: /Mira/ });

    fireEvent.change(input, { target: { value: "new" } });
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading mention suggestions",
    );
    expect(screen.queryByRole("option", { name: /Mira/ })).toBeNull();
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.click(staleOption);
    expect(selected).not.toHaveBeenCalled();

    await act(async () => vi.advanceTimersByTimeAsync(150));
    expect(screen.queryByRole("option", { name: /Mira/ })).toBeNull();
  });

  it("removes and disables prior-context candidates as soon as scope changes", async () => {
    vi.useFakeTimers();
    const selected = vi.fn();
    const signals: AbortSignal[] = [];
    const pending: Array<(values: MentionCandidate[]) => void> = [];
    const api: MentionAPI = {
      candidates: vi.fn(
        (_context, _query, signal) =>
          new Promise<MentionCandidate[]>((resolve) => {
            if (signal) signals.push(signal);
            pending.push(resolve);
          }),
      ),
      save: vi.fn(),
    };
    const view = render(
      <MentionPicker context={context} api={api} onSelect={selected} />,
    );
    const input = screen.getByRole("combobox");
    await act(async () => vi.advanceTimersByTimeAsync(150));
    pending.shift()?.([peopleAndTeams[0]]);
    await act(async () => Promise.resolve());
    const staleOption = screen.getByRole("option", { name: /Mira/ });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute("aria-activedescendant");

    view.rerender(
      <MentionPicker
        context={{ ...context, clientId: "client-2", parentId: "work-2" }}
        api={api}
        onSelect={selected}
      />,
    );
    expect(signals[0].aborted).toBe(true);
    expect(screen.queryByRole("option", { name: /Mira/ })).toBeNull();
    expect(input).not.toHaveAttribute("aria-activedescendant");
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.click(staleOption);
    expect(selected).not.toHaveBeenCalled();

    await act(async () => vi.advanceTimersByTimeAsync(150));
    const fresh = {
      targetType: "staff" as const,
      id: "staff-fresh",
      label: "Morgan",
      version: 1,
    };
    pending.shift()?.([fresh]);
    await act(async () => Promise.resolve());
    expect(screen.getByRole("option", { name: /Morgan/ })).toBeVisible();
    expect(input).not.toHaveAttribute("aria-activedescendant");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(selected).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(selected).toHaveBeenCalledWith(fresh, undefined);
  });

  it("announces loading, errors, and empty results and dismisses with Escape", async () => {
    vi.useFakeTimers();
    const dismiss = vi.fn();
    const api = apiReturning([]);
    vi.mocked(api.candidates)
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce([]);
    const view = render(
      <MentionPicker
        context={context}
        api={api}
        onSelect={() => {}}
        onDismiss={dismiss}
      />,
    );
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "mi" } });
    await act(async () => vi.advanceTimersByTimeAsync(150));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Mention suggestions are unavailable",
    );
    fireEvent.change(input, { target: { value: "none" } });
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading mention suggestions",
    );
    await act(async () => vi.advanceTimersByTimeAsync(150));
    expect(screen.getByRole("status")).toHaveTextContent(
      "No matching people or teams",
    );
    fireEvent.keyDown(input, { key: "Escape" });
    expect(dismiss).toHaveBeenCalled();
    vi.useRealTimers();
    expect(await findA11yViolations(view.container)).toEqual([]);
  });

  it("requires an exact confirmation for partial teams but inserts fully eligible teams directly", async () => {
    const user = userEvent.setup();
    const partial: MentionCandidate = {
      targetType: "team",
      id: "team-partial",
      label: "Field Team",
      eligibleCount: 2,
      excludedCount: 1,
      eligibleMemberIds: ["staff-2", "staff-3"],
      version: 8,
    };
    const selected = vi.fn();
    render(
      <MentionPicker
        context={context}
        api={apiReturning([partial, peopleAndTeams[1]])}
        initialQuery="team"
        onSelect={selected}
      />,
    );
    expect(
      await screen.findByRole("option", { name: /Field Team/ }),
    ).toBeVisible();
    await user.click(screen.getByRole("option", { name: /Field Team/ }));
    expect(selected).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        /2 eligible members will be mentioned; 1 member will be excluded/,
      ),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Confirm Field Team mention" }),
    );
    expect(selected).toHaveBeenCalledWith(partial, {
      teamVersion: 8,
      eligibleMemberIds: ["staff-2", "staff-3"],
    });

    await user.click(screen.getByRole("option", { name: /NOC/ }));
    expect(selected).toHaveBeenLastCalledWith(peopleAndTeams[1], undefined);
  });

  it("returns focus exactly once to the combobox when partial-team confirmation is cancelled", async () => {
    const user = userEvent.setup();
    const partial: MentionCandidate = {
      targetType: "team",
      id: "team-partial",
      label: "Field Team",
      eligibleCount: 1,
      excludedCount: 1,
      eligibleMemberIds: ["staff-2"],
      version: 2,
    };
    render(
      <MentionPicker
        context={context}
        api={apiReturning([partial])}
        onSelect={() => {}}
      />,
    );
    const input = screen.getByRole("combobox");
    await user.click(await screen.findByRole("option", { name: /Field Team/ }));
    const focus = vi.spyOn(input, "focus");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(input).toHaveFocus());
    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("applies grouped picker layout and option styling to the rendered list", async () => {
    render(
      <MentionPicker
        context={context}
        api={apiReturning(peopleAndTeams)}
        onSelect={() => {}}
      />,
    );
    const option = await screen.findByRole("option", { name: /Mira/ });
    const group = screen.getByRole("group", { name: "People" });
    expect(option.closest(".rti-option-list")).not.toBeNull();
    expect(getComputedStyle(group).display).toBe("grid");
    expect(getComputedStyle(option).display).toBe("flex");
    expect(getComputedStyle(option).width).toBe("100%");
  });

  it("keeps keyboard traversal in the same People-then-Teams order as the DOM", async () => {
    const selected = vi.fn();
    const alphabeticTeam: MentionCandidate = {
      targetType: "team",
      id: "team-a",
      label: "Alpha Team",
      eligibleCount: 1,
      excludedCount: 0,
      eligibleMemberIds: ["staff-2"],
      version: 1,
    };
    const person: MentionCandidate = {
      targetType: "staff",
      id: "staff-z",
      label: "Zara",
      version: 1,
    };
    render(
      <MentionPicker
        context={context}
        api={apiReturning([alphabeticTeam, person])}
        onSelect={selected}
      />,
    );
    const input = screen.getByRole("combobox");
    await screen.findByRole("option", { name: /Zara/ });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(selected).toHaveBeenCalledWith(person, undefined);
  });

  it("dismisses from partial-team actions with Escape", async () => {
    const user = userEvent.setup();
    const dismiss = vi.fn();
    const partial: MentionCandidate = {
      targetType: "team",
      id: "team-partial",
      label: "Field Team",
      eligibleCount: 1,
      excludedCount: 1,
      eligibleMemberIds: ["staff-2"],
      version: 2,
    };
    render(
      <MentionPicker
        context={context}
        api={apiReturning([partial])}
        onSelect={() => {}}
        onDismiss={dismiss}
      />,
    );
    await user.click(await screen.findByRole("option", { name: /Field Team/ }));
    const cancel = screen.getByRole("button", { name: "Cancel" });
    cancel.focus();
    await user.keyboard("{Escape}");
    expect(dismiss).toHaveBeenCalled();
  });
});
