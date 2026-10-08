import { useState } from "react";
import { Button, Notice } from "../../design-system";
import { array, number, record } from "./api";
import {
  Choice,
  Field,
  IntervalFields,
  MutationForm,
  intervalPayload,
  textValue,
  value,
  int,
  useSourceRows,
  type Option,
  type SourceRow,
} from "./formSupport";
import { calendarRequest } from "./requests";

export function PTOForm({
  principalID,
  onSaved,
}: {
  principalID: string;
  onSaved: () => void;
}) {
  return (
    <MutationForm
      label="Request PTO"
      onSaved={onSaved}
      onSubmit={(data, key) => {
        const payload = {
          technician_id: principalID,
          pto_type: value(data, "pto_type"),
          ...intervalPayload(data),
        };
        return calendarRequest("workforce/pto", {
          method: "POST",
          body: { ...payload, idempotency_key: key(payload) },
        });
      }}
    >
      <p>
        PTO is tentative until approved and does not reduce committed capacity
        while requested.
      </p>
      <Choice
        label="PTO type"
        name="pto_type"
        required
        options={["vacation", "sick", "personal", "training", "other"]}
      />
      <IntervalFields />
    </MutationForm>
  );
}
export function PTOManagement({
  principalID,
  technicians,
  canManage,
  onChanged,
}: {
  principalID: string;
  technicians: Option[];
  canManage: boolean;
  onChanged: () => void;
}) {
  const source = useSourceRows("workforce/pto"),
    [selected, setSelected] = useState<SourceRow>();
  return (
    <section>
      <h3>PTO requests</h3>
      <Button
        onClick={() => {
          setSelected(undefined);
          source.reload();
        }}
      >
        Reload PTO requests
      </Button>
      {source.error ? (
        <Notice tone="danger" title="PTO unavailable">
          {source.error}
        </Notice>
      ) : null}
      {!source.ready && !source.error ? (
        <p role="status">Loading PTO…</p>
      ) : null}
      {source.ready && !source.rows.length ? (
        <p>No visible PTO requests.</p>
      ) : null}
      <ul>
        {source.rows.map((row) => (
          <li key={row.id}>
            {technicians.find((tech) => tech.id === row.technician_id)?.name ||
              (row.technician_id === principalID ? "You" : "Technician")}{" "}
            · {textValue(row.starts_on || row.starts_at).slice(0, 10)} ·{" "}
            {row.state === "requested"
              ? "Tentative — does not reduce committed capacity"
              : textValue(row.state)}{" "}
            <Button onClick={() => setSelected(row)}>Review PTO request</Button>
          </li>
        ))}
      </ul>
      {selected ? (
        <MutationForm
          key={`${selected.id}-${selected.version}`}
          label="Confirm PTO action"
          onSaved={() => {
            setSelected(undefined);
            source.reload();
            onChanged();
          }}
          onSubmit={(data, key) => {
            const action = value(data, "action"),
              payload = {
                expected_version: selected.version,
                ...(action === "cancel"
                  ? {}
                  : { decision: action, reason: value(data, "reason") }),
              };
            return calendarRequest(
              `workforce/pto/${encodeURIComponent(selected.id)}/${action === "cancel" ? "cancel" : "decision"}`,
              {
                method: "POST",
                body: { ...payload, idempotency_key: key(payload) },
              },
            );
          }}
        >
          <p>
            Only the current manager or a workforce administrator can approve or
            reject a request.
          </p>
          <Choice
            label="PTO action"
            name="action"
            required
            options={[
              ...((selected.technician_id !== principalID || canManage) &&
              selected.state === "requested"
                ? [
                    { id: "approved", name: "Approve" },
                    { id: "rejected", name: "Reject" },
                  ]
                : []),
              ...((selected.technician_id === principalID || canManage) &&
              ["requested", "approved"].includes(textValue(selected.state))
                ? [{ id: "cancel", name: "Cancel request" }]
                : []),
            ]}
          />
          <Field
            label="Decision reason"
            name="reason"
            required
            maxLength={1000}
          />
        </MutationForm>
      ) : null}
    </section>
  );
}
type WeeklyWindow = {
  weekday: number;
  starts_minute: number;
  ends_minute: number;
  capacity_percent: number;
};
export function validateWeeklyWindows(windows: WeeklyWindow[]) {
  for (const window of windows)
    if (
      window.starts_minute >= window.ends_minute ||
      window.starts_minute < 0 ||
      window.ends_minute > 1440 ||
      window.capacity_percent < 0 ||
      window.capacity_percent > 100
    )
      throw new Error("Check each working window and its capacity.");
  for (let i = 0; i < windows.length; i++)
    for (let j = i + 1; j < windows.length; j++)
      if (
        windows[i].weekday === windows[j].weekday &&
        windows[i].starts_minute < windows[j].ends_minute &&
        windows[j].starts_minute < windows[i].ends_minute
      )
        throw new Error("Working windows must not overlap on the same day.");
}
const clock = (minutes: number) =>
  `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
const minutes = (time: string) => {
  const [hours, minutes] = time.split(":").map(Number);
  return hours * 60 + minutes;
};
export function WorkforceScheduleSettings({
  technicians,
  onChanged,
}: {
  technicians: Option[];
  onChanged: () => void;
}) {
  const [technician, setTechnician] = useState(""),
    source = useSourceRows(
      technician
        ? `workforce/schedules?technician_ids=${encodeURIComponent(technician)}`
        : undefined,
    );
  const current = [...source.rows].sort((a, b) => b.version - a.version)[0];
  return (
    <section>
      <h3>Working schedules</h3>
      <Choice
        label="Technician"
        options={technicians}
        value={technician}
        onChange={(e) => setTechnician(e.target.value)}
      />
      {source.error ? (
        <Notice tone="danger" title="Schedules unavailable">
          {source.error}
        </Notice>
      ) : null}
      {technician ? (
        <Button onClick={source.reload}>Reload working schedule</Button>
      ) : null}
      {source.ready ? (
        <>
          <ScheduleForm
            key={`${technician}-${current?.version ?? 0}`}
            technicianID={technician}
            current={current}
            onSaved={() => {
              source.reload();
              onChanged();
            }}
          />
          {source.rows.map((row) => (
            <details key={`${row.id}-${row.version}`}>
              <summary>
                {textValue(row.effective_from)} · {textValue(row.timezone)} ·
                version {row.version}
              </summary>
              <p>{array(row.exceptions).length} dated exceptions</p>
              <MutationForm
                label="Add dated exception"
                onSaved={() => {
                  source.reload();
                  onChanged();
                }}
                onSubmit={(data, key) => {
                  const all_day = data.get("exception_all_day") === "on",
                    payload = {
                      exception_on: value(data, "exception_on"),
                      state: value(data, "state"),
                      all_day,
                      starts_minute: all_day
                        ? 0
                        : minutes(value(data, "exception_start")),
                      ends_minute: all_day
                        ? 0
                        : minutes(value(data, "exception_end")),
                      capacity_percent:
                        value(data, "state") === "unavailable"
                          ? 0
                          : int(data, "capacity_percent"),
                      reason: value(data, "reason"),
                      expected_version: row.version,
                    };
                  return calendarRequest(
                    `workforce/schedules/${encodeURIComponent(row.id)}/exceptions`,
                    {
                      method: "POST",
                      body: { ...payload, idempotency_key: key(payload) },
                    },
                  );
                }}
              >
                <Field
                  label="Exception date"
                  name="exception_on"
                  type="date"
                  required
                />
                <Choice
                  label="Availability"
                  name="state"
                  options={["available", "unavailable"]}
                  required
                />
                <label>
                  <input
                    type="checkbox"
                    name="exception_all_day"
                    defaultChecked
                  />
                  All day exception
                </label>
                <Field
                  label="Exception start"
                  name="exception_start"
                  type="time"
                  defaultValue="09:00"
                />
                <Field
                  label="Exception end"
                  name="exception_end"
                  type="time"
                  defaultValue="17:00"
                />
                <Field
                  label="Available capacity percent"
                  name="capacity_percent"
                  type="number"
                  min={0}
                  max={100}
                  defaultValue={100}
                />
                <Field label="Exception reason" name="reason" required />
              </MutationForm>
            </details>
          ))}
        </>
      ) : null}
    </section>
  );
}
function ScheduleForm({
  technicianID,
  current,
  onSaved,
}: {
  technicianID: string;
  current?: SourceRow;
  onSaved: () => void;
}) {
  const [windows, setWindows] = useState<WeeklyWindow[]>(
    current
      ? array(current.windows).map((value) => {
          const row = record(value);
          return {
            weekday: number(row.weekday),
            starts_minute: number(row.starts_minute),
            ends_minute: number(row.ends_minute),
            capacity_percent: number(row.capacity_percent),
          };
        })
      : [1, 2, 3, 4, 5].map((weekday) => ({
          weekday,
          starts_minute: 540,
          ends_minute: 1020,
          capacity_percent: 100,
        })),
  );
  return (
    <MutationForm
      label="Publish working schedule"
      onSaved={onSaved}
      onSubmit={(data, key) => {
        validateWeeklyWindows(windows);
        const payload = {
          technician_id: technicianID,
          timezone: value(data, "timezone"),
          effective_from: value(data, "effective_from"),
          effective_through: value(data, "effective_through"),
          expected_version: current?.version ?? 0,
          windows,
          exceptions: [],
        };
        return calendarRequest("workforce/schedules", {
          method: "POST",
          body: { ...payload, idempotency_key: key(payload) },
        });
      }}
    >
      <p>
        Publishing adds a new effective schedule. Existing dated exceptions
        remain on their original schedule.
      </p>
      <Field
        label="Schedule timezone"
        name="timezone"
        required
        defaultValue={textValue(current?.timezone) || "UTC"}
      />
      <Field
        label="Effective from"
        name="effective_from"
        type="date"
        required
      />
      <Field label="Effective through" name="effective_through" type="date" />
      {windows.map((window, index) => (
        <div className="calendar-toolbar" key={index}>
          <Choice
            label={`Weekday ${index + 1}`}
            empty={false}
            value={window.weekday}
            options={[
              "Sunday",
              "Monday",
              "Tuesday",
              "Wednesday",
              "Thursday",
              "Friday",
              "Saturday",
            ].map((name, index) => ({ id: String(index), name }))}
            onChange={(e) =>
              setWindows(
                windows.map((item, i) =>
                  i === index
                    ? { ...item, weekday: Number(e.target.value) }
                    : item,
                ),
              )
            }
          />
          <Field
            label={`Start ${index + 1}`}
            type="time"
            required
            value={clock(window.starts_minute)}
            onChange={(e) =>
              setWindows(
                windows.map((item, i) =>
                  i === index
                    ? { ...item, starts_minute: minutes(e.target.value) }
                    : item,
                ),
              )
            }
          />
          <Field
            label={`End ${index + 1}`}
            type="time"
            required
            value={clock(window.ends_minute)}
            onChange={(e) =>
              setWindows(
                windows.map((item, i) =>
                  i === index
                    ? { ...item, ends_minute: minutes(e.target.value) }
                    : item,
                ),
              )
            }
          />
          <Field
            label={`Capacity percent ${index + 1}`}
            type="number"
            required
            min={0}
            max={100}
            value={window.capacity_percent}
            onChange={(e) =>
              setWindows(
                windows.map((item, i) =>
                  i === index
                    ? { ...item, capacity_percent: Number(e.target.value) }
                    : item,
                ),
              )
            }
          />
          <Button
            onClick={() => setWindows(windows.filter((_, i) => i !== index))}
          >
            Remove window {index + 1}
          </Button>
        </div>
      ))}
      <Button
        onClick={() =>
          setWindows([
            ...windows,
            {
              weekday: 1,
              starts_minute: 540,
              ends_minute: 1020,
              capacity_percent: 100,
            },
          ])
        }
      >
        Add working window
      </Button>
    </MutationForm>
  );
}
