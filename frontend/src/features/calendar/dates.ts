import type { CalendarEvent, CalendarLens } from "./types";

export function dateInZone(value: Date, timezone: string): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(value);
  return ["year", "month", "day"]
    .map((type) => parts.find((part) => part.type === type)?.value)
    .join("-");
}
export function shiftDate(date: string, days: number): string {
  return new Date(Date.parse(`${date}T12:00:00Z`) + days * 86400000)
    .toISOString()
    .slice(0, 10);
}
export function zonedTimestamp(
  local: string,
  timezone: string,
  allowAmbiguous = false,
): string {
  const target = Date.parse(`${local}Z`);
  if (!Number.isFinite(target))
    throw new Error("Enter a valid local date and time.");
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  });
  const representedAt = (value: number) => {
    const parts = formatter.formatToParts(new Date(value));
    const part = (key: string) => parts.find((p) => p.type === key)?.value;
    return Date.parse(
      `${part("year")}-${part("month")}-${part("day")}T${part("hour")}:${part("minute")}:${part("second")}Z`,
    );
  };
  let value = target;
  for (let i = 0; i < 4; i++) {
    const represented = representedAt(value);
    if (represented === target) {
      if (!allowAmbiguous)
        for (let offset = -180; offset <= 180; offset += 15) {
          if (offset && representedAt(value + offset * 60000) === target)
            throw new Error(
              "That local time occurs twice in this timezone. Choose an unambiguous time or enter the instant in UTC.",
            );
        }
      return new Date(value).toISOString();
    }
    value += target - represented;
  }
  throw new Error(
    "That local time does not exist in this timezone. Choose another time.",
  );
}
export function calendarWindow(
  date: string,
  lens: CalendarLens,
  timezone: string,
) {
  const day = new Date(`${date}T12:00:00Z`).getUTCDay();
  const startDate =
    lens === "day"
      ? date
      : lens === "month"
        ? `${date.slice(0, 7)}-01`
        : shiftDate(date, -((day + 6) % 7));
  const endDate =
    lens === "day"
      ? shiftDate(startDate, 1)
      : lens === "month"
        ? new Date(
            Date.UTC(Number(date.slice(0, 4)), Number(date.slice(5, 7)), 1),
          )
            .toISOString()
            .slice(0, 10)
        : shiftDate(
            startDate,
            lens === "agenda" || lens === "timeline" ? 28 : 7,
          );
  return {
    start: zonedTimestamp(`${startDate}T00:00:00`, timezone, true),
    end: zonedTimestamp(`${endDate}T00:00:00`, timezone, true),
    startDate,
    endDate,
  };
}
export function eventDate(event: CalendarEvent, timezone: string) {
  return event.allDay
    ? event.startsOn!
    : dateInZone(new Date(event.startsAt!), timezone);
}
export function eventTime(event: CalendarEvent, timezone: string) {
  if (event.allDay) return "All day";
  const format = new Intl.DateTimeFormat(undefined, {
    timeZone: timezone,
    hour: "numeric",
    minute: "2-digit",
  });
  return `${format.format(new Date(event.startsAt!))}${event.endsAt ? ` – ${format.format(new Date(event.endsAt))}` : ""}`;
}
