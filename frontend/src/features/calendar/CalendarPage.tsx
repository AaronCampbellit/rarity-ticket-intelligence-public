import { useMemo, useRef, useState, type FormEvent } from "react";
import { Button, Notice, Page, Select, TextInput } from "../../design-system";
import { CalendarAdministration } from "./CalendarAdministration";
import { useCalendarDirectory } from "./useCalendarDirectory";
import { calendarAPI } from "./api";
import { CalendarLensSwitcher } from "./CalendarEventCard";
import { CalendarFilters } from "./CalendarFilters";
import {
  AgendaView,
  CapacityView,
  DayWeekGrid,
  MonthGrid,
  TimelineView,
} from "./CalendarViews";
import { dateInZone, shiftDate } from "./dates";
import { EventDetailsPanel } from "./EventDetailsPanel";
import { ScheduleProposalPanel } from "./ScheduleProposalPanel";
import { useCalendarWorkspace } from "./useCalendarWorkspace";
import type { CalendarAPI, CalendarEvent } from "./types";
import "./calendar.css";

const noCapabilities = new Set<string>();
export function CalendarPage({
  principalID,
  capabilities = noCapabilities,
  clients = [],
  api = calendarAPI,
}: {
  principalID: string;
  capabilities?: ReadonlySet<string>;
  clients?: Array<{ id: string; name: string }>;
  api?: CalendarAPI;
}) {
  const directory = useCalendarDirectory();
  const workspace = useCalendarWorkspace(principalID, api);
  const [groupBy, setGroupBy] = useState<"none" | "technician">("none");
  const [scheduling, setScheduling] = useState<{
    event: CalendarEvent;
    targetDate?: string;
  }>();
  const [notice, setNotice] = useState("");
  const [saveBusy, setSaveBusy] = useState(false);
  const dragging = useRef<CalendarEvent | undefined>(undefined);
  const names = useMemo(
    () => ({
      ...directory.names,
      ...Object.fromEntries(clients.map((client) => [client.id, client.name])),
    }),
    [clients, directory.names],
  );
  const can = (capability: string) =>
    capabilities.has("*") || capabilities.has(capability);
  const selected = scheduling ? undefined : workspace.selected;
  async function saveView(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setSaveBusy(true);
    try {
      const view = await api.saveView(
        String(data.get("name")),
        {
          ...workspace.filter,
          lens: workspace.lens,
          viewer_timezone: workspace.timezone,
        },
        { type: data.get("share") === "on" ? "msp" : "private" },
      );
      workspace.setViews((current) => [...current, view]);
      setNotice("Calendar view saved.");
    } catch {
      setNotice(
        "Calendar view could not be saved. Check permissions and try again.",
      );
    } finally {
      setSaveBusy(false);
    }
  }
  const viewProps = {
    events: workspace.events,
    timezone: workspace.timezone,
    startDate: workspace.windowRange.startDate,
    endDate: workspace.windowRange.endDate,
    onSelect: workspace.select,
    names,
    groupBy,
    onDragEvent: (event: CalendarEvent) => {
      if (can("calendar.schedule")) dragging.current = event;
    },
    onDropEvent: (targetDate: string) => {
      const event = dragging.current;
      dragging.current = undefined;
      if (event && can("calendar.schedule"))
        setScheduling({ event, targetDate });
    },
  };
  return (
    <Page
      className="calendar-page"
      eyebrow="Service operations"
      title="Calendar"
      description="Schedules and commitments across your authorized clients."
      actions={
        <CalendarAdministration
          principalID={principalID}
          capabilities={capabilities}
          clients={clients}
          onChanged={workspace.refresh}
        />
      }
    >
      <div className="calendar-workspace">
        <div className="calendar-toolbar">
          <Button
            aria-label="Previous period"
            onClick={() => workspace.move(-1)}
          >
            Previous
          </Button>
          <Button
            onClick={() =>
              workspace.setDate(dateInZone(new Date(), workspace.timezone))
            }
          >
            Today
          </Button>
          <Button aria-label="Next period" onClick={() => workspace.move(1)}>
            Next
          </Button>
          <label>
            Date
            <TextInput
              type="date"
              required
              value={workspace.date}
              onChange={(event) => {
                if (/^\d{4}-\d\d-\d\d$/.test(event.target.value))
                  workspace.setDate(event.target.value);
              }}
            />
          </label>
          <label>
            Viewer timezone
            <Select
              value={workspace.timezone}
              onChange={(event) => workspace.setTimezone(event.target.value)}
            >
              {[
                ...new Set([
                  "UTC",
                  workspace.timezone,
                  ...Intl.supportedValuesOf("timeZone"),
                ]),
              ].map((timezone) => (
                <option key={timezone}>{timezone}</option>
              ))}
            </Select>
          </label>
          <Button onClick={workspace.refresh}>Refresh</Button>
        </div>

        <CalendarLensSwitcher
          value={workspace.lens}
          onChange={workspace.setLens}
        />
        <div className="calendar-toolbar">
          <label>
            Saved calendar view
            <Select
              defaultValue=""
              onChange={(event) => {
                const view = workspace.views.find(
                  (view) => view.id === event.target.value,
                );
                if (view) {
                  const { lens, viewer_timezone, ...filter } = view.query;
                  workspace.setLens(lens);
                  workspace.setFilter(filter);
                  if (viewer_timezone) workspace.setTimezone(viewer_timezone);
                }
              }}
            >
              <option value="">Current view</option>
              {workspace.views.map((view) => (
                <option key={view.id} value={view.id}>
                  {view.name}
                </option>
              ))}
            </Select>
          </label>
          <label>
            Group schedule by
            <Select
              value={groupBy}
              onChange={(event) =>
                setGroupBy(event.target.value as typeof groupBy)
              }
            >
              <option value="none">Date</option>
              <option value="technician">Technician</option>
            </Select>
          </label>
        </div>
        <CalendarFilters
          filter={workspace.filter}
          options={workspace.options}
          names={names}
          onChange={workspace.setFilter}
        />
        {can("view.save") ? (
          <details>
            <summary>Save current view</summary>
            <form
              className="calendar-toolbar"
              onSubmit={(event) => void saveView(event)}
            >
              <label>
                Name
                <TextInput name="name" required maxLength={100} />
              </label>
              {can("view.share") ? (
                <label>
                  <input type="checkbox" name="share" /> Share with MSP
                </label>
              ) : null}
              <Button type="submit" disabled={saveBusy}>
                Save view
              </Button>
            </form>
          </details>
        ) : null}
        {notice ? <Notice title="Calendar">{notice}</Notice> : null}
        {workspace.auxiliaryError ? (
          <Notice tone="warning" title="Some calendar controls are unavailable">
            {workspace.auxiliaryError}
          </Notice>
        ) : null}
        {workspace.error ? (
          <Notice tone="danger" title="Calendar request failed">
            {workspace.error.replaceAll("_", " ")}{" "}
            <Button onClick={workspace.refresh}>Retry</Button>
          </Notice>
        ) : null}
        {workspace.state === "loading" ? (
          <p role="status">Loading calendar…</p>
        ) : workspace.state === "ready" ? (
          <>
            <p className="calendar-muted">
              {workspace.windowRange.startDate} through{" "}
              {shiftDate(workspace.windowRange.endDate, -1)} ·{" "}
              {workspace.events.length} loaded events
              {workspace.nextCursor
                ? " · More events available; load them to complete this view."
                : ""}
            </p>
            {!workspace.events.length && workspace.lens !== "capacity" ? (
              <p>No scheduled events match this window and filters.</p>
            ) : null}
            {workspace.lens === "day" || workspace.lens === "week" ? (
              <DayWeekGrid {...viewProps} />
            ) : workspace.lens === "month" ? (
              <MonthGrid
                {...viewProps}
                onOpenDay={(date) => {
                  workspace.setDate(date);
                  workspace.setLens("day");
                }}
              />
            ) : workspace.lens === "agenda" ? (
              <AgendaView {...viewProps} />
            ) : workspace.lens === "timeline" ? (
              <TimelineView {...viewProps} />
            ) : (
              <CapacityView capacity={workspace.capacity} names={names} />
            )}
            {workspace.nextCursor ? (
              <Button
                disabled={workspace.loadingMore}
                onClick={() => void workspace.loadMore()}
              >
                {workspace.loadingMore ? "Loading more…" : "Load more events"}
              </Button>
            ) : null}
          </>
        ) : null}
        <EventDetailsPanel
          event={selected}
          events={workspace.events}
          canSchedule={can("calendar.schedule")}
          timezone={workspace.timezone}
          onClose={() => workspace.select(undefined)}
          onSchedule={(event) => {
            if (can("calendar.schedule")) setScheduling({ event });
          }}
        />
        {scheduling ? (
          <ScheduleProposalPanel
            key={`${scheduling.event.occurrenceID}-${scheduling.targetDate ?? ""}`}
            event={scheduling.event}
            targetDate={scheduling.targetDate}
            api={api}
            onClose={() => setScheduling(undefined)}
            onApplied={() => {
              setScheduling(undefined);
              workspace.select(undefined);
              setNotice(
                "Schedule changes applied. The calendar will refresh as projections update.",
              );
              workspace.refresh();
            }}
          />
        ) : null}
      </div>
    </Page>
  );
}
