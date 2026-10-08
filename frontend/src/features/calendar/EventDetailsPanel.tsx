import { Button, Dialog } from "../../design-system";
import { DependenciesPanel } from "./DependenciesPanel";
import { eventTime } from "./dates";
import type { CalendarEvent } from "./types";

export function calendarSourceHref(event: CalendarEvent): string | undefined {
  if (
    event.privacy !== "full" ||
    !event.capabilities.viewSource ||
    !event.source
  )
    return undefined;
  const source = event.source,
    query = new URLSearchParams();
  if (source.client_id) query.set("clientID", source.client_id);
  if (source.type === "work_record") {
    query.set("workRecordID", source.id);
    query.set("recordType", "ticket");
    query.set("recordID", source.id);
    query.set("label", event.title.slice(0, 80));
    return `#/work?${query}`;
  }
  if (source.type === "project" || source.type === "task") {
    query.set("recordType", source.type);
    query.set("recordID", source.id);
    query.set("label", event.title.slice(0, 80));
    return `#/project?${query}`;
  }
  return undefined;
}
export function EventDetailsPanel({
  event,
  timezone,
  onClose,
  onSchedule,
  canSchedule = false,
  events = [],
}: {
  event?: CalendarEvent;
  timezone: string;
  onClose: () => void;
  onSchedule: (event: CalendarEvent) => void;
  canSchedule?: boolean;
  events?: CalendarEvent[];
}) {
  const sourceHref = event ? calendarSourceHref(event) : undefined;
  return (
    <Dialog
      open={!!event}
      title={
        event?.privacy === "busy" ? "Busy" : (event?.title ?? "Event details")
      }
      onClose={onClose}
      variant="drawer"
    >
      {event ? (
        <div className="calendar-details">
          <p>
            {event.allDay
              ? `${event.startsOn}${event.endsOn ? ` – ${event.endsOn}` : ""} · All day`
              : `${eventTime(event, timezone)} · ${timezone}`}
          </p>
          {event.privacy === "busy" ? (
            <p>Unavailable time. Source details are outside your access.</p>
          ) : (
            <>
              <dl>
                <dt>Event role</dt>
                <dd>{event.eventRole?.replaceAll("_", " ")}</dd>
                <dt>Health</dt>
                <dd>{event.health?.replaceAll("_", " ") ?? "Not evaluated"}</dd>
                <dt>Scheduling</dt>
                <dd>
                  {event.schedulingMode?.replaceAll("_", " ") ??
                    "Source managed"}
                </dd>
                {event.timezone ? (
                  <>
                    <dt>Original timezone</dt>
                    <dd>{event.timezone}</dd>
                  </>
                ) : null}
                <dt>Capacity</dt>
                <dd>
                  {event.capacityBearing
                    ? `${event.plannedMinutes ?? 0} planned minutes`
                    : "Does not consume capacity"}
                </dd>
              </dl>
              {event.healthReasons.length ? (
                <ul>
                  {event.healthReasons.map((reason) => (
                    <li key={reason}>{reason.replaceAll("_", " ")}</li>
                  ))}
                </ul>
              ) : null}
              {event.hasConflict ? (
                <p>
                  Schedule conflict. Review the scheduling preview before making
                  changes.
                </p>
              ) : null}
              {event.recurrence ? (
                <p>
                  Repeats every {event.recurrence.interval}{" "}
                  {event.recurrence.frequency} interval
                  {event.recurrence.count
                    ? `, ${event.recurrence.count} occurrences`
                    : ""}
                  .
                </p>
              ) : null}
              {sourceHref ? (
                <a href={sourceHref} onClick={onClose}>
                  Open source record
                </a>
              ) : (
                <p>Edit this record through its typed administration form.</p>
              )}
              {event.projectionID ? (
                <DependenciesPanel
                  key={event.projectionID}
                  event={event}
                  events={events}
                  canSchedule={canSchedule}
                />
              ) : null}
              {canSchedule &&
              event.capabilities.schedule &&
              event.projectionID &&
              event.schedulingMode !== "informational" ? (
                <Button onClick={() => onSchedule(event)}>Reschedule</Button>
              ) : (
                <p>Dates are managed on the source record.</p>
              )}
            </>
          )}
        </div>
      ) : null}
    </Dialog>
  );
}
