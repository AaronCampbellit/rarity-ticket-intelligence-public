export const calendarLenses = [
  "day",
  "week",
  "month",
  "timeline",
  "capacity",
  "agenda",
] as const;
export type CalendarLens = (typeof calendarLenses)[number];
export type CalendarHealth =
  "blocked" | "overdue" | "at_risk" | "on_track" | "terminal";
export type SchedulingMode =
  "fixed_block" | "effort_allocation" | "informational";
export type Recurrence = {
  frequency: "daily" | "weekly" | "monthly" | "yearly";
  interval: number;
  weekdays?: number[];
  count?: number;
  until?: string;
};
export type CalendarSource = {
  type: string;
  id: string;
  client_id?: string;
  msp_id?: string;
};
export type CalendarEvent = {
  id: string;
  occurrenceID: string;
  title: string;
  allDay: boolean;
  privacy: "full" | "busy";
  startsOn?: string;
  endsOn?: string;
  startsAt?: string;
  endsAt?: string;
  timezone?: string;
  assigneeID?: string;
  eventRole?: string;
  source?: CalendarSource;
  projectionID?: string;
  occurrenceKey?: string;
  sourceRevision?: number;
  health?: CalendarHealth;
  healthReasons: string[];
  hasConflict: boolean;
  schedulingMode?: SchedulingMode;
  recurrence?: Recurrence;
  plannedMinutes?: number;
  capacityBearing: boolean;
  capabilities: { viewSource: boolean; schedule: boolean };
};
export const filterKeys = [
  "client_ids",
  "technician_ids",
  "owner_ids",
  "team_ids",
  "technology_ids",
  "project_ids",
  "phase_ids",
  "sla_ids",
  "ticket_types",
  "tag_ids",
  "priorities",
  "event_roles",
  "health_states",
  "source_types",
  "scheduling_modes",
  "terminal_states",
] as const;
export type CalendarFilter = Partial<
  Record<(typeof filterKeys)[number], string[]>
> & { conflicts_only?: boolean };
export type CalendarRequest = {
  start: string;
  end: string;
  filter?: CalendarFilter;
  cursor?: string;
  limit?: number;
};
export type CalendarPageResult = {
  events: CalendarEvent[];
  nextCursor?: string;
};
export type FilterOptions = Record<string, Record<string, number>>;
export type CapacitySummary = {
  available_minutes: number;
  fixed_minutes: number;
  allocated_minutes: number;
  committed_minutes: number;
  remaining_minutes: number;
  overbooked_minutes: number;
  tentative_unavailable_minutes: number;
  unscheduled_minutes: number;
  dependency_blocked_minutes: number;
};
export type ScheduleChange = {
  projection_id: string;
  occurrence_key?: string;
  occurrence_scope?: "this_occurrence" | "this_and_future" | "entire_series";
  all_day: boolean;
  starts_on?: string;
  ends_on?: string;
  starts_at?: string;
  ends_at?: string;
  timezone?: string;
  recurrence?: Recurrence;
};
export type ProposalChange = {
  id: string;
  required: boolean;
  title: string;
  start?: string;
  end?: string;
};
export type SchedulingProposal = {
  id: string;
  version: number;
  expiresAt: string;
  state: string;
  changes: ProposalChange[];
  conflicts: { severity: string; reasonCode: string }[];
  blocked: string[];
  capacity: {
    technicianID: string;
    delta: number;
    available: number;
    committed: number;
    overbooked: number;
  }[];
  health: { state: string; reasons: string[] }[];
  notifications: { recipientID: string; reason: string }[];
};
export type SavedCalendarLens = {
  id: string;
  name: string;
  query: CalendarFilter & { lens: CalendarLens; viewer_timezone?: string };
};
export type CalendarAPI = {
  events(
    request: CalendarRequest,
    signal?: AbortSignal,
  ): Promise<CalendarPageResult>;
  options(
    request: CalendarRequest,
    signal?: AbortSignal,
  ): Promise<FilterOptions>;
  capacity(
    request: CalendarRequest,
    signal?: AbortSignal,
  ): Promise<Record<string, CapacitySummary>>;
  live(
    cursor: string,
    signal?: AbortSignal,
  ): Promise<{ cursor: string; changed: boolean }>;
  views(signal?: AbortSignal): Promise<SavedCalendarLens[]>;
  saveView(
    name: string,
    query: SavedCalendarLens["query"],
    audience: { type: string; id?: string },
  ): Promise<SavedCalendarLens>;
  preview(change: ScheduleChange): Promise<SchedulingProposal>;
  apply(
    proposal: SchedulingProposal,
    optionalIDs: string[],
    reason: string,
  ): Promise<void>;
};
