import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { TicketTimer } from "./TicketTimer";
import type { TicketTimeCapture } from "./api";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("stops a ticket timer and emits the authoritative server capture", async () => {
  const onCapture = vi.fn<(capture: TicketTimeCapture) => void>();
  const startedAt = "2026-08-04T18:00:00Z";
  const stoppedAt = "2026-08-04T18:15:00Z";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (!init?.method) return Response.json([]);
      if (url.endsWith("/work-records/ticket-1/timers")) {
        return Response.json({
          id: "capture-1",
          msp_id: "msp",
          client_id: "client-1",
          work_record_id: "ticket-1",
          technician_id: "technician",
          state: "running",
          started_at: startedAt,
          version: 1,
          created_at: startedAt,
          updated_at: startedAt,
        });
      }
      if (url.endsWith("/timers/capture-1:stop")) {
        return Response.json({
          id: "capture-1",
          msp_id: "msp",
          client_id: "client-1",
          work_record_id: "ticket-1",
          technician_id: "technician",
          state: "stopped",
          started_at: startedAt,
          stopped_at: stoppedAt,
          duration_seconds: 900,
          version: 2,
          created_at: startedAt,
          updated_at: stoppedAt,
        });
      }
      return Response.json({}, { status: 404 });
    },
  );
  vi.stubGlobal("fetch", fetcher);

  render(
    <TicketTimer
      clientID="client-1"
      workRecordID="ticket-1"
      onCapture={onCapture}
    />,
  );
  await screen.findByRole("button", { name: "Start timer" });
  fireEvent.click(screen.getByRole("button", { name: "Start timer" }));
  await screen.findByRole("button", { name: "Stop timer" });
  fireEvent.click(screen.getByRole("button", { name: "Stop timer" }));

  await waitFor(() =>
    expect(onCapture).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "capture-1",
        durationSeconds: 900,
        version: 2,
      }),
    ),
  );
  expect(screen.getByText("Captured 15m")).toBeVisible();
});

it("restores an existing running timer when the ticket is reopened", async () => {
  const startedAt = new Date(Date.now() - 60_000).toISOString();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json([
        {
          id: "running-1",
          msp_id: "msp",
          client_id: "client-1",
          work_record_id: "ticket-1",
          technician_id: "technician",
          state: "running",
          started_at: startedAt,
          version: 1,
          created_at: startedAt,
          updated_at: startedAt,
        },
      ]),
    ),
  );

  render(
    <TicketTimer
      clientID="client-1"
      workRecordID="ticket-1"
      onCapture={() => undefined}
    />,
  );

  expect(
    await screen.findByRole("button", { name: "Stop timer" }),
  ).toBeVisible();
});
