import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import type {
  InternalContentSource,
  CollaborationAPI,
  MentionAPI,
  MentionCandidate,
  MentionContext,
  MentionClientAPI,
  MentionDeepLink,
  MentionDocument,
  MentionItemState,
  MentionParentType,
  MentionSourceKind,
  MentionTargetType,
  MentionToken,
  MentionWidgetItem,
  MentionWidgetPage,
  TeamConfirmation,
} from "./types";

type Fetcher = typeof fetch;
type RecordValue = Record<string, unknown>;
const browserFetcher: Fetcher = (input, init) => globalThis.fetch(input, init);

const parentTypes = new Set<MentionParentType>([
  "work_record",
  "task",
  "project",
]);
const sourceKinds = new Set<MentionSourceKind>(["details", "comment", "note"]);
const targetTypes = new Set<MentionTargetType>(["staff", "team"]);
const itemStates = new Set<MentionItemState>(["unread", "read", "archived"]);
const origins = new Set(["direct", "team", "both"] as const);

export class MentionAPIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    message = "Mention request failed",
  ) {
    super(message);
    this.name = "MentionAPIError";
  }
}

function record(value: unknown): RecordValue {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return value as RecordValue;
}

function requiredString(value: RecordValue, name: string): string {
  const item = value[name];
  if (typeof item !== "string" || item.length === 0) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return item;
}

function optionalString(value: RecordValue, name: string): string | undefined {
  const item = value[name];
  if (item === undefined || item === null) return undefined;
  if (typeof item !== "string") {
    throw new MentionAPIError("invalid_response", 502);
  }
  return item;
}

function integer(value: RecordValue, name: string, minimum = 0): number {
  const item = value[name];
  if (!Number.isSafeInteger(item) || Number(item) < minimum) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return Number(item);
}

function boolean(value: RecordValue, name: string): boolean {
  if (typeof value[name] !== "boolean") {
    throw new MentionAPIError("invalid_response", 502);
  }
  return value[name];
}

function strings(value: unknown): string[] {
  if (
    !Array.isArray(value) ||
    !value.every((item) => typeof item === "string" && item.length > 0)
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return [...value];
}

function enumValue<T extends string>(value: unknown, allowed: Set<T>): T {
  if (typeof value !== "string" || !allowed.has(value as T)) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return value as T;
}

function safeCandidateLabel(value: RecordValue): string {
  const label = requiredString(value, "label");
  if (
    label !== label.trim() ||
    label.startsWith("@") ||
    /[\r\n\u0000-\u001f\u007f]/u.test(label)
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return label;
}

function candidate(value: unknown): MentionCandidate {
  const body = record(value);
  const targetType = enumValue(body.target_type, targetTypes);
  const base = {
    targetType,
    id: requiredString(body, "id"),
    label: safeCandidateLabel(body),
    version: integer(body, "version", 1),
  };
  if (targetType === "staff") {
    if (
      "eligible_count" in body ||
      "excluded_count" in body ||
      "eligible_member_ids" in body
    ) {
      throw new MentionAPIError("invalid_response", 502);
    }
    return { ...base, targetType: "staff" };
  }
  const eligibleCount = integer(body, "eligible_count", 1);
  const eligibleMemberIds = strings(body.eligible_member_ids);
  if (
    eligibleMemberIds.length !== eligibleCount ||
    new Set(eligibleMemberIds).size !== eligibleMemberIds.length ||
    eligibleMemberIds.some(
      (id, index) => index > 0 && eligibleMemberIds[index - 1] >= id,
    )
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return {
    ...base,
    targetType: "team",
    eligibleCount,
    excludedCount: integer(body, "excluded_count"),
    eligibleMemberIds,
  };
}

async function responseError(response: Response): Promise<never> {
  const payload = (await response.json().catch(() => ({}))) as {
    error?: { code?: unknown; message?: unknown };
  };
  throw new MentionAPIError(
    typeof payload.error?.code === "string"
      ? payload.error.code
      : "request_failed",
    response.status,
    typeof payload.error?.message === "string"
      ? payload.error.message
      : "Mention request failed",
  );
}

function token(value: unknown): MentionToken {
  const body = record(value);
  return {
    id: requiredString(body, "id"),
    targetType: enumValue(body.target_type, targetTypes),
    targetId: requiredString(body, "target_id"),
    label: requiredString(body, "label"),
    start: integer(body, "start"),
    end: integer(body, "end"),
  };
}

function validDocument(body: string, tokens: MentionToken[]): boolean {
  let previousEnd = 0;
  const ids = new Set<string>();
  return (
    tokens.length <= 100 &&
    tokens.every((item) => {
      const valid =
        !ids.has(item.id) &&
        item.start >= previousEnd &&
        item.end === item.start + item.label.length &&
        body.slice(item.start, item.end) === item.label;
      ids.add(item.id);
      previousEnd = item.end;
      return valid;
    })
  );
}

function source(
  value: unknown,
  confirmations: Record<string, TeamConfirmation>,
): InternalContentSource {
  const body = record(value);
  const tokensValue = Array.isArray(body.tokens) ? body.tokens.map(token) : [];
  const lifecycleState = requiredString(body, "lifecycle_state");
  if (lifecycleState !== "active" && lifecycleState !== "redacted") {
    throw new MentionAPIError("invalid_response", 502);
  }
  const textValue = body.body;
  if (
    typeof textValue !== "string" ||
    (lifecycleState === "active" && textValue.length === 0) ||
    (lifecycleState === "redacted" && textValue !== "")
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  const text = textValue;
  if (!Array.isArray(body.tokens) || !validDocument(text, tokensValue)) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return {
    id: requiredString(body, "id"),
    parentType: enumValue(body.parent_type, parentTypes),
    parentId: requiredString(body, "parent_id"),
    sourceKind: enumValue(body.source_kind, sourceKinds),
    document: {
      body: text,
      tokens: tokensValue,
      confirmedTeamSnapshots: confirmations,
    },
    authorId: requiredString(body, "author_id"),
    lifecycleState,
    version: integer(body, "version", 1),
    createdAt: requiredString(body, "created_at"),
    updatedAt: requiredString(body, "updated_at"),
    redactedAt: optionalString(body, "redacted_at"),
    readOnly: boolean(body, "read_only"),
    legacy: boolean(body, "legacy"),
  };
}

function exactSource(
  value: unknown,
  confirmations: Record<string, TeamConfirmation>,
  context: {
    parentType: MentionParentType;
    parentId: string;
    sourceKind?: MentionSourceKind;
  },
  sourceId?: string,
): InternalContentSource {
  const mapped = source(value, confirmations);
  if (
    mapped.parentType !== context.parentType ||
    mapped.parentId !== context.parentId ||
    (context.sourceKind && mapped.sourceKind !== context.sourceKind) ||
    (sourceId && mapped.id !== sourceId)
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return mapped;
}

function serializeToken(value: MentionToken) {
  return {
    id: value.id,
    target_type: value.targetType,
    target_id: value.targetId,
    label: value.label,
    start: value.start,
    end: value.end,
  };
}

function serializeConfirmations(
  values: MentionDocument["confirmedTeamSnapshots"],
) {
  return Object.fromEntries(
    Object.entries(values).map(([teamID, confirmation]) => [
      teamID,
      {
        team_version: confirmation.teamVersion,
        eligible_member_ids: [...confirmation.eligibleMemberIds],
      },
    ]),
  );
}

function resource(parentType: MentionParentType): string {
  if (parentType === "work_record") return "work-records";
  return `${parentType}s`;
}

function widgetItem(value: unknown): MentionWidgetItem {
  const body = record(value);
  const preview = optionalString(body, "preview");
  const lastMentionedAt = requiredString(body, "last_mentioned_at");
  if (Number.isNaN(Date.parse(lastMentionedAt))) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return {
    id: requiredString(body, "id"),
    parentType: enumValue(body.parent_type, parentTypes),
    parentId: requiredString(body, "parent_id"),
    parentDisplayId: requiredString(body, "parent_display_id"),
    parentSubject: requiredString(body, "parent_subject"),
    latestOccurrenceId: requiredString(body, "latest_occurrence_id"),
    authorLabel: requiredString(body, "author_label"),
    origin: enumValue(body.origin, origins),
    ...(preview ? { preview } : {}),
    state: enumValue(body.state, itemStates),
    lastMentionedAt,
    version: integer(body, "version", 1),
  };
}

function widgetPage(
  value: unknown,
  expectedState: MentionItemState,
): MentionWidgetPage {
  const body = record(value);
  const countsBody = record(body.counts);
  if (!Array.isArray(body.items) || body.items.length > 50) {
    throw new MentionAPIError("invalid_response", 502);
  }
  const items = body.items.map(widgetItem);
  if (
    items.some((item) => item.state !== expectedState) ||
    items.some(
      (item, index) =>
        index > 0 &&
        (Date.parse(items[index - 1].lastMentionedAt) <
          Date.parse(item.lastMentionedAt) ||
          (items[index - 1].lastMentionedAt === item.lastMentionedAt &&
            items[index - 1].id <= item.id)),
    )
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return {
    counts: {
      unread: integer(countsBody, "unread"),
      read: integer(countsBody, "read"),
      archived: integer(countsBody, "archived"),
    },
    items,
    ...(optionalString(body, "next_cursor")
      ? { nextCursor: optionalString(body, "next_cursor") }
      : {}),
  };
}

function stateResult(
  value: unknown,
  expectedID: string,
  expectedState: MentionItemState,
) {
  const body = record(value);
  const id = requiredString(body, "id");
  const state = enumValue(body.state, itemStates);
  if (id !== expectedID || state !== expectedState) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return { id, state, version: integer(body, "version", 1) };
}

function deepLink(value: unknown): MentionDeepLink {
  const body = record(value);
  const sourceAvailable = boolean(body, "source_available");
  const sourceId = optionalString(body, "source_id");
  const tokenId = optionalString(body, "token_id");
  if (
    (sourceAvailable && (!sourceId || !tokenId)) ||
    (!sourceAvailable && (sourceId !== undefined || tokenId !== undefined))
  ) {
    throw new MentionAPIError("invalid_response", 502);
  }
  return {
    href: requiredString(body, "href"),
    clientId: requiredString(body, "client_id"),
    parentType: enumValue(body.parent_type, parentTypes),
    parentId: requiredString(body, "parent_id"),
    ...(sourceId ? { sourceId } : {}),
    ...(tokenId ? { tokenId } : {}),
    sourceAvailable,
    itemVersion: integer(body, "item_version", 1),
  };
}

export function createMentionAPI(
  fetcher: Fetcher = browserFetcher,
): MentionClientAPI {
  return {
    async list(context, signal) {
      const response = await fetcher(
        `/api/v1/${resource(context.parentType)}/${encodeURIComponent(context.parentId)}/internal-content`,
        {
          credentials: "same-origin",
          headers: clientContextHeaders(context.clientId),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      const values: unknown = await response.json();
      if (!Array.isArray(values)) {
        throw new MentionAPIError("invalid_response", 502);
      }
      return values.map((value) => exactSource(value, {}, context));
    },

    async candidates(context, query, signal, exactTarget) {
      const parameters = new URLSearchParams({
        parent_type: context.parentType,
        parent_id: context.parentId,
        source_kind: context.sourceKind,
        q: query,
      });
      if (exactTarget) {
        parameters.set("target_type", exactTarget.targetType);
        parameters.set("target_id", exactTarget.targetId);
      }
      const response = await fetcher(
        `/api/v1/mentions/candidates?${parameters.toString()}`,
        {
          credentials: "same-origin",
          headers: clientContextHeaders(context.clientId),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      const values: unknown = await response.json();
      if (!Array.isArray(values) || values.length > 50) {
        throw new MentionAPIError("invalid_response", 502);
      }
      return values.map(candidate);
    },

    async save(context, document, options, signal) {
      if (!validDocument(document.body, document.tokens)) {
        throw new MentionAPIError("mention_token_invalid", 422);
      }
      const createPath = `/api/v1/${resource(context.parentType)}/${encodeURIComponent(context.parentId)}/${
        context.sourceKind === "details"
          ? "internal-details"
          : context.sourceKind === "comment"
            ? "internal-comments"
            : "notes"
      }`;
      const editing = Boolean(options.sourceId);
      const payload: RecordValue = {
        body: document.body,
        tokens: document.tokens.map(serializeToken),
        confirmed_team_snapshots: serializeConfirmations(
          document.confirmedTeamSnapshots,
        ),
        expected_version: options.expectedVersion,
        idempotency_key: options.idempotencyKey,
      };
      if (editing) {
        payload.parent_type = context.parentType;
        payload.parent_id = context.parentId;
        payload.source_kind = context.sourceKind;
      }
      const response = await fetcher(
        editing
          ? `/api/v1/internal-content/${encodeURIComponent(options.sourceId!)}`
          : createPath,
        {
          method: editing
            ? "PATCH"
            : context.sourceKind === "details"
              ? "PUT"
              : "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(context.clientId),
            ...csrfHeaders(),
          },
          body: JSON.stringify(payload),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      return exactSource(
        await response.json(),
        document.confirmedTeamSnapshots,
        context,
        options.sourceId,
      );
    },

    async redact(context, sourceId, expectedVersion, idempotencyKey, signal) {
      const response = await fetcher(
        `/api/v1/internal-content/${encodeURIComponent(sourceId)}/redact`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(context.clientId),
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            parent_type: context.parentType,
            parent_id: context.parentId,
            source_kind: context.sourceKind,
            expected_version: expectedVersion,
            idempotency_key: idempotencyKey,
          }),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      return exactSource(await response.json(), {}, context, sourceId);
    },

    async listWidget(state, cursor, limit = 20, signal) {
      const parameters = new URLSearchParams({ state, limit: String(limit) });
      if (cursor) parameters.set("cursor", cursor);
      const response = await fetcher(`/api/v1/mentions/widget?${parameters}`, {
        credentials: "same-origin",
        signal,
      });
      if (!response.ok) return responseError(response);
      return widgetPage(await response.json(), state);
    },

    async changeItemState(itemId, state, expectedVersion, signal) {
      const response = await fetcher(
        `/api/v1/mentions/items/${encodeURIComponent(itemId)}`,
        {
          method: "PATCH",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ state, expected_version: expectedVersion }),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      return stateResult(await response.json(), itemId, state);
    },

    async resolveOccurrence(occurrenceId, itemId, expectedVersion, signal) {
      const response = await fetcher(
        `/api/v1/mentions/occurrences/${encodeURIComponent(occurrenceId)}/resolve`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            item_id: itemId,
            expected_version: expectedVersion,
          }),
          signal,
        },
      );
      if (!response.ok) return responseError(response);
      return deepLink(await response.json());
    },
  };
}
