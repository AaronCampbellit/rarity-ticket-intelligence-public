import { csrfHeaders } from "../../api/browserSession";
import { CalendarAPIError, record } from "./api";

export async function calendarRequest(
  path: string,
  options: {
    method?: "POST" | "PATCH" | "PUT" | "DELETE";
    body?: unknown;
    signal?: AbortSignal;
    clientID?: string;
  } = {},
): Promise<unknown> {
  const response = await fetch(`/api/v1/${path}`, {
    credentials: "same-origin",
    signal: options.signal,
    method: options.method,
    headers: {
      ...(options.clientID ? { "X-Rarity-Client-ID": options.clientID } : {}),
      ...(options.method
        ? { "Content-Type": "application/json", ...csrfHeaders() }
        : {}),
    },
    ...(options.body === undefined
      ? {}
      : { body: JSON.stringify(options.body) }),
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    const error = record(payload).error;
    throw new CalendarAPIError(
      error &&
        typeof error === "object" &&
        "code" in error &&
        typeof error.code === "string"
        ? error.code
        : "calendar_unavailable",
      response.status,
    );
  }
  return response.status === 204 ? undefined : response.json();
}
export const calendarError = (error: unknown) =>
  error instanceof CalendarAPIError && error.status === 409
    ? "This record changed. Reload its latest values before saving again."
    : error instanceof Error
      ? error.message.replaceAll("_", " ")
      : "Request failed. Try again.";
