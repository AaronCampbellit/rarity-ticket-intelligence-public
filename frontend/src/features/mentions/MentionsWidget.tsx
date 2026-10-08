import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";

import { MentionAPIError, createMentionAPI } from "./api";
import type {
  MentionDeepLink,
  MentionItemState,
  MentionStateCounts,
  MentionWidgetAPI,
  MentionWidgetItem,
} from "./types";

const defaultAPI = createMentionAPI();
const pageSize = 20;
const states: MentionItemState[] = ["unread", "read", "archived"];
const emptyCounts: MentionStateCounts = { unread: 0, read: 0, archived: 0 };

function stateLabel(state: MentionItemState) {
  return state[0].toUpperCase() + state.slice(1);
}

function originLabel(origin: MentionWidgetItem["origin"]) {
  if (origin === "direct") return "Direct mention";
  if (origin === "team") return "Team mention";
  return "Direct and team mention";
}

export function MentionsWidget({
  api = defaultAPI,
  onOpen,
}: {
  api?: MentionWidgetAPI;
  onOpen: (link: MentionDeepLink) => void;
}) {
  const [state, setState] = useState<MentionItemState>("unread");
  const [counts, setCounts] = useState<MentionStateCounts>(emptyCounts);
  const [items, setItems] = useState<MentionWidgetItem[]>([]);
  const [nextCursor, setNextCursor] = useState<string>();
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const tabsID = useId();
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const requestSequence = useRef(0);
  const requestController = useRef<AbortController | undefined>(undefined);
  const resolveSequence = useRef(0);
  const resolveController = useRef<AbortController | undefined>(undefined);
  const stateRef = useRef(state);
  stateRef.current = state;

  const load = useCallback(
    async (requestedState: MentionItemState, cursor?: string) => {
      const sequence = ++requestSequence.current;
      requestController.current?.abort();
      if (!cursor) {
        setItems([]);
        setNextCursor(undefined);
        setLoadState("loading");
      }
      setMessage("");
      const controller = new AbortController();
      requestController.current = controller;
      try {
        const page = await api.listWidget(
          requestedState,
          cursor,
          pageSize,
          controller.signal,
        );
        if (
          sequence !== requestSequence.current ||
          requestedState !== stateRef.current
        )
          return;
        const ordered = [...page.items].sort((left, right) => {
          const time =
            Date.parse(right.lastMentionedAt) -
            Date.parse(left.lastMentionedAt);
          return time || right.id.localeCompare(left.id);
        });
        setCounts(page.counts);
        setItems((current) => {
          if (!cursor) return ordered;
          const merged = new Map(current.map((item) => [item.id, item]));
          for (const item of ordered) merged.set(item.id, item);
          return Array.from(merged.values());
        });
        setNextCursor(page.nextCursor);
        setLoadState("ready");
      } catch (error: unknown) {
        if (
          sequence !== requestSequence.current ||
          (error instanceof DOMException && error.name === "AbortError")
        )
          return;
        setLoadState("error");
      }
    },
    [api],
  );

  useEffect(() => {
    void load(state);
    return () => requestController.current?.abort();
  }, [load, state]);

  useEffect(
    () => () => {
      resolveSequence.current += 1;
      resolveController.current?.abort();
    },
    [],
  );

  useEffect(() => {
    const refresh = () => void load(stateRef.current);
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [load]);

  async function changeState(item: MentionWidgetItem, next: MentionItemState) {
    const mutationState = stateRef.current;
    const previousItems = items;
    const previousCounts = counts;
    setItems((current) => current.filter((value) => value.id !== item.id));
    setCounts((current) => ({
      ...current,
      [mutationState]: Math.max(0, current[mutationState] - 1),
      [next]: current[next] + 1,
    }));
    setMessage("");
    try {
      await api.changeItemState(item.id, next, item.version);
      await load(stateRef.current);
    } catch (error: unknown) {
      if (
        error instanceof MentionAPIError &&
        error.code === "version_conflict"
      ) {
        await load(stateRef.current);
        return;
      }
      if (stateRef.current === mutationState) {
        setItems(previousItems);
        setCounts(previousCounts);
      } else {
        await load(stateRef.current);
      }
      setMessage("The mention state could not be changed. Try again.");
    }
  }

  async function open(item: MentionWidgetItem) {
    const sequence = ++resolveSequence.current;
    resolveController.current?.abort();
    const controller = new AbortController();
    resolveController.current = controller;
    setMessage("");
    try {
      const link = await api.resolveOccurrence(
        item.latestOccurrenceId,
        item.id,
        item.version,
        controller.signal,
      );
      if (sequence !== resolveSequence.current || controller.signal.aborted)
        return;
      if (item.state === "read") {
        setItems((current) =>
          current.map((value) =>
            value.id === item.id
              ? { ...value, version: link.itemVersion }
              : value,
          ),
        );
      } else {
        setItems((current) => current.filter((value) => value.id !== item.id));
        setCounts((current) => ({
          ...current,
          [item.state]: Math.max(0, current[item.state] - 1),
          read: current.read + 1,
        }));
      }
      onOpen(link);
      await load(stateRef.current);
    } catch (error: unknown) {
      if (sequence !== resolveSequence.current || controller.signal.aborted)
        return;
      if (
        error instanceof MentionAPIError &&
        (error.status === 404 || error.code === "not_found")
      ) {
        setMessage("This mention is no longer available.");
      } else if (
        error instanceof MentionAPIError &&
        error.code === "version_conflict"
      ) {
        await load(stateRef.current);
        setMessage(
          "This mention changed. Review the refreshed item and try again.",
        );
      } else {
        setMessage("This mention could not be opened. Try again.");
      }
    }
  }

  function moveTab(
    event: KeyboardEvent<HTMLButtonElement>,
    currentIndex: number,
  ) {
    let nextIndex: number | undefined;
    if (event.key === "ArrowRight")
      nextIndex = (currentIndex + 1) % states.length;
    if (event.key === "ArrowLeft")
      nextIndex = (currentIndex - 1 + states.length) % states.length;
    if (event.key === "Home") nextIndex = 0;
    if (event.key === "End") nextIndex = states.length - 1;
    if (nextIndex === undefined) return;
    event.preventDefault();
    setState(states[nextIndex]);
    tabRefs.current[nextIndex]?.focus();
  }

  return (
    <section
      className="rti-mentions-widget"
      aria-labelledby="mentions-widget-title"
    >
      <header>
        <div>
          <h2 id="mentions-widget-title">Mentions</h2>
          <p>Internal notifications across all authorized clients</p>
        </div>
      </header>
      <div
        className="rti-mentions-widget__tabs"
        role="tablist"
        aria-label="Mention states"
      >
        {states.map((value, index) => (
          <button
            key={value}
            ref={(element) => {
              tabRefs.current[index] = element;
            }}
            id={`${tabsID}-${value}-tab`}
            type="button"
            role="tab"
            aria-selected={state === value}
            aria-controls={`${tabsID}-${value}-panel`}
            tabIndex={state === value ? 0 : -1}
            onClick={() => setState(value)}
            onKeyDown={(event) => moveTab(event, index)}
          >
            {stateLabel(value)} {counts[value]}
          </button>
        ))}
      </div>
      {states.map((value) => (
        <div
          key={value}
          id={`${tabsID}-${value}-panel`}
          role="tabpanel"
          aria-labelledby={`${tabsID}-${value}-tab`}
          hidden={state !== value}
        >
          {state === value ? (
            <>
              {message ? <p role="alert">{message}</p> : null}
              {loadState === "loading" ? (
                <p role="status">Loading mentions…</p>
              ) : null}
              {loadState === "error" ? (
                <div role="alert">
                  <p>Mentions are unavailable.</p>
                  <button type="button" onClick={() => void load(state)}>
                    Retry mentions
                  </button>
                </div>
              ) : null}
              {loadState === "ready" && items.length === 0 ? (
                <p>No {state} mentions.</p>
              ) : null}
              {loadState === "ready" ? (
                <div className="rti-mentions-widget__list">
                  {items.map((item) => (
                    <article key={item.id}>
                      <header>
                        <div>
                          <strong>{item.parentDisplayId}</strong>
                          <span>{item.parentSubject}</span>
                        </div>
                        <time dateTime={item.lastMentionedAt}>
                          {new Intl.DateTimeFormat(undefined, {
                            month: "short",
                            day: "numeric",
                            hour: "numeric",
                            minute: "2-digit",
                          }).format(new Date(item.lastMentionedAt))}
                        </time>
                      </header>
                      <small>
                        {item.authorLabel} · {originLabel(item.origin)}
                      </small>
                      {item.preview ? <p>{item.preview}</p> : null}
                      <footer>
                        <button
                          type="button"
                          onClick={() => void open(item)}
                          aria-label={`Open ${item.parentDisplayId}`}
                        >
                          Open
                        </button>
                        {item.state === "unread" ? (
                          <button
                            type="button"
                            onClick={() => void changeState(item, "read")}
                          >
                            Mark read
                          </button>
                        ) : (
                          <button
                            type="button"
                            onClick={() => void changeState(item, "unread")}
                          >
                            Mark unread
                          </button>
                        )}
                        {item.state !== "archived" ? (
                          <button
                            type="button"
                            onClick={() => void changeState(item, "archived")}
                          >
                            Archive
                          </button>
                        ) : null}
                      </footer>
                    </article>
                  ))}
                  {nextCursor ? (
                    <button
                      type="button"
                      onClick={() => void load(state, nextCursor)}
                    >
                      Load more mentions
                    </button>
                  ) : null}
                </div>
              ) : null}
            </>
          ) : null}
        </div>
      ))}
    </section>
  );
}
