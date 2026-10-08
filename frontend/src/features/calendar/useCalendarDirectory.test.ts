import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useCalendarDirectory } from "./useCalendarDirectory";

afterEach(() => vi.unstubAllGlobals());

it("loads calendar names and choices from mixed deployed directory envelopes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        clients: [{ ID: "client", DisplayID: "CLIENT-001", Name: "Alpha" }],
        departments: [{ id: "department", name: "Delivery" }],
        teams: [{ id: "team", name: "Support" }],
        queues: [{ id: "queue", name: "Triage" }],
        technicians: [{ id: "technician", display_name: "Calendar operator" }],
      }),
    ),
  );

  const { result } = renderHook(() => useCalendarDirectory());
  await waitFor(() =>
    expect(result.current.names).toEqual({
      client: "Alpha",
      department: "Delivery",
      team: "Support",
      queue: "Triage",
      technician: "Calendar operator",
    }),
  );
  expect(result.current.technicians).toEqual([
    { id: "technician", name: "Calendar operator" },
  ]);
  expect(result.current.teams).toEqual([{ id: "team", name: "Support" }]);
  expect(result.current.error).toBe("");
});
