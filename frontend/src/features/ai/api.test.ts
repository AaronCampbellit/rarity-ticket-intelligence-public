import { describe, expect, it, vi } from "vitest";

import {
  AISettingsAPIError,
  createAIAssistAPI,
  createAISettingsAPI,
} from "./api";
import {
  AIWorkspaceClarificationError,
  createAIWorkspaceAPI,
} from "./workspaceApi";

const provider = {
  id: "provider-1",
  name: "Models",
  adapter: "openai_compatible",
  network_mode: "remote",
  base_url: "https://models.example.test",
  credential_configured: true,
  enabled: true,
  timeout_seconds: 300,
  request_limit_bytes: 1_048_576,
  response_limit_bytes: 5_242_880,
  local_network_acknowledged_at: "2026-07-30T12:00:00Z",
  health: "healthy",
  last_tested_at: "2026-07-30T12:00:00Z",
  last_succeeded_at: "2026-07-30T12:01:00Z",
  last_error_code: "timeout",
  version: 4,
};
const model = {
  id: "model-1",
  connection_id: "provider-1",
  provider_model_id: "model-id",
  display_name: "Model",
  supported_features: ["summary"],
  context_limit: 8192,
  output_limit: 1024,
  zero_cost: false,
  input_cost_per_million_minor: 15,
  output_cost_per_million_minor: 30,
  enabled: true,
  version: 3,
};
const policy = {
  enabled: true,
  provider_disclosure_accepted: true,
  prompt_version: "v2",
  allowed_features: ["summary"],
  summary_model_profile_id: "model-1",
  reply_draft_model_profile_id: "",
  similar_suggestions_model_profile_id: "",
  calendar_recommendation_model_profile_id: "",
  cost_limit_enabled: true,
  allow_unmetered_unknown: false,
  monthly_cost_limit_minor: 12500,
  current_monthly_cost_minor: 25,
  version: 2,
};

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function requestBody(init?: RequestInit) {
  return JSON.parse(String(init?.body));
}

describe("AI settings API", () => {
  it("uses every Task 7 settings route with snake_case, ETags, reasons, and write-only credentials", async () => {
    const rawFetch = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path.endsWith("/models") || path.endsWith("discover-models"))
          return response([model]);
        if (path.endsWith("/policy")) return response(policy);
        if (path.endsWith("/providers") && !init?.method)
          return response([provider]);
        return response(provider);
      },
    );
    const fetcher = rawFetch as unknown as typeof fetch;
    const api = createAISettingsAPI(fetcher);
    const connection = {
      name: "Models",
      adapter: "openai_compatible" as const,
      networkMode: "remote" as const,
      baseUrl: "https://models.example.test",
      timeoutSeconds: 300,
      requestLimitBytes: 1_048_576,
      responseLimitBytes: 5_242_880,
      localNetworkAcknowledged: false,
      reason: "initial",
    };

    const signals = Array.from(
      { length: 11 },
      () => new AbortController().signal,
    );
    expect((await api.listConnections(signals[0]))[0]).toMatchObject({
      networkMode: "remote",
      credentialConfigured: true,
      lastSucceededAt: "2026-07-30T12:01:00Z",
    });
    await api.createConnection(
      {
        ...connection,
        credential: "synthetic-secret",
      },
      signals[1],
    );
    await api.updateConnection(
      "provider-1",
      {
        ...connection,
        expectedVersion: 4,
      },
      signals[2],
    );
    await api.setConnectionEnabled(
      "provider-1",
      {
        enabled: false,
        expectedVersion: 4,
        reason: "maintenance",
      },
      signals[3],
    );
    await api.replaceCredential(
      "provider-1",
      {
        credential: "replacement-secret",
        expectedVersion: 4,
        reason: "rotation",
      },
      signals[4],
    );
    await api.testConnection("provider-1", signals[5]);
    await api.discoverModels(
      "provider-1",
      {
        expectedVersion: 4,
        reason: "inventory",
      },
      signals[6],
    );
    expect((await api.listModels("provider-1", signals[7]))[0]).toMatchObject({
      inputCostPerMillionMinor: 15,
      outputCostPerMillionMinor: 30,
    });
    await api.updateModels(
      "provider-1",
      [
        {
          id: "model-1",
          displayName: "Model",
          supportedFeatures: ["summary"],
          contextLimit: 8192,
          outputLimit: 1024,
          zeroCost: false,
          inputCostPerMillionMinor: 15,
          outputCostPerMillionMinor: 30,
          enabled: true,
          version: 3,
          expectedVersion: 3,
        },
      ],
      "pricing review",
      signals[8],
    );
    expect(await api.getPolicy(signals[9])).toMatchObject({
      monthlyCostLimitMinor: 12500,
      currentMonthlyCostMinor: 25,
    });
    await api.updatePolicy(
      {
        enabled: true,
        providerDisclosureAccepted: true,
        promptVersion: "v2",
        allowedFeatures: ["summary"],
        summaryModelProfileId: "model-1",
        replyDraftModelProfileId: "",
        similarSuggestionsModelProfileId: "",
        calendarRecommendationModelProfileId: "",
        costLimitEnabled: true,
        allowUnmeteredUnknown: false,
        monthlyCostLimitMinor: 12500,
        expectedVersion: 2,
        reason: "pilot",
      },
      signals[10],
    );

    const calls = rawFetch.mock.calls as Array<
      [string, RequestInit | undefined]
    >;
    expect(calls.map(([path]) => path)).toEqual([
      "/api/v1/ai/providers",
      "/api/v1/ai/providers",
      "/api/v1/ai/providers/provider-1",
      "/api/v1/ai/providers/provider-1",
      "/api/v1/ai/providers/provider-1/credential",
      "/api/v1/ai/providers/provider-1/test",
      "/api/v1/ai/providers/provider-1/discover-models",
      "/api/v1/ai/providers/provider-1/models",
      "/api/v1/ai/providers/provider-1/models",
      "/api/v1/ai/policy",
      "/api/v1/ai/policy",
    ]);
    const [
      ,
      create,
      update,
      enabled,
      replace,
      test,
      discover,
      ,
      modelUpdate,
      ,
      policyUpdate,
    ] = calls;
    expect(calls.map(([, init]) => init?.signal)).toEqual(signals);
    expect(create[1]).toEqual({
      method: "POST",
      headers: { "Content-Type": "application/json" },
      signal: signals[1],
      body: JSON.stringify({
        name: "Models",
        adapter: "openai_compatible",
        network_mode: "remote",
        base_url: "https://models.example.test",
        timeout_seconds: 300,
        request_limit_bytes: 1_048_576,
        response_limit_bytes: 5_242_880,
        local_network_acknowledged: false,
        reason: "initial",
        credential: "synthetic-secret",
      }),
    });
    expect(requestBody(update[1])).toEqual({
      name: "Models",
      adapter: "openai_compatible",
      network_mode: "remote",
      base_url: "https://models.example.test",
      timeout_seconds: 300,
      request_limit_bytes: 1_048_576,
      response_limit_bytes: 5_242_880,
      local_network_acknowledged: false,
      reason: "initial",
      expected_version: 4,
    });
    expect(update[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"4"',
    });
    expect(requestBody(enabled[1])).toEqual({
      enabled: false,
      expected_version: 4,
      reason: "maintenance",
    });
    expect(enabled[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"4"',
    });
    expect(requestBody(replace[1])).toEqual({
      credential: "replacement-secret",
      expected_version: 4,
      reason: "rotation",
    });
    expect(replace[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"4"',
    });
    expect(requestBody(test[1])).toEqual({});
    expect(test[1]?.headers).toEqual({ "Content-Type": "application/json" });
    expect(requestBody(discover[1])).toEqual({
      expected_version: 4,
      reason: "inventory",
    });
    expect(discover[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"4"',
    });
    expect(modelUpdate[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"3"',
    });
    expect(requestBody(modelUpdate[1])).toEqual({
      reason: "pricing review",
      updates: [
        {
          id: "model-1",
          expected_version: 3,
          display_name: "Model",
          supported_features: ["summary"],
          context_limit: 8192,
          output_limit: 1024,
          zero_cost: false,
          input_cost_per_million_minor: 15,
          output_cost_per_million_minor: 30,
          enabled: true,
        },
      ],
    });
    expect(requestBody(policyUpdate[1])).toEqual({
      enabled: true,
      provider_disclosure_accepted: true,
      prompt_version: "v2",
      allowed_features: ["summary"],
      summary_model_profile_id: "model-1",
      reply_draft_model_profile_id: "",
      similar_suggestions_model_profile_id: "",
      calendar_recommendation_model_profile_id: "",
      cost_limit_enabled: true,
      allow_unmetered_unknown: false,
      monthly_cost_limit_minor: 12500,
      expected_version: 2,
      reason: "pilot",
    });
    expect(policyUpdate[1]?.headers).toEqual({
      "Content-Type": "application/json",
      "If-Match": '"2"',
    });
  });

  it("rejects malformed responses and maps conflicts without exposing provider error text", async () => {
    const malformed = createAISettingsAPI(
      vi
        .fn()
        .mockResolvedValue(
          response({ id: "provider-1" }),
        ) as unknown as typeof fetch,
    );
    await expect(
      malformed.createConnection({
        name: "Models",
        adapter: "ollama",
        networkMode: "local",
        baseUrl: "http://127.0.0.1:11434",
        timeoutSeconds: 900,
        requestLimitBytes: 1,
        responseLimitBytes: 1,
        localNetworkAcknowledged: true,
        reason: "test",
      }),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });
    const conflict = createAISettingsAPI(
      vi.fn().mockResolvedValue(
        response(
          {
            error: {
              code: "version_conflict",
              message: "synthetic-secret must never escape",
            },
          },
          409,
        ),
      ) as unknown as typeof fetch,
    );
    await expect(conflict.getPolicy()).rejects.toEqual(
      expect.objectContaining<Partial<AISettingsAPIError>>({
        code: "version_conflict",
        status: 409,
        message: "version_conflict",
      }),
    );
  });
});

const job = {
  id: "job-1",
  work_record_id: "work-1",
  feature: "summary",
  model_profile_id: "model-1",
  state: "failed",
  attempt: 1,
  max_attempts: 3,
  safe_error_code: "provider_timeout",
  recommendation_id: "recommendation-1",
  created_at: "2026-07-30T12:00:00Z",
  updated_at: "2026-07-30T12:00:01Z",
  completed_at: "2026-07-30T12:00:01Z",
  version: 4,
};
const recommendation = {
  id: "recommendation-1",
  job_id: "job-1",
  client_id: "client-1",
  work_record_id: "work-1",
  feature: "summary",
  text: "Plain text only.",
  candidate_ids: ["work-2"],
  confidence: 0.8,
  relevant_inputs: ["title", "description"],
  state: "pending_human",
  generated_at: "2026-07-30T12:00:01Z",
  version: 1,
};

describe("AI assist API", () => {
  it("uses durable job and review routes with exact bodies, versions, idempotency, signals, and Retry-After", async () => {
    const rawFetch = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path.endsWith("/decide"))
          return response({
            id: "recommendation-1",
            state: "accepted",
            applied: false,
            sent: false,
          });
        if (path.includes("/recommendations/")) return response(recommendation);
        return new Response(JSON.stringify(job), {
          headers: { "content-type": "application/json", "retry-after": "3" },
        });
      },
    );
    const api = createAIAssistAPI(rawFetch as unknown as typeof fetch);
    const signals = Array.from(
      { length: 6 },
      () => new AbortController().signal,
    );

    expect(
      await api.submit(
        "work 1",
        { feature: "summary", idempotencyKey: "action-1" },
        signals[0],
      ),
    ).toMatchObject({ id: "job-1", retryAfterSeconds: 3 });
    await api.getJob("job 1", signals[1]);
    await api.cancel(
      "job 1",
      { expectedVersion: 4, reason: "No longer needed" },
      signals[2],
    );
    await api.retry(
      "job 1",
      { expectedVersion: 4, reason: "Network recovered" },
      signals[3],
    );
    expect(
      await api.getRecommendation("recommendation 1", signals[4]),
    ).toMatchObject({ text: "Plain text only.", candidateIDs: ["work-2"] });
    await api.decide(
      "recommendation 1",
      { decision: "accepted", reason: "Technician reviewed" },
      signals[5],
    );

    const calls = rawFetch.mock.calls as Array<
      [string, RequestInit | undefined]
    >;
    expect(calls.map(([path]) => path)).toEqual([
      "/api/v1/work-records/work%201/ai/jobs",
      "/api/v1/ai/jobs/job%201",
      "/api/v1/ai/jobs/job%201/cancel",
      "/api/v1/ai/jobs/job%201/retry",
      "/api/v1/ai/recommendations/recommendation%201",
      "/api/v1/ai/recommendations/recommendation%201/decide",
    ]);
    expect(calls.map(([, init]) => init?.signal)).toEqual(signals);
    expect(calls[0][1]).toMatchObject({
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": "action-1",
      },
    });
    expect(requestBody(calls[0][1])).toEqual({ feature: "summary" });
    for (const call of [calls[2], calls[3]]) {
      expect(call[1]?.headers).toEqual({
        "Content-Type": "application/json",
        "If-Match": '"4"',
      });
      expect(requestBody(call[1])).toHaveProperty("expected_version", 4);
      expect(requestBody(call[1])).toHaveProperty("reason");
    }
    expect(requestBody(calls[5][1])).toEqual({
      decision: "accepted",
      reason: "Technician reviewed",
    });
  });

  it("rejects malformed review output and preserves a safe error code", async () => {
    const malformed = createAIAssistAPI(
      vi
        .fn()
        .mockResolvedValue(
          response({ id: "recommendation-1" }),
        ) as unknown as typeof fetch,
    );
    await expect(
      malformed.getRecommendation("recommendation-1"),
    ).rejects.toMatchObject({ code: "invalid_response", status: 502 });
    const unavailable = createAIAssistAPI(
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: "provider_unavailable",
              message: "raw provider response",
            },
          }),
          {
            status: 503,
            headers: {
              "content-type": "application/json",
              "retry-after": "4",
            },
          },
        ),
      ) as unknown as typeof fetch,
    );
    await expect(unavailable.getJob("job-1")).rejects.toMatchObject({
      code: "provider_unavailable",
      status: 503,
      message: "provider_unavailable",
      retryAfterSeconds: 4,
    });
  });
});

describe("AI workspace API", () => {
  it("preserves safe structured clarification details from the API", async () => {
    const rawFetch = vi.fn(async () =>
      Response.json(
        {
          error: {
            code: "ambiguous_reference",
            message:
              "More than one exact Project matches. Use its display ID and try again.",
          },
        },
        { status: 409 },
      ),
    );
    const api = createAIWorkspaceAPI(rawFetch as unknown as typeof fetch);

    await expect(
      api.runRead("conversation-1", "project.get", {
        client: "CLIENT-001",
        project: "Modernization",
      }),
    ).rejects.toEqual(
      new AIWorkspaceClarificationError(
        "More than one exact Project matches. Use its display ID and try again.",
      ),
    );
  });

  it("runs the bounded Client-resource read tool with only the selected Client reference and kind", async () => {
    const rawFetch = vi.fn(async () =>
      response({
        summary: "Listed client resources",
        data: {
          resources: [
            {
              id: "location-1",
              kind: "location",
              display_id: "LOC-1",
              name: "Head Office",
              version: 2,
            },
          ],
        },
      }),
    );
    const api = createAIWorkspaceAPI(rawFetch as unknown as typeof fetch);

    await expect(
      api.runRead("conversation 1", "client_resource.list", {
        client: "CLIENT-002",
        kind: "location",
      }),
    ).resolves.toEqual({
      summary: "Listed client resources",
      data: {
        resources: [
          {
            id: "location-1",
            kind: "location",
            display_id: "LOC-1",
            name: "Head Office",
            version: 2,
          },
        ],
      },
    });

    expect(rawFetch).toHaveBeenCalledWith(
      "/api/v1/ai/workspace/conversations/conversation%201/tools/read",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          tool_name: "client_resource.list",
          input: { client: "CLIENT-002", kind: "location" },
        }),
      }),
    );
  });

  it("uses every MSP-wide workspace route without a Client context header", async () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const workspaceConversation = {
      id: "conversation-1",
      title: "Support",
      version: 1,
      created_at: "2026-08-04T12:00:00Z",
      updated_at: "2026-08-04T12:00:00Z",
    };
    const workspaceMessage = {
      id: "message-1",
      conversation_id: "conversation-1",
      role: "assistant",
      text: "How can I help?",
      created_at: "2026-08-04T12:00:00Z",
    };
    const workspaceProposal = {
      id: "proposal-1",
      tool_name: "ticket.transition",
      tool_version: 1,
      preview: {
        summary: "Change ticket status",
        target_type: "work_record",
        target_id: "ticket-1",
      },
      required_capability: "work_record.transition",
      expires_at: "2026-08-04T12:10:00Z",
      state: "pending",
      version: 2,
    };
    const rawFetch = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path.endsWith("/confirm")) {
          return response({ summary: "Updated ticket" });
        }
        if (path.endsWith("/reject"))
          return new Response(null, { status: 204 });
        if (path.endsWith("/messages")) {
          return response({
            user_message: {
              ...workspaceMessage,
              id: "message-2",
              role: "user",
            },
            assistant_message: { ...workspaceMessage, id: "message-3" },
          });
        }
        if (path.endsWith("/proposals")) return response(workspaceProposal);
        if (path.endsWith("/conversations") && init?.method === "POST") {
          return response(workspaceConversation);
        }
        if (path.endsWith("/conversations"))
          return response([workspaceConversation]);
        return response({
          conversation: workspaceConversation,
          messages: [workspaceMessage],
        });
      },
    );
    const api = createAIWorkspaceAPI(rawFetch as unknown as typeof fetch);
    const signals = Array.from(
      { length: 7 },
      () => new AbortController().signal,
    );

    await api.list(signals[0]);
    await api.create("New conversation", signals[1]);
    await api.get("conversation 1", signals[2]);
    await api.sendMessage("conversation 1", "Help me", signals[3]);
    await api.propose(
      "conversation 1",
      "ticket.transition",
      {
        client_id: "client-2",
        id: "ticket-1",
        expected_version: 3,
        status: "resolved",
      },
      signals[4],
    );
    await api.confirm("proposal 1", 2, signals[5]);
    await api.reject("proposal 1", 2, signals[6]);

    const calls = rawFetch.mock.calls as Array<
      [string, RequestInit | undefined]
    >;
    expect(calls.map(([path]) => path)).toEqual([
      "/api/v1/ai/workspace/conversations",
      "/api/v1/ai/workspace/conversations",
      "/api/v1/ai/workspace/conversations/conversation%201",
      "/api/v1/ai/workspace/conversations/conversation%201/messages",
      "/api/v1/ai/workspace/conversations/conversation%201/proposals",
      "/api/v1/ai/workspace/proposals/proposal%201/confirm",
      "/api/v1/ai/workspace/proposals/proposal%201/reject",
    ]);
    expect(calls.map(([, init]) => init?.signal)).toEqual(signals);
    for (const [, init] of calls) {
      const headers = new Headers(init?.headers);
      expect(headers.get("X-Rarity-CSRF")).toBe("csrf-token");
      expect(headers.has("X-Rarity-Client-ID")).toBe(false);
      expect(init?.credentials).toBe("same-origin");
    }
    expect(requestBody(calls[1][1])).toEqual({ title: "New conversation" });
    expect(requestBody(calls[3][1])).toEqual({ text: "Help me" });
    expect(requestBody(calls[4][1])).toEqual({
      tool_name: "ticket.transition",
      input: {
        client_id: "client-2",
        id: "ticket-1",
        expected_version: 3,
        status: "resolved",
      },
    });
    expect(requestBody(calls[5][1])).toEqual({ expected_version: 2 });
    expect(requestBody(calls[6][1])).toEqual({ expected_version: 2 });
    document.cookie = "rarity_csrf=; Max-Age=0; path=/";
  });
});
