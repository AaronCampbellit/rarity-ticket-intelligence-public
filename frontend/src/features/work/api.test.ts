import { afterEach, expect, it, vi } from "vitest";

import {
  getWorkTask,
  listSavedWorkViews,
  listWorkRecords,
  saveWorkView,
} from "./api";

afterEach(() => vi.unstubAllGlobals());

it("direct-loads a client-scoped task for the technician workspace", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    Response.json({
      id: "task-1",
      title: "Call the vendor",
      status: "open",
      owner_id: "technician-1",
      estimate_minutes: 30,
      parent: { type: "work_record", id: "work-1" },
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  const controller = new AbortController();

  const task = await getWorkTask("client-1", "task-1", controller.signal);

  expect(task).toEqual({
    id: "task-1",
    title: "Call the vendor",
    status: "open",
    ownerID: "technician-1",
    estimateMinutes: 30,
    parent: { type: "work_record", id: "work-1" },
  });
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/tasks/task-1",
    expect.objectContaining({
      credentials: "same-origin",
      headers: { "X-Rarity-Client-ID": "client-1" },
      signal: controller.signal,
    }),
  );
});

it("loads client-scoped work records from the technician worklist", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          ID: "work-1",
          DisplayID: "INC-10482",
          Type: "incident",
          Title: "VPN unavailable",
          Status: "triage",
          Priority: "critical",
          QueueID: "queue-1",
          PrimaryOwnerID: "",
          UpdatedAt: "2026-07-30T12:00:00Z",
          Version: 2,
        },
      ]),
      { status: 200 },
    ),
  );
  vi.stubGlobal("fetch", fetcher);

  const found = await listWorkRecords("client-1");

  expect(found[0]).toMatchObject({
    id: "work-1",
    displayID: "INC-10482",
    title: "VPN unavailable",
  });
  expect(fetcher.mock.calls[0][1]).toMatchObject({
    headers: { "X-Rarity-Client-ID": "client-1" },
  });
});

it("lists and saves client-scoped private work views", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json([
        {
          id: "view-1",
          name: "Urgent work",
          query: { priority: "urgent" },
          audience: { type: "private" },
          version: 1,
        },
      ]),
    )
    .mockResolvedValueOnce(
      Response.json(
        {
          id: "view-2",
          name: "My queue",
          query: { ownership: "assigned" },
          audience: { type: "private" },
          version: 1,
        },
        { status: 201 },
      ),
    );
  vi.stubGlobal("fetch", fetcher);

  const listed = await listSavedWorkViews("client-1");
  const saved = await saveWorkView(
    "client-1",
    "My queue",
    { ownership: "assigned" },
    false,
  );

  expect(listed[0].name).toBe("Urgent work");
  expect(saved.name).toBe("My queue");
  expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
  });
  expect(fetcher.mock.calls[1][1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(fetcher.mock.calls[1][1]?.body)).toContain(
    '"audience":{"type":"private"}',
  );
});

it("sends filters and the exact last-row position to load older matching work", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json([]));
  vi.stubGlobal("fetch", fetcher);
  await listWorkRecords("client-1", undefined, {
    filters: {
      text: "older & urgent",
      status: "open",
      priority: "high",
      ownership: "unassigned",
    },
    before: { updatedAt: "2026-09-04T12:00:00.123456Z", id: "last-id" },
  });
  const url = new URL(fetcher.mock.calls[0][0], "http://localhost");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: "100",
    text: "older & urgent",
    status: "open",
    priority: "high",
    ownership: "unassigned",
    before_updated_at: "2026-09-04T12:00:00.123456Z",
    before_id: "last-id",
  });
});
