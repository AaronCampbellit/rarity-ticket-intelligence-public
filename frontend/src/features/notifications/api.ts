import { csrfHeaders } from "../../api/browserSession";
import type {
  NotificationCenterAPI,
  NotificationItem,
  NotificationPage,
} from "./types";

type Fetcher = typeof fetch;
type RecordValue = Record<string, unknown>;
const browserFetcher: Fetcher = (input, init) => globalThis.fetch(input, init);

export type {
  NotificationCenterAPI,
  NotificationItem,
  NotificationPage,
} from "./types";

export class NotificationAPIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    message = "Notification request failed",
  ) {
    super(message);
    this.name = "NotificationAPIError";
  }
}

function invalidResponse(): never {
  throw new NotificationAPIError("invalid_response", 502);
}

function record(value: unknown): RecordValue {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return invalidResponse();
  }
  return value as RecordValue;
}

function requiredString(value: RecordValue, name: string): string {
  const candidate = value[name];
  if (typeof candidate !== "string" || candidate.trim().length === 0) {
    return invalidResponse();
  }
  return candidate;
}

function optionalString(value: RecordValue, name: string): string | undefined {
  const candidate = value[name];
  if (candidate === undefined) return undefined;
  if (typeof candidate !== "string" || candidate.trim().length === 0) {
    return invalidResponse();
  }
  return candidate;
}

function timestamp(
  value: RecordValue,
  name: string,
  optional = false,
): string | undefined {
  const candidate = optional
    ? optionalString(value, name)
    : requiredString(value, name);
  if (candidate === undefined) return undefined;
  if (Number.isNaN(Date.parse(candidate))) return invalidResponse();
  return candidate;
}

function positiveInteger(value: RecordValue, name: string): number {
  const candidate = value[name];
  if (!Number.isSafeInteger(candidate) || Number(candidate) < 1) {
    return invalidResponse();
  }
  return Number(candidate);
}

function notification(value: unknown): NotificationItem {
  const body = record(value);
  const readAt = timestamp(body, "read_at", true);
  return {
    id: requiredString(body, "id"),
    title: requiredString(body, "title"),
    body: requiredString(body, "body"),
    actionPath: requiredString(body, "action_path"),
    contentClassification: requiredString(body, "content_classification"),
    createdAt: timestamp(body, "created_at")!,
    ...(readAt === undefined ? {} : { readAt }),
    version: positiveInteger(body, "version"),
  };
}

function readNotification(
  value: unknown,
  requestedID: string,
): NotificationItem {
  const item = notification(value);
  if (item.id !== requestedID || item.readAt === undefined) {
    return invalidResponse();
  }
  return item;
}

function page(value: unknown): NotificationPage {
  const body = record(value);
  if (!Array.isArray(body.notifications)) return invalidResponse();
  const nextCursor = optionalString(body, "next_cursor");
  return {
    notifications: body.notifications.map(notification),
    ...(nextCursor === undefined ? {} : { nextCursor }),
  };
}

async function responseError(response: Response): Promise<never> {
  const body = (await response.json().catch(() => ({}))) as {
    error?: { code?: unknown; message?: unknown };
  };
  throw new NotificationAPIError(
    typeof body.error?.code === "string" ? body.error.code : "request_failed",
    response.status,
    typeof body.error?.message === "string"
      ? body.error.message
      : "Notification request failed",
  );
}

async function successJSON(response: Response): Promise<unknown> {
  return response.json().catch(() => invalidResponse());
}

export function createNotificationCenterAPI(
  fetcher: Fetcher = browserFetcher,
): NotificationCenterAPI {
  return {
    async list(cursor, limit = 25, signal) {
      const parameters = new URLSearchParams({ limit: String(limit) });
      if (cursor !== undefined) parameters.set("cursor", cursor);
      const response = await fetcher(`/api/v1/notifications?${parameters}`, {
        credentials: "same-origin",
        signal,
      });
      if (!response.ok) return responseError(response);
      return page(await successJSON(response));
    },

    async unreadCount(signal) {
      const response = await fetcher("/api/v1/notifications/unread-count", {
        credentials: "same-origin",
        signal,
      });
      if (!response.ok) return responseError(response);
      const body = record(await successJSON(response));
      const count = body.count;
      if (!Number.isSafeInteger(count) || Number(count) < 0) {
        return invalidResponse();
      }
      return Number(count);
    },

    async markRead(id, expectedVersion, signal) {
      const response = await fetcher(
        `/api/v1/notifications/${encodeURIComponent(id)}/read`,
        {
          method: "PATCH",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ expected_version: expectedVersion }),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      return readNotification(await successJSON(response), id);
    },
  };
}

export const notificationCenterAPI = createNotificationCenterAPI();
