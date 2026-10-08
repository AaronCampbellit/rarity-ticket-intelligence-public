import { csrfHeaders } from "../../api/browserSession";
import type {
  ClassificationAPI,
  ClassificationAdminAPI,
  ClassificationAIPolicy,
  ClassificationCatalog,
  ClassificationSuggestion,
  ClassificationHealth,
  ClassificationImpact,
  ClassificationMigrationRun,
  ClassificationReport,
  ClassificationReportFilter,
  ClassificationReportKind,
  ReplaceDirectInput,
  Tag,
  TagAssignment,
  TagGroup,
  TagHistoryEntry,
  TagSuggestion,
  TagObjectType,
  TagSource,
  TagState,
  TagTarget,
  TaggedObject,
} from "./types";

type Fetcher = typeof fetch;
type RecordValue = Record<string, unknown>;

const objectTypes = new Set<TagObjectType>([
  "work_record",
  "task",
  "project",
  "asset",
  "knowledge_article",
  "time_entry",
]);
const states = new Set<TagState>(["active", "merged", "archived"]);
const sources = new Set<TagSource>([
  "human",
  "ai_confirmed",
  "ai_automatic",
  "automation",
  "integration",
  "migration",
  "system_fallback",
]);

export class ClassificationAPIError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    message = "Classification request failed",
    public readonly recoveryURL?: string,
  ) {
    super(message);
    this.name = "ClassificationAPIError";
  }
}

/** Preserves structured API failures for governed create and terminal actions. */
export async function classificationResponseError(
  response: Response,
  fallback = "request_failed",
): Promise<never> {
  const body = (await response.json().catch(() => ({}))) as {
    error?: { code?: unknown; message?: unknown; recovery_url?: unknown };
  };
  const detail = body.error;
  throw new ClassificationAPIError(
    typeof detail?.code === "string" ? detail.code : fallback,
    response.status,
    typeof detail?.message === "string"
      ? detail.message
      : "Classification request failed",
    typeof detail?.recovery_url === "string" ? detail.recovery_url : undefined,
  );
}

export function isClassificationCreateError(cause: unknown) {
  const code =
    cause instanceof ClassificationAPIError
      ? cause.code
      : typeof cause === "object" && cause && "code" in cause
        ? String(cause.code)
        : cause instanceof Error
          ? cause.message
          : "";
  return ["classification_required", "tag_archived", "tag_ambiguous"].includes(
    code,
  );
}

function record(value: unknown): RecordValue {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return value as RecordValue;
}

function string(value: RecordValue, name: string): string {
  if (typeof value[name] !== "string") {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return value[name];
}

function optionalString(value: RecordValue, name: string): string | undefined {
  const item = value[name];
  if (item === undefined || item === null) return undefined;
  if (typeof item !== "string")
    throw new ClassificationAPIError("invalid_response", 502);
  return item;
}

function number(value: RecordValue, name: string): number {
  const item = value[name];
  if (typeof item !== "number" || !Number.isFinite(item)) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return item;
}

function optionalBoolean(
  value: RecordValue,
  name: string,
): boolean | undefined {
  const item = value[name];
  if (item === undefined || item === null) return undefined;
  if (typeof item !== "boolean")
    throw new ClassificationAPIError("invalid_response", 502);
  return item;
}

function requiredBoolean(value: RecordValue, name: string): boolean {
  if (typeof value[name] !== "boolean") {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return value[name];
}

function strings(value: unknown): string[] {
  if (
    !Array.isArray(value) ||
    !value.every((item) => typeof item === "string")
  ) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return [...value];
}

function objectType(value: unknown): TagObjectType {
  if (typeof value !== "string" || !objectTypes.has(value as TagObjectType)) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return value as TagObjectType;
}

function tag(value: unknown): Tag {
  const body = record(value);
  const state = string(body, "state");
  if (!states.has(state as TagState))
    throw new ClassificationAPIError("invalid_response", 502);
  return {
    id: string(body, "id"),
    label: string(body, "label"),
    groupId: string(body, "group_id"),
    state: state as TagState,
    synonyms: strings(body.synonyms),
    version: number(body, "version"),
    description: optionalString(body, "description"),
    color: optionalString(body, "color"),
    systemManaged: optionalBoolean(body, "system_managed"),
    systemFallback:
      optionalString(body, "internal_key") === "taxonomy.system.unclassified",
    mergedIntoTagId: optionalString(body, "merged_into_tag_id"),
  };
}

function group(value: unknown): TagGroup {
  const body = record(value);
  const state = optionalString(body, "state");
  if (state !== undefined && state !== "active" && state !== "archived") {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  return {
    id: string(body, "id"),
    label: string(body, "label"),
    description: optionalString(body, "description") ?? "",
    position:
      typeof body.position === "number" ? number(body, "position") : undefined,
    state: state as TagGroup["state"],
    systemManaged: optionalBoolean(body, "system_managed"),
    version:
      typeof body.version === "number" ? number(body, "version") : undefined,
  };
}

function assignment(value: unknown): TagAssignment {
  const body = record(value);
  const source = string(body, "source");
  if (
    !sources.has(source as TagSource) ||
    typeof body.inherited !== "boolean"
  ) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  const sourceObjectType =
    body.source_object_type === undefined || body.source_object_type === ""
      ? undefined
      : objectType(body.source_object_type);
  return {
    id: string(body, "id"),
    tag: tag(body.tag),
    source: source as TagSource,
    assignedAt: optionalString(body, "assigned_at"),
    assignedBy: optionalString(body, "assigned_by"),
    inherited: body.inherited,
    sourceObjectType,
    sourceObjectId: optionalString(body, "source_object_id"),
  };
}

function target(value: unknown): TagTarget {
  const body = record(value);
  return {
    objectType: objectType(body.object_type),
    objectId: string(body, "object_id"),
  };
}

function impact(value: unknown): ClassificationImpact {
  const body = record(value);
  const operation = string(body, "operation");
  if (!["rename", "move", "merge", "archive"].includes(operation)) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  const fallback = record(body.fallback_by_object_type ?? {});
  return {
    operation: operation as ClassificationImpact["operation"],
    tagId: string(body, "tag_id"),
    replacementTagId: optionalString(body, "replacement_tag_id"),
    affectedObjects: number(body, "affected_objects"),
    affectedSavedViews: number(body, "affected_saved_views"),
    affectedReports: number(body, "affected_reports"),
    affectedAutomations: number(body, "affected_automations"),
    fallbackByObjectType: Object.fromEntries(
      Object.entries(fallback).map(([key, count]) => [
        objectType(key),
        number({ count }, "count"),
      ]),
    ),
  };
}

function health(value: unknown): ClassificationHealth {
  const types = record(record(value).by_object_type ?? {});
  return {
    byObjectType: Object.fromEntries(
      Object.entries(types).map(([key, entry]) => {
        const body = record(entry);
        return [
          objectType(key),
          {
            meaningful: number(body, "meaningful"),
            unclassified: number(body, "unclassified"),
            archiveFallback: number(body, "archive_fallback"),
          },
        ];
      }),
    ),
  };
}

function migrationHistory(value: unknown): ClassificationMigrationRun[] {
  if (!Array.isArray(value))
    throw new ClassificationAPIError("invalid_response", 502);
  return value.map((entry) => {
    const body = record(entry);
    return {
      id: string(body, "id"),
      status: string(body, "status"),
      startedAt: string(body, "started_at"),
      completedAt: optionalString(body, "completed_at"),
      rowsDiscovered: number(body, "rows_discovered"),
      rowsMigrated: number(body, "rows_migrated"),
      fallbackAssignments: number(body, "fallback_assignments"),
      categorySourcePresent: requiredBoolean(body, "category_source_present"),
    };
  });
}

function taggedObject(value: unknown): TaggedObject {
  const body = record(value);
  const state = string(body, "classification_state");
  if (
    state !== "classified" &&
    state !== "unclassified" &&
    state !== "missing"
  ) {
    throw new ClassificationAPIError("invalid_response", 502);
  }
  const assignments = (name: string) => {
    if (!Array.isArray(body[name]))
      throw new ClassificationAPIError("invalid_response", 502);
    return body[name].map(assignment);
  };
  return {
    target: target(body.target),
    objectVersion: number(body, "object_version"),
    direct: assignments("direct"),
    inherited: assignments("inherited"),
    effective: assignments("effective"),
    classificationState: state,
  };
}

function history(value: unknown): TagHistoryEntry[] {
  if (!Array.isArray(value))
    throw new ClassificationAPIError("invalid_response", 502);
  return value.map((item) => {
    const body = record(item);
    const operation = string(body, "operation");
    if (
      (operation !== "added" && operation !== "removed") ||
      typeof body.inherited !== "boolean"
    ) {
      throw new ClassificationAPIError("invalid_response", 502);
    }
    return {
      id: string(body, "id"),
      operation,
      assignment: assignment(body.assignment),
      targetVersion: number(body, "target_version"),
      occurredAt: string(body, "occurred_at"),
      actorId: optionalString(body, "actor_id"),
      inherited: body.inherited,
      sourceObjectType:
        body.source_object_type === undefined || body.source_object_type === ""
          ? undefined
          : objectType(body.source_object_type),
      sourceObjectId: optionalString(body, "source_object_id"),
    };
  });
}

function classificationSuggestion(value: unknown): ClassificationSuggestion {
  const body = record(value);
  const raw = Array.isArray(body.suggestions) ? body.suggestions : body.items;
  if (!Array.isArray(raw))
    throw new ClassificationAPIError("invalid_response", 502);
  const suggestions: TagSuggestion[] = raw.map((value) => {
    const item = record(value);
    return {
      id: optionalString(item, "id") ?? string(item, "tag_id"),
      tagId: string(item, "tag_id"),
      confidence:
        typeof item.confidence === "number"
          ? number(item, "confidence")
          : undefined,
      rationale: optionalString(item, "rationale"),
      disposition: optionalString(
        item,
        "disposition",
      ) as TagSuggestion["disposition"],
    };
  });
  return {
    id: string(body, "id"),
    status: string(body, "status"),
    version: number(body, "version"),
    suggestions,
  };
}

function aiPolicy(value: unknown): ClassificationAIPolicy {
  const body = record(value);
  const options = Array.isArray(body.model_options) ? body.model_options : [];
  return {
    automaticApplyEnabled: Boolean(body.automatic_apply_enabled),
    automaticApplyThreshold: number(body, "automatic_apply_threshold"),
    modelProfileId: optionalString(body, "model_profile_id") ?? "",
    version: number(body, "version"),
    modelOptions: options.map((value) => {
      const option = record(value);
      return { id: string(option, "id"), label: string(option, "label") };
    }),
    retainedRate:
      typeof body.retained_rate === "number" ? body.retained_rate : undefined,
    changeRate:
      typeof body.change_rate === "number" ? body.change_rate : undefined,
    providerFailureHealth:
      optionalString(body, "provider_failure_health") ?? "unknown",
  };
}

function classificationReport(value: unknown): ClassificationReport {
  const body = record(value);
  const kind = string(body, "kind") as ClassificationReportKind;
  if (!Array.isArray(body.rows))
    throw new ClassificationAPIError("invalid_response", 502);
  return {
    kind,
    rows: body.rows.map((value) => {
      const row = record(value);
      return {
        tagId: optionalString(row, "tag_id"),
        leftTagId: optionalString(row, "left_tag_id"),
        rightTagId: optionalString(row, "right_tag_id"),
        objectType: row.object_type ? objectType(row.object_type) : undefined,
        date: optionalString(row, "date"),
        count: number(row, "count"),
        addedCount:
          typeof row.added_count === "number"
            ? number(row, "added_count")
            : undefined,
        removedCount:
          typeof row.removed_count === "number"
            ? number(row, "removed_count")
            : undefined,
        activeCount:
          typeof row.active_count === "number"
            ? number(row, "active_count")
            : undefined,
        ageSeconds:
          typeof row.age_seconds === "number"
            ? number(row, "age_seconds")
            : undefined,
        source: optionalString(row, "source"),
        objectIds:
          row.object_ids === undefined ? undefined : strings(row.object_ids),
        evidenceCursor: optionalString(row, "evidence_cursor"),
      };
    }),
    nextCursor: optionalString(body, "next_cursor"),
    projectionAsOf: string(body, "projection_as_of"),
  };
}

function classificationEvidence(value: unknown) {
  const body = record(value);
  if (!Array.isArray(body.items))
    throw new ClassificationAPIError("invalid_response", 502);
  return {
    items: body.items.map((value) => {
      const item = record(value);
      return {
        objectType: objectType(item.object_type),
        objectId: string(item, "object_id"),
        label: string(item, "label"),
        clientId: optionalString(item, "client_id"),
        parentObjectId: optionalString(item, "parent_object_id"),
      };
    }),
    nextCursor: optionalString(body, "next_cursor"),
    projectionAsOf: string(body, "projection_as_of"),
  };
}

function reportQuery(filter: ClassificationReportFilter) {
  const query = new URLSearchParams({ from: filter.from, to: filter.to });
  const values: Array<[string, string | undefined]> = [
    ["object_type", filter.objectType],
    ["group_id", filter.groupID],
    ["match", filter.match],
    ["source", filter.source],
    ["inheritance", filter.inheritance],
    ["technician_id", filter.technicianID],
    ["team_id", filter.teamID],
    ["priority", filter.priority],
    ["status", filter.status],
    ["cursor", filter.cursor],
    ["limit", filter.limit?.toString()],
  ];
  values.forEach(([name, value]) => {
    if (value) query.set(name, value);
  });
  if (filter.tagIDs?.length) query.set("tag_ids", filter.tagIDs.join(","));
  return query;
}

async function request(
  fetcher: Fetcher,
  path: string,
  init: RequestInit = {},
): Promise<{ body: unknown; response: Response }> {
  const response = await fetcher(path, { credentials: "same-origin", ...init });
  const body: unknown = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error =
      body && typeof body === "object" && !Array.isArray(body)
        ? (body as RecordValue).error
        : undefined;
    const detail =
      error && typeof error === "object" && !Array.isArray(error)
        ? (error as RecordValue)
        : {};
    throw new ClassificationAPIError(
      typeof detail.code === "string" ? detail.code : "request_failed",
      response.status,
      typeof detail.message === "string"
        ? detail.message
        : "Classification request failed",
      typeof detail.recovery_url === "string" ? detail.recovery_url : undefined,
    );
  }
  return { body, response };
}

function pathFor(target: TagTarget, suffix = "tags") {
  return `/api/v1/objects/${encodeURIComponent(target.objectType)}/${encodeURIComponent(target.objectId)}/${suffix}`;
}

export function createClassificationAPI(
  fetcher: Fetcher = fetch,
  clientID = "",
): ClassificationAPI {
  const scoped = (headers: HeadersInit = {}) => ({
    ...headers,
    ...(clientID ? { "X-Rarity-Client-ID": clientID } : {}),
  });
  return {
    async catalog(signal) {
      const [groups, tags] = await Promise.all([
        request(fetcher, "/api/v1/tag-groups", { signal, headers: scoped() }),
        request(fetcher, "/api/v1/tags", { signal, headers: scoped() }),
      ]);
      if (!Array.isArray(groups.body) || !Array.isArray(tags.body)) {
        throw new ClassificationAPIError("invalid_response", 502);
      }
      return {
        groups: groups.body.map(group),
        tags: tags.body.map(tag),
      } satisfies ClassificationCatalog;
    },
    async object(objectTarget, signal) {
      const result = await request(fetcher, pathFor(objectTarget), {
        signal,
        headers: scoped(),
      });
      return taggedObject(result.body);
    },
    async replaceDirect(objectTarget, input: ReplaceDirectInput, signal) {
      const result = await request(fetcher, pathFor(objectTarget), {
        method: "PUT",
        signal,
        headers: {
          "Content-Type": "application/json",
          "If-Match": `"${input.expectedVersion}"`,
          ...scoped(csrfHeaders()),
        },
        body: JSON.stringify({
          tag_ids: input.tagIDs,
          reason: input.reason,
          idempotency_key: input.idempotencyKey,
        }),
      });
      return taggedObject(result.body);
    },
    async history(objectTarget, signal) {
      const result = await request(
        fetcher,
        pathFor(objectTarget, "tag-history"),
        { signal, headers: scoped() },
      );
      return history(result.body);
    },
    async requestSuggestions(objectTarget, signal) {
      const result = await request(
        fetcher,
        pathFor(objectTarget, "classification-suggestions"),
        { method: "POST", signal, headers: scoped(csrfHeaders()) },
      );
      return classificationSuggestion(result.body);
    },
    async suggestion(id, signal) {
      const result = await request(
        fetcher,
        `/api/v1/classification-suggestions/${encodeURIComponent(id)}`,
        { signal, headers: scoped() },
      );
      return classificationSuggestion(result.body);
    },
    async decideSuggestion(id, tagId, decision, signal) {
      const result = await request(
        fetcher,
        `/api/v1/classification-suggestions/${encodeURIComponent(id)}/decide`,
        {
          method: "POST",
          signal,
          headers: {
            "Content-Type": "application/json",
            ...scoped(csrfHeaders()),
          },
          body: JSON.stringify({ tag_id: tagId, decision }),
        },
      );
      return classificationSuggestion(result.body);
    },
  };
}

export const classificationAPI = createClassificationAPI();

function adminHeaders(version?: number) {
  return {
    "Content-Type": "application/json",
    ...(version === undefined ? {} : { "If-Match": `"${version}"` }),
    ...csrfHeaders(),
  };
}

export function createClassificationAdminAPI(
  fetcher: Fetcher = fetch,
): ClassificationAdminAPI {
  const catalog = async (signal?: AbortSignal) =>
    createClassificationAPI(fetcher).catalog(signal);
  return {
    catalog,
    async createGroup(input) {
      const { body } = await request(fetcher, "/api/v1/tag-groups", {
        method: "POST",
        headers: adminHeaders(),
        body: JSON.stringify({
          label: input.label,
          description: input.description,
          position: input.position,
        }),
      });
      return group(body);
    },
    async updateGroup(id, input) {
      const { body } = await request(
        fetcher,
        `/api/v1/tag-groups/${encodeURIComponent(id)}`,
        {
          method: "PATCH",
          headers: adminHeaders(input.expectedVersion),
          body: JSON.stringify({
            label: input.label,
            description: input.description,
            position: input.position,
            state: input.state,
            expected_version: input.expectedVersion,
          }),
        },
      );
      return group(body);
    },
    async createTag(input) {
      const { body } = await request(fetcher, "/api/v1/tags", {
        method: "POST",
        headers: adminHeaders(),
        body: JSON.stringify({
          group_id: input.groupId,
          label: input.label,
          description: input.description,
          color: input.color,
          synonyms: input.synonyms,
        }),
      });
      return tag(body);
    },
    async updateTag(id, input) {
      const { body } = await request(
        fetcher,
        `/api/v1/tags/${encodeURIComponent(id)}`,
        {
          method: "PATCH",
          headers: adminHeaders(input.expectedVersion),
          body: JSON.stringify({
            group_id: input.groupId,
            label: input.label,
            description: input.description,
            color: input.color,
            synonyms: input.synonyms,
            expected_version: input.expectedVersion,
          }),
        },
      );
      return tag(body);
    },
    async impact(id, operation, replacementTagId) {
      const query = new URLSearchParams({ operation });
      if (replacementTagId) query.set("replacement_tag_id", replacementTagId);
      const { body } = await request(
        fetcher,
        `/api/v1/tags/${encodeURIComponent(id)}/impact?${query.toString()}`,
      );
      return impact(body);
    },
    async merge(id, input) {
      const { body } = await request(
        fetcher,
        `/api/v1/tags/${encodeURIComponent(id)}/merge`,
        {
          method: "POST",
          headers: adminHeaders(input.expectedVersion),
          body: JSON.stringify({
            survivor_tag_id: input.survivorTagId,
            reason: input.reason,
            expected_version: input.expectedVersion,
          }),
        },
      );
      return tag(body);
    },
    async archive(id, input) {
      const { body } = await request(
        fetcher,
        `/api/v1/tags/${encodeURIComponent(id)}/archive`,
        {
          method: "POST",
          headers: adminHeaders(input.expectedVersion),
          body: JSON.stringify({
            replacement_tag_id: input.replacementTagId,
            reason: input.reason,
            expected_version: input.expectedVersion,
          }),
        },
      );
      return tag(body);
    },
    async health(clientID, signal) {
      const { body } = await request(fetcher, "/api/v1/classification/health", {
        signal,
        headers: { "X-Rarity-Client-ID": clientID },
      });
      return health(body);
    },
    async migrationHistory(signal) {
      const { body } = await request(
        fetcher,
        "/api/v1/classification/migration-runs",
        { signal },
      );
      return migrationHistory(body);
    },
    async report(kind, filter, signal) {
      const query = reportQuery(filter);
      const { body } = await request(
        fetcher,
        `/api/v1/tag-reports/${encodeURIComponent(kind)}?${query.toString()}`,
        { signal, headers: { "X-Rarity-Client-ID": filter.clientID } },
      );
      return classificationReport(body);
    },
    async reportEvidence(tagID, filter, signal) {
      const query = reportQuery(filter);
      const { body } = await request(
        fetcher,
        `/api/v1/tag-reports/recurring-issues/${encodeURIComponent(tagID)}/evidence?${query.toString()}`,
        { signal, headers: { "X-Rarity-Client-ID": filter.clientID } },
      );
      return classificationEvidence(body);
    },
    async reportTechnicians(clientID, signal) {
      const { body } = await request(
        fetcher,
        "/api/v1/tag-reports/options/technicians",
        { signal, headers: { "X-Rarity-Client-ID": clientID } },
      );
      if (!Array.isArray(body))
        throw new ClassificationAPIError("invalid_response", 502);
      return body.map((value) => {
        const item = record(value);
        return { id: string(item, "id"), label: string(item, "label") };
      });
    },
    async aiPolicy(signal) {
      const { body } = await request(
        fetcher,
        "/api/v1/classification/ai-policy",
        { signal },
      );
      return aiPolicy(body);
    },
    async updateAIPolicy(input) {
      const { body } = await request(
        fetcher,
        "/api/v1/classification/ai-policy",
        {
          method: "PATCH",
          headers: adminHeaders(input.expectedVersion),
          body: JSON.stringify({
            automatic_apply_enabled: input.automaticApplyEnabled,
            automatic_apply_threshold: input.automaticApplyThreshold,
            model_profile_id: input.modelProfileId,
            expected_version: input.expectedVersion,
          }),
        },
      );
      return aiPolicy(body);
    },
  };
}

export const classificationAdminAPI = createClassificationAdminAPI();
