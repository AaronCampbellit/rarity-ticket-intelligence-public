import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CalendarPage } from "./CalendarPage";
import { dateInZone } from "./dates";
import type { CalendarAPI, CalendarEvent } from "./types";

afterEach(cleanup);
const event: CalendarEvent = {
  id: "event",
  occurrenceID: "occurrence",
  title: "Visit Alpha",
  privacy: "full",
  allDay: true,
  startsOn: dateInZone(new Date(), "UTC"),
  eventRole: "scheduled_work",
  projectionID: "projection",
  occurrenceKey: "occurrence-key",
  source: { type: "work_record", id: "ticket", client_id: "alpha" },
  health: "on_track",
  healthReasons: [],
  hasConflict: false,
  capacityBearing: false,
  schedulingMode: "fixed_block",
  capabilities: { viewSource: true, schedule: true },
};
function api(): CalendarAPI {
  return {
    events: vi.fn().mockResolvedValue({ events: [event] }),
    options: vi.fn().mockResolvedValue({ clients: { alpha: 1, bravo: 1 } }),
    capacity: vi.fn().mockResolvedValue({}),
    live: vi.fn().mockResolvedValue({ cursor: "one", changed: false }),
    views: vi.fn().mockResolvedValue([]),
    saveView: vi.fn(),
    preview: vi.fn(),
    apply: vi.fn(),
  };
}
it("loads all authorized clients, changes lenses and opens safe source details", async () => {
  const service = api();
  render(
    <CalendarPage
      principalID="tech"
      api={service}
      clients={[{ id: "alpha", name: "Alpha" }]}
    />,
  );
  const card = await screen.findByRole("button", { name: /Visit Alpha/ });
  expect(service.events).toHaveBeenCalledWith(
    expect.objectContaining({ filter: {} }),
    expect.any(AbortSignal),
  );
  fireEvent.click(card);
  const dialog = screen.getByRole("dialog", { name: "Visit Alpha" });
  expect(
    within(dialog).getByRole("link", { name: "Open source record" }),
  ).toHaveAttribute("href", expect.stringContaining("clientID=alpha"));
  fireEvent.keyDown(dialog, { key: "Escape" });
  fireEvent.click(screen.getByRole("button", { name: "Agenda view" }));
  await screen.findByRole("button", { name: /Visit Alpha/ });
  expect(screen.getByRole("button", { name: "Agenda view" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});
it("ignores a response from an earlier filter window", async () => {
  const service = api();
  let resolve!: (value: { events: CalendarEvent[] }) => void;
  service.events = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    )
    .mockResolvedValue({ events: [] });
  render(<CalendarPage principalID="tech" api={service} />);
  await waitFor(() => expect(service.events).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole("button", { name: "Next period" }));
  await screen.findByText("No scheduled events match this window and filters.");
  await act(async () => resolve({ events: [event] }));
  expect(screen.queryByText("Visit Alpha")).not.toBeInTheDocument();
});
it("does not turn failed capacity into a successful empty view", async () => {
  const service = api();
  service.capacity = vi
    .fn()
    .mockRejectedValue(new Error("Capacity unavailable"));
  render(<CalendarPage principalID="tech" api={service} />);
  await screen.findByRole("button", { name: /Visit Alpha/ });
  fireEvent.click(screen.getByRole("button", { name: "Capacity view" }));
  await screen.findByText("Capacity unavailable");
  expect(
    screen.queryByText("No authorized technician capacity in this window."),
  ).not.toBeInTheDocument();
});
it("requires a preview and a reason before applying a schedule change", async () => {
  const service = api();
  service.preview = vi.fn().mockResolvedValue({
    id: "proposal",
    version: 3,
    expiresAt: new Date(Date.now() + 600000).toISOString(),
    state: "pending",
    changes: [
      {
        id: "change",
        required: true,
        title: "Visit Alpha",
        start: event.startsOn,
      },
    ],
    conflicts: [],
    blocked: [],
    capacity: [],
    health: [],
    notifications: [],
  });
  service.apply = vi.fn().mockResolvedValue(undefined);
  render(
    <CalendarPage
      principalID="tech"
      capabilities={new Set(["calendar.schedule"])}
      api={service}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Visit Alpha/ }));
  fireEvent.click(screen.getByRole("button", { name: "Reschedule" }));
  fireEvent.click(
    screen.getByRole("button", { name: "Preview schedule change" }),
  );
  const confirm = await screen.findByRole("button", {
    name: "Confirm schedule changes",
  });
  expect(confirm).toBeDisabled();
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Customer requested a later visit" },
  });
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(service.apply).toHaveBeenCalledWith(
      expect.objectContaining({ id: "proposal", version: 3 }),
      [],
      "Customer requested a later visit",
    ),
  );
  await screen.findByText(/Schedule changes applied/);
});
