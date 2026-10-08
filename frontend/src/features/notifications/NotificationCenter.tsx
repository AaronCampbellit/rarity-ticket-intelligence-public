import { Bell, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import { Button, Dialog } from "../../design-system";
import { navigateNotificationAction, safeNotificationAction } from "./action";
import { notificationCenterAPI } from "./api";
import type { NotificationCenterProps, NotificationItem } from "./types";
import "./notification-center.css";

type LoadState = "idle" | "loading" | "ready" | "error";

export function formatRelativeNotificationTime(
  value: string,
  now = Date.now(),
) {
  const elapsedSeconds = Math.max(
    0,
    Math.floor((now - Date.parse(value)) / 1000),
  );
  if (elapsedSeconds < 60) return "Just now";
  const elapsedMinutes = Math.floor(elapsedSeconds / 60);
  if (elapsedMinutes < 60) {
    return `${elapsedMinutes} minute${elapsedMinutes === 1 ? "" : "s"} ago`;
  }
  const elapsedHours = Math.floor(elapsedMinutes / 60);
  if (elapsedHours < 24) {
    return `${elapsedHours} hour${elapsedHours === 1 ? "" : "s"} ago`;
  }
  const elapsedDays = Math.floor(elapsedHours / 24);
  return `${elapsedDays} day${elapsedDays === 1 ? "" : "s"} ago`;
}

function appendUnique(
  current: NotificationItem[],
  incoming: NotificationItem[],
) {
  const seen = new Set<string>();
  return [...current, ...incoming].filter((item) => {
    if (seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
}

export function NotificationCenter({
  api = notificationCenterAPI,
  onNavigate = navigateNotificationAction,
}: NotificationCenterProps) {
  const [open, setOpen] = useState(false);
  const [unreadCount, setUnreadCount] = useState<number>();
  const [countError, setCountError] = useState(false);
  const [items, setItems] = useState<NotificationItem[]>([]);
  const [nextCursor, setNextCursor] = useState<string>();
  const [listState, setListState] = useState<LoadState>("idle");
  const [loadingMore, setLoadingMore] = useState(false);
  const [pageError, setPageError] = useState(false);
  const [itemErrors, setItemErrors] = useState<Record<string, string>>({});
  const [mutatingItems, setMutatingItems] = useState<Set<string>>(new Set());
  const listGeneration = useRef(0);
  const countGeneration = useRef(0);
  const itemGenerations = useRef(new Map<string, number>());
  const mutationLifecycle = useRef(0);
  const mutationControllers = useRef(new Set<AbortController>());
  const paginationRequest = useRef<
    { cursor: string; token: symbol } | undefined
  >(undefined);
  const centerRef = useRef<HTMLDivElement>(null);
  const focusAfterRead = useRef<
    { itemID: string; control: HTMLElement } | undefined
  >(undefined);
  const dialogID = useId();

  const refreshUnread = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++countGeneration.current;
      setCountError(false);
      try {
        const count = await api.unreadCount(signal);
        if (signal?.aborted || generation !== countGeneration.current) return;
        setUnreadCount(count);
        setCountError(false);
      } catch {
        if (signal?.aborted || generation !== countGeneration.current) return;
        setUnreadCount(undefined);
        setCountError(true);
      }
    },
    [api],
  );

  const loadFirstPage = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++listGeneration.current;
      paginationRequest.current = undefined;
      setListState("loading");
      setItems([]);
      setNextCursor(undefined);
      setLoadingMore(false);
      setPageError(false);
      try {
        const result = await api.list(undefined, undefined, signal);
        if (signal?.aborted || generation !== listGeneration.current) return;
        setItems(result.notifications);
        setNextCursor(result.nextCursor);
        setListState("ready");
      } catch {
        if (signal?.aborted || generation !== listGeneration.current) return;
        setListState("error");
      }
    },
    [api],
  );

  const loadMore = useCallback(async () => {
    const cursor = nextCursor;
    if (!cursor || paginationRequest.current) return;
    const generation = listGeneration.current;
    const controller = new AbortController();
    const token = Symbol("pagination request");
    paginationRequest.current = { cursor, token };
    setLoadingMore(true);
    setPageError(false);
    try {
      const result = await api.list(cursor, undefined, controller.signal);
      if (controller.signal.aborted || generation !== listGeneration.current) {
        return;
      }
      setItems((current) => appendUnique(current, result.notifications));
      setNextCursor(result.nextCursor);
    } catch {
      if (controller.signal.aborted || generation !== listGeneration.current) {
        return;
      }
      setPageError(true);
    } finally {
      if (paginationRequest.current?.token === token) {
        paginationRequest.current = undefined;
        if (generation === listGeneration.current) setLoadingMore(false);
      }
    }
  }, [api, nextCursor]);

  const markItemRead = useCallback(
    async (item: NotificationItem) => {
      if (item.readAt) return true;
      const generation = (itemGenerations.current.get(item.id) ?? 0) + 1;
      const capturedListGeneration = listGeneration.current;
      const capturedMutationLifecycle = mutationLifecycle.current;
      const controller = new AbortController();
      itemGenerations.current.set(item.id, generation);
      mutationControllers.current.add(controller);
      setItemErrors((current) => {
        const next = { ...current };
        delete next[item.id];
        return next;
      });
      setMutatingItems((current) => new Set(current).add(item.id));

      try {
        const updated = await api.markRead(
          item.id,
          item.version,
          controller.signal,
        );
        const current =
          !controller.signal.aborted &&
          mutationLifecycle.current === capturedMutationLifecycle &&
          itemGenerations.current.get(item.id) === generation &&
          listGeneration.current === capturedListGeneration;
        if (!current) return false;
        setItems((currentItems) =>
          currentItems.map((candidate) =>
            candidate.id === item.id ? updated : candidate,
          ),
        );
        void refreshUnread();
        return true;
      } catch {
        if (
          !controller.signal.aborted &&
          mutationLifecycle.current === capturedMutationLifecycle &&
          itemGenerations.current.get(item.id) === generation &&
          listGeneration.current === capturedListGeneration
        ) {
          setItemErrors((current) => ({
            ...current,
            [item.id]: "Could not be marked read. Try again.",
          }));
        }
        return false;
      } finally {
        mutationControllers.current.delete(controller);
        if (
          mutationLifecycle.current === capturedMutationLifecycle &&
          itemGenerations.current.get(item.id) === generation
        ) {
          setMutatingItems((current) => {
            const next = new Set(current);
            next.delete(item.id);
            return next;
          });
        }
      }
    },
    [api, refreshUnread],
  );

  const openItem = useCallback(
    async (item: NotificationItem, href: string) => {
      if (!item.readAt && !(await markItemRead(item))) return;
      onNavigate(href);
    },
    [markItemRead, onNavigate],
  );

  const closeDrawer = useCallback(() => {
    ++listGeneration.current;
    ++mutationLifecycle.current;
    for (const controller of mutationControllers.current) controller.abort();
    mutationControllers.current.clear();
    paginationRequest.current = undefined;
    focusAfterRead.current = undefined;
    setLoadingMore(false);
    setMutatingItems(new Set());
    setOpen(false);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refreshUnread(controller.signal);
    return () => {
      controller.abort();
      ++mutationLifecycle.current;
      for (const mutationController of mutationControllers.current) {
        mutationController.abort();
      }
      mutationControllers.current.clear();
    };
  }, [refreshUnread]);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const listPromise = loadFirstPage(controller.signal);
    const countPromise = refreshUnread(controller.signal);
    void Promise.allSettled([listPromise, countPromise]);
    return () => controller.abort();
  }, [loadFirstPage, open, refreshUnread]);

  useEffect(() => {
    const intent = focusAfterRead.current;
    if (
      !intent ||
      !items.some((item) => item.id === intent.itemID && item.readAt)
    ) {
      return;
    }
    focusAfterRead.current = undefined;
    const dialog =
      centerRef.current?.querySelector<HTMLElement>('[role="dialog"]');
    const close = dialog?.querySelector<HTMLElement>(
      ".rti-dialog__actions button",
    );
    if (
      document.activeElement === intent.control ||
      !dialog?.contains(document.activeElement)
    ) {
      (close ?? dialog)?.focus();
    }
  }, [items]);

  const badge =
    unreadCount === undefined
      ? undefined
      : unreadCount > 99
        ? "99+"
        : String(unreadCount);
  const triggerLabel =
    unreadCount === undefined
      ? countError
        ? "Notifications, unread count unavailable"
        : "Notifications, unread count loading"
      : `Notifications, ${unreadCount} unread`;

  return (
    <div className="notification-center" ref={centerRef}>
      <button
        className="notification-center__trigger"
        type="button"
        aria-label={triggerLabel}
        aria-controls={dialogID}
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        <Bell size={20} aria-hidden="true" />
        {unreadCount !== undefined && unreadCount > 0 ? (
          <span className="notification-center__badge" aria-hidden="true">
            {badge}
          </span>
        ) : null}
      </button>

      <Dialog
        id={dialogID}
        open={open}
        title="Notifications"
        variant="drawer"
        onClose={closeDrawer}
        actions={
          <Button intent="secondary" leadingIcon={X} onClick={closeDrawer}>
            Close
          </Button>
        }
      >
        {countError ? (
          <div className="notification-center__state-action">
            <p className="notification-center__error" role="alert">
              Unread count could not be refreshed.
            </p>
            <Button size="compact" onClick={() => void refreshUnread()}>
              Retry unread count
            </Button>
          </div>
        ) : null}
        {listState === "loading" ? (
          <p role="status">Loading notifications…</p>
        ) : null}
        {listState === "error" ? (
          <div className="notification-center__state-action">
            <p className="notification-center__error" role="alert">
              Notifications could not be loaded.
            </p>
            <Button size="compact" onClick={() => void loadFirstPage()}>
              Retry notifications
            </Button>
          </div>
        ) : null}
        {listState === "ready" && items.length === 0 ? (
          <p>You have no notifications.</p>
        ) : null}
        {items.length > 0 ? (
          <ul className="notification-center__list" aria-label="Notifications">
            {items.map((item) => (
              <li
                className="notification-center__item"
                data-read={item.readAt ? "true" : "false"}
                key={item.id}
              >
                <article>
                  <div className="notification-center__item-heading">
                    <h3>{item.title}</h3>
                    <span className="notification-center__read-state">
                      {item.readAt ? "Read" : "Unread"}
                    </span>
                  </div>
                  <p>{item.body}</p>
                  <time dateTime={item.createdAt}>
                    {formatRelativeNotificationTime(item.createdAt)}
                  </time>
                  <div className="notification-center__item-actions">
                    {!item.readAt ? (
                      <Button
                        size="compact"
                        loading={mutatingItems.has(item.id)}
                        loadingLabel={`Marking ${item.title} as read`}
                        aria-label={`Mark ${item.title} as read`}
                        onClick={(event) => {
                          if (document.activeElement === event.currentTarget) {
                            focusAfterRead.current = {
                              itemID: item.id,
                              control: event.currentTarget,
                            };
                          }
                          void markItemRead(item).then((marked) => {
                            if (
                              !marked &&
                              focusAfterRead.current?.itemID === item.id
                            ) {
                              focusAfterRead.current = undefined;
                            }
                          });
                        }}
                      >
                        Mark read
                      </Button>
                    ) : null}
                    {safeNotificationAction(item.actionPath) ? (
                      <Button
                        size="compact"
                        intent="primary"
                        loading={mutatingItems.has(item.id)}
                        loadingLabel={`Opening ${item.title}`}
                        aria-label={`Open ${item.title}`}
                        onClick={() =>
                          void openItem(
                            item,
                            safeNotificationAction(item.actionPath)!,
                          )
                        }
                      >
                        Open
                      </Button>
                    ) : null}
                  </div>
                  {itemErrors[item.id] ? (
                    <p className="notification-center__error" role="alert">
                      {itemErrors[item.id]}
                    </p>
                  ) : null}
                </article>
              </li>
            ))}
          </ul>
        ) : null}
        {nextCursor ? (
          <div className="notification-center__pagination">
            <Button
              loading={loadingMore}
              loadingLabel="Loading more notifications"
              onClick={() => void loadMore()}
            >
              {pageError ? "Retry load more" : "Load more"}
            </Button>
            {pageError ? (
              <p className="notification-center__error" role="alert">
                More notifications could not be loaded.
              </p>
            ) : null}
          </div>
        ) : null}
      </Dialog>
    </div>
  );
}
