export type ProviderAdapter = "ollama" | "openai_compatible";
export type NetworkMode = "local" | "remote";
export type HealthState =
  "pending" | "healthy" | "degraded" | "failed" | "disabled";
export type AIFeature =
  | "summary"
  | "reply_draft"
  | "similar_suggestions"
  | "classification"
  | "calendar_recommendation";
export type AIJobState =
  "queued" | "running" | "completed" | "failed" | "cancelled";
export type AIRecommendationState = "pending_human" | "accepted" | "rejected";

export type ProviderConnection = {
  id: string;
  name: string;
  adapter: ProviderAdapter;
  networkMode: NetworkMode;
  baseUrl: string;
  credentialConfigured: boolean;
  enabled: boolean;
  timeoutSeconds: number;
  requestLimitBytes: number;
  responseLimitBytes: number;
  localNetworkAcknowledgedAt?: string;
  health: HealthState;
  lastTestedAt?: string;
  lastSucceededAt?: string;
  lastErrorCode?: string;
  version: number;
};

export type ModelProfile = {
  id: string;
  connectionId: string;
  providerModelId: string;
  displayName: string;
  supportedFeatures: AIFeature[];
  contextLimit: number;
  outputLimit: number;
  zeroCost: boolean;
  inputCostPerMillionMinor?: number;
  outputCostPerMillionMinor?: number;
  enabled: boolean;
  version: number;
};

export type AIPolicy = {
  enabled: boolean;
  providerDisclosureAccepted: boolean;
  promptVersion: string;
  allowedFeatures: AIFeature[];
  summaryModelProfileId: string;
  replyDraftModelProfileId: string;
  similarSuggestionsModelProfileId: string;
  classificationModelProfileId?: string;
  calendarRecommendationModelProfileId: string;
  costLimitEnabled: boolean;
  allowUnmeteredUnknown: boolean;
  monthlyCostLimitMinor: number;
  currentMonthlyCostMinor?: number;
  version: number;
};

export type CreateConnectionInput = {
  name: string;
  adapter: ProviderAdapter;
  networkMode: NetworkMode;
  baseUrl: string;
  credential?: string;
  timeoutSeconds: number;
  requestLimitBytes: number;
  responseLimitBytes: number;
  localNetworkAcknowledged: boolean;
  reason: string;
};

export type UpdateConnectionInput = Omit<
  CreateConnectionInput,
  "credential"
> & {
  expectedVersion: number;
};

export type CredentialReplacementInput = {
  credential: string;
  expectedVersion: number;
  reason: string;
};

export type ModelUpdate = Omit<
  ModelProfile,
  "connectionId" | "providerModelId"
> & {
  expectedVersion: number;
};

export type UpdatePolicyInput = Omit<
  AIPolicy,
  "currentMonthlyCostMinor" | "version"
> & {
  expectedVersion: number;
  reason: string;
};

export type AISettingsAPI = {
  listConnections(signal?: AbortSignal): Promise<ProviderConnection[]>;
  createConnection(
    input: CreateConnectionInput,
    signal?: AbortSignal,
  ): Promise<ProviderConnection>;
  updateConnection(
    id: string,
    input: UpdateConnectionInput,
    signal?: AbortSignal,
  ): Promise<ProviderConnection>;
  setConnectionEnabled(
    id: string,
    input: { enabled: boolean; expectedVersion: number; reason: string },
    signal?: AbortSignal,
  ): Promise<ProviderConnection>;
  replaceCredential(
    id: string,
    input: CredentialReplacementInput,
    signal?: AbortSignal,
  ): Promise<ProviderConnection>;
  testConnection(id: string, signal?: AbortSignal): Promise<ProviderConnection>;
  discoverModels(
    id: string,
    input: { expectedVersion: number; reason: string },
    signal?: AbortSignal,
  ): Promise<ModelProfile[]>;
  listModels(id: string, signal?: AbortSignal): Promise<ModelProfile[]>;
  updateModels(
    id: string,
    updates: ModelUpdate[],
    reason: string,
    signal?: AbortSignal,
  ): Promise<ModelProfile[]>;
  getPolicy(signal?: AbortSignal): Promise<AIPolicy>;
  updatePolicy(
    input: UpdatePolicyInput,
    signal?: AbortSignal,
  ): Promise<AIPolicy>;
};

export type AIAssistJob = {
  id: string;
  workRecordID: string;
  feature: AIFeature;
  modelProfileID?: string;
  state: AIJobState;
  attempt: number;
  maxAttempts: number;
  cancellationRequestedAt?: string;
  safeErrorCode?: string;
  recommendationID?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  version: number;
  retryAfterSeconds?: number;
};

export type AIAssistRecommendation = {
  id: string;
  jobID: string;
  clientID: string;
  workRecordID: string;
  feature: AIFeature;
  text: string;
  candidateIDs: string[];
  confidence?: number;
  relevantInputs: string[];
  state: AIRecommendationState;
  generatedAt: string;
  version: number;
};

export type AIAssistDecision = {
  id: string;
  state: Exclude<AIRecommendationState, "pending_human">;
  applied: false;
  sent: false;
};

export type AIAssistAPI = {
  submit(
    workRecordID: string,
    input: { feature: AIFeature; idempotencyKey: string },
    signal?: AbortSignal,
  ): Promise<AIAssistJob>;
  getJob(id: string, signal?: AbortSignal): Promise<AIAssistJob>;
  cancel(
    id: string,
    input: { expectedVersion: number; reason: string },
    signal?: AbortSignal,
  ): Promise<AIAssistJob>;
  retry(
    id: string,
    input: { expectedVersion: number; reason: string },
    signal?: AbortSignal,
  ): Promise<AIAssistJob>;
  getRecommendation(
    id: string,
    signal?: AbortSignal,
  ): Promise<AIAssistRecommendation>;
  decide(
    id: string,
    input: { decision: "accepted" | "rejected"; reason: string },
    signal?: AbortSignal,
  ): Promise<AIAssistDecision>;
};

export type AIWorkspaceConversation = {
  id: string;
  client_id?: string;
  title: string;
  archived_at?: string;
  version: number;
  created_at: string;
  updated_at: string;
};

export type AIWorkspaceMessage = {
  id: string;
  client_id?: string;
  conversation_id: string;
  role: "user" | "assistant" | "tool" | "system";
  text: string;
  referenced_objects?: Record<string, unknown>;
  created_at: string;
};

export type AIWorkspaceChange = {
  before: unknown;
  after: unknown;
};

export type AIWorkspaceLocationTarget = {
  id: string;
  client_id: string;
  kind: "location";
  display_id: string;
  name: string;
};

export type AIWorkspaceProposal = {
  id: string;
  client_id?: string;
  target_client_id?: string;
  tool_name: string;
  tool_version: number;
  preview: {
    summary: string;
    target_type: string;
    target_id: string;
    target_version?: number;
    location_target?: AIWorkspaceLocationTarget;
    changes?: Record<string, AIWorkspaceChange>;
  };
  required_capability: string;
  expires_at: string;
  state: "pending" | "confirmed" | "rejected" | "expired" | "failed";
  result?: { summary: string; data?: Record<string, unknown> };
  version: number;
};

export type ClientResourceKind =
  "location" | "contact" | "asset" | "service" | "contract";

export type ClientResourceSummary = {
  id: string;
  kind: ClientResourceKind;
  display_id: string;
  name: string;
  detail?: string;
  location_id?: string;
  version: number;
};

export type AIWorkspaceReadResult = {
  summary: string;
  data?: Record<string, unknown>;
};

export type AIWorkspaceReadTool =
  | "ticket.get"
  | "ticket.search"
  | "project.search"
  | "project.get"
  | "client_resource.list"
  | "knowledge.search"
  | "knowledge.get"
  | "prospect.list"
  | "opportunity.list"
  | "opportunity.get"
  | "proposal.list"
  | "proposal.get";

export type AIWorkspaceWriteTool =
  | "ticket.transition"
  | "ticket.priority"
  | "ticket.note"
  | "ticket.reply"
  | "ticket.route"
  | "project.create"
  | "task.create"
  | "client.create"
  | `${ClientResourceKind}.create`
  | `${ClientResourceKind}.update`
  | `${ClientResourceKind}.deactivate`
  | `${ClientResourceKind}.reactivate`
  | "knowledge.draft.create"
  | "knowledge.draft.revise"
  | "prospect.create"
  | "ticket.create"
  | "ticket.assign"
  | "opportunity.transition"
  | "opportunity.activity.create"
  | "proposal.create"
  | "knowledge.publish";

export type AIWorkspaceAPI = {
  list(signal?: AbortSignal): Promise<AIWorkspaceConversation[]>;
  create(title: string, signal?: AbortSignal): Promise<AIWorkspaceConversation>;
  get(
    id: string,
    signal?: AbortSignal,
  ): Promise<{
    conversation: AIWorkspaceConversation;
    messages: AIWorkspaceMessage[];
  }>;
  sendMessage(
    conversationID: string,
    text: string,
    signal?: AbortSignal,
  ): Promise<{
    user_message: AIWorkspaceMessage;
    assistant_message: AIWorkspaceMessage;
    proposal?: AIWorkspaceProposal;
  }>;
  propose(
    conversationID: string,
    toolName: AIWorkspaceWriteTool,
    input: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<AIWorkspaceProposal>;
  runRead(
    conversationID: string,
    toolName: AIWorkspaceReadTool,
    input: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<AIWorkspaceReadResult>;
  confirm(
    proposalID: string,
    expectedVersion: number,
    signal?: AbortSignal,
  ): Promise<{ summary: string; data?: Record<string, unknown> }>;
  reject(
    proposalID: string,
    expectedVersion: number,
    signal?: AbortSignal,
  ): Promise<void>;
};
