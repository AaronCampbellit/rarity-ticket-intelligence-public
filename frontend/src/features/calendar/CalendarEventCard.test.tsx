import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CalendarEventCard, CalendarLensSwitcher } from "./CalendarEventCard";
import type { CalendarEvent } from "./types";
afterEach(cleanup);
it("offers exactly six pressed-state lens controls", () => {
  const change = vi.fn();
  render(<CalendarLensSwitcher value="week" onChange={change} />);
  expect(screen.getAllByRole("button")).toHaveLength(6);
  expect(screen.getByRole("button", { name: "Week view" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  fireEvent.click(screen.getByRole("button", { name: "Agenda view" }));
  expect(change).toHaveBeenCalledWith("agenda");
});
it("never renders source information from a busy event", () => {
  const event: CalendarEvent = {
    id: "one",
    occurrenceID: "one",
    title: "Private client name",
    privacy: "busy",
    allDay: true,
    startsOn: "2026-09-04",
    health: "blocked",
    hasConflict: true,
    healthReasons: [],
    capacityBearing: false,
    capabilities: { viewSource: true, schedule: true },
  };
  render(
    <CalendarEventCard
      event={event}
      timezone="America/Chicago"
      onSelect={vi.fn()}
    />,
  );
  expect(screen.getByText("Busy")).toBeInTheDocument();
  expect(screen.queryByText("Private client name")).not.toBeInTheDocument();
  expect(screen.queryByText("blocked")).not.toBeInTheDocument();
});
