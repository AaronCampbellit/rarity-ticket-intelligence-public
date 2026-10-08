import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { findA11yViolations } from "../../design-system/testing/renderA11y";
import { createNotificationCenterAPI } from "./api";
import type {
  NotificationCenterAPI,
  NotificationItem,
  NotificationPage,
} from "./types";
import {
  formatRelativeNotificationTime,
  NotificationCenter,
} from "./NotificationCenter";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const unreadItem: NotificationItem = {
  id: "00000000-0000-4000-8000-000000000101",
  title: "Calendar schedule changed",
  body: "Northwind's on-site visit moved to 2:00 PM.",
  actionPath: "/calendar",
  contentClassification: "internal",
  createdAt: "2026-08-16T14:00:00Z",
  version: 3,
};

const readItem: NotificationItem = {
  id: "00000000-0000-4000-8000-000000000102",
  title: "Ticket assigned",
  body: "Ticket 1042 was assigned to you.",
  actionPath: "#/work?workRecordID=work-1042",
  contentClassification: "internal",
  createdAt: "2026-08-16T13:00:00Z",
  readAt: "2026-08-16T13:05:00Z",
  version: 2,
};

function page(
  notifications: NotificationItem[],
  nextCursor?: string,
): NotificationPage {
  return { notifications, ...(nextCursor ? { nextCursor } : {}) };
}

function notificationAPI({
  unread = 0,
  firstPage = page([]),
}: {
  unread?: number;
  firstPage?: NotificationPage;
} = {}): NotificationCenterAPI {
  return {
    list: vi.fn().mockResolvedValue(firstPage),
    unreadCount: vi.fn().mockResolvedValue(unread),
    markRead: vi.fn(),
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function notificationRow(title: string) {
  const row = screen.getByRole("heading", { name: title }).closest("li");
  expect(row).not.toBeNull();
  return row!;
}

describe("NotificationCenter", () => {
  it("shows the unread count and opens a labelled, focus-contained drawer that restores focus", async () => {
    const api = notificationAPI({
      unread: 2,
      firstPage: page([unreadItem, readItem]),
    });
    render(<NotificationCenter api={api} />);

    const bell = await screen.findByRole("button", {
      name: "Notifications, 2 unread",
    });
    expect(bell).toHaveTextContent("2");
    expect(bell).toHaveAttribute("aria-expanded", "false");
    const dialogID = bell.getAttribute("aria-controls");
    expect(dialogID).toBeTruthy();

    await userEvent.click(bell);

    const dialog = await screen.findByRole("dialog", {
      name: "Notifications",
    });
    expect(dialog).toBeVisible();
    expect(dialog).toHaveAttribute("id", dialogID);
    expect(bell).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("heading", { name: "Notifications" }),
    ).toBeVisible();
    expect(dialog).toContainElement(document.activeElement as HTMLElement);

    fireEvent.keyDown(dialog, { key: "Escape" });

    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    expect(bell).toHaveAttribute("aria-expanded", "false");
    expect(bell).toHaveAttribute("aria-controls", dialogID);
    expect(bell).toHaveFocus();
  });

  it("caps the visible badge at 99+ while keeping the exact count accessible", async () => {
    render(<NotificationCenter api={notificationAPI({ unread: 104 })} />);

    const bell = await screen.findByRole("button", {
      name: "Notifications, 104 unread",
    });

    expect(bell).toHaveTextContent("99+");
    expect(bell).not.toHaveTextContent("104");
  });

  it("starts list and unread-count refreshes in parallel on open and preserves newest-first response order without Mentions", async () => {
    const openPage = deferred<NotificationPage>();
    const openCount = deferred<number>();
    const fetcher = vi
      .spyOn(globalThis, "fetch")
      .mockRejectedValue(new Error("unexpected network request"));
    const api: NotificationCenterAPI = {
      list: vi.fn().mockReturnValue(openPage.promise),
      unreadCount: vi
        .fn()
        .mockResolvedValueOnce(0)
        .mockReturnValueOnce(openCount.promise),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 0 unread",
    });

    await userEvent.click(bell);

    await waitFor(() => {
      expect(api.list).toHaveBeenCalledTimes(1);
      expect(api.unreadCount).toHaveBeenCalledTimes(2);
    });
    expect(
      screen.getByRole("dialog", { name: "Notifications" }),
    ).toHaveTextContent("Loading notifications");

    openPage.resolve(page([unreadItem, readItem]));
    openCount.resolve(1);

    const list = await screen.findByRole("list", { name: "Notifications" });
    const titles = within(list).getAllByRole("heading", { level: 3 });
    expect(titles.map((title) => title.textContent)).toEqual([
      "Calendar schedule changed",
      "Ticket assigned",
    ]);
    expect(screen.queryByText(/mentions/i)).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("keeps an unread-count success when the list request fails", async () => {
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockRejectedValueOnce(new Error("list unavailable"))
        .mockResolvedValueOnce(page([unreadItem])),
      unreadCount: vi.fn().mockResolvedValueOnce(1).mockResolvedValueOnce(7),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });

    await userEvent.click(bell);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /notifications could not be loaded/i,
    );
    expect(
      screen.getByRole("button", { name: "Notifications, 7 unread" }),
    ).toBeVisible();
    const countCalls = vi.mocked(api.unreadCount).mock.calls.length;

    await userEvent.click(
      screen.getByRole("button", { name: "Retry notifications" }),
    );

    expect(await screen.findByText("Calendar schedule changed")).toBeVisible();
    expect(api.list).toHaveBeenCalledTimes(2);
    expect(api.unreadCount).toHaveBeenCalledTimes(countCalls);
  });

  it("announces a failed unread count as unknown and retries it independently", async () => {
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem])),
      unreadCount: vi
        .fn()
        .mockRejectedValueOnce(new Error("mount count unavailable"))
        .mockRejectedValueOnce(new Error("open count unavailable"))
        .mockResolvedValueOnce(4),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);

    await userEvent.click(
      await screen.findByRole("button", {
        name: "Notifications, unread count unavailable",
      }),
    );

    expect(await screen.findByText("Calendar schedule changed")).toBeVisible();
    expect(screen.getByRole("alert")).toHaveTextContent(
      /unread count could not be refreshed/i,
    );
    expect(
      screen.queryByRole("button", { name: "Notifications, 0 unread" }),
    ).toBeNull();
    expect(api.list).toHaveBeenCalledTimes(1);

    await userEvent.click(
      screen.getByRole("button", { name: "Retry unread count" }),
    );

    expect(
      await screen.findByRole("button", { name: "Notifications, 4 unread" }),
    ).toBeVisible();
    expect(api.list).toHaveBeenCalledTimes(1);
  });

  it("keeps count and first-page failure retries accessible together", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    const api: NotificationCenterAPI = {
      list: vi.fn().mockRejectedValue(new Error("list unavailable")),
      unreadCount: vi.fn().mockRejectedValue(new Error("count unavailable")),
      markRead: vi.fn(),
    };
    const view = render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Notifications, unread count unavailable",
      }),
    );

    expect(
      await screen.findByRole("button", { name: "Retry unread count" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Retry notifications" }),
    ).toBeVisible();
    expect(await findA11yViolations(view.container)).toEqual([]);
  });

  it("coalesces two rapid Load more activations and appends one deduplicated page", async () => {
    const more = deferred<NotificationPage>();
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem], "cursor-1"))
        .mockReturnValueOnce(more.promise),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );
    const loadMore = await screen.findByRole("button", { name: "Load more" });

    fireEvent.click(loadMore);
    fireEvent.click(loadMore);

    expect(api.list).toHaveBeenCalledTimes(2);
    more.resolve(page([unreadItem, readItem]));

    await screen.findByText("Ticket assigned");
    expect(
      within(screen.getByRole("list", { name: "Notifications" })).getAllByRole(
        "listitem",
      ),
    ).toHaveLength(2);
  });

  it("retains loaded items after a page failure and retries the same cursor", async () => {
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem], "cursor-1"))
        .mockRejectedValueOnce(new Error("page failed"))
        .mockResolvedValueOnce(page([readItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    await userEvent.click(
      await screen.findByRole("button", { name: "Load more" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /more notifications could not be loaded/i,
    );
    expect(screen.getByText("Calendar schedule changed")).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: "Retry load more" }),
    );

    expect(await screen.findByText("Ticket assigned")).toBeVisible();
    expect(api.list).toHaveBeenNthCalledWith(
      2,
      "cursor-1",
      undefined,
      expect.any(AbortSignal),
    );
    expect(api.list).toHaveBeenNthCalledWith(
      3,
      "cursor-1",
      undefined,
      expect.any(AbortSignal),
    );
  });

  it("clears prior rows and pagination while a reopened first page is pending", async () => {
    const reopenedPage = deferred<NotificationPage>();
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem], "cursor-1"))
        .mockReturnValueOnce(reopenedPage.promise),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    expect(await screen.findByText("Calendar schedule changed")).toBeVisible();
    expect(screen.getByRole("button", { name: "Load more" })).toBeVisible();

    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));

    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading notifications",
    );
    expect(screen.queryByText("Calendar schedule changed")).toBeNull();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("keeps a newer same-cursor pagination request active when an older cycle settles", async () => {
    const oldPage = deferred<NotificationPage>();
    const newPage = deferred<NotificationPage>();
    const reloadedItem = { ...readItem, title: "Reloaded ticket" };
    const newPageItem = {
      ...readItem,
      id: "00000000-0000-4000-8000-000000000104",
      title: "New page ticket",
    };
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem], "cursor-1"))
        .mockReturnValueOnce(oldPage.promise)
        .mockResolvedValueOnce(page([reloadedItem], "cursor-1"))
        .mockReturnValueOnce(newPage.promise),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    await userEvent.click(
      await screen.findByRole("button", { name: "Load more" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    expect(await screen.findByText("Reloaded ticket")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(api.list).toHaveBeenCalledTimes(4);

    await act(async () => {
      oldPage.resolve(page([{ ...unreadItem, title: "Stale page" }]));
      await oldPage.promise;
    });

    const loading = screen.getByRole("button", {
      name: "Loading more notifications",
    });
    expect(loading).toBeDisabled();
    fireEvent.click(loading);
    expect(api.list).toHaveBeenCalledTimes(4);

    newPage.resolve(page([newPageItem]));
    expect(await screen.findByText("New page ticket")).toBeVisible();
    expect(screen.queryByText("Stale page")).toBeNull();
  });

  it("marks read with the displayed version, applies only the authoritative item, and refreshes the count", async () => {
    const mutation = deferred<NotificationItem>();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem, readItem])),
      unreadCount: vi
        .fn()
        .mockResolvedValueOnce(1)
        .mockResolvedValueOnce(1)
        .mockResolvedValueOnce(0),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );
    const markRead = await screen.findByRole("button", {
      name: "Mark Calendar schedule changed as read",
    });

    await userEvent.click(markRead);

    expect(api.markRead).toHaveBeenCalledWith(
      unreadItem.id,
      3,
      expect.any(AbortSignal),
    );
    expect(notificationRow("Calendar schedule changed")).toHaveTextContent(
      "Unread",
    );
    expect(
      screen.getByRole("button", { name: "Notifications, 1 unread" }),
    ).toBeVisible();

    mutation.resolve({
      ...unreadItem,
      title: "Authoritative calendar update",
      readAt: "2026-08-16T14:05:00Z",
      version: 4,
    });

    expect(
      await screen.findByText("Authoritative calendar update"),
    ).toBeVisible();
    expect(notificationRow("Authoritative calendar update")).toHaveTextContent(
      "Read",
    );
    expect(screen.getByText("Ticket assigned")).toBeVisible();
    expect(
      await screen.findByRole("button", { name: "Notifications, 0 unread" }),
    ).toBeVisible();
  });

  it("keeps focus inside the drawer when a read response removes the focused Mark read control", async () => {
    const mutation = deferred<NotificationItem>();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem])),
      unreadCount: vi
        .fn()
        .mockResolvedValueOnce(1)
        .mockResolvedValueOnce(1)
        .mockResolvedValueOnce(0),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    const dialog = await screen.findByRole("dialog", {
      name: "Notifications",
    });
    const markRead = await screen.findByRole("button", {
      name: "Mark Calendar schedule changed as read",
    });

    await userEvent.click(markRead);
    expect(markRead).toHaveFocus();
    await act(async () => {
      mutation.resolve({
        ...unreadItem,
        readAt: "2026-08-16T14:05:00Z",
        version: 4,
      });
      await mutation.promise;
    });

    await screen.findByText("Read", { exact: true });
    await waitFor(() =>
      expect(dialog).toContainElement(document.activeElement as HTMLElement),
    );
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Escape" });

    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    expect(bell).toHaveFocus();
  });

  it("does not steal focus when the user moves to another drawer control while Mark read is pending", async () => {
    const mutation = deferred<NotificationItem>();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem, readItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );
    const pendingMark = await screen.findByRole("button", {
      name: "Mark Calendar schedule changed as read",
    });
    await userEvent.click(pendingMark);
    expect(pendingMark).toHaveFocus();
    const survivingOpen = screen.getByRole("button", {
      name: "Open Ticket assigned",
    });
    survivingOpen.focus();
    expect(survivingOpen).toHaveFocus();

    await act(async () => {
      mutation.resolve({
        ...unreadItem,
        readAt: "2026-08-16T14:05:00Z",
        version: 4,
      });
      await mutation.promise;
    });

    await waitFor(() =>
      expect(
        screen.queryByRole("button", {
          name: "Mark Calendar schedule changed as read",
        }),
      ).toBeNull(),
    );
    expect(survivingOpen).toHaveFocus();
  });

  it("leaves a conflicted item unread and shows retry copy inside that item", async () => {
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem, readItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockRejectedValue(new Error("version conflict")),
    };
    render(<NotificationCenter api={api} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    await userEvent.click(
      await screen.findByRole("button", {
        name: "Mark Calendar schedule changed as read",
      }),
    );

    const row = notificationRow("Calendar schedule changed");
    expect(row).toHaveTextContent("Unread");
    expect(within(row).getByRole("alert")).toHaveTextContent(
      /could not be marked read.*try again/i,
    );
    expect(
      within(row).getByRole("button", {
        name: "Mark Calendar schedule changed as read",
      }),
    ).toBeEnabled();
  });

  it("waits for an unread item's PATCH before opening its sanitized action", async () => {
    const mutation = deferred<NotificationItem>();
    const onNavigate = vi.fn();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} onNavigate={onNavigate} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    await userEvent.click(
      await screen.findByRole("button", {
        name: "Open Calendar schedule changed",
      }),
    );

    expect(onNavigate).not.toHaveBeenCalled();
    mutation.resolve({
      ...unreadItem,
      readAt: "2026-08-16T14:05:00Z",
      version: 4,
    });

    await waitFor(() => expect(onNavigate).toHaveBeenCalledWith("/calendar"));
  });

  it("does not navigate or refresh from an Open PATCH invalidated by close and reopen", async () => {
    const mutation = deferred<NotificationItem>();
    const onNavigate = vi.fn();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} onNavigate={onNavigate} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Open Calendar schedule changed",
      }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    await screen.findByText("Calendar schedule changed");
    const countCallsBeforeMutation = vi.mocked(api.unreadCount).mock.calls
      .length;

    mutation.resolve({
      ...unreadItem,
      readAt: "2026-08-16T14:05:00Z",
      version: 4,
    });
    await act(async () => {
      await mutation.promise;
    });

    expect(onNavigate).not.toHaveBeenCalled();
    expect(api.unreadCount).toHaveBeenCalledTimes(countCallsBeforeMutation);
    expect(notificationRow("Calendar schedule changed")).toHaveTextContent(
      "Unread",
    );
  });

  it("does not update or navigate when PATCH success omits authoritative read state", async () => {
    const onNavigate = vi.fn();
    const wireItem = {
      id: unreadItem.id,
      title: unreadItem.title,
      body: unreadItem.body,
      action_path: unreadItem.actionPath,
      content_classification: unreadItem.contentClassification,
      created_at: unreadItem.createdAt,
      version: unreadItem.version,
    };
    const fetcher = vi
      .fn()
      .mockImplementation(
        async (input: RequestInfo | URL, init?: RequestInit) => {
          const url = String(input);
          if (url.endsWith("/unread-count")) {
            return Response.json({ count: 1 });
          }
          if (init?.method === "PATCH") {
            return Response.json({ ...wireItem, version: 4 });
          }
          return Response.json({ notifications: [wireItem] });
        },
      );
    render(
      <NotificationCenter
        api={createNotificationCenterAPI(fetcher)}
        onNavigate={onNavigate}
      />,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    await userEvent.click(
      await screen.findByRole("button", {
        name: "Open Calendar schedule changed",
      }),
    );

    const row = notificationRow("Calendar schedule changed");
    expect(await within(row).findByRole("alert")).toHaveTextContent(
      /could not be marked read/i,
    );
    expect(row).toHaveTextContent("Unread");
    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("aborts and invalidates a pending Open PATCH when the center unmounts", async () => {
    const mutation = deferred<NotificationItem>();
    const onNavigate = vi.fn();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([unreadItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    const view = render(
      <NotificationCenter api={api} onNavigate={onNavigate} />,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Open Calendar schedule changed",
      }),
    );
    const mutationSignal = vi.mocked(api.markRead).mock.calls[0][2];

    view.unmount();

    expect(mutationSignal).toBeInstanceOf(AbortSignal);
    expect(mutationSignal?.aborted).toBe(true);
    mutation.resolve({
      ...unreadItem,
      readAt: "2026-08-16T14:05:00Z",
      version: 4,
    });
    await act(async () => {
      await mutation.promise;
    });
    expect(onNavigate).not.toHaveBeenCalled();
  });

  it("does not render Open for an invalid external action", async () => {
    const unsafeItem = {
      ...unreadItem,
      id: "00000000-0000-4000-8000-000000000103",
      title: "Unsafe destination",
      actionPath: "https://attacker.example/calendar",
    };
    render(
      <NotificationCenter
        api={notificationAPI({
          unread: 1,
          firstPage: page([unreadItem, unsafeItem]),
        })}
      />,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    const row = notificationRow("Unsafe destination");
    expect(
      screen.getByRole("button", { name: "Open Calendar schedule changed" }),
    ).toBeVisible();
    expect(
      within(row).queryByRole("button", { name: /open unsafe destination/i }),
    ).toBeNull();
  });

  it("renders deterministic relative timestamps with the original machine-readable value", async () => {
    const now = Date.parse("2026-08-16T14:05:00Z");
    vi.spyOn(Date, "now").mockReturnValue(now);
    expect(formatRelativeNotificationTime("2026-08-16T14:04:30Z", now)).toBe(
      "Just now",
    );
    expect(formatRelativeNotificationTime("2026-08-16T12:05:00Z", now)).toBe(
      "2 hours ago",
    );
    render(
      <NotificationCenter
        api={notificationAPI({ unread: 1, firstPage: page([unreadItem]) })}
      />,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );

    const timestamp = await screen.findByText("5 minutes ago");
    expect(timestamp).toHaveAttribute("datetime", unreadItem.createdAt);
  });

  it("does not let a late first-page response from a closed cycle repaint a reopened drawer", async () => {
    const first = deferred<NotificationPage>();
    const second = deferred<NotificationPage>();
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });

    await userEvent.click(bell);
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
    second.resolve(page([readItem]));
    expect(await screen.findByText("Ticket assigned")).toBeVisible();

    await act(async () => {
      first.resolve(page([unreadItem]));
      await first.promise;
    });

    expect(screen.getByText("Ticket assigned")).toBeVisible();
    expect(screen.queryByText("Calendar schedule changed")).toBeNull();
  });

  it("does not let a late mount count overwrite the newer open refresh", async () => {
    const mountCount = deferred<number>();
    const openCount = deferred<number>();
    const api: NotificationCenterAPI = {
      list: vi.fn().mockResolvedValue(page([])),
      unreadCount: vi
        .fn()
        .mockReturnValueOnce(mountCount.promise)
        .mockReturnValueOnce(openCount.promise),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);

    await userEvent.click(
      screen.getByRole("button", {
        name: "Notifications, unread count loading",
      }),
    );
    openCount.resolve(5);
    expect(
      await screen.findByRole("button", { name: "Notifications, 5 unread" }),
    ).toBeVisible();

    await act(async () => {
      mountCount.resolve(1);
      await mountCount.promise;
    });

    expect(
      screen.getByRole("button", { name: "Notifications, 5 unread" }),
    ).toBeVisible();
  });

  it("does not append a late page after a newer reopened first page", async () => {
    const latePage = deferred<NotificationPage>();
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem], "cursor-1"))
        .mockReturnValueOnce(latePage.promise)
        .mockResolvedValueOnce(page([readItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn(),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    await userEvent.click(
      await screen.findByRole("button", { name: "Load more" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    expect(await screen.findByText("Ticket assigned")).toBeVisible();

    await act(async () => {
      latePage.resolve(page([{ ...unreadItem, title: "Stale page item" }]));
      await latePage.promise;
    });

    expect(screen.queryByText("Stale page item")).toBeNull();
    expect(screen.getByText("Ticket assigned")).toBeVisible();
  });

  it("does not let a late mutation replace an item superseded by a reopened first page", async () => {
    const mutation = deferred<NotificationItem>();
    const reloadedItem = {
      ...unreadItem,
      title: "Reloaded calendar update",
      version: 9,
    };
    const api: NotificationCenterAPI = {
      list: vi
        .fn()
        .mockResolvedValueOnce(page([unreadItem]))
        .mockResolvedValueOnce(page([reloadedItem])),
      unreadCount: vi.fn().mockResolvedValue(1),
      markRead: vi.fn().mockReturnValue(mutation.promise),
    };
    render(<NotificationCenter api={api} />);
    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    await userEvent.click(bell);
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Mark Calendar schedule changed as read",
      }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.click(bell);
    expect(await screen.findByText("Reloaded calendar update")).toBeVisible();

    await act(async () => {
      mutation.resolve({
        ...unreadItem,
        title: "Stale mutation update",
        readAt: "2026-08-16T14:05:00Z",
        version: 4,
      });
      await mutation.promise;
    });

    expect(screen.getByText("Reloaded calendar update")).toBeVisible();
    expect(screen.queryByText("Stale mutation update")).toBeNull();
  });

  it("has no automated accessibility violations when populated and open", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    const view = render(
      <NotificationCenter
        api={notificationAPI({
          unread: 1,
          firstPage: page([unreadItem, readItem]),
        })}
      />,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Notifications, 1 unread" }),
    );
    await screen.findByText("Calendar schedule changed");

    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});
