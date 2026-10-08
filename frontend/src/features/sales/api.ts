import { clientContextHeaders } from "../../api/clientContext";
import { csrfHeaders } from "../../api/browserSession";

export class APIError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export type MoneyResponse = { minor: number; currency: string };

export type OpportunityResponse = {
  id: string;
  client_id?: string;
  prospect_id?: string;
  pipeline_id: string;
  stage_id: string;
  display_id: string;
  name: string;
  amount: MoneyResponse;
  fields: Record<string, string>;
  custom_fields?: Record<string, string>;
  team_id?: string;
  contact_ids?: string[];
  proposal_issued: boolean;
  approval_granted: boolean;
  version: number;
  updated_at: string;
};

export type PipelineStageResponse = {
  id: string;
  pipeline_id: string;
  key: string;
  name: string;
  position: number;
  probability: number;
  forecast_category: string;
  required_fields: string[];
  allowed_next_stage_ids: string[];
  requires_proposal: boolean;
  requires_approval: boolean;
};

export type PipelineResponse = {
  id: string;
  key: string;
  name: string;
  stages: PipelineStageResponse[];
};

export type ProspectResponse = {
  id: string;
  display_id: string;
  name: string;
  email?: string;
  phone?: string;
  version: number;
  created_at: string;
};

export type ForecastBucketResponse = {
  pipeline_id: string;
  stage_id: string;
  stage_name: string;
  forecast_category: string;
  probability: number;
  opportunity_count: number;
  amount: MoneyResponse;
  weighted_amount: MoneyResponse;
};

export type OpportunityActivityResponse = {
  id: string;
  opportunity_id: string;
  kind: string;
  summary: string;
  details?: string;
  occurred_at: string;
};

export type OpportunityTaskResponse = {
  id: string;
  title: string;
  status: string;
  position: number;
  parent_task_id?: string;
  version: number;
};

export type OpportunityAttachmentResponse = {
  id: string;
  opportunity_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  version: number;
  created_at: string;
};

export type ProposalResponse = {
  id: string;
  client_id?: string;
  prospect_id?: string;
  opportunity_id: string;
  display_id: string;
  current_version: number;
  current_version_id?: string;
  state: "draft" | "issued" | "accepted";
  version: number;
  updated_at: string;
};

export type ProposalVersionResponse = {
  id: string;
  proposal_id: string;
  version: number;
  state: string;
  currency: string;
  lines: Array<{
    id: string;
    type: string;
    description: string;
    quantity: number;
    unit_price: MoneyResponse;
    unit_cost: MoneyResponse;
    discount: MoneyResponse;
    tax_treatment: string;
    tax: MoneyResponse;
    recurrence?: string;
    planned_minutes: number;
  }>;
  subtotal: MoneyResponse;
  tax_total: MoneyResponse;
  total: MoneyResponse;
  cost: MoneyResponse;
  margin: MoneyResponse;
  requires_internal_approval: boolean;
};

export type InternalApprovalResponse = {
  id: string;
  proposal_version_id: string;
  state: "pending" | "approved" | "rejected";
  approver_id?: string;
  reason?: string;
  decision_at?: string;
  version: number;
};

type APIErrorBody = {
  error?: { code?: string; message?: string };
};

async function parseResponse<T>(response: Response): Promise<T> {
  if (response.ok) {
    return (await response.json()) as T;
  }
  const body = (await response.json().catch(() => ({}))) as APIErrorBody;
  throw new APIError(
    body.error?.code ?? "request_failed",
    body.error?.message ?? "The request could not be completed.",
    response.status,
  );
}

export async function listOpportunities(
  clientID: string,
  signal?: AbortSignal,
): Promise<OpportunityResponse[]> {
  const response = await fetch("/api/v1/opportunities?limit=100", {
    headers: clientContextHeaders(clientID),
    credentials: "same-origin",
    signal,
  });
  return parseResponse<OpportunityResponse[]>(response);
}

export async function listOpportunityForecast(
  clientID: string,
  signal?: AbortSignal,
): Promise<ForecastBucketResponse[]> {
  const response = await fetch("/api/v1/opportunity-forecast", {
    headers: clientContextHeaders(clientID),
    credentials: "same-origin",
    signal,
  });
  return parseResponse<ForecastBucketResponse[]>(response);
}

export async function listPipelines(
  signal?: AbortSignal,
): Promise<PipelineResponse[]> {
  const response = await fetch("/api/v1/pipelines", {
    credentials: "same-origin",
    signal,
  });
  return parseResponse<PipelineResponse[]>(response);
}

export async function listProspects(
  signal?: AbortSignal,
): Promise<ProspectResponse[]> {
  const response = await fetch("/api/v1/prospects?limit=500", {
    credentials: "same-origin",
    signal,
  });
  return parseResponse<ProspectResponse[]>(response);
}

export async function createProspect(request: {
  displayID: string;
  name: string;
  email: string;
  phone: string;
}): Promise<ProspectResponse> {
  const response = await fetch("/api/v1/prospects", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify({
      display_id: request.displayID,
      name: request.name,
      email: request.email,
      phone: request.phone,
    }),
  });
  return parseResponse<ProspectResponse>(response);
}

export async function createProspectOpportunity(request: {
  prospectID: string;
  pipelineID: string;
  stageID: string;
  displayID: string;
  name: string;
  description: string;
  amountMinor: number;
  currency: string;
  ownerID: string;
  expectedCloseOn: string;
}): Promise<OpportunityResponse> {
  const response = await fetch("/api/v1/opportunities", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify({
      prospect_id: request.prospectID,
      pipeline_id: request.pipelineID,
      stage_id: request.stageID,
      display_id: request.displayID,
      name: request.name,
      description: request.description,
      amount_minor: request.amountMinor,
      currency: request.currency,
      owner_id: request.ownerID,
      expected_close_on: request.expectedCloseOn,
    }),
  });
  return parseResponse<OpportunityResponse>(response);
}

export async function createOpportunity(
  clientID: string,
  request: {
    pipelineID: string;
    stageID: string;
    displayID: string;
    name: string;
    description: string;
    amountMinor: number;
    currency: string;
    ownerID: string;
    expectedCloseOn: string;
  },
): Promise<OpportunityResponse> {
  const response = await fetch("/api/v1/opportunities", {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({
      client_id: clientID,
      pipeline_id: request.pipelineID,
      stage_id: request.stageID,
      display_id: request.displayID,
      name: request.name,
      description: request.description,
      amount_minor: request.amountMinor,
      currency: request.currency,
      owner_id: request.ownerID,
      expected_close_on: request.expectedCloseOn,
    }),
  });
  return parseResponse<OpportunityResponse>(response);
}

export async function listOpportunityActivities(
  clientID: string,
  opportunityID: string,
  signal?: AbortSignal,
): Promise<OpportunityActivityResponse[]> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/activities?limit=100`,
    {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    },
  );
  return parseResponse<OpportunityActivityResponse[]>(response);
}

export async function createOpportunityActivity(
  clientID: string,
  opportunityID: string,
  request: { kind: string; summary: string; details: string },
): Promise<OpportunityActivityResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/activities`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify(request),
    },
  );
  return parseResponse<OpportunityActivityResponse>(response);
}

export async function listOpportunityAttachments(
  clientID: string,
  opportunityID: string,
  signal?: AbortSignal,
): Promise<OpportunityAttachmentResponse[]> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/attachments`,
    {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    },
  );
  return parseResponse<OpportunityAttachmentResponse[]>(response);
}

export async function uploadOpportunityAttachment(
  clientID: string,
  opportunityID: string,
  file: File,
): Promise<OpportunityAttachmentResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/attachments`,
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
  return parseResponse<OpportunityAttachmentResponse>(response);
}

export async function listOpportunityTasks(
  clientID: string,
  opportunityID: string,
  signal?: AbortSignal,
): Promise<OpportunityTaskResponse[]> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/tasks`,
    {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal,
    },
  );
  return parseResponse<OpportunityTaskResponse[]>(response);
}

export async function createOpportunityTask(
  clientID: string,
  opportunityID: string,
  request: {
    title: string;
    ownerID?: string;
    estimateMinutes: number;
    tagIDs: string[];
  },
): Promise<OpportunityTaskResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(opportunityID)}/tasks`,
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
        ...(request.ownerID ? { owner_id: request.ownerID } : {}),
        estimate_minutes: request.estimateMinutes,
        tag_ids: request.tagIDs,
      }),
    },
  );
  return parseResponse<OpportunityTaskResponse>(response);
}

export async function listProposals(
  clientID: string,
  signal?: AbortSignal,
): Promise<ProposalResponse[]> {
  const response = await fetch("/api/v1/proposals?limit=100", {
    headers: clientContextHeaders(clientID),
    credentials: "same-origin",
    signal,
  });
  return parseResponse<ProposalResponse[]>(response);
}

export async function createProposal(
  clientID: string,
  opportunityID: string,
  displayID: string,
): Promise<ProposalResponse> {
  const response = await fetch("/api/v1/proposals", {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...clientContextHeaders(clientID),
    },
    body: JSON.stringify({
      opportunity_id: opportunityID,
      display_id: displayID,
    }),
  });
  return parseResponse<ProposalResponse>(response);
}

export async function issueProposalVersion(
  clientID: string,
  proposalID: string,
  request: {
    expectedVersion: number;
    currency: string;
    lines: Array<{
      type: string;
      description: string;
      quantity: number;
      unitPriceMinor: number;
      unitCostMinor: number;
      discountMinor: number;
      taxMinor: number;
      taxTreatment: string;
      recurrence: string;
      plannedMinutes: number;
    }>;
    maximumWithoutApprovalMinor: number;
    minimumMarginBasisPoints: number;
    expiresAt?: string;
  },
): Promise<ProposalVersionResponse> {
  const currency = request.currency.toUpperCase();
  const money = (minor: number) => ({ minor, currency });
  const response = await fetch(
    `/api/v1/proposals/${encodeURIComponent(proposalID)}/versions`,
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
        currency,
        lines: request.lines.map((line) => ({
          id: "",
          type: line.type,
          description: line.description,
          quantity: line.quantity,
          unit_price: money(line.unitPriceMinor),
          unit_cost: money(line.unitCostMinor),
          discount: money(line.discountMinor),
          tax_treatment: line.taxTreatment,
          tax: money(line.taxMinor),
          recurrence: line.recurrence,
          planned_minutes: line.plannedMinutes,
        })),
        approval_rule: {
          maximum_without_approval_minor: request.maximumWithoutApprovalMinor,
          minimum_margin_basis_points: request.minimumMarginBasisPoints,
        },
        expires_at: request.expiresAt || undefined,
      }),
    },
  );
  return parseResponse<ProposalVersionResponse>(response);
}

export async function getProposalVersion(
  clientID: string,
  versionID: string,
  signal?: AbortSignal,
): Promise<ProposalVersionResponse> {
  const response = await fetch(
    `/api/v1/proposal-versions/${encodeURIComponent(versionID)}`,
    {
      headers: clientContextHeaders(clientID),
      credentials: "same-origin",
      signal,
    },
  );
  return parseResponse<ProposalVersionResponse>(response);
}

export async function getInternalApproval(
  clientID: string,
  versionID: string,
  signal?: AbortSignal,
): Promise<InternalApprovalResponse> {
  const response = await fetch(
    `/api/v1/proposal-versions/${encodeURIComponent(versionID)}/internal-approval`,
    {
      headers: clientContextHeaders(clientID),
      credentials: "same-origin",
      signal,
    },
  );
  return parseResponse<InternalApprovalResponse>(response);
}

export async function decideInternalApproval(
  clientID: string,
  versionID: string,
  expectedVersion: number,
  decision: "approved" | "rejected",
  reason: string,
): Promise<InternalApprovalResponse> {
  const response = await fetch(
    `/api/v1/proposal-versions/${encodeURIComponent(versionID)}/internal-approval`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${expectedVersion}"`,
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: expectedVersion,
        decision,
        reason,
      }),
    },
  );
  return parseResponse<InternalApprovalResponse>(response);
}

export async function recordOfflineAcceptance(
  clientID: string,
  versionID: string,
  signerName: string,
  signerEmail: string,
  acceptedAt: string,
) {
  const response = await fetch(
    `/api/v1/proposal-versions/${encodeURIComponent(versionID)}/accept`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        method: "offline",
        signer_name: signerName,
        signer_email: signerEmail,
        accepted_at: acceptedAt,
      }),
    },
  );
  return parseResponse(response);
}

export async function transitionOpportunity(
  clientID: string,
  id: string,
  expectedVersion: number,
  stageId: string,
  reason: string,
  signal?: AbortSignal,
): Promise<OpportunityResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${expectedVersion}"`,
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: expectedVersion,
        stage_id: stageId,
        reason,
      }),
      signal,
    },
  );
  return parseResponse<OpportunityResponse>(response);
}

export async function replaceOpportunityCustomFields(
  clientID: string,
  id: string,
  expectedVersion: number,
  fields: Record<string, string>,
  signal?: AbortSignal,
): Promise<OpportunityResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(id)}/custom-fields`,
    {
      method: "PUT",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${expectedVersion}"`,
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({ expected_version: expectedVersion, fields }),
      signal,
    },
  );
  return parseResponse<OpportunityResponse>(response);
}

export async function replaceOpportunityParticipants(
  clientID: string,
  id: string,
  expectedVersion: number,
  teamID: string,
  contactIDs: string[],
  signal?: AbortSignal,
): Promise<OpportunityResponse> {
  const response = await fetch(
    `/api/v1/opportunities/${encodeURIComponent(id)}/participants`,
    {
      method: "PUT",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${expectedVersion}"`,
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        expected_version: expectedVersion,
        team_id: teamID,
        contact_ids: contactIDs,
      }),
      signal,
    },
  );
  return parseResponse<OpportunityResponse>(response);
}

export async function acceptProposalVersion(
  clientID: string,
  versionId: string,
  request: {
    method: "electronic" | "offline";
    signerName: string;
    signerEmail: string;
    acceptedAt: string;
    evidence?: Record<string, string>;
  },
  signal?: AbortSignal,
) {
  const response = await fetch(
    `/api/v1/proposal-versions/${encodeURIComponent(versionId)}/accept`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...csrfHeaders(),
        ...clientContextHeaders(clientID),
      },
      body: JSON.stringify({
        method: request.method,
        signer_name: request.signerName,
        signer_email: request.signerEmail,
        accepted_at: request.acceptedAt,
        evidence: request.evidence,
      }),
      signal,
    },
  );
  return parseResponse(response);
}
