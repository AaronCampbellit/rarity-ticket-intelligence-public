import {
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type FormEvent,
  type ReactNode,
} from "react";
import { Button, Notice, Select, TextInput } from "../../design-system";
import { array, number, record, string } from "./api";
import { calendarError, calendarRequest } from "./requests";
import { dateInZone, shiftDate, zonedTimestamp } from "./dates";
import type { Recurrence } from "./types";

export type Option = { id: string; name: string };
export type SourceRow = Record<string, unknown> & {
  id: string;
  version: number;
};
export const textValue = (value: unknown) =>
  typeof value === "string" ? value : "";
export function useSourceRows(path?: string, clientID?: string) {
  const [rows, setRows] = useState<SourceRow[]>([]),
    [error, setError] = useState(""),
    [ready, setReady] = useState(false),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setRows([]);
    setError("");
    setReady(false);
    if (path)
      void calendarRequest(path, { signal: controller.signal, clientID })
        .then((value) => {
          const found = array(value).map((value) => {
            const row = record(value);
            return { ...row, id: string(row.id), version: number(row.version) };
          });
          if (!controller.signal.aborted) {
            setRows(found);
            setReady(true);
          }
        })
        .catch((error) => {
          if (!controller.signal.aborted) setError(calendarError(error));
        });
    return () => controller.abort();
  }, [path, clientID, revision]);
  return {
    rows,
    error,
    ready,
    reload: () => setRevision((value) => value + 1),
  };
}
export function Field({
  label,
  ...props
}: ComponentProps<typeof TextInput> & { label: string }) {
  return (
    <label>
      {label}
      <TextInput {...props} />
    </label>
  );
}
export function Choice({
  label,
  options,
  empty = "Choose…",
  ...props
}: ComponentProps<typeof Select> & {
  label: string;
  options: readonly (string | Option)[];
  empty?: string | false;
}) {
  return (
    <label>
      {label}
      <Select aria-label={label} {...props}>
        {empty !== false ? <option value="">{empty}</option> : null}
        {options.map((item) =>
          typeof item === "string" ? (
            <option key={item} value={item}>
              {item.replaceAll("_", " ")}
            </option>
          ) : (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ),
        )}
      </Select>
    </label>
  );
}
export function MutationForm({
  children,
  label,
  onSubmit,
  onSaved,
}: {
  children: ReactNode;
  label: string;
  onSubmit: (
    data: FormData,
    idempotencyKey: (payload: unknown) => string,
  ) => Promise<unknown>;
  onSaved: () => void;
}) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const active = useRef(false),
    attempt = useRef({ payload: "", key: "" });
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (active.current) return;
    const dialog = event.currentTarget.closest<HTMLElement>('[role="dialog"]');
    active.current = true;
    setBusy(true);
    setError("");
    try {
      await onSubmit(new FormData(event.currentTarget), (payload) => {
        const serialized = JSON.stringify(payload);
        if (attempt.current.payload !== serialized)
          attempt.current = { payload: serialized, key: crypto.randomUUID() };
        return attempt.current.key;
      });
      onSaved();
    } catch (error) {
      setError(calendarError(error));
    } finally {
      active.current = false;
      setBusy(false);
      requestAnimationFrame(() => {
        if (dialog?.isConnected && !dialog.contains(document.activeElement)) {
          dialog
            .querySelector<HTMLElement>(
              "select:not(:disabled), button:not(:disabled), input:not(:disabled)",
            )
            ?.focus();
        }
      });
    }
  }
  return (
    <form className="calendar-form" onSubmit={(event) => void submit(event)}>
      {error ? (
        <Notice tone="danger" title="Changes were not saved">
          {error}
        </Notice>
      ) : null}
      <fieldset disabled={busy}>{children}</fieldset>
      <Button type="submit" disabled={busy}>
        {busy ? "Saving…" : label}
      </Button>
    </form>
  );
}
export const value = (data: FormData, key: string) =>
  String(data.get(key) ?? "").trim();
export const int = (data: FormData, key: string) => {
  const result = Number(value(data, key));
  if (!Number.isSafeInteger(result)) throw new Error("Enter a whole number.");
  return result;
};
export function intervalPayload(data: FormData) {
  const all_day = data.get("all_day") === "on";
  if (all_day) {
    const starts_on = value(data, "starts_on"),
      last = value(data, "last_on");
    if (!starts_on || !last || last < starts_on)
      throw new Error("The last day must be on or after the first day.");
    return { all_day, starts_on, ends_on: shiftDate(last, 1) };
  }
  const timezone = value(data, "timezone"),
    starts_at = zonedTimestamp(`${value(data, "starts_at")}:00`, timezone),
    ends_at = zonedTimestamp(`${value(data, "ends_at")}:00`, timezone);
  if (ends_at <= starts_at)
    throw new Error("End time must be after start time.");
  return { all_day, starts_at, ends_at, timezone };
}
export function localInput(instant: unknown, timezone: string) {
  if (typeof instant !== "string" || !instant) return "";
  const date = new Date(instant),
    parts = new Intl.DateTimeFormat("en-GB", {
      timeZone: timezone,
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    }).format(date);
  return `${dateInZone(date, timezone)}T${parts}`;
}
export function IntervalFields({
  initial = {},
  defaultTimezone = "UTC",
}: {
  initial?: Record<string, unknown>;
  defaultTimezone?: string;
}) {
  const [allDay, setAllDay] = useState(initial.all_day !== false),
    timezone = textValue(initial.timezone) || defaultTimezone;
  return (
    <>
      <label>
        <input
          name="all_day"
          type="checkbox"
          checked={allDay}
          onChange={(e) => setAllDay(e.target.checked)}
        />{" "}
        All day
      </label>
      {allDay ? (
        <>
          <Field
            label="First day"
            name="starts_on"
            type="date"
            required
            defaultValue={textValue(initial.starts_on)}
          />
          <Field
            label="Last day (included)"
            name="last_on"
            type="date"
            required
            defaultValue={
              initial.ends_on
                ? shiftDate(textValue(initial.ends_on), -1)
                : textValue(initial.starts_on)
            }
          />
        </>
      ) : (
        <>
          <Field
            label="Start time"
            name="starts_at"
            type="datetime-local"
            required
            defaultValue={localInput(initial.starts_at, timezone)}
          />
          <Field
            label="End time"
            name="ends_at"
            type="datetime-local"
            required
            defaultValue={localInput(initial.ends_at, timezone)}
          />
          <Field
            label="Timezone"
            name="timezone"
            required
            defaultValue={timezone}
          />
        </>
      )}
    </>
  );
}
export function RecurrenceEditor({ initial }: { initial?: Recurrence }) {
  const [frequency, setFrequency] = useState(initial?.frequency ?? ""),
    [end, setEnd] = useState(initial?.until ? "until" : "count");
  return (
    <fieldset>
      <legend>Repeat</legend>
      <Choice
        label="Frequency"
        name="frequency"
        value={frequency}
        onChange={(e) => setFrequency(e.target.value as typeof frequency)}
        empty="Does not repeat"
        options={["daily", "weekly", "monthly", "yearly"]}
      />
      {frequency ? (
        <>
          <Field
            label="Repeat every"
            name="interval"
            type="number"
            min={1}
            max={365}
            required
            defaultValue={initial?.interval ?? 1}
          />
          {frequency === "weekly" ? (
            <fieldset>
              <legend>Weekdays</legend>
              {[
                "Sunday",
                "Monday",
                "Tuesday",
                "Wednesday",
                "Thursday",
                "Friday",
                "Saturday",
              ].map((day, index) => (
                <label key={day}>
                  <input
                    type="checkbox"
                    name="weekdays"
                    value={index}
                    defaultChecked={initial?.weekdays?.includes(index)}
                  />
                  {day}
                </label>
              ))}
            </fieldset>
          ) : null}
          <Choice
            label="Repeat ends"
            name="recurrence_end"
            value={end}
            onChange={(e) => setEnd(e.target.value)}
            empty={false}
            options={[
              { id: "count", name: "After occurrences" },
              { id: "until", name: "On date" },
            ]}
          />
          {end === "count" ? (
            <Field
              label="Occurrence count"
              name="count"
              type="number"
              min={1}
              max={1000}
              required
              defaultValue={initial?.count ?? 12}
            />
          ) : (
            <Field
              label="Repeat through"
              name="until"
              type="date"
              required
              defaultValue={initial?.until?.slice(0, 10)}
            />
          )}
        </>
      ) : null}
    </fieldset>
  );
}
export function recurrencePayload(data: FormData): Recurrence | null {
  const frequency = value(data, "frequency") as Recurrence["frequency"];
  if (!frequency) return null;
  const weekdays = data.getAll("weekdays").map(Number);
  if (frequency === "weekly" && !weekdays.length)
    throw new Error("Choose at least one weekday.");
  return {
    frequency,
    interval: int(data, "interval"),
    ...(frequency === "weekly" ? { weekdays } : {}),
    ...(value(data, "recurrence_end") === "until"
      ? { until: `${value(data, "until")}T23:59:59Z` }
      : { count: int(data, "count") }),
  };
}
