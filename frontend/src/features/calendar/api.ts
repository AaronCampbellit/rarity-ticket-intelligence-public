import { csrfHeaders } from "../../api/browserSession";
import { validTimezone } from "./preferences";
import {
  calendarLenses,
  filterKeys,
  type CalendarAPI,
  type CalendarEvent,
  type CalendarFilter,
  type CalendarRequest,
  type CapacitySummary,
  type Recurrence,
  type SavedCalendarLens,
  type SchedulingProposal,
} from "./types";

export class CalendarAPIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status = 502,
  ) {
    super(code);
  }
}
export function invalid(): never {
  throw new CalendarAPIError("invalid_response");
}
export function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    return invalid();
  return value as Record<string, unknown>;
}
export function string(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) return invalid();
  return value;
}
export function optional(value: unknown): string | undefined {
  return value == null || value === "" ? undefined : string(value);
}
export function number(value: unknown): number {
  if (!Number.isSafeInteger(value)) return invalid();
  return value as number;
}
export function array(value: unknown): unknown[] {
  if (value == null) return [];
  if (!Array.isArray(value)) return invalid();
  return value;
}
function boolean(value: unknown): boolean {
  if (typeof value !== "boolean") return invalid();
  return value;
}
function timestamp(value: unknown): string {
  const text = string(value);
  if (!/^\d{4}-\d\d-\d\dT/.test(text) || !Number.isFinite(Date.parse(text)))
    return invalid();
  return text;
}
function date(value: unknown): string {
  const text = string(value).slice(0, 10);
  if (
    !/^\d{4}-\d\d-\d\d$/.test(text) ||
    !Number.isFinite(Date.parse(`${text}T00:00:00Z`)) ||
    new Date(`${text}T00:00:00Z`).toISOString().slice(0, 10) !== text
  )
    return invalid();
  return text;
}
function choice<const T extends string>(
  value: unknown,
  choices: readonly T[],
): T {
  if (typeof value !== "string" || !choices.includes(value as T))
    return invalid();
  return value as T;
}
function recurrence(value: unknown): Recurrence | undefined {
  if (value == null) return undefined;
  const row = record(value);
  const interval = number(row.interval);
  if (interval < 1) return invalid();
  return {
    frequency: choice(row.frequency, ["daily", "weekly", "monthly", "yearly"]),
    interval,
    ...(row.count == null ? {} : { count: number(row.count) }),
    ...(row.until == null ? {} : { until: timestamp(row.until) }),
    ...(row.weekdays == null
      ? {}
      : { weekdays: array(row.weekdays).map(number) }),
  };
}
export function parseEvent(value: unknown): CalendarEvent {
  const row = record(value);
  const privacy = choice(row.privacy, ["full", "busy"]);
  const allDay = boolean(row.all_day);
  const capabilities = record(row.capabilities);
  const source = privacy === "full" ? record(row.source) : undefined;
  const start = allDay ? date(row.starts_on) : timestamp(row.starts_at);
  const end =
    (allDay ? row.ends_on : row.ends_at) == null
      ? undefined
      : allDay
        ? date(row.ends_on)
        : timestamp(row.ends_at);
  if (end && (allDay ? end < start : Date.parse(end) < Date.parse(start)))
    return invalid();
  if (
    (allDay && (row.starts_at != null || row.ends_at != null)) ||
    (!allDay &&
      (row.starts_on != null ||
        row.ends_on != null ||
        !validTimezone(row.timezone)))
  )
    return invalid();
  const full = privacy === "full";
  return {
    id: string(row.id),
    occurrenceID: string(row.occurrence_id),
    privacy,
    title: full ? string(row.title) : "Busy",
    allDay,
    ...(allDay
      ? { startsOn: start, endsOn: end }
      : { startsAt: start, endsAt: end, timezone: string(row.timezone) }),
    assigneeID: optional(row.assignee_id),
    eventRole: full ? optional(row.event_role) : undefined,
    source: source
      ? {
          type: string(source.type),
          id: string(source.id),
          client_id: optional(source.client_id),
          msp_id: optional(source.msp_id),
        }
      : undefined,
    projectionID: full ? optional(row.projection_id) : undefined,
    occurrenceKey: full ? optional(row.occurrence_key) : undefined,
    sourceRevision:
      full && row.source_revision != null
        ? number(row.source_revision)
        : undefined,
    health:
      full && row.health
        ? choice(row.health, [
            "blocked",
            "overdue",
            "at_risk",
            "on_track",
            "terminal",
          ])
        : undefined,
    healthReasons: full
      ? array(row.health_reasons).map((reason) => string(record(reason).code))
      : [],
    hasConflict: full && row.has_conflict === true,
    schedulingMode:
      full && row.scheduling_mode
        ? choice(row.scheduling_mode, [
            "fixed_block",
            "effort_allocation",
            "informational",
          ])
        : undefined,
    recurrence: full ? recurrence(row.recurrence) : undefined,
    plannedMinutes:
      full && row.planned_minutes != null
        ? number(row.planned_minutes)
        : undefined,
    capacityBearing: full && row.capacity_bearing === true,
    capabilities: {
      viewSource: full && boolean(capabilities.view_source),
      schedule: full && boolean(capabilities.schedule),
    },
  };
}
export function parseFilter(value: unknown): CalendarFilter {
  const body = record(value);
  const filter: CalendarFilter = {};
  for (const key of filterKeys)
    if (body[key] != null) filter[key] = array(body[key]).map(string);
  if (body.conflicts_only != null)
    filter.conflicts_only = boolean(body.conflicts_only);
  return filter;
}
function savedLens(value: unknown): SavedCalendarLens {
  const body = record(value),
    query = record(body.query);
  const timezone = optional(query.viewer_timezone);
  if (timezone && !validTimezone(timezone)) return invalid();
  return {
    id: string(body.id),
    name: string(body.name),
    query: {
      ...parseFilter(query),
      lens: choice(query.lens, calendarLenses),
      viewer_timezone: timezone,
    },
  };
}
function proposal(value: unknown): SchedulingProposal {
  const body = record(value);
  const version = number(body.version);
  if (version < 1) return invalid();
  return {
    id: string(body.id),
    version,
    expiresAt: timestamp(body.expires_at),
    state: string(body.state),
    changes: array(body.changes).map((value) => {
      const row = record(value),
        requested = record(row.requested),
        source = record(requested.Source);
      return {
        id: string(row.id),
        required: boolean(row.required),
        title: `${string(source.Type ?? source.type).replaceAll("_", " ")} · ${string(source.ID ?? source.id)}`,
        start: optional(requested.StartsOn ?? requested.StartsAt),
        end: optional(requested.EndsOn ?? requested.EndsAt),
      };
    }),
    conflicts: array(body.conflicts).map((value) => {
      const row = record(value);
      return {
        severity: string(row.severity),
        reasonCode: string(row.reason_code),
      };
    }),
    blocked: array(body.blocked_sources).map((value) =>
      string(record(value).reason_code),
    ),
    capacity: array(body.capacity).map((value) => {
      const row = record(value);
      return {
        technicianID: string(row.technician_id),
        delta: number(row.delta_minutes),
        available: number(row.available_minutes),
        committed: number(row.committed_minutes),
        overbooked: number(row.overbooked_minutes),
      };
    }),
    health: array(body.health).map((value) => {
      const row = record(value);
      return {
        state: string(row.state),
        reasons: array(row.reasons).map((value) => string(record(value).code)),
      };
    }),
    notifications: array(body.notifications).map((value) => {
      const row = record(value);
      return {
        recipientID: string(row.recipient_id),
        reason: string(row.reason_code),
      };
    }),
  };
}
function query(request: CalendarRequest) {
  const parameters = new URLSearchParams({
    start: request.start,
    end: request.end,
    limit: String(request.limit ?? 250),
  });
  for (const [key, value] of Object.entries(request.filter ?? {})) {
    if (Array.isArray(value)) {
      if (key === "client_ids" && value.length === 0)
        parameters.append(key, "");
      for (const item of value) parameters.append(key, item);
    } else if (value === true) parameters.set(key, "true");
  }
  if (request.cursor) parameters.set("cursor", request.cursor);
  return parameters;
}
export function createCalendarAPI(
  fetcher: typeof fetch = (input, init) => globalThis.fetch(input, init),
): CalendarAPI {
  async function request(path: string, signal?: AbortSignal, body?: unknown) {
    const response = await fetcher(path, {
      credentials: "same-origin",
      signal,
      ...(body === undefined
        ? {}
        : {
            method: "POST",
            headers: { "Content-Type": "application/json", ...csrfHeaders() },
            body: JSON.stringify(body),
          }),
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      throw new CalendarAPIError(
        typeof error?.error?.code === "string"
          ? error.error.code
          : "calendar_unavailable",
        response.status,
      );
    }
    return response.json();
  }
  return {
    async events(input, signal) {
      const body = record(
        await request(`/api/v1/calendar/events?${query(input)}`, signal),
      );
      if (!Array.isArray(body.events)) return invalid();
      return {
        events: body.events.map(parseEvent),
        nextCursor: optional(body.next_cursor),
      };
    },
    async options(input, signal) {
      const body = record(
        await request(
          `/api/v1/calendar/filter-options?${query(input)}`,
          signal,
        ),
      );
      return Object.fromEntries(
        Object.entries(body).map(([key, values]) => [
          key,
          Object.fromEntries(
            Object.entries(record(values)).map(([id, count]) => [
              id,
              number(count),
            ]),
          ),
        ]),
      );
    },
    async capacity(input, signal) {
      const body = record(
        await request(`/api/v1/calendar/capacity?${query(input)}`, signal),
      );
      const keys = [
        "available_minutes",
        "fixed_minutes",
        "allocated_minutes",
        "committed_minutes",
        "remaining_minutes",
        "overbooked_minutes",
        "tentative_unavailable_minutes",
        "unscheduled_minutes",
        "dependency_blocked_minutes",
      ] as const;
      return Object.fromEntries(
        Object.entries(body).map(([id, value]) => {
          const row = record(value);
          return [
            id,
            Object.fromEntries(
              keys.map((key) => [key, number(row[key])]),
            ) as CapacitySummary,
          ];
        }),
      );
    },
    async live(cursor, signal) {
      const response = await fetcher(
        `/api/v1/calendar/live?${new URLSearchParams({ cursor })}`,
        {
          credentials: "same-origin",
          signal,
          headers: { Accept: "text/event-stream" },
        },
      );
      if (!response.ok)
        throw new CalendarAPIError(
          "calendar_live_unavailable",
          response.status,
        );
      let next = cursor,
        changed = false;
      for (const block of (await response.text()).split(/\r?\n\r?\n/)) {
        const event = block.match(/^event: (.+)$/m)?.[1];
        const data = block.match(/^data: (.+)$/m)?.[1];
        if (!event || !data) continue;
        if (["refetch", "upsert", "remove"].includes(event)) changed = true;
        if (event === "cursor" || event === "refetch")
          next = string(record(JSON.parse(data)).cursor);
      }
      return { cursor: next, changed };
    },
    async views(signal) {
      return array(
        await request("/api/v1/views?kind=calendar_lens", signal),
      ).map(savedLens);
    },
    async saveView(name, query, audience) {
      return savedLens(
        await request("/api/v1/views", undefined, {
          name,
          kind: "calendar_lens",
          query,
          audience,
        }),
      );
    },
    async preview(change) {
      return proposal(
        await request("/api/v1/calendar/proposals", undefined, { change }),
      );
    },
    async apply(accepted, optionalIDs, reason) {
      const body = record(
        await request(
          `/api/v1/calendar/proposals/${encodeURIComponent(accepted.id)}/apply`,
          undefined,
          {
            expected_proposal_version: accepted.version,
            accepted_optional_change_ids: optionalIDs,
            reason,
          },
        ),
      );
      if (body.proposal_id !== accepted.id) return invalid();
    },
  };
}
export const calendarAPI = createCalendarAPI();
