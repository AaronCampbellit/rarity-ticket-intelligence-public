import type { TeamsConnection, TeamsHealth, TeamsSettingsAPI } from "./types";
import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";

const basePath = "/api/v1/integrations/teams/connections";
const healthValues = new Set<TeamsHealth>([
  "pending",
  "healthy",
  "degraded",
  "failed",
  "disabled",
]);

export class TeamsAPIError extends Error {
  constructor(
    public readonly code: string,
    message = "Teams request failed",
  ) {
    super(message);
  }
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new TeamsAPIError("invalid_response");
  return value as Record<string, unknown>;
}

function connection(value: unknown): TeamsConnection {
  const body = object(value);
  const health = body.health;
  if (
    typeof body.id !== "string" ||
    typeof body.name !== "string" ||
    typeof body.credential_configured !== "boolean" ||
    typeof body.enabled !== "boolean" ||
    typeof health !== "string" ||
    !healthValues.has(health as TeamsHealth) ||
    typeof body.version !== "number"
  )
    throw new TeamsAPIError("invalid_response");
  return {
    id: body.id,
    clientID: typeof body.client_id === "string" ? body.client_id : undefined,
    name: body.name,
    credentialConfigured: body.credential_configured,
    enabled: body.enabled,
    health: health as TeamsHealth,
    lastTestedAt:
      typeof body.last_tested_at === "string" ? body.last_tested_at : undefined,
    lastSuccessAt:
      typeof body.last_success_at === "string"
        ? body.last_success_at
        : undefined,
    lastErrorCode:
      typeof body.last_error_code === "string"
        ? body.last_error_code
        : undefined,
    version: body.version,
  };
}

async function request(
  fetcher: typeof fetch,
  path: string,
  init: RequestInit = {},
  clientID = "",
): Promise<unknown> {
  const response = await fetcher(path, {
    credentials: "same-origin",
    ...init,
    headers: {
      ...init.headers,
      ...(clientID ? clientContextHeaders(clientID) : {}),
    },
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = object(body).error;
    const detail = error && typeof error === "object" ? object(error) : {};
    throw new TeamsAPIError(
      typeof detail.code === "string" ? detail.code : "request_failed",
    );
  }
  return body;
}

function mutationInit(
  method: "POST" | "PATCH",
  body: Record<string, unknown>,
  signal?: AbortSignal,
): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify(body),
    signal,
  };
}

export function createTeamsSettingsAPI(
  fetcher: typeof fetch = fetch,
  clientID = "",
): TeamsSettingsAPI {
  return {
    async list(signal) {
      const body = await request(fetcher, basePath, { signal }, clientID);
      if (!Array.isArray(body)) throw new TeamsAPIError("invalid_response");
      return body.map(connection);
    },
    async create(input, signal) {
      return connection(
        await request(
          fetcher,
          basePath,
          mutationInit(
            "POST",
            {
              name: input.name,
              webhook_url: input.webhookURL,
              reason: input.reason,
            },
            signal,
          ),
          clientID,
        ),
      );
    },
    async updateName(id, input, signal) {
      return connection(
        await request(
          fetcher,
          `${basePath}/${encodeURIComponent(id)}`,
          mutationInit(
            "PATCH",
            {
              expected_version: input.expectedVersion,
              name: input.name,
              reason: input.reason,
            },
            signal,
          ),
          clientID,
        ),
      );
    },
    async setEnabled(id, input, signal) {
      return connection(
        await request(
          fetcher,
          `${basePath}/${encodeURIComponent(id)}`,
          mutationInit(
            "PATCH",
            {
              expected_version: input.expectedVersion,
              enabled: input.enabled,
              reason: input.reason,
            },
            signal,
          ),
          clientID,
        ),
      );
    },
    async replaceCredential(id, input, signal) {
      return connection(
        await request(
          fetcher,
          `${basePath}/${encodeURIComponent(id)}/credential`,
          mutationInit(
            "POST",
            {
              expected_version: input.expectedVersion,
              webhook_url: input.webhookURL,
              reason: input.reason,
            },
            signal,
          ),
          clientID,
        ),
      );
    },
    async test(id, input, signal) {
      return connection(
        await request(
          fetcher,
          `${basePath}/${encodeURIComponent(id)}/test`,
          mutationInit("POST", { reason: input.reason }, signal),
          clientID,
        ),
      );
    },
  };
}

export const teamsSettingsAPI = createTeamsSettingsAPI();
