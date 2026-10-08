import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { calendarAPI } from "./api";
import { calendarWindow, dateInZone, shiftDate } from "./dates";
import {
  readCalendarPreferences,
  validTimezone,
  writeCalendarPreferences,
} from "./preferences";
import type {
  CalendarAPI,
  CalendarEvent,
  CalendarFilter,
  CalendarLens,
  CapacitySummary,
  FilterOptions,
  SavedCalendarLens,
} from "./types";

export function useCalendarWorkspace(
  principalID: string,
  api: CalendarAPI = calendarAPI,
) {
  const [timezone, setTimezoneValue] = useState(
    () => readCalendarPreferences(window.localStorage, principalID).timezone,
  );
  const [date, setDate] = useState(() => dateInZone(new Date(), timezone));
  const [lens, setLens] = useState<CalendarLens>("week");
  const [filter, setFilter] = useState<CalendarFilter>({});
  const [events, setEvents] = useState<CalendarEvent[]>([]);
  const [options, setOptions] = useState<FilterOptions>({});
  const [capacity, setCapacity] = useState<Record<string, CapacitySummary>>({});
  const [views, setViews] = useState<SavedCalendarLens[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [viewError, setViewError] = useState("");
  const [optionError, setOptionError] = useState("");
  const auxiliaryError = [viewError, optionError].filter(Boolean).join(" ");
  const [nextCursor, setNextCursor] = useState<string>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [selectedID, setSelectedID] = useState<string>();
  const [revision, setRevision] = useState(0);
  const generation = useRef(0);
  const pageController = useRef<AbortController | undefined>(undefined);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const windowRange = useMemo(
    () => calendarWindow(date, lens, timezone),
    [date, lens, timezone],
  );
  const request = useMemo(
    () => ({ start: windowRange.start, end: windowRange.end, filter }),
    [windowRange, filter],
  );
  const selected = events.find((event) => event.occurrenceID === selectedID);

  useEffect(() => {
    const controller = new AbortController();
    void api
      .views(controller.signal)
      .then((found) => {
        if (!controller.signal.aborted) {
          setViews(found);
          setViewError("");
        }
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setViewError("Saved views could not be loaded.");
      });
    return () => controller.abort();
  }, [api, principalID, revision]);

  useEffect(() => {
    const token = ++generation.current;
    const controller = new AbortController();
    pageController.current?.abort();
    setLoadingMore(false);
    setEvents([]);
    setNextCursor(undefined);
    setCapacity({});
    setOptions({});
    setError("");
    setState("loading");
    setSelectedID(undefined);
    void Promise.all([
      api.events(request, controller.signal),
      lens === "capacity"
        ? api.capacity(request, controller.signal)
        : Promise.resolve({}),
    ])
      .then(([page, capacity]) => {
        if (controller.signal.aborted || token !== generation.current) return;
        setEvents(page.events);
        setCapacity(capacity);
        setNextCursor(page.nextCursor);
        setState("ready");
      })
      .catch((error) => {
        if (!controller.signal.aborted && token === generation.current) {
          setError(
            error instanceof Error ? error.message : "calendar_unavailable",
          );
          setState("error");
        }
      });
    void api
      .options(request, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted && token === generation.current) {
          setOptions(value);
          setOptionError("");
        }
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setOptionError("Some filter choices could not be loaded.");
      });
    return () => {
      controller.abort();
      pageController.current?.abort();
    };
  }, [api, request, lens, principalID, revision]);

  useEffect(() => {
    const controller = new AbortController();
    let cursor = "",
      timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      try {
        const page = await api.live(cursor, controller.signal);
        if (controller.signal.aborted) return;
        if (cursor && page.changed) refresh();
        cursor = page.cursor;
      } catch {
        /* Explicit refresh remains available during a live-feed outage. */
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 15000);
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [api, principalID, refresh]);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    const token = generation.current;
    const controller = new AbortController();
    pageController.current?.abort();
    pageController.current = controller;
    setLoadingMore(true);
    setError("");
    try {
      const page = await api.events(
        { ...request, cursor: nextCursor },
        controller.signal,
      );
      if (controller.signal.aborted || token !== generation.current) return;
      setEvents((current) => {
        const seen = new Set(current.map((event) => event.occurrenceID));
        return [
          ...current,
          ...page.events.filter((event) => !seen.has(event.occurrenceID)),
        ];
      });
      setNextCursor(
        page.nextCursor === nextCursor ? undefined : page.nextCursor,
      );
    } catch {
      if (!controller.signal.aborted && token === generation.current)
        setError("More events could not be loaded. Try again.");
    } finally {
      if (!controller.signal.aborted && token === generation.current)
        setLoadingMore(false);
    }
  }
  function setTimezone(value: string) {
    if (!validTimezone(value)) return;
    setTimezoneValue(value);
    writeCalendarPreferences(window.localStorage, principalID, {
      timezone: value,
    });
  }
  function move(direction: number) {
    if (lens === "month") {
      const parsed = new Date(`${date.slice(0, 7)}-01T12:00:00Z`);
      parsed.setUTCMonth(parsed.getUTCMonth() + direction);
      setDate(parsed.toISOString().slice(0, 10));
    } else
      setDate(
        shiftDate(
          date,
          direction *
            (lens === "day"
              ? 1
              : lens === "agenda" || lens === "timeline"
                ? 28
                : 7),
        ),
      );
  }
  return {
    timezone,
    setTimezone,
    date,
    setDate,
    lens,
    setLens,
    filter,
    setFilter,
    events,
    options,
    capacity,
    views,
    setViews,
    state,
    error,
    auxiliaryError,
    nextCursor,
    loadingMore,
    selected,
    select: setSelectedID,
    windowRange,
    request,
    refresh,
    loadMore,
    move,
  };
}
