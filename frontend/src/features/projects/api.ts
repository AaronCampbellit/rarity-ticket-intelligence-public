import { clientContextHeaders } from "../../api/clientContext";
import { csrfHeaders } from "../../api/browserSession";
import type { ConversionPreviewModel } from "./types";

type APIErrorBody = { error?: { code?: string; message?: string } };

async function requestJSON<T>(
  input: RequestInfo | URL,
  init: RequestInit,
): Promise<T> {
  const response = await fetch(input, init);
  if (response.ok) return (await response.json()) as T;
  const body = (await response.json().catch(() => ({}))) as APIErrorBody;
  throw new Error(body.error?.code ?? "request_failed");
}

export async function previewOpportunityConversion(
  clientID: string,
  opportunityID: string,
  request: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<ConversionPreviewModel> {
  const preview = await requestJSON<{
    hash: string;
    opportunity_id: string;
    proposal_version_id: string;
    client: { action: "match" | "create"; client_id?: string; name: string };
    project_display_id: string;
    project_name: string;
    phases: Array<{
      position: number;
      name: string;
      proposal_line_ids: string[];
      planned_minutes: number;
      budget: { minor: number; currency: string };
    }>;
    tasks: Array<{ id: string; version: number }>;
    original_budget: { minor: number; currency: string };
    planned_minutes: number;
  }>(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/conversion-preview`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify(request),
      signal,
    },
  );
  return {
    hash: preview.hash,
    opportunityID: preview.opportunity_id,
    proposalVersionID: preview.proposal_version_id,
    client: {
      action: preview.client.action,
      clientID: preview.client.client_id,
      name: preview.client.name,
    },
    projectDisplayID: preview.project_display_id,
    projectName: preview.project_name,
    currency: preview.original_budget.currency,
    originalBudgetMinor: preview.original_budget.minor,
    plannedMinutes: preview.planned_minutes,
    phases: preview.phases.map((phase) => ({
      position: phase.position,
      name: phase.name,
      proposalLineIDs: phase.proposal_line_ids,
      plannedMinutes: phase.planned_minutes,
      budgetMinor: phase.budget.minor,
    })),
    tasks: preview.tasks.map((task) => ({
      id: task.id,
      title: `Opportunity task ${task.id.slice(0, 8)}`,
      version: task.version,
      completed: false,
    })),
  };
}

export async function convertOpportunity(
  clientID: string,
  opportunityID: string,
  request: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<{ projectID: string; clientID: string; conversionID: string }> {
  const result = await requestJSON<{
    project_id: string;
    client_id: string;
    conversion_id: string;
  }>(`/api/v1/opportunities/${encodeURIComponent(opportunityID)}/convert`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify(request),
    signal,
  });
  return {
    projectID: result.project_id,
    clientID: result.client_id,
    conversionID: result.conversion_id,
  };
}

export async function overrideChangeOrderApproval(
  clientID: string,
  versionID: string,
  expectedVersion: number,
  reason: string,
  signal?: AbortSignal,
) {
  return requestJSON(
    `/api/v1/change-order-versions/${encodeURIComponent(versionID)}/override-approval`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: expectedVersion,
        reason,
      }),
      signal,
    },
  );
}

export type ProjectSummaryResponse = {
  id: string;
  display_id: string;
  name: string;
  lifecycle_state: string;
  planned_start?: string;
  planned_end?: string;
  version: number;
};

export type FinancialSummaryResponse = {
  original_budget: { minor: number; currency: string };
  current_budget: { minor: number; currency: string };
  planned_labor: { minor: number; currency: string };
  actual_labor: { minor: number; currency: string };
  cost_actuals: { minor: number; currency: string };
  committed_cost: { minor: number; currency: string };
  billable_work: { minor: number; currency: string };
  profit: { minor: number; currency: string };
  projected_profit: { minor: number; currency: string };
  margin_basis_points: number;
  actual_labor_complete: boolean;
  profit_available: boolean;
};

export type ProjectWorkspaceResponse = {
  id: string;
  display_id: string;
  name: string;
  client_name: string;
  lifecycle_state: string;
  planned_start?: string;
  planned_end?: string;
  original_proposal_version: number;
  original_baseline: {
    currency: string;
    revenue_minor: number;
    cost_minor: number;
    planned_minutes: number;
  };
  current_baseline: {
    currency: string;
    revenue_minor: number;
    cost_minor: number;
    planned_minutes: number;
  };
  phases: Array<{
    id: string;
    position: number;
    name: string;
    state: string;
    owner_id?: string;
    participating_teams: string[];
    planned_start?: string;
    planned_end?: string;
    planned_minutes: number;
    actual_minutes: number;
    budget: { minor: number; currency: string };
    deliverables: string[];
    completion_criteria: string[];
    financials?: FinancialSummaryResponse;
    tasks: Array<{
      id: string;
      title: string;
      status: string;
      owner_id?: string;
      owner_name?: string;
      subtasks: number;
      estimate_minutes: number;
      actual_minutes: number;
      version: number;
    }>;
    version: number;
  }>;
  project_tasks: Array<{
    id: string;
    title: string;
    status: string;
    owner_id?: string;
    owner_name?: string;
    subtasks: number;
    estimate_minutes: number;
    actual_minutes: number;
    version: number;
  }>;
  resource_plans: Array<{
    id: string;
    phase_id?: string;
    resource_type: string;
    resource_name: string;
    starts_on: string;
    ends_on: string;
    planned_minutes: number;
    version: number;
  }>;
  cost_actuals: Array<{
    id: string;
    phase_id?: string;
    cost_type: string;
    description: string;
    amount: { minor: number; currency: string };
    committed: boolean;
    incurred_at: string;
    version: number;
  }>;
  financials?: FinancialSummaryResponse;
  capacity: Array<{
    id: string;
    name: string;
    available_minutes: number;
    scheduled_minutes: number;
    actual_minutes: number;
    remaining_minutes: number;
    overbooked_minutes: number;
  }>;
  change_orders: Array<{
    order: {
      id: string;
      display_id: string;
      state:
        "draft" | "issued" | "approved" | "rejected" | "applied" | "cancelled";
      version: number;
    };
    current_version?: {
      id: string;
      version: number;
      description: string;
      currency: string;
      revenue_delta_minor: number;
      cost_delta_minor: number;
      labor_delta_minutes: number;
    };
    decision?: {
      id: string;
      decision: "approved" | "rejected";
      override: boolean;
      reason?: string;
      decided_by: string;
      decided_at: string;
    };
  }>;
  financials_visible: boolean;
  version: number;
};

async function json<T>(response: Response): Promise<T> {
  if (!response.ok) throw new Error(`project_request_${response.status}`);
  return (await response.json()) as T;
}

export async function listProjects(clientID: string, signal?: AbortSignal) {
  return json<ProjectSummaryResponse[]>(
    await fetch("/api/v1/projects?limit=100", {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    }),
  );
}

export async function getProject(
  clientID: string,
  projectID: string,
  signal?: AbortSignal,
) {
  return json<ProjectWorkspaceResponse>(
    await fetch(`/api/v1/projects/${encodeURIComponent(projectID)}`, {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    }),
  );
}

export type ProjectTaskDetail = {
  id: string;
  title: string;
  status: string;
  owner_id?: string;
  estimate_minutes: number;
  parent: { type: string; id: string };
};

export async function getProjectTask(
  clientID: string,
  taskID: string,
  signal?: AbortSignal,
) {
  return json<ProjectTaskDetail>(
    await fetch(`/api/v1/tasks/${encodeURIComponent(taskID)}`, {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    }),
  );
}

export async function createChangeOrder(
  clientID: string,
  projectID: string,
  displayID: string,
) {
  return requestJSON<{
    id: string;
    display_id: string;
    state: "draft";
    version: number;
  }>(`/api/v1/projects/${encodeURIComponent(projectID)}/change-orders`, {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({ display_id: displayID }),
  });
}

export async function createProjectTask(
  clientID: string,
  parent: { type: "project" | "phase"; id: string },
  request: {
    title: string;
    parentTaskID?: string;
    ownerID?: string;
    estimateMinutes: number;
    tagIDs: string[];
  },
) {
  return requestJSON(
    `/api/v1/${parent.type === "project" ? "projects" : "phases"}/${encodeURIComponent(parent.id)}/tasks`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        title: request.title,
        ...(request.parentTaskID
          ? { parent_task_id: request.parentTaskID }
          : {}),
        ...(request.ownerID ? { owner_id: request.ownerID } : {}),
        estimate_minutes: request.estimateMinutes,
        tag_ids: request.tagIDs,
      }),
    },
  );
}

export async function issueChangeOrderVersion(
  clientID: string,
  changeOrderID: string,
  request: {
    expectedVersion: number;
    description: string;
    currency: string;
    revenueDeltaMinor: number;
    costDeltaMinor: number;
    laborDeltaMinutes: number;
  },
) {
  return requestJSON(
    `/api/v1/change-orders/${encodeURIComponent(changeOrderID)}/versions`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: request.expectedVersion,
        description: request.description,
        currency: request.currency.toUpperCase(),
        revenue_delta_minor: request.revenueDeltaMinor,
        cost_delta_minor: request.costDeltaMinor,
        labor_delta_minutes: request.laborDeltaMinutes,
      }),
    },
  );
}

export async function createTaskTimeEntry(
  clientID: string,
  taskID: string,
  request: {
    technicianID: string;
    minutes: number;
    billable: boolean;
    note: string;
    tagIDs: string[];
  },
) {
  const endedAt = new Date();
  const startedAt = new Date(endedAt.getTime() - request.minutes * 60_000);
  return requestJSON(
    `/api/v1/tasks/${encodeURIComponent(taskID)}/time-entries`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        technician_id: request.technicianID,
        started_at: startedAt.toISOString(),
        ended_at: endedAt.toISOString(),
        billable: request.billable,
        note: request.note,
        tag_ids: request.tagIDs,
      }),
    },
  );
}

export async function createCostActual(
  clientID: string,
  projectID: string,
  request: {
    phaseID?: string;
    costType: string;
    description: string;
    amountMinor: number;
    currency: string;
    committed: boolean;
    incurredAt: string;
  },
) {
  return requestJSON(
    `/api/v1/projects/${encodeURIComponent(projectID)}/cost-actuals`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        ...(request.phaseID ? { phase_id: request.phaseID } : {}),
        cost_type: request.costType,
        description: request.description,
        amount: {
          minor: request.amountMinor,
          currency: request.currency.toUpperCase(),
        },
        committed: request.committed,
        incurred_at: new Date(`${request.incurredAt}T12:00:00Z`).toISOString(),
      }),
    },
  );
}

export async function recognizeBillableWork(
  clientID: string,
  projectID: string,
  request: {
    phaseID?: string;
    description: string;
    amountMinor: number;
    currency: string;
    recognizedAt: string;
  },
) {
  return requestJSON(
    `/api/v1/projects/${encodeURIComponent(projectID)}/billable-work`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        ...(request.phaseID ? { phase_id: request.phaseID } : {}),
        description: request.description,
        amount: {
          minor: request.amountMinor,
          currency: request.currency.toUpperCase(),
        },
        recognized_at: new Date(
          `${request.recognizedAt}T12:00:00Z`,
        ).toISOString(),
      }),
    },
  );
}

async function changeOrderAction(
  clientID: string,
  versionID: string,
  action: "approve" | "override-approval" | "apply",
  expectedVersion: number,
  reason = "",
) {
  const response = await fetch(
    `/api/v1/change-order-versions/${encodeURIComponent(versionID)}/${action}`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: expectedVersion,
        ...(reason ? { reason } : {}),
      }),
    },
  );
  if (!response.ok)
    throw new Error(`change_order_${action}_${response.status}`);
}

export const approveChangeOrder = (
  clientID: string,
  versionID: string,
  expectedVersion: number,
) => changeOrderAction(clientID, versionID, "approve", expectedVersion);

export const overrideChangeOrder = (
  clientID: string,
  versionID: string,
  expectedVersion: number,
  reason: string,
) =>
  changeOrderAction(
    clientID,
    versionID,
    "override-approval",
    expectedVersion,
    reason,
  );

export const applyChangeOrder = (
  clientID: string,
  versionID: string,
  expectedVersion: number,
) => changeOrderAction(clientID, versionID, "apply", expectedVersion);

export async function createResourcePlan(
  clientID: string,
  projectID: string,
  request: {
    phaseID: string;
    resourceType: "role" | "team";
    resourceID: string;
    startsOn: string;
    endsOn: string;
    plannedMinutes: number;
  },
) {
  return requestJSON(
    `/api/v1/projects/${encodeURIComponent(projectID)}/resource-plans`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        phase_id: request.phaseID,
        [request.resourceType === "role" ? "role_id" : "team_id"]:
          request.resourceID,
        starts_on: new Date(`${request.startsOn}T00:00:00Z`).toISOString(),
        ends_on: new Date(`${request.endsOn}T00:00:00Z`).toISOString(),
        planned_minutes: request.plannedMinutes,
      }),
    },
  );
}

export async function updatePhase(
  clientID: string,
  phase: ProjectWorkspaceResponse["phases"][number],
  request: {
    name: string;
    ownerID: string;
    participatingTeams: string[];
    plannedStart: string;
    plannedEnd: string;
    plannedMinutes: number;
    budgetMinor: number;
    currency: string;
    deliverables: string[];
    completionCriteria: string[];
  },
) {
  return requestJSON(`/api/v1/phases/${encodeURIComponent(phase.id)}`, {
    method: "PATCH",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "If-Match": `"${phase.version}"`,
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({
      expected_version: phase.version,
      name: request.name,
      owner_id: request.ownerID,
      participating_teams: request.participatingTeams,
      planned_start: request.plannedStart
        ? new Date(`${request.plannedStart}T00:00:00Z`).toISOString()
        : undefined,
      planned_end: request.plannedEnd
        ? new Date(`${request.plannedEnd}T00:00:00Z`).toISOString()
        : undefined,
      planned_minutes: request.plannedMinutes,
      budget: {
        minor: request.budgetMinor,
        currency: request.currency.toUpperCase(),
      },
      deliverables: request.deliverables,
      completion_criteria: request.completionCriteria,
    }),
  });
}
