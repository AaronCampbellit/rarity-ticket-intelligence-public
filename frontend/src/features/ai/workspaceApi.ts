import { csrfHeaders } from "../../api/browserSession";
import type {
  AIWorkspaceAPI,
  AIWorkspaceConversation,
  AIWorkspaceReadResult,
  AIWorkspaceMessage,
  AIWorkspaceProposal,
} from "./types";

type Fetcher = typeof fetch;

export class AIWorkspaceClarificationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AIWorkspaceClarificationError";
  }
}

async function request(
  fetcher: Fetcher,
  path: string,
  init: RequestInit = {},
): Promise<Response> {
  const response = await fetcher(path, {
    credentials: "same-origin",
    ...init,
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...csrfHeaders(),
      ...init.headers,
    },
  });
  if (!response.ok) {
    try {
      const body = (await response.clone().json()) as {
        error?: { code?: unknown; message?: unknown };
      };
      if (
        body.error?.code === "ambiguous_reference" &&
        typeof body.error.message === "string"
      ) {
        throw new AIWorkspaceClarificationError(body.error.message);
      }
    } catch (error) {
      if (error instanceof AIWorkspaceClarificationError) throw error;
    }
    throw new Error(`ai_workspace_${response.status}`);
  }
  return response;
}

function conversation(value: unknown): AIWorkspaceConversation {
  const body = value as Partial<AIWorkspaceConversation>;
  if (
    !body ||
    typeof body.id !== "string" ||
    typeof body.title !== "string" ||
    typeof body.version !== "number"
  ) {
    throw new Error("ai_workspace_invalid_response");
  }
  return body as AIWorkspaceConversation;
}

function message(value: unknown): AIWorkspaceMessage {
  const body = value as Partial<AIWorkspaceMessage>;
  if (
    !body ||
    typeof body.id !== "string" ||
    typeof body.conversation_id !== "string" ||
    typeof body.role !== "string" ||
    typeof body.text !== "string"
  ) {
    throw new Error("ai_workspace_invalid_response");
  }
  return body as AIWorkspaceMessage;
}

function proposal(value: unknown): AIWorkspaceProposal {
  const body = value as Partial<AIWorkspaceProposal>;
  if (
    !body ||
    typeof body.id !== "string" ||
    typeof body.tool_name !== "string" ||
    typeof body.version !== "number" ||
    !body.preview ||
    typeof body.preview.summary !== "string"
  ) {
    throw new Error("ai_workspace_invalid_response");
  }
  return body as AIWorkspaceProposal;
}

function readResult(value: unknown): AIWorkspaceReadResult {
  const body = value as Partial<AIWorkspaceReadResult>;
  if (
    !body ||
    typeof body.summary !== "string" ||
    (body.data !== undefined &&
      (!body.data || typeof body.data !== "object" || Array.isArray(body.data)))
  ) {
    throw new Error("ai_workspace_invalid_response");
  }
  return {
    summary: body.summary,
    ...(body.data === undefined ? {} : { data: body.data }),
  };
}

export function createAIWorkspaceAPI(fetcher: Fetcher = fetch): AIWorkspaceAPI {
  return {
    async list(signal) {
      const response = await request(
        fetcher,
        "/api/v1/ai/workspace/conversations",
        { signal },
      );
      const body = (await response.json()) as unknown;
      if (!Array.isArray(body))
        throw new Error("ai_workspace_invalid_response");
      return body.map(conversation);
    },
    async create(title, signal) {
      const response = await request(
        fetcher,
        "/api/v1/ai/workspace/conversations",
        { method: "POST", signal, body: JSON.stringify({ title }) },
      );
      return conversation(await response.json());
    },
    async get(id, signal) {
      const response = await request(
        fetcher,
        `/api/v1/ai/workspace/conversations/${encodeURIComponent(id)}`,
        { signal },
      );
      const body = (await response.json()) as {
        conversation?: unknown;
        messages?: unknown;
      };
      if (!Array.isArray(body.messages)) {
        throw new Error("ai_workspace_invalid_response");
      }
      return {
        conversation: conversation(body.conversation),
        messages: body.messages.map(message),
      };
    },
    async sendMessage(conversationID, text, signal) {
      const response = await request(
        fetcher,
        `/api/v1/ai/workspace/conversations/${encodeURIComponent(conversationID)}/messages`,
        { method: "POST", signal, body: JSON.stringify({ text }) },
      );
      const body = (await response.json()) as {
        user_message?: unknown;
        assistant_message?: unknown;
        proposal?: unknown;
      };
      return {
        user_message: message(body.user_message),
        assistant_message: message(body.assistant_message),
        ...(body.proposal === undefined
          ? {}
          : { proposal: proposal(body.proposal) }),
      };
    },
    async propose(conversationID, toolName, input, signal) {
      const response = await request(
        fetcher,
        `/api/v1/ai/workspace/conversations/${encodeURIComponent(conversationID)}/proposals`,
        {
          method: "POST",
          signal,
          body: JSON.stringify({ tool_name: toolName, input }),
        },
      );
      return proposal(await response.json());
    },
    async runRead(conversationID, toolName, input, signal) {
      const response = await request(
        fetcher,
        `/api/v1/ai/workspace/conversations/${encodeURIComponent(conversationID)}/tools/read`,
        {
          method: "POST",
          signal,
          body: JSON.stringify({ tool_name: toolName, input }),
        },
      );
      return readResult(await response.json());
    },
    async confirm(proposalID, expectedVersion, signal) {
      const response = await request(
        fetcher,
        `/api/v1/ai/workspace/proposals/${encodeURIComponent(proposalID)}/confirm`,
        {
          method: "POST",
          signal,
          body: JSON.stringify({ expected_version: expectedVersion }),
        },
      );
      return (await response.json()) as {
        summary: string;
        data?: Record<string, unknown>;
      };
    },
    async reject(proposalID, expectedVersion, signal) {
      await request(
        fetcher,
        `/api/v1/ai/workspace/proposals/${encodeURIComponent(proposalID)}/reject`,
        {
          method: "POST",
          signal,
          body: JSON.stringify({ expected_version: expectedVersion }),
        },
      );
    },
  };
}

export const aiWorkspaceAPI = createAIWorkspaceAPI();
