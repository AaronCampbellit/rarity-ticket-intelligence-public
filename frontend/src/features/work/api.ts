import { clientContextHeaders } from "../../api/clientContext";
import { csrfHeaders } from "../../api/browserSession";
import { classificationResponseError } from "../classification/api";

type WorkRecordWire = {
  ID: string;
  DisplayID: string;
  Type: string;
  Title: string;
  Description: string;
  Status: string;
  Priority: string;
  QueueID: string;
  PrimaryOwnerID: string;
  UpdatedAt: string;
  Version: number;
};

export type WorkRecord = {
  id: string;
  displayID: string;
  type: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  queueID: string;
  primaryOwnerID: string;
  updatedAt: string;
  version: number;
};

type WorkTaskWire = {
  id: string;
  title: string;
  status: string;
  owner_id?: string;
  estimate_minutes: number;
  parent: { type: string; id: string };
};

export type WorkTask = {
  id: string;
  title: string;
  status: string;
  ownerID?: string;
  estimateMinutes: number;
  parent: { type: string; id: string };
};

export type CreateWorkRecordInput = {
  displayID: string;
  type: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  tagIDs: string[];
};

type TicketTimeCaptureWire = {
  id: string;
  msp_id: string;
  client_id: string;
  work_record_id: string;
  technician_id: string;
  state: "running" | "stopped" | "consumed" | "discarded";
  started_at: string;
  stopped_at?: string;
  duration_seconds?: number;
  version: number;
  created_at: string;
  updated_at: string;
};

export type TicketTimeCapture = {
  id: string;
  workRecordID: string;
  technicianID: string;
  state: "running" | "stopped" | "consumed" | "discarded";
  startedAt: string;
  stoppedAt?: string;
  durationSeconds: number;
  version: number;
};

type LaborRoleWire = {
  id: string;
  key: string;
  version: number;
  current_version: {
    id: string;
    name: string;
    internal_cost_minor: number;
    bill_rate_minor: number;
    currency: string;
  };
};

export type LaborRole = {
  id: string;
  key: string;
  version: number;
  versionID: string;
  name: string;
  internalCostMinor: number;
  billRateMinor: number;
  currency: string;
};

type SearchResultWire = {
  ID: string;
  ObjectType: string;
  ClientID: string;
  Title: string;
  Snippet: string;
};

export type SearchResult = {
  id: string;
  objectType: string;
  clientID: string;
  title: string;
  snippet: string;
};

export type WorkViewQuery = {
  text?: string;
  status?: string;
  priority?: string;
  ownership?: "all" | "assigned" | "unassigned";
};

export type SavedWorkView = {
  id: string;
  name: string;
  query: WorkViewQuery;
  audience: { type: "private" | "msp"; id?: string };
  version: number;
};

function mapWorkRecord(record: WorkRecordWire): WorkRecord {
  return {
    id: record.ID,
    displayID: record.DisplayID,
    type: record.Type,
    title: record.Title,
    description: record.Description || "",
    status: record.Status,
    priority: record.Priority,
    queueID: record.QueueID,
    primaryOwnerID: record.PrimaryOwnerID,
    updatedAt: record.UpdatedAt,
    version: record.Version,
  };
}

function mapTicketTimeCapture(
  capture: TicketTimeCaptureWire,
): TicketTimeCapture {
  return {
    id: capture.id,
    workRecordID: capture.work_record_id,
    technicianID: capture.technician_id,
    state: capture.state,
    startedAt: capture.started_at,
    stoppedAt: capture.stopped_at,
    durationSeconds: capture.duration_seconds ?? 0,
    version: capture.version,
  };
}

export type WorkRecordCursor = { updatedAt: string; id: string };

export async function listWorkRecords(
  clientID: string,
  signal?: AbortSignal,
  options: { filters?: WorkViewQuery; before?: WorkRecordCursor } = {},
): Promise<WorkRecord[]> {
  const query = new URLSearchParams({ limit: "100" });
  for (const [key, value] of Object.entries(options.filters ?? {})) {
    if (value && value !== "all") query.set(key, value);
  }
  if (options.before) {
    query.set("before_updated_at", options.before.updatedAt);
    query.set("before_id", options.before.id);
  }
  const response = await fetch(`/api/v1/work-records?${query}`, {
    credentials: "same-origin",
    headers: clientContextHeaders(clientID),
    signal,
  });
  if (!response.ok) throw new Error(`work_records_${response.status}`);
  const records = (await response.json()) as WorkRecordWire[];
  return records.map(mapWorkRecord);
}

export async function getWorkRecord(
  clientID: string,
  recordID: string,
  signal?: AbortSignal,
): Promise<WorkRecord> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}`,
    {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    },
  );
  if (!response.ok) throw new Error(`work_record_${response.status}`);
  return mapWorkRecord((await response.json()) as WorkRecordWire);
}

export async function getWorkTask(
  clientID: string,
  taskID: string,
  signal?: AbortSignal,
): Promise<WorkTask> {
  const response = await fetch(`/api/v1/tasks/${encodeURIComponent(taskID)}`, {
    credentials: "same-origin",
    headers: clientContextHeaders(clientID),
    signal,
  });
  if (!response.ok) throw new Error(`task_${response.status}`);
  const task = (await response.json()) as WorkTaskWire;
  return {
    id: task.id,
    title: task.title,
    status: task.status,
    ownerID: task.owner_id,
    estimateMinutes: task.estimate_minutes,
    parent: task.parent,
  };
}

export async function currentPrincipalID(): Promise<string> {
  const response = await fetch("/api/v1/me", { credentials: "same-origin" });
  if (!response.ok) throw new Error(`principal_${response.status}`);
  return ((await response.json()) as { id: string }).id;
}

export async function searchRarity(
  clientID: string,
  query: string,
  signal?: AbortSignal,
): Promise<SearchResult[]> {
  const parameters = new URLSearchParams({ q: query, limit: "25" });
  const response = await fetch(`/api/v1/search?${parameters}`, {
    credentials: "same-origin",
    headers: clientContextHeaders(clientID),
    signal,
  });
  if (!response.ok) throw new Error(`search_${response.status}`);
  return ((await response.json()) as SearchResultWire[]).map((result) => ({
    id: result.ID,
    objectType: result.ObjectType,
    clientID: result.ClientID,
    title: result.Title,
    snippet: result.Snippet || "",
  }));
}

export async function createWorkRecord(
  clientID: string,
  work: CreateWorkRecordInput,
): Promise<WorkRecord> {
  const response = await fetch("/api/v1/work-records", {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({
      display_id: work.displayID,
      type: work.type,
      title: work.title,
      description: work.description,
      status: work.status,
      priority: work.priority,
      tag_ids: work.tagIDs,
    }),
  });
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as {
      error?: { code?: string };
    };
    throw new Error(body.error?.code ?? `work_create_${response.status}`);
  }
  return mapWorkRecord((await response.json()) as WorkRecordWire);
}

export async function listSavedWorkViews(
  clientID: string,
  signal?: AbortSignal,
): Promise<SavedWorkView[]> {
  const response = await fetch("/api/v1/views?kind=saved_search", {
    credentials: "same-origin",
    headers: clientContextHeaders(clientID),
    signal,
  });
  if (!response.ok) throw new Error(`views_${response.status}`);
  const found = (await response.json()) as SavedWorkView[];
  return found.filter(
    (view) =>
      typeof view.id === "string" &&
      typeof view.name === "string" &&
      typeof view.query === "object" &&
      (view.audience?.type === "private" || view.audience?.type === "msp"),
  );
}

export async function saveWorkView(
  clientID: string,
  name: string,
  query: WorkViewQuery,
  shared: boolean,
): Promise<SavedWorkView> {
  const response = await fetch("/api/v1/views", {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({
      kind: "saved_search",
      name,
      query,
      audience: { type: shared ? "msp" : "private" },
    }),
  });
  if (!response.ok) throw new Error(`view_save_${response.status}`);
  return (await response.json()) as SavedWorkView;
}

async function mutateRecord(
  clientID: string,
  recordID: string,
  action: string,
  body: Record<string, unknown>,
): Promise<WorkRecord> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/${action}`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify(body),
    },
  );
  if (!response.ok)
    await classificationResponseError(response, `work_${action}_failed`);
  return mapWorkRecord((await response.json()) as WorkRecordWire);
}

async function mutationErrorCode(
  response: Response,
  fallback: string,
): Promise<never> {
  return classificationResponseError(response, fallback);
}

export const claimWorkRecord = (
  clientID: string,
  record: WorkRecord,
  technicianID: string,
) =>
  mutateRecord(clientID, record.id, "assign", {
    expected_version: record.version,
    owner_id: technicianID,
    reason: "Claimed from technician worklist",
  });

export const transitionWorkRecord = (
  clientID: string,
  record: WorkRecord,
  toStatus: string,
  reason: string,
) =>
  mutateRecord(clientID, record.id, "transition", {
    expected_version: record.version,
    to_status: toStatus,
    reason,
  });

export const changeWorkPriority = (
  clientID: string,
  record: WorkRecord,
  priority: string,
  reason: string,
) =>
  mutateRecord(clientID, record.id, "priority", {
    expected_version: record.version,
    priority,
    reason,
  });

export async function createWorkComment(
  clientID: string,
  recordID: string,
  visibility: "client" | "internal",
  body: string,
  time?: {
    capture: TicketTimeCapture;
    laborRoleID: string;
    billable: boolean;
    tagIDs: string[];
  },
): Promise<void> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/comments`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        visibility,
        body,
        ...(time
          ? {
              time_capture: {
                id: time.capture.id,
                expected_version: time.capture.version,
                labor_role_id: time.laborRoleID,
                billable: time.billable,
                tag_ids: time.tagIDs,
              },
            }
          : {}),
      }),
    },
  );
  if (!response.ok)
    await classificationResponseError(response, "comment_failed");
}

export async function createWorkTimeEntry(
  clientID: string,
  recordID: string,
  technicianID: string,
  minutes: number,
  billable: boolean,
  note: string,
  tagIDs: string[],
): Promise<void> {
  const endedAt = new Date();
  const startedAt = new Date(endedAt.getTime() - minutes * 60_000);
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/time-entries`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        technician_id: technicianID,
        started_at: startedAt.toISOString(),
        ended_at: endedAt.toISOString(),
        billable,
        note,
        tag_ids: tagIDs,
      }),
    },
  );
  if (!response.ok) await mutationErrorCode(response, "time_entry_failed");
}

export async function listTicketTimers(
  clientID: string,
  recordID: string,
  signal?: AbortSignal,
): Promise<TicketTimeCapture[]> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/timers`,
    {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    },
  );
  if (!response.ok) throw new Error(`ticket_timers_${response.status}`);
  return ((await response.json()) as TicketTimeCaptureWire[]).map(
    mapTicketTimeCapture,
  );
}

export async function listLaborRoles(
  clientID: string,
  signal?: AbortSignal,
): Promise<LaborRole[]> {
  const response = await fetch("/api/v1/labor-roles", {
    credentials: "same-origin",
    headers: clientContextHeaders(clientID),
    signal,
  });
  if (!response.ok) throw new Error(`labor_roles_${response.status}`);
  return ((await response.json()) as LaborRoleWire[]).map((role) => ({
    id: role.id,
    key: role.key,
    version: role.version,
    versionID: role.current_version.id,
    name: role.current_version.name,
    internalCostMinor: role.current_version.internal_cost_minor,
    billRateMinor: role.current_version.bill_rate_minor,
    currency: role.current_version.currency,
  }));
}

export async function createWorkTimeEntryFromCapture(
  clientID: string,
  recordID: string,
  capture: TicketTimeCapture,
  laborRoleID: string,
  billable: boolean,
  note: string,
  tagIDs: string[],
): Promise<void> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/time-entries`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        time_capture: {
          id: capture.id,
          expected_version: capture.version,
          labor_role_id: laborRoleID,
          billable,
        },
        note,
        tag_ids: tagIDs,
      }),
    },
  );
  if (!response.ok)
    await mutationErrorCode(response, "time_capture_entry_failed");
}

export async function startTicketTimer(
  clientID: string,
  recordID: string,
  idempotencyKey: string,
): Promise<TicketTimeCapture> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/timers`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({ idempotency_key: idempotencyKey }),
    },
  );
  if (!response.ok) throw new Error(`ticket_timer_start_${response.status}`);
  return mapTicketTimeCapture((await response.json()) as TicketTimeCaptureWire);
}

export async function stopTicketTimer(
  clientID: string,
  capture: TicketTimeCapture,
): Promise<TicketTimeCapture> {
  const response = await fetch(
    `/api/v1/timers/${encodeURIComponent(capture.id)}:stop`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({ expected_version: capture.version }),
    },
  );
  if (!response.ok) throw new Error(`ticket_timer_stop_${response.status}`);
  return mapTicketTimeCapture((await response.json()) as TicketTimeCaptureWire);
}

export async function uploadWorkAttachment(
  clientID: string,
  recordID: string,
  file: File,
): Promise<void> {
  const response = await fetch(
    `/api/v1/work-records/${encodeURIComponent(recordID)}/attachments`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": file.type || "application/octet-stream",
        "X-Rarity-Filename": file.name,
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: file,
    },
  );
  if (!response.ok) throw new Error(`attachment_${response.status}`);
}
