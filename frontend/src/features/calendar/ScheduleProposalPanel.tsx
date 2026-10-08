import { useEffect, useRef, useState, type FormEvent } from "react";
import { Button, Dialog, Notice, Select, TextInput } from "../../design-system";
import { zonedTimestamp, shiftDate } from "./dates";
import type {
  CalendarAPI,
  CalendarEvent,
  ScheduleChange,
  SchedulingProposal,
} from "./types";

function localTimestamp(timestamp: string, timezone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(new Date(timestamp));
  const part = (type: string) =>
    parts.find((part) => part.type === type)?.value;
  return `${part("year")}-${part("month")}-${part("day")}T${part("hour")}:${part("minute")}`;
}
export function ScheduleProposalPanel({
  event,
  targetDate,
  api,
  onClose,
  onApplied,
}: {
  event: CalendarEvent;
  targetDate?: string;
  api: CalendarAPI;
  onClose: () => void;
  onApplied: () => void;
}) {
  const [proposal, setProposal] = useState<SchedulingProposal>();
  const [optionalIDs, setOptionalIDs] = useState<string[]>([]);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
  const originalStart = event.allDay
    ? event.startsOn!
    : localTimestamp(event.startsAt!, event.timezone!);
  const originalEnd = event.allDay
    ? (event.endsOn ?? originalStart)
    : event.endsAt
      ? localTimestamp(event.endsAt, event.timezone!)
      : originalStart;
  const start = targetDate
    ? targetDate + originalStart.slice(10)
    : originalStart;
  const dayOffset = Math.round(
    (Date.parse(`${originalEnd.slice(0, 10)}T12:00:00Z`) -
      Date.parse(`${originalStart.slice(0, 10)}T12:00:00Z`)) /
      86400000,
  );
  const end = targetDate
    ? shiftDate(targetDate, dayOffset) + originalEnd.slice(10)
    : originalEnd;
  async function preview(form: FormEvent<HTMLFormElement>) {
    form.preventDefault();
    const data = new FormData(form.currentTarget);
    const token = ++generation.current;
    setBusy(true);
    setError("");
    try {
      const change: ScheduleChange = {
        projection_id: event.projectionID!,
        occurrence_key: event.occurrenceKey,
        all_day: event.allDay,
      };
      if (event.recurrence)
        change.occurrence_scope = String(
          data.get("scope"),
        ) as ScheduleChange["occurrence_scope"];
      if (event.allDay) {
        change.starts_on = String(data.get("start"));
        change.ends_on = String(data.get("end"));
      } else {
        change.timezone = String(data.get("timezone"));
        change.starts_at = zonedTimestamp(
          `${data.get("start")}:00`,
          change.timezone,
        );
        change.ends_at = zonedTimestamp(
          `${data.get("end")}:00`,
          change.timezone,
        );
      }
      const result = await api.preview(change);
      if (token === generation.current) {
        setProposal(result);
        setOptionalIDs([]);
      }
    } catch (error) {
      if (token === generation.current)
        setError(
          error instanceof Error
            ? error.message.replaceAll("_", " ")
            : "Preview unavailable",
        );
    } finally {
      if (token === generation.current) setBusy(false);
    }
  }
  async function apply() {
    if (!proposal || busy) return;
    const token = ++generation.current;
    setBusy(true);
    setError("");
    try {
      await api.apply(proposal, optionalIDs, reason.trim());
      if (token === generation.current) onApplied();
    } catch (error) {
      if (token === generation.current) {
        setError(
          `${error instanceof Error ? error.message.replaceAll("_", " ") : "Schedule change failed"}. Refresh the preview before trying again.`,
        );
        setProposal(undefined);
      }
    } finally {
      if (token === generation.current) setBusy(false);
    }
  }
  const hardBlocked =
    !!proposal &&
    (proposal.conflicts.some(
      (conflict) => conflict.severity === "hard_block",
    ) ||
      proposal.blocked.length > 0 ||
      Date.parse(proposal.expiresAt) <= Date.now());
  return (
    <Dialog
      open
      title={`Reschedule ${event.title}`}
      onClose={onClose}
      variant="drawer"
    >
      {error ? (
        <Notice tone="danger" title="Schedule not changed">
          {error}
        </Notice>
      ) : null}
      {!proposal ? (
        <form
          className="calendar-form-grid"
          onSubmit={(event) => void preview(event)}
        >
          <label>
            Starts
            <TextInput
              name="start"
              type={event.allDay ? "date" : "datetime-local"}
              required
              defaultValue={start}
            />
          </label>
          <label>
            {event.allDay ? "Ends (first day not included)" : "Ends"}
            <TextInput
              name="end"
              type={event.allDay ? "date" : "datetime-local"}
              required
              defaultValue={end}
            />
          </label>
          {!event.allDay ? (
            <label>
              Original timezone
              <TextInput
                name="timezone"
                required
                defaultValue={event.timezone}
              />
            </label>
          ) : null}
          {event.recurrence ? (
            <label>
              Change applies to
              <Select name="scope" required defaultValue="">
                <option value="" disabled>
                  Choose recurrence scope
                </option>
                <option value="this_occurrence">This occurrence</option>
                <option value="this_and_future">
                  This and future occurrences
                </option>
                <option value="entire_series">Entire series</option>
              </Select>
            </label>
          ) : null}
          <p>
            Preview reloads current permissions, source versions, dependencies
            and conflicts. No dates change until you confirm.
          </p>
          <Button type="submit" disabled={busy}>
            {busy ? "Preparing preview…" : "Preview schedule change"}
          </Button>
        </form>
      ) : (
        <section
          aria-label="Schedule change preview"
          className="calendar-proposal"
        >
          <h2>Review schedule changes</h2>
          <p>Expires {new Date(proposal.expiresAt).toLocaleString()}.</p>
          <ul>
            {proposal.changes.map((change) => (
              <li key={change.id}>
                {change.required ? (
                  <strong>Required: {change.title}</strong>
                ) : (
                  <label>
                    <input
                      type="checkbox"
                      checked={optionalIDs.includes(change.id)}
                      onChange={(event) =>
                        setOptionalIDs((current) =>
                          event.target.checked
                            ? [...current, change.id]
                            : current.filter((id) => id !== change.id),
                        )
                      }
                    />{" "}
                    Optional: {change.title}
                  </label>
                )}
                <p>
                  {change.start ?? "Source date"}
                  {change.end ? ` → ${change.end}` : ""}
                </p>
              </li>
            ))}
          </ul>
          <h3>Conflicts and blocked work</h3>
          {proposal.conflicts.length || proposal.blocked.length ? (
            <ul>
              {proposal.conflicts.map((conflict, index) => (
                <li key={index}>
                  {conflict.severity.replaceAll("_", " ")}:{" "}
                  {conflict.reasonCode.replaceAll("_", " ")}
                </li>
              ))}
              {proposal.blocked.map((reason, index) => (
                <li key={`blocked-${index}`}>{reason.replaceAll("_", " ")}</li>
              ))}
            </ul>
          ) : (
            <p>No conflicts reported.</p>
          )}
          <h3>Capacity impact</h3>
          {proposal.capacity.length ? (
            <ul>
              {proposal.capacity.map((impact, index) => (
                <li key={index}>
                  {impact.technicianID}: {impact.delta} minute change;{" "}
                  {impact.overbooked} minutes overbooked
                </li>
              ))}
            </ul>
          ) : (
            <p>No capacity impact reported.</p>
          )}
          <h3>Health and notifications</h3>
          <ul>
            {proposal.health.map((impact, index) => (
              <li key={index}>
                {impact.state.replaceAll("_", " ")}:{" "}
                {impact.reasons
                  .map((reason) => reason.replaceAll("_", " "))
                  .join(", ")}
              </li>
            ))}
            {proposal.notifications.map((notification, index) => (
              <li key={`notice-${index}`}>
                {notification.reason.replaceAll("_", " ")} ·{" "}
                {notification.recipientID}
              </li>
            ))}
          </ul>
          <label>
            Reason
            <TextInput
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              required
            />
          </label>
          {hardBlocked ? (
            <p role="alert">
              This preview is blocked or expired. Resolve the conflict and
              request a new preview.
            </p>
          ) : null}
          <Button
            disabled={busy || hardBlocked || !reason.trim()}
            onClick={() => void apply()}
          >
            {busy ? "Applying…" : "Confirm schedule changes"}
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              setProposal(undefined);
              setReason("");
            }}
          >
            Edit request
          </Button>
        </section>
      )}
    </Dialog>
  );
}
