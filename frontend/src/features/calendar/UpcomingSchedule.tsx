import { useEffect, useState } from "react";
import { calendarAPI } from "./api";
import type { CalendarEvent } from "./types";
export function UpcomingSchedule({ onNavigate }: { onNavigate: () => void }) {
  const [events, setEvents] = useState<CalendarEvent[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  useEffect(() => {
    const controller = new AbortController();
    const start = new Date(),
      end = new Date(start.getTime() + 7 * 86400000);
    void calendarAPI
      .events(
        { start: start.toISOString(), end: end.toISOString(), limit: 5 },
        controller.signal,
      )
      .then((page) => {
        if (!controller.signal.aborted) {
          setEvents(page.events);
          setState("ready");
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, []);
  return (
    <>
      <p>
        {state === "loading"
          ? "Loading upcoming schedule…"
          : state === "error"
            ? "Schedule could not be loaded."
            : !events.length
              ? "No upcoming commitments in the next seven days."
              : "Next scheduled commitments"}
      </p>
      {events.length ? (
        <ul>
          {events.map((event) => (
            <li key={event.occurrenceID}>
              {event.title} ·{" "}
              {event.startsOn ?? new Date(event.startsAt!).toLocaleString()}
            </li>
          ))}
        </ul>
      ) : null}
      <button type="button" onClick={onNavigate}>
        Open calendar
      </button>
    </>
  );
}
