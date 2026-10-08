import { StatusBadge } from "../../design-system";
import { eventTime } from "./dates";
import type { CalendarEvent, CalendarLens } from "./types";
import { calendarLenses } from "./types";

export function CalendarLensSwitcher({
  value,
  onChange,
}: {
  value: CalendarLens;
  onChange: (value: CalendarLens) => void;
}) {
  return (
    <div role="group" aria-label="Calendar views" className="calendar-lenses">
      {calendarLenses.map((lens) => (
        <button
          type="button"
          key={lens}
          aria-label={`${lens[0].toUpperCase()}${lens.slice(1)} view`}
          aria-pressed={value === lens}
          onClick={() => onChange(lens)}
        >
          {lens[0].toUpperCase()}
          {lens.slice(1)}
        </button>
      ))}
    </div>
  );
}
export function CalendarEventCard({
  event,
  timezone,
  onSelect,
  draggable = false,
  onDragStart,
}: {
  event: CalendarEvent;
  timezone: string;
  onSelect: () => void;
  draggable?: boolean;
  onDragStart?: () => void;
}) {
  return (
    <button
      type="button"
      className="calendar-event"
      onClick={onSelect}
      draggable={draggable && event.privacy === "full"}
      onDragStart={onDragStart}
    >
      <time>{eventTime(event, timezone)}</time>
      <strong>{event.privacy === "busy" ? "Busy" : event.title}</strong>
      {event.privacy === "full" ? (
        <>
          <span>{event.eventRole?.replaceAll("_", " ")}</span>
          {event.health ? (
            <StatusBadge
              tone={
                event.health === "blocked" || event.health === "overdue"
                  ? "danger"
                  : event.health === "at_risk"
                    ? "warning"
                    : "neutral"
              }
            >
              {event.health.replaceAll("_", " ")}
            </StatusBadge>
          ) : null}
          {event.hasConflict ? <span>Schedule conflict</span> : null}
        </>
      ) : (
        <span>Unavailable</span>
      )}
    </button>
  );
}
