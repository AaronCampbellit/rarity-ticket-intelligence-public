import { afterEach, expect, it, vi } from "vitest";
import { createCalendarAPI } from "./api";

afterEach(() => vi.restoreAllMocks());
const base = {
  id: "event",
  occurrence_id: "occurrence",
  privacy: "full",
  title: "On-site visit",
  all_day: false,
  starts_at: "2026-09-04T14:00:00Z",
  ends_at: "2026-09-04T15:00:00Z",
  timezone: "America/Chicago",
  source: { type: "work_record", id: "ticket", client_id: "alpha" },
  capabilities: { view_source: true, schedule: true },
};
it("maps date-only records without shifting dates and keeps scope unrestricted by default", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    Response.json({
      events: [
        base,
        {
          ...base,
          all_day: true,
          starts_at: undefined,
          ends_at: undefined,
          timezone: undefined,
          starts_on: "2026-09-04T00:00:00Z",
        },
      ],
      next_cursor: "opaque + cursor",
    }),
  );
  const page = await createCalendarAPI(fetcher).events({
    start: "2026-09-01T00:00:00Z",
    end: "2026-10-01T00:00:00Z",
  });
  expect(page.events[1]).toMatchObject({
    allDay: true,
    startsOn: "2026-09-04",
  });
  expect(page.events[1].startsAt).toBeUndefined();
  expect(page.events[0].timezone).toBe("America/Chicago");
  expect(page.nextCursor).toBe("opaque + cursor");
  expect(String(fetcher.mock.calls[0][0])).not.toContain("client_ids");
  expect(
    new Headers(fetcher.mock.calls[0][1].headers).has("X-Rarity-Client-ID"),
  ).toBe(false);
});
it("redacts unexpected source details in busy events and rejects malformed intervals", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({
        events: [
          {
            ...base,
            privacy: "busy",
            projection_id: "secret",
            health: "blocked",
          },
        ],
      }),
    )
    .mockResolvedValueOnce(
      Response.json({ events: [{ ...base, starts_at: "invalid" }] }),
    );
  const api = createCalendarAPI(fetcher);
  const request = {
    start: "2026-09-01T00:00:00Z",
    end: "2026-10-01T00:00:00Z",
  };
  const result = await api.events(request);
  expect(result.events[0]).toMatchObject({
    title: "Busy",
    source: undefined,
    projectionID: undefined,
    health: undefined,
    capabilities: { viewSource: false, schedule: false },
  });
  await expect(api.events(request)).rejects.toMatchObject({
    code: "invalid_response",
  });
});
it("passes opaque cursors unchanged and explicit client filters visibly", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json({ events: [] }));
  await createCalendarAPI(fetcher).events({
    start: "2026-09-01T00:00:00Z",
    end: "2026-10-01T00:00:00Z",
    cursor: "opaque + /",
    filter: { client_ids: ["alpha", "bravo"], conflicts_only: true },
  });
  const query = new URL(fetcher.mock.calls[0][0], "http://localhost")
    .searchParams;
  expect(query.get("cursor")).toBe("opaque + /");
  expect(query.getAll("client_ids")).toEqual(["alpha", "bravo"]);
});

it("preserves an explicitly empty saved client scope instead of broadening it", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json({ events: [] }));
  await createCalendarAPI(fetcher).events({
    start: "2026-09-01T00:00:00Z",
    end: "2026-09-02T00:00:00Z",
    filter: { client_ids: [] },
  });
  expect(String(fetcher.mock.calls[0][0])).toContain("client_ids=");
});
