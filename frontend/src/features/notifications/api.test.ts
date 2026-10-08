import { describe, expect, it, vi } from "vitest";

import { navigateNotificationAction, safeNotificationAction } from "./action";
import { createNotificationCenterAPI, NotificationAPIError } from "./api";

const item = {
  id: "00000000-0000-4000-8000-000000000101",
  title: "Calendar schedule changed",
  body: "schedule: Northwind: On-site visit",
  action_path: "/calendar",
  content_classification: "internal",
  created_at: "2026-08-16T12:00:00Z",
  version: 1,
};

describe("notification center API", () => {
  it("loads a validated page with an opaque cursor", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        Response.json({ notifications: [item], next_cursor: "cursor + / =" }),
      );
    const api = createNotificationCenterAPI(fetcher);

    await expect(api.list("prior + / =", 25)).resolves.toEqual({
      notifications: [
        {
          id: item.id,
          title: item.title,
          body: item.body,
          actionPath: "/calendar",
          contentClassification: "internal",
          createdAt: "2026-08-16T12:00:00Z",
          version: 1,
        },
      ],
      nextCursor: "cursor + / =",
    });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/notifications?limit=25&cursor=prior+%2B+%2F+%3D",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("loads the validated unread count", async () => {
    const fetcher = vi.fn().mockResolvedValue(Response.json({ count: 4 }));

    await expect(
      createNotificationCenterAPI(fetcher).unreadCount(),
    ).resolves.toBe(4);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/notifications/unread-count",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("marks a notification read with CSRF protection and its expected version", async () => {
    document.cookie = "rarity_csrf=csrf-token";
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        Response.json({ ...item, read_at: "2026-08-16T12:01:00Z", version: 4 }),
      );

    await expect(
      createNotificationCenterAPI(fetcher).markRead(item.id, 3),
    ).resolves.toEqual({
      id: item.id,
      title: item.title,
      body: item.body,
      actionPath: "/calendar",
      contentClassification: "internal",
      createdAt: "2026-08-16T12:00:00Z",
      readAt: "2026-08-16T12:01:00Z",
      version: 4,
    });
    expect(fetcher).toHaveBeenCalledWith(
      `/api/v1/notifications/${encodeURIComponent(item.id)}/read`,
      expect.objectContaining({
        method: "PATCH",
        credentials: "same-origin",
        headers: expect.objectContaining({
          "Content-Type": "application/json",
          "X-Rarity-CSRF": "csrf-token",
        }),
        body: JSON.stringify({ expected_version: 3 }),
      }),
    );
  });

  it("rejects PATCH success that is not a read response for the requested notification", async () => {
    const missingReadAt = createNotificationCenterAPI(
      vi.fn().mockResolvedValue(Response.json(item)),
    );
    await expect(missingReadAt.markRead(item.id, 1)).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });

    const mismatchedID = createNotificationCenterAPI(
      vi.fn().mockResolvedValue(
        Response.json({
          ...item,
          id: "00000000-0000-4000-8000-000000000999",
          read_at: "2026-08-16T12:01:00Z",
          version: 2,
        }),
      ),
    );
    await expect(mismatchedID.markRead(item.id, 1)).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });
  });

  it("rejects malformed payloads and preserves API error envelopes", async () => {
    const malformed = vi.fn().mockResolvedValue(
      Response.json({
        notifications: [{ ...item, created_at: "not-a-timestamp" }],
      }),
    );
    await expect(
      createNotificationCenterAPI(malformed).list(),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });

    const nullCursor = vi
      .fn()
      .mockResolvedValue(
        Response.json({ notifications: [], next_cursor: null }),
      );
    await expect(
      createNotificationCenterAPI(nullCursor).list(),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });

    const rejected = vi.fn().mockResolvedValue(
      Response.json(
        {
          error: {
            code: "version_conflict",
            message: "Notification changed",
          },
        },
        { status: 409 },
      ),
    );
    await expect(
      createNotificationCenterAPI(rejected).markRead(item.id, 3),
    ).rejects.toEqual(
      new NotificationAPIError("version_conflict", 409, "Notification changed"),
    );
  });

  it("normalizes malformed successful JSON bodies into invalid response errors", async () => {
    const malformedResponse = () => new Response("{", { status: 200 });
    const list = createNotificationCenterAPI(
      vi.fn().mockResolvedValue(malformedResponse()),
    );
    const count = createNotificationCenterAPI(
      vi.fn().mockResolvedValue(malformedResponse()),
    );
    const markRead = createNotificationCenterAPI(
      vi.fn().mockResolvedValue(malformedResponse()),
    );

    await expect(list.list()).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });
    await expect(count.unreadCount()).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });
    await expect(markRead.markRead(item.id, 3)).rejects.toMatchObject({
      code: "invalid_response",
      status: 502,
    });
  });
});

describe("notification actions", () => {
  it("accepts only bounded internal actions", () => {
    expect(safeNotificationAction("/calendar")).toBe("/calendar");
    expect(safeNotificationAction("#/work?workRecordID=record-1")).toBe(
      "#/work?workRecordID=record-1",
    );
    expect(
      safeNotificationAction("https://attacker.example/calendar"),
    ).toBeUndefined();
    expect(
      safeNotificationAction("//attacker.example/calendar"),
    ).toBeUndefined();
    expect(safeNotificationAction("/calendar?token=secret")).toBeUndefined();
    expect(safeNotificationAction(" /calendar")).toBeUndefined();
    expect(safeNotificationAction("/calendar\n")).toBeUndefined();
    expect(safeNotificationAction("/calendar\u0085")).toBeUndefined();
  });

  it("navigates only sanitized actions", () => {
    window.location.hash = "";
    expect(navigateNotificationAction("#/work?workRecordID=record-1")).toBe(
      true,
    );
    expect(window.location.hash).toBe("#/work?workRecordID=record-1");
    expect(
      navigateNotificationAction("https://attacker.example/calendar"),
    ).toBe(false);
  });
});
