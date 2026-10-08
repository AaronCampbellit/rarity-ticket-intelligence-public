import { Button } from "../../design-system";
import { CalendarEventCard } from "./CalendarEventCard";
import { eventDate, shiftDate } from "./dates";
import type { CalendarEvent, CapacitySummary } from "./types";

export type ViewProps = {
  events: CalendarEvent[];
  timezone: string;
  startDate: string;
  endDate: string;
  onSelect: (id: string) => void;
  names?: Record<string, string>;
  groupBy?: "none" | "technician";
  onDropEvent?: (date: string) => void;
  onDragEvent?: (event: CalendarEvent) => void;
};
export function datesBetween(start: string, end: string) {
  const dates = [];
  for (
    let date = start;
    date < end && dates.length < 42;
    date = shiftDate(date, 1)
  )
    dates.push(date);
  return dates;
}
function eventsOnDate(events: CalendarEvent[], date: string, timezone: string) {
  return events.filter((event) => {
    const first = eventDate(event, timezone);
    if (event.allDay)
      return (
        first <= date && (event.endsOn ? event.endsOn > date : first === date)
      );
    const last = event.endsAt
      ? eventDate(
          {
            ...event,
            startsAt: new Date(Date.parse(event.endsAt) - 1).toISOString(),
          },
          timezone,
        )
      : first;
    return first <= date && last >= date;
  });
}
function Cards({
  events,
  timezone,
  onSelect,
  onDragEvent,
}: Pick<ViewProps, "events" | "timezone" | "onSelect" | "onDragEvent">) {
  return (
    <>
      {events.map((event) => (
        <CalendarEventCard
          key={event.occurrenceID}
          event={event}
          timezone={timezone}
          onSelect={() => onSelect(event.occurrenceID)}
          draggable={
            event.capabilities.schedule &&
            event.schedulingMode !== "informational" &&
            !!event.projectionID
          }
          onDragStart={() => onDragEvent?.(event)}
        />
      ))}
    </>
  );
}
export function DayWeekGrid(props: ViewProps) {
  const days = datesBetween(props.startDate, props.endDate);
  const groups =
    props.groupBy === "technician"
      ? [
          ...new Set(
            props.events.map((event) => event.assigneeID ?? "Unassigned"),
          ),
        ]
      : [""];
  return (
    <div className="calendar-day-week">
      {groups.map((group) => (
        <section
          key={group}
          aria-label={
            group ? (props.names?.[group] ?? group) : "Calendar schedule"
          }
        >
          {group ? <h2>{props.names?.[group] ?? group}</h2> : null}
          <div
            className="calendar-days"
            style={{
              gridTemplateColumns: `repeat(${days.length}, minmax(140px, 1fr))`,
            }}
          >
            {days.map((date) => {
              const events = eventsOnDate(
                props.events.filter(
                  (event) =>
                    !group || (event.assigneeID ?? "Unassigned") === group,
                ),
                date,
                props.timezone,
              );
              return (
                <section
                  className="calendar-day"
                  key={date}
                  onDragOver={(event) => event.preventDefault()}
                  onDrop={(event) => {
                    event.preventDefault();
                    props.onDropEvent?.(date);
                  }}
                >
                  <h3>
                    {new Intl.DateTimeFormat(undefined, {
                      weekday: "short",
                      month: "short",
                      day: "numeric",
                      timeZone: "UTC",
                    }).format(new Date(`${date}T12:00:00Z`))}
                  </h3>
                  <div role="group" aria-label="All-day events">
                    <Cards
                      {...props}
                      events={events.filter((event) => event.allDay)}
                    />
                  </div>
                  <div role="group" aria-label="Timed schedule">
                    <Cards
                      {...props}
                      events={events.filter((event) => !event.allDay)}
                    />
                  </div>
                  {!events.length ? (
                    <p className="calendar-muted">No events</p>
                  ) : null}
                </section>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}
export function MonthGrid(
  props: ViewProps & { onOpenDay: (date: string) => void },
) {
  const firstDay = new Date(`${props.startDate}T12:00:00Z`).getUTCDay();
  const first = shiftDate(props.startDate, -((firstDay + 6) % 7));
  const days = datesBetween(first, shiftDate(props.endDate, 6));
  const length =
    Math.ceil(days.findIndex((date) => date === props.endDate) / 7) * 7 ||
    days.length;
  return (
    <div className="calendar-month">
      {days.slice(0, length).map((date) => {
        const events = eventsOnDate(props.events, date, props.timezone);
        return (
          <section
            className="calendar-day"
            key={date}
            data-outside={date.slice(0, 7) !== props.startDate.slice(0, 7)}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              props.onDropEvent?.(date);
            }}
          >
            <Button
              aria-label={`Open ${date}`}
              onClick={() => props.onOpenDay(date)}
            >
              {Number(date.slice(8))}
            </Button>
            <Cards {...props} events={events.slice(0, 3)} />
            {events.length > 3 ? (
              <Button onClick={() => props.onOpenDay(date)}>
                View all {events.length} events
              </Button>
            ) : null}
          </section>
        );
      })}
    </div>
  );
}
export function AgendaView(props: ViewProps) {
  const dates = [
    ...new Set(props.events.map((event) => eventDate(event, props.timezone))),
  ].sort();
  return (
    <div className="calendar-agenda">
      {dates.map((date) => (
        <section key={date}>
          <h2>{date}</h2>
          <Cards
            {...props}
            events={props.events.filter(
              (event) => eventDate(event, props.timezone) === date,
            )}
          />
        </section>
      ))}
    </div>
  );
}
export function TimelineView(props: ViewProps) {
  const total =
    (Date.parse(props.endDate) - Date.parse(props.startDate)) / 86400000;
  return (
    <section aria-label="Schedule timeline" className="calendar-timeline">
      <div className="calendar-timeline-dates">
        <span>{props.startDate}</span>
        <span>{shiftDate(props.endDate, -1)}</span>
      </div>
      {props.events.map((event) => {
        const start = Math.max(
          0,
          (Date.parse(eventDate(event, props.timezone)) -
            Date.parse(props.startDate)) /
            86400000,
        );
        const end = event.allDay
          ? (event.endsOn ?? event.startsOn!)
          : eventDate(
              { ...event, startsAt: event.endsAt ?? event.startsAt },
              props.timezone,
            );
        const duration = Math.max(
          1,
          (Date.parse(end) - Date.parse(eventDate(event, props.timezone))) /
            86400000 +
            (event.allDay && event.endsOn ? 0 : 1),
        );
        return (
          <div className="calendar-timeline-row" key={event.occurrenceID}>
            <div
              style={{
                marginLeft: `${Math.min(95, (start / total) * 100)}%`,
                width: `${Math.max(5, (Math.min(total - start, duration) / total) * 100)}%`,
              }}
            >
              <CalendarEventCard
                event={event}
                timezone={props.timezone}
                onSelect={() => props.onSelect(event.occurrenceID)}
              />
            </div>
          </div>
        );
      })}
    </section>
  );
}
export function CapacityView({
  capacity,
  names,
}: {
  capacity: Record<string, CapacitySummary>;
  names: Record<string, string>;
}) {
  return (
    <section aria-label="Technician capacity">
      <p>
        Authoritative availability and commitments for this window. Totals
        include authorized Busy time; source details remain hidden.
      </p>
      <div className="calendar-table-scroll">
        <table>
          <thead>
            <tr>
              {[
                "Technician",
                "Available",
                "Committed",
                "Remaining",
                "Overbooked",
                "Tentative PTO",
                "Unscheduled",
              ].map((title) => (
                <th key={title} scope="col">
                  {title}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Object.entries(capacity).map(([id, row]) => (
              <tr key={id}>
                <th scope="row">{names[id] ?? id}</th>
                {[
                  row.available_minutes,
                  row.committed_minutes,
                  row.remaining_minutes,
                  row.overbooked_minutes,
                  row.tentative_unavailable_minutes,
                  row.unscheduled_minutes,
                ].map((value, index) => (
                  <td key={index}>
                    {(value / 60).toLocaleString(undefined, {
                      maximumFractionDigits: 1,
                    })}
                    h
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!Object.keys(capacity).length ? (
        <p>No authorized technician capacity in this window.</p>
      ) : null}
    </section>
  );
}
