import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MentionAPIError } from "./api";
import { MentionsWidget } from "./MentionsWidget";
import type {
  MentionDeepLink,
  MentionWidgetAPI,
  MentionWidgetItem,
  MentionWidgetPage,
} from "./types";

afterEach(cleanup);

const unreadItem: MentionWidgetItem = {
  id: "item-1",
  parentType: "work_record",
  parentId: "work-1",
  parentDisplayId: "RTY-1042",
  parentSubject: "VPN access unavailable",
  latestOccurrenceId: "occurrence-1",
  authorLabel: "Mira Patel",
  origin: "direct",
  preview: "Please investigate the VPN concentrator.",
  state: "unread",
  lastMentionedAt: "2026-08-08T13:00:00Z",
  version: 3,
};

function linkFor(item = unreadItem): MentionDeepLink {
  return {
    href: `#/work?parentID=${item.parentId}&mentionOccurrenceID=${item.latestOccurrenceId}&sourceID=source-1`,
    clientId: "client-authorized",
    parentType: item.parentType,
    parentId: item.parentId,
    sourceId: "source-1",
    tokenId: "token-1",
    sourceAvailable: true,
    itemVersion: item.version + 1,
  } as MentionDeepLink;
}

function page(
  items: MentionWidgetItem[] = [unreadItem],
  counts = { unread: 1, read: 2, archived: 3 },
  nextCursor?: string,
): MentionWidgetPage {
  return { counts, items, nextCursor };
}

function apiWith(initial: MentionWidgetPage): MentionWidgetAPI {
  return {
    listWidget: vi.fn().mockResolvedValue(initial),
    changeItemState: vi
      .fn()
      .mockImplementation(async (itemId, state, version) => ({
        id: itemId,
        state,
        version: version + 1,
      })),
    resolveOccurrence: vi.fn(),
  };
}

describe("MentionsWidget", () => {
  it("shows counted state tabs, newest-first metadata, safe previews, and no inbox route", async () => {
    const teamItem: MentionWidgetItem = {
      ...unreadItem,
      id: "item-2",
      parentDisplayId: "RTY-1041",
      latestOccurrenceId: "occurrence-2",
      origin: "team",
      preview: undefined,
      lastMentionedAt: "2026-08-08T12:00:00Z",
    };
    const api = apiWith(page([teamItem, unreadItem]));
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    expect(
      await screen.findByRole("tab", { name: "Unread 1" }),
    ).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Read 2" })).toBeVisible();
    expect(screen.getByRole("tab", { name: "Archived 3" })).toBeVisible();
    const rows = screen.getAllByRole("article");
    expect(rows[0]).toHaveTextContent("RTY-1042");
    expect(rows[0]).toHaveTextContent("Direct mention");
    expect(rows[1]).toHaveTextContent("Team mention");
    expect(rows[1]).not.toHaveTextContent("Please investigate");
    expect(screen.queryByRole("link", { name: /inbox/i })).toBeNull();
  });

  it("implements roving keyboard tabs with linked tabpanel semantics", async () => {
    const user = userEvent.setup();
    render(<MentionsWidget api={apiWith(page())} onOpen={vi.fn()} />);

    const unread = await screen.findByRole("tab", { name: "Unread 1" });
    const read = screen.getByRole("tab", { name: "Read 2" });
    const archived = screen.getByRole("tab", { name: "Archived 3" });
    expect(unread).toHaveAttribute("tabindex", "0");
    expect(read).toHaveAttribute("tabindex", "-1");
    expect(archived).toHaveAttribute("tabindex", "-1");
    const panels = screen.getAllByRole("tabpanel", { hidden: true });
    expect(panels).toHaveLength(3);
    for (const tab of [unread, read, archived]) {
      expect(tab).toHaveAttribute("aria-controls");
      expect(
        document.getElementById(tab.getAttribute("aria-controls")!),
      ).not.toBeNull();
    }
    const panel = screen.getByRole("tabpanel");
    expect(panel.id).toBe(unread.getAttribute("aria-controls"));
    expect(panel).toHaveAttribute("aria-labelledby", unread.id);

    unread.focus();
    await user.keyboard("{ArrowRight}");
    expect(read).toHaveFocus();
    expect(read).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{End}");
    expect(archived).toHaveFocus();
    expect(archived).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{Home}");
    expect(unread).toHaveFocus();
    await user.keyboard("{ArrowLeft}");
    expect(archived).toHaveFocus();
  });

  it("optimistically changes state and reloads after a version conflict", async () => {
    const user = userEvent.setup();
    const api = apiWith(page());
    let rejectChange!: (reason: unknown) => void;
    vi.mocked(api.changeItemState).mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectChange = reject;
        }),
    );
    vi.mocked(api.listWidget)
      .mockResolvedValueOnce(page())
      .mockResolvedValueOnce(page([{ ...unreadItem, version: 4 }]));
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    const row = await screen.findByRole("article");
    await user.click(within(row).getByRole("button", { name: "Mark read" }));
    expect(screen.queryByText("RTY-1042")).toBeNull();
    rejectChange(new MentionAPIError("version_conflict", 409));
    await waitFor(() => expect(api.listWidget).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("RTY-1042")).toBeVisible();
  });

  it("does not let a late state mutation overwrite a newly selected tab", async () => {
    const user = userEvent.setup();
    const readItem = {
      ...unreadItem,
      id: "item-read",
      parentDisplayId: "RTY-2000",
      state: "read" as const,
    };
    const api = apiWith(page());
    let rejectChange!: (reason: unknown) => void;
    vi.mocked(api.changeItemState).mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectChange = reject;
        }),
    );
    vi.mocked(api.listWidget)
      .mockResolvedValueOnce(page())
      .mockResolvedValueOnce(
        page([readItem], { unread: 1, read: 1, archived: 0 }),
      )
      .mockResolvedValueOnce(
        page([readItem], { unread: 1, read: 1, archived: 0 }),
      );
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    await user.click(
      within(await screen.findByRole("article")).getByRole("button", {
        name: "Mark read",
      }),
    );
    await user.click(screen.getByRole("tab", { name: "Read 3" }));
    expect(await screen.findByText("RTY-2000")).toBeVisible();
    rejectChange(new Error("offline"));

    await waitFor(() => expect(api.listWidget).toHaveBeenCalledTimes(3));
    expect(await screen.findByText("RTY-2000")).toBeVisible();
    expect(screen.queryByText("RTY-1042")).toBeNull();
  });

  it("loads one bounded page at a time and refreshes archived re-mentions on focus", async () => {
    const user = userEvent.setup();
    const archived = { ...unreadItem, state: "archived" as const };
    const api = apiWith(
      page([archived], { unread: 0, read: 0, archived: 1 }, "next"),
    );
    vi.mocked(api.listWidget)
      .mockResolvedValueOnce(page([], { unread: 0, read: 0, archived: 1 }))
      .mockResolvedValueOnce(
        page([archived], { unread: 0, read: 0, archived: 1 }, "next"),
      )
      .mockResolvedValueOnce(
        page([{ ...unreadItem, version: 5 }], {
          unread: 1,
          read: 0,
          archived: 0,
        }),
      );
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    await user.click(await screen.findByRole("tab", { name: "Archived 1" }));
    expect(await screen.findByText("RTY-1042")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Load more mentions" }),
    );
    expect(api.listWidget).toHaveBeenLastCalledWith(
      "archived",
      "next",
      20,
      expect.any(AbortSignal),
    );
    fireEvent.focus(window);
    await user.click(await screen.findByRole("tab", { name: "Unread 1" }));
    expect(await screen.findByText("RTY-1042")).toBeVisible();
  });

  it("opens only the authenticated resolver result and hides revoked access", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();
    const api = apiWith(page());
    const resolved: MentionDeepLink = {
      href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
      clientId: "client-authorized",
      parentType: "work_record",
      parentId: "work-1",
      sourceId: "source-1",
      tokenId: "token-1",
      sourceAvailable: true,
      itemVersion: 4,
    };
    vi.mocked(api.resolveOccurrence).mockResolvedValueOnce(resolved);
    const view = render(<MentionsWidget api={api} onOpen={onOpen} />);
    await user.click(
      within(await screen.findByRole("article")).getByRole("button", {
        name: "Open RTY-1042",
      }),
    );
    expect(onOpen).toHaveBeenCalledWith(resolved);

    view.unmount();
    vi.mocked(api.listWidget).mockResolvedValueOnce(page());
    vi.mocked(api.resolveOccurrence).mockRejectedValueOnce(
      new MentionAPIError("not_found", 404),
    );
    render(<MentionsWidget api={api} onOpen={onOpen} />);
    await screen.findByRole("article");
    await user.click(screen.getByRole("button", { name: "Open RTY-1042" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This mention is no longer available.",
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent("work-1");
  });

  it("refreshes an already-read item after Open instead of removing it from the Read list", async () => {
    const user = userEvent.setup();
    const readItem = { ...unreadItem, state: "read" as const };
    const api = apiWith(page([], { unread: 0, read: 1, archived: 0 }));
    vi.mocked(api.listWidget)
      .mockResolvedValueOnce(page([], { unread: 0, read: 1, archived: 0 }))
      .mockResolvedValueOnce(
        page([readItem], { unread: 0, read: 1, archived: 0 }),
      )
      .mockResolvedValueOnce(
        page([{ ...readItem, version: 4 }], {
          unread: 0,
          read: 1,
          archived: 0,
        }),
      );
    vi.mocked(api.resolveOccurrence).mockResolvedValueOnce(linkFor(readItem));
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    await user.click(await screen.findByRole("tab", { name: "Read 1" }));
    await user.click(
      within(await screen.findByRole("article")).getByRole("button", {
        name: "Open RTY-1042",
      }),
    );

    await waitFor(() => expect(api.listWidget).toHaveBeenCalledTimes(3));
    expect(await screen.findByText("RTY-1042")).toBeVisible();
    expect(screen.getByRole("tab", { name: "Read 1" })).toBeVisible();
  });

  it("reloads the current tab after a late Open version conflict", async () => {
    const user = userEvent.setup();
    const readItem = {
      ...unreadItem,
      id: "item-read",
      parentDisplayId: "RTY-2000",
      state: "read" as const,
    };
    const api = apiWith(
      page([unreadItem], { unread: 1, read: 1, archived: 0 }),
    );
    let rejectResolve!: (reason: unknown) => void;
    vi.mocked(api.resolveOccurrence).mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectResolve = reject;
        }),
    );
    vi.mocked(api.listWidget)
      .mockResolvedValueOnce(
        page([unreadItem], { unread: 1, read: 1, archived: 0 }),
      )
      .mockResolvedValueOnce(
        page([readItem], { unread: 1, read: 1, archived: 0 }),
      )
      .mockResolvedValueOnce(
        page([readItem], { unread: 1, read: 1, archived: 0 }),
      );
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);

    await user.click(
      within(await screen.findByRole("article")).getByRole("button", {
        name: "Open RTY-1042",
      }),
    );
    await user.click(screen.getByRole("tab", { name: "Read 1" }));
    expect(await screen.findByText("RTY-2000")).toBeVisible();
    rejectResolve(new MentionAPIError("version_conflict", 409));

    await waitFor(() =>
      expect(api.listWidget).toHaveBeenLastCalledWith(
        "read",
        undefined,
        20,
        expect.any(AbortSignal),
      ),
    );
    expect(await screen.findByText("RTY-2000")).toBeVisible();
  });

  it("aborts and ignores an older Open when a newer Open resolves first", async () => {
    const user = userEvent.setup();
    const second = {
      ...unreadItem,
      id: "item-2",
      latestOccurrenceId: "occurrence-2",
      parentDisplayId: "RTY-2000",
    };
    const api = apiWith(
      page([unreadItem, second], { unread: 2, read: 0, archived: 0 }),
    );
    const pending = new Map<string, (link: MentionDeepLink) => void>();
    const signals: AbortSignal[] = [];
    vi.mocked(api.resolveOccurrence).mockImplementation(
      (occurrenceId, _itemId, _version, signal) => {
        if (signal) signals.push(signal);
        return new Promise((resolve) => pending.set(occurrenceId, resolve));
      },
    );
    const onOpen = vi.fn();
    render(<MentionsWidget api={api} onOpen={onOpen} />);

    await user.click(
      await screen.findByRole("button", { name: "Open RTY-1042" }),
    );
    await user.click(screen.getByRole("button", { name: "Open RTY-2000" }));
    pending.get("occurrence-2")?.(linkFor(second));
    await waitFor(() => expect(onOpen).toHaveBeenCalledTimes(1));
    pending.get("occurrence-1")?.(linkFor());
    await Promise.resolve();

    expect(onOpen).toHaveBeenCalledWith(linkFor(second));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(signals[0]?.aborted).toBe(true);
  });

  it("aborts and ignores an Open response after unmount", async () => {
    const user = userEvent.setup();
    const api = apiWith(page());
    let resolveOpen!: (link: MentionDeepLink) => void;
    let signal: AbortSignal | undefined;
    vi.mocked(api.resolveOccurrence).mockImplementationOnce(
      (_occurrence, _item, _version, requestSignal) => {
        signal = requestSignal;
        return new Promise((resolve) => {
          resolveOpen = resolve;
        });
      },
    );
    const onOpen = vi.fn();
    const view = render(<MentionsWidget api={api} onOpen={onOpen} />);
    await user.click(
      within(await screen.findByRole("article")).getByRole("button", {
        name: "Open RTY-1042",
      }),
    );

    view.unmount();
    resolveOpen(linkFor());
    await Promise.resolve();

    expect(signal?.aborted).toBe(true);
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("renders loading, empty, and retryable error states", async () => {
    let reject!: (reason: unknown) => void;
    const api = apiWith(page([]));
    vi.mocked(api.listWidget).mockImplementationOnce(
      () =>
        new Promise((_, rejectPromise) => {
          reject = rejectPromise;
        }),
    );
    render(<MentionsWidget api={api} onOpen={vi.fn()} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading mentions");
    reject(new Error("offline"));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Mentions are unavailable",
    );
    vi.mocked(api.listWidget).mockResolvedValueOnce(page([]));
    await userEvent.click(
      screen.getByRole("button", { name: "Retry mentions" }),
    );
    expect(await screen.findByText("No unread mentions.")).toBeVisible();
  });
});
