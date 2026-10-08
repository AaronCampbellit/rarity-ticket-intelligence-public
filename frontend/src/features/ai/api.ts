import type {
  AIFeature,
  AIAssistAPI,
  AIAssistDecision,
  AIAssistJob,
  AIAssistRecommendation,
  AIJobState,
  AIRecommendationState,
  AIPolicy,
  AISettingsAPI,
  CreateConnectionInput,
  CredentialReplacementInput,
  HealthState,
  ModelProfile,
  ModelUpdate,
  NetworkMode,
  ProviderAdapter,
  ProviderConnection,
  UpdateConnectionInput,
  UpdatePolicyInput,
} from "./types";
import { csrfHeaders } from "../../api/browserSession";

export class AISettingsAPIError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    readonly retryAfterSeconds?: number,
  ) {
    super(code);
  }
}

type Fetcher = typeof fetch;
type UnknownRecord = Record<string, unknown>;

const features = new Set<AIFeature>([
  "summary",
  "reply_draft",
  "similar_suggestions",
  "calendar_recommendation",
]);
const adapters = new Set<ProviderAdapter>(["ollama", "openai_compatible"]);
const networks = new Set<NetworkMode>(["local", "remote"]);
const healthStates = new Set<HealthState>([
  "pending",
  "healthy",
  "degraded",
  "failed",
  "disabled",
]);
const jobStates = new Set<AIJobState>([
  "queued",
  "running",
  "completed",
  "failed",
  "cancelled",
]);
const recommendationStates = new Set<AIRecommendationState>([
  "pending_human",
  "accepted",
  "rejected",
]);

function record(value: unknown): UnknownRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return value as UnknownRecord;
}

function stringField(value: UnknownRecord, field: string): string {
  const result = value[field];
  if (typeof result !== "string")
    throw new AISettingsAPIError("invalid_response", 502);
  return result;
}

function numberField(value: UnknownRecord, field: string): number {
  const result = value[field];
  if (typeof result !== "number" || !Number.isFinite(result)) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return result;
}

function booleanField(value: UnknownRecord, field: string): boolean {
  const result = value[field];
  if (typeof result !== "boolean")
    throw new AISettingsAPIError("invalid_response", 502);
  return result;
}

function optionalString(
  value: UnknownRecord,
  field: string,
): string | undefined {
  const result = value[field];
  if (result === undefined || result === null) return undefined;
  if (typeof result !== "string")
    throw new AISettingsAPIError("invalid_response", 502);
  return result;
}

function optionalNumber(
  value: UnknownRecord,
  field: string,
): number | undefined {
  const result = value[field];
  if (result === undefined || result === null) return undefined;
  if (typeof result !== "number" || !Number.isFinite(result))
    throw new AISettingsAPIError("invalid_response", 502);
  return result;
}

function stringList(value: unknown): string[] {
  if (
    !Array.isArray(value) ||
    !value.every((item) => typeof item === "string")
  ) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return [...value];
}

function optionalDate(value: UnknownRecord, field: string): string | undefined {
  const result = optionalString(value, field);
  if (result !== undefined && Number.isNaN(Date.parse(result))) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return result;
}

function dateField(value: UnknownRecord, field: string): string {
  const result = stringField(value, field);
  if (Number.isNaN(Date.parse(result))) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return result;
}

function featureList(value: unknown): AIFeature[] {
  if (
    !Array.isArray(value) ||
    !value.every(
      (feature): feature is AIFeature =>
        typeof feature === "string" && features.has(feature as AIFeature),
    )
  ) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return [...value];
}

function decodeProvider(value: unknown): ProviderConnection {
  const body = record(value);
  const adapter = stringField(body, "adapter");
  const networkMode = stringField(body, "network_mode");
  const health = stringField(body, "health");
  if (
    !adapters.has(adapter as ProviderAdapter) ||
    !networks.has(networkMode as NetworkMode) ||
    !healthStates.has(health as HealthState)
  ) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return {
    id: stringField(body, "id"),
    name: stringField(body, "name"),
    adapter: adapter as ProviderAdapter,
    networkMode: networkMode as NetworkMode,
    baseUrl: stringField(body, "base_url"),
    credentialConfigured: booleanField(body, "credential_configured"),
    enabled: booleanField(body, "enabled"),
    timeoutSeconds: numberField(body, "timeout_seconds"),
    requestLimitBytes: numberField(body, "request_limit_bytes"),
    responseLimitBytes: numberField(body, "response_limit_bytes"),
    localNetworkAcknowledgedAt: optionalString(
      body,
      "local_network_acknowledged_at",
    ),
    health: health as HealthState,
    lastTestedAt: optionalString(body, "last_tested_at"),
    lastSucceededAt: optionalString(body, "last_succeeded_at"),
    lastErrorCode: optionalString(body, "last_error_code"),
    version: numberField(body, "version"),
  };
}

function decodeModel(value: unknown): ModelProfile {
  const body = record(value);
  return {
    id: stringField(body, "id"),
    connectionId: stringField(body, "connection_id"),
    providerModelId: stringField(body, "provider_model_id"),
    displayName: stringField(body, "display_name"),
    supportedFeatures: featureList(body.supported_features),
    contextLimit: numberField(body, "context_limit"),
    outputLimit: numberField(body, "output_limit"),
    zeroCost: booleanField(body, "zero_cost"),
    inputCostPerMillionMinor: optionalNumber(
      body,
      "input_cost_per_million_minor",
    ),
    outputCostPerMillionMinor: optionalNumber(
      body,
      "output_cost_per_million_minor",
    ),
    enabled: booleanField(body, "enabled"),
    version: numberField(body, "version"),
  };
}

function decodePolicy(value: unknown): AIPolicy {
  const body = record(value);
  return {
    enabled: booleanField(body, "enabled"),
    providerDisclosureAccepted: booleanField(
      body,
      "provider_disclosure_accepted",
    ),
    promptVersion: stringField(body, "prompt_version"),
    allowedFeatures: featureList(body.allowed_features),
    summaryModelProfileId:
      optionalString(body, "summary_model_profile_id") ?? "",
    replyDraftModelProfileId:
      optionalString(body, "reply_draft_model_profile_id") ?? "",
    similarSuggestionsModelProfileId:
      optionalString(body, "similar_suggestions_model_profile_id") ?? "",
    classificationModelProfileId:
      optionalString(body, "classification_model_profile_id") ?? "",
    calendarRecommendationModelProfileId:
      optionalString(body, "calendar_recommendation_model_profile_id") ?? "",
    costLimitEnabled: booleanField(body, "cost_limit_enabled"),
    allowUnmeteredUnknown: booleanField(body, "allow_unmetered_unknown"),
    monthlyCostLimitMinor: numberField(body, "monthly_cost_limit_minor"),
    currentMonthlyCostMinor: optionalNumber(body, "current_monthly_cost_minor"),
    version: numberField(body, "version"),
  };
}

function decodeJob(value: unknown, retryAfterSeconds?: number): AIAssistJob {
  const body = record(value);
  const feature = stringField(body, "feature");
  const state = stringField(body, "state");
  if (
    !features.has(feature as AIFeature) ||
    !jobStates.has(state as AIJobState)
  ) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return {
    id: stringField(body, "id"),
    workRecordID: stringField(body, "work_record_id"),
    feature: feature as AIFeature,
    modelProfileID: optionalString(body, "model_profile_id"),
    state: state as AIJobState,
    attempt: numberField(body, "attempt"),
    maxAttempts: numberField(body, "max_attempts"),
    cancellationRequestedAt: optionalDate(body, "cancellation_requested_at"),
    safeErrorCode: optionalString(body, "safe_error_code"),
    recommendationID: optionalString(body, "recommendation_id"),
    createdAt: dateField(body, "created_at"),
    updatedAt: dateField(body, "updated_at"),
    completedAt: optionalDate(body, "completed_at"),
    version: numberField(body, "version"),
    retryAfterSeconds,
  };
}

function decodeRecommendation(value: unknown): AIAssistRecommendation {
  const body = record(value);
  const feature = stringField(body, "feature");
  const state = stringField(body, "state");
  if (
    !features.has(feature as AIFeature) ||
    !recommendationStates.has(state as AIRecommendationState)
  ) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  const confidence = optionalNumber(body, "confidence");
  if (confidence !== undefined && (confidence < 0 || confidence > 1)) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return {
    id: stringField(body, "id"),
    jobID: stringField(body, "job_id"),
    clientID: stringField(body, "client_id"),
    workRecordID: stringField(body, "work_record_id"),
    feature: feature as AIFeature,
    text: stringField(body, "text"),
    candidateIDs: stringList(body.candidate_ids),
    confidence,
    relevantInputs: stringList(body.relevant_inputs),
    state: state as AIRecommendationState,
    generatedAt: dateField(body, "generated_at"),
    version: numberField(body, "version"),
  };
}

function decodeDecision(value: unknown): AIAssistDecision {
  const body = record(value);
  const state = stringField(body, "state");
  if (state !== "accepted" && state !== "rejected") {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  if (booleanField(body, "applied") || booleanField(body, "sent")) {
    throw new AISettingsAPIError("invalid_response", 502);
  }
  return { id: stringField(body, "id"), state, applied: false, sent: false };
}

function retryAfter(response: Response): number | undefined {
  const value = response.headers.get("retry-after");
  if (!value || !/^\d+$/.test(value)) return undefined;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) && seconds >= 0 ? seconds : undefined;
}

async function request(
  fetcher: Fetcher,
  path: string,
  init: RequestInit = {},
): Promise<unknown> {
  const response = await fetcher(path, init);
  if (response.status === 204) return undefined;
  const contentType = response.headers.get("content-type") ?? "";
  const body = contentType.includes("application/json")
    ? await response.json().catch(() => undefined)
    : undefined;
  if (!response.ok) {
    const error =
      body && typeof body === "object" && !Array.isArray(body)
        ? (body as UnknownRecord).error
        : undefined;
    const code =
      error &&
      typeof error === "object" &&
      !Array.isArray(error) &&
      typeof (error as UnknownRecord).code === "string"
        ? ((error as UnknownRecord).code as string)
        : "request_failed";
    throw new AISettingsAPIError(code, response.status);
  }
  if (body === undefined) throw new AISettingsAPIError("invalid_response", 502);
  return body;
}

async function requestWithMetadata(
  fetcher: Fetcher,
  path: string,
  init: RequestInit = {},
): Promise<{ body: unknown; retryAfterSeconds?: number }> {
  const response = await fetcher(path, init);
  const contentType = response.headers.get("content-type") ?? "";
  const body = contentType.includes("application/json")
    ? await response.json().catch(() => undefined)
    : undefined;
  const retryAfterSeconds = retryAfter(response);
  if (!response.ok) {
    const error =
      body && typeof body === "object" && !Array.isArray(body)
        ? (body as UnknownRecord).error
        : undefined;
    const code =
      error &&
      typeof error === "object" &&
      !Array.isArray(error) &&
      typeof (error as UnknownRecord).code === "string"
        ? ((error as UnknownRecord).code as string)
        : "request_failed";
    throw new AISettingsAPIError(code, response.status, retryAfterSeconds);
  }
  if (body === undefined) throw new AISettingsAPIError("invalid_response", 502);
  return { body, retryAfterSeconds };
}

function json(
  method: string,
  body?: unknown,
  expectedVersion?: number,
): RequestInit {
  return {
    method,
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders(),
      ...(expectedVersion === undefined
        ? {}
        : { "If-Match": `"${expectedVersion}"` }),
    },
    body: JSON.stringify(body ?? {}),
  };
}

function connectionRequest(
  input: CreateConnectionInput | UpdateConnectionInput,
) {
  return {
    name: input.name,
    adapter: input.adapter,
    network_mode: input.networkMode,
    base_url: input.baseUrl,
    timeout_seconds: input.timeoutSeconds,
    request_limit_bytes: input.requestLimitBytes,
    response_limit_bytes: input.responseLimitBytes,
    local_network_acknowledged: input.localNetworkAcknowledged,
    reason: input.reason,
  };
}

function modelRequest(model: ModelUpdate) {
  return {
    id: model.id,
    expected_version: model.expectedVersion,
    display_name: model.displayName,
    supported_features: model.supportedFeatures,
    context_limit: model.contextLimit,
    output_limit: model.outputLimit,
    zero_cost: model.zeroCost,
    input_cost_per_million_minor: model.inputCostPerMillionMinor,
    output_cost_per_million_minor: model.outputCostPerMillionMinor,
    enabled: model.enabled,
  };
}

export function createAISettingsAPI(fetcher: Fetcher = fetch): AISettingsAPI {
  return {
    async listConnections(signal) {
      const body = await request(fetcher, "/api/v1/ai/providers", { signal });
      if (!Array.isArray(body))
        throw new AISettingsAPIError("invalid_response", 502);
      return body.map(decodeProvider);
    },
    async createConnection(input, signal) {
      const body = await request(fetcher, "/api/v1/ai/providers", {
        ...json("POST", {
          ...connectionRequest(input),
          credential: input.credential,
        }),
        signal,
      });
      return decodeProvider(body);
    },
    async updateConnection(id, input, signal) {
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}`,
        {
          ...json(
            "PATCH",
            {
              ...connectionRequest(input),
              expected_version: input.expectedVersion,
            },
            input.expectedVersion,
          ),
          signal,
        },
      );
      return decodeProvider(body);
    },
    async setConnectionEnabled(id, input, signal) {
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}`,
        {
          ...json(
            "PATCH",
            {
              enabled: input.enabled,
              expected_version: input.expectedVersion,
              reason: input.reason,
            },
            input.expectedVersion,
          ),
          signal,
        },
      );
      return decodeProvider(body);
    },
    async replaceCredential(id, input: CredentialReplacementInput, signal) {
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}/credential`,
        {
          ...json(
            "POST",
            {
              credential: input.credential,
              expected_version: input.expectedVersion,
              reason: input.reason,
            },
            input.expectedVersion,
          ),
          signal,
        },
      );
      return decodeProvider(body);
    },
    async testConnection(id, signal) {
      return decodeProvider(
        await request(
          fetcher,
          `/api/v1/ai/providers/${encodeURIComponent(id)}/test`,
          { ...json("POST"), signal },
        ),
      );
    },
    async discoverModels(id, input, signal) {
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}/discover-models`,
        {
          ...json(
            "POST",
            { expected_version: input.expectedVersion, reason: input.reason },
            input.expectedVersion,
          ),
          signal,
        },
      );
      if (!Array.isArray(body))
        throw new AISettingsAPIError("invalid_response", 502);
      return body.map(decodeModel);
    },
    async listModels(id, signal) {
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}/models`,
        { signal },
      );
      if (!Array.isArray(body))
        throw new AISettingsAPIError("invalid_response", 502);
      return body.map(decodeModel);
    },
    async updateModels(id, updates, reason, signal) {
      const version =
        updates.length === 1 ? updates[0].expectedVersion : undefined;
      const body = await request(
        fetcher,
        `/api/v1/ai/providers/${encodeURIComponent(id)}/models`,
        {
          ...json(
            "PATCH",
            { reason, updates: updates.map(modelRequest) },
            version,
          ),
          signal,
        },
      );
      if (!Array.isArray(body))
        throw new AISettingsAPIError("invalid_response", 502);
      return body.map(decodeModel);
    },
    async getPolicy(signal) {
      return decodePolicy(
        await request(fetcher, "/api/v1/ai/policy", { signal }),
      );
    },
    async updatePolicy(input: UpdatePolicyInput, signal) {
      const body = await request(fetcher, "/api/v1/ai/policy", {
        ...json(
          "PATCH",
          {
            enabled: input.enabled,
            provider_disclosure_accepted: input.providerDisclosureAccepted,
            prompt_version: input.promptVersion,
            allowed_features: input.allowedFeatures,
            summary_model_profile_id: input.summaryModelProfileId,
            reply_draft_model_profile_id: input.replyDraftModelProfileId,
            similar_suggestions_model_profile_id:
              input.similarSuggestionsModelProfileId,
            calendar_recommendation_model_profile_id:
              input.calendarRecommendationModelProfileId,
            cost_limit_enabled: input.costLimitEnabled,
            allow_unmetered_unknown: input.allowUnmeteredUnknown,
            monthly_cost_limit_minor: input.monthlyCostLimitMinor,
            expected_version: input.expectedVersion,
            reason: input.reason,
          },
          input.expectedVersion,
        ),
        signal,
      });
      return decodePolicy(body);
    },
  };
}

export function createAIAssistAPI(fetcher: Fetcher = fetch): AIAssistAPI {
  const jobMutation = async (
    operation: "cancel" | "retry",
    id: string,
    input: { expectedVersion: number; reason: string },
    signal?: AbortSignal,
  ) => {
    const result = await requestWithMetadata(
      fetcher,
      `/api/v1/ai/jobs/${encodeURIComponent(id)}/${operation}`,
      {
        ...json(
          "POST",
          { expected_version: input.expectedVersion, reason: input.reason },
          input.expectedVersion,
        ),
        signal,
      },
    );
    return decodeJob(result.body, result.retryAfterSeconds);
  };
  return {
    async submit(workRecordID, input, signal) {
      const result = await requestWithMetadata(
        fetcher,
        `/api/v1/work-records/${encodeURIComponent(workRecordID)}/ai/jobs`,
        {
          ...json("POST", { feature: input.feature }),
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": input.idempotencyKey,
          },
          signal,
        },
      );
      return decodeJob(result.body, result.retryAfterSeconds);
    },
    async getJob(id, signal) {
      const result = await requestWithMetadata(
        fetcher,
        `/api/v1/ai/jobs/${encodeURIComponent(id)}`,
        { signal },
      );
      return decodeJob(result.body, result.retryAfterSeconds);
    },
    cancel(id, input, signal) {
      return jobMutation("cancel", id, input, signal);
    },
    retry(id, input, signal) {
      return jobMutation("retry", id, input, signal);
    },
    async getRecommendation(id, signal) {
      const result = await requestWithMetadata(
        fetcher,
        `/api/v1/ai/recommendations/${encodeURIComponent(id)}`,
        { signal },
      );
      return decodeRecommendation(result.body);
    },
    async decide(id, input, signal) {
      return decodeDecision(
        await request(
          fetcher,
          `/api/v1/ai/recommendations/${encodeURIComponent(id)}/decide`,
          { ...json("POST", input), signal },
        ),
      );
    },
  };
}

export const aiSettingsAPI = createAISettingsAPI();
export const aiAssistAPI = createAIAssistAPI();
