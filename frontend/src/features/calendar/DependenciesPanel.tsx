import { useEffect, useState } from "react";
import { Button, Notice, Select, TextInput } from "../../design-system";
import { array, number, record, string } from "./api";
import { calendarError, calendarRequest } from "./requests";
import type { CalendarEvent } from "./types";

type Edge = {
  id: string;
  predecessor: string;
  successor: string;
  type: string;
  lag: number;
  version: number;
};
function parseEdge(value: unknown): Edge {
  const row = record(value);
  return {
    id: string(row.id),
    predecessor: string(row.predecessor_id),
    successor: string(row.successor_id),
    type: string(row.type),
    lag: number(row.lead_lag_minutes),
    version: number(row.version),
  };
}
export function DependenciesPanel({
  event,
  events,
  canSchedule,
}: {
  event: CalendarEvent;
  events: CalendarEvent[];
  canSchedule: boolean;
}) {
  const [edges, setEdges] = useState<Edge[]>([]),
    [error, setError] = useState(""),
    [ready, setReady] = useState(false),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0);
  const [successor, setSuccessor] = useState(""),
    [type, setType] = useState("finish_to_start"),
    [lag, setLag] = useState(0);
  const [preview, setPreview] = useState<{
    required: number;
    optional: number;
    blocked: number;
  }>();
  const candidates = [
    ...new Map(
      events
        .filter(
          (item) =>
            item.privacy === "full" &&
            item.projectionID &&
            item.projectionID !== event.projectionID &&
            item.source?.client_id === event.source?.client_id &&
            item.schedulingMode !== "informational",
        )
        .map((item) => [item.projectionID, item]),
    ).values(),
  ];
  const title = (id: string) =>
    events.find((item) => item.privacy === "full" && item.projectionID === id)
      ?.title ?? "Record outside this calendar window";
  useEffect(() => {
    const controller = new AbortController();
    setReady(false);
    setError("");
    setEdges([]);
    void calendarRequest(
      `calendar/dependencies?projection_id=${encodeURIComponent(event.projectionID!)}`,
      { signal: controller.signal },
    )
      .then((value) => {
        if (!controller.signal.aborted) {
          setEdges(array(value).map(parseEdge));
          setReady(true);
        }
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(calendarError(error));
      });
    return () => controller.abort();
  }, [event.projectionID, revision]);
  useEffect(() => setPreview(undefined), [successor, type, lag]);
  const body = {
    predecessor_id: event.projectionID,
    successor_id: successor,
    type,
    lead_lag_minutes: lag,
  };
  async function perform(action: "preview" | "create" | "delete", edge?: Edge) {
    setBusy(true);
    setError("");
    try {
      if (action === "delete")
        await calendarRequest(
          `calendar/dependencies/${encodeURIComponent(edge!.id)}?expected_version=${edge!.version}`,
          { method: "DELETE" },
        );
      else {
        const result = await calendarRequest(
          `calendar/dependencies${action === "preview" ? "/preview" : ""}`,
          { method: "POST", body },
        );
        if (action === "preview") {
          const impact = record(record(result).impact);
          setPreview({
            required: array(impact.required_moves).length,
            optional: array(impact.optional_moves).length,
            blocked: array(impact.blocked_sources).length,
          });
        }
      }
      if (action !== "preview") {
        setPreview(undefined);
        setRevision((value) => value + 1);
      }
    } catch (error) {
      setError(calendarError(error));
      setPreview(undefined);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section aria-label="Dependencies">
      <h3>Dependencies</h3>
      {error ? (
        <Notice tone="danger" title="Dependencies unavailable">
          {error}{" "}
          <Button onClick={() => setRevision((value) => value + 1)}>
            Reload dependencies
          </Button>
        </Notice>
      ) : null}
      {!ready && !error ? <p role="status">Loading dependencies…</p> : null}
      {ready && !edges.length ? <p>No visible dependencies.</p> : null}
      <ul>
        {edges.map((edge) => (
          <li key={edge.id}>
            {title(edge.predecessor)} → {title(edge.successor)} ·{" "}
            {edge.type.replaceAll("_", " ")} · {edge.lag} minutes{" "}
            {canSchedule ? (
              <Button
                disabled={busy}
                onClick={() => void perform("delete", edge)}
              >
                Remove dependency
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
      {canSchedule && candidates.length ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void perform("preview");
          }}
        >
          <p>
            Add a successor from the loaded calendar events. Dependency changes
            do not move records; use a scheduling preview to review date
            changes.
          </p>
          <label>
            Successor
            <Select
              required
              value={successor}
              onChange={(e) => setSuccessor(e.target.value)}
            >
              <option value="">Choose record</option>
              {candidates.map((item) => (
                <option key={item.projectionID} value={item.projectionID}>
                  {item.title}
                </option>
              ))}
            </Select>
          </label>
          <label>
            Dependency type
            <Select value={type} onChange={(e) => setType(e.target.value)}>
              {["finish_to_start", "start_to_start", "finish_to_finish"].map(
                (value) => (
                  <option key={value} value={value}>
                    {value.replaceAll("_", " ")}
                  </option>
                ),
              )}
            </Select>
          </label>
          <label>
            Lead or lag in minutes
            <TextInput
              type="number"
              min={-525600}
              max={525600}
              value={lag}
              onChange={(e) => setLag(Number(e.target.value))}
            />
          </label>
          <Button type="submit" disabled={busy}>
            Preview dependency
          </Button>
          {preview ? (
            <div>
              <p>
                {preview.required} required moves · {preview.optional} optional
                moves · {preview.blocked} blocked sources
              </p>
              <Button
                disabled={busy || preview.blocked > 0}
                onClick={() => void perform("create")}
              >
                Confirm dependency
              </Button>
            </div>
          ) : null}
        </form>
      ) : null}
    </section>
  );
}
