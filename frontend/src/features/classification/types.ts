export type TagObjectType =
  | "work_record"
  | "task"
  | "project"
  | "asset"
  | "knowledge_article"
  | "time_entry";

export type TagState = "active" | "merged" | "archived";
export type TagSource =
  | "human"
  | "ai_confirmed"
  | "ai_automatic"
  | "automation"
  | "integration"
  | "migration"
  | "system_fallback";

/** A visible catalog tag. Internal catalog identifiers intentionally stay server-side. */
export type Tag = {
  id: string;
  label: string;
  groupId: string;
  state: TagState;
  synonyms: string[];
  version: number;
  description?: string;
  color?: string;
  systemManaged?: boolean;
  /** Immutable server identity, never rendered. */
  systemFallback?: boolean;
  mergedIntoTagId?: string;
};

export type TagGroup = {
  id: string;
  label: string;
  description: string;
  position?: number;
  state?: "active" | "archived";
  systemManaged?: boolean;
  version?: number;
};

export type TagSuggestion = {
  id: string;
  tagId: string;
  confidence?: number;
  rationale?: string;
  disposition?: "suggested" | "accepted" | "rejected" | "automatically_applied";
};

export type ClassificationSuggestion = {
  id: string;
  status: string;
  version: number;
  suggestions: TagSuggestion[];
};

export type ClassificationAIPolicy = {
  automaticApplyEnabled: boolean;
  automaticApplyThreshold: number;
  modelProfileId: string;
  version: number;
  modelOptions: { id: string; label: string }[];
  retainedRate?: number;
  changeRate?: number;
  providerFailureHealth: string;
};

export type TagTarget = {
  objectType: TagObjectType;
  objectId: string;
};

export type TagAssignment = {
  id: string;
  tag: Tag;
  source: TagSource;
  assignedAt?: string;
  assignedBy?: string;
  inherited: boolean;
  sourceObjectType?: TagObjectType;
  sourceObjectId?: string;
};

export type TaggedObject = {
  target: TagTarget;
  objectVersion: number;
  direct: TagAssignment[];
  inherited: TagAssignment[];
  effective: TagAssignment[];
  classificationState: "classified" | "unclassified" | "missing";
};

export type TagHistoryEntry = {
  id: string;
  operation: "added" | "removed";
  assignment: TagAssignment;
  targetVersion: number;
  occurredAt: string;
  actorId?: string;
  inherited: boolean;
  sourceObjectType?: TagObjectType;
  sourceObjectId?: string;
};

export type ClassificationCatalog = { groups: TagGroup[]; tags: Tag[] };

export type ClassificationImpact = {
  operation: "rename" | "move" | "merge" | "archive";
  tagId: string;
  replacementTagId?: string;
  affectedObjects: number;
  affectedSavedViews: number;
  affectedReports: number;
  affectedAutomations: number;
  fallbackByObjectType: Partial<Record<TagObjectType, number>>;
};

export type ClassificationHealth = {
  byObjectType: Partial<
    Record<
      TagObjectType,
      { meaningful: number; unclassified: number; archiveFallback: number }
    >
  >;
};

export type ClassificationMigrationRun = {
  id: string;
  status: string;
  startedAt: string;
  completedAt?: string;
  rowsDiscovered: number;
  rowsMigrated: number;
  fallbackAssignments: number;
  categorySourcePresent: boolean;
};

export type ClassificationReportKind =
  | "usage"
  | "combinations"
  | "trends"
  | "classification-health"
  | "recurring-issues";

export type ClassificationReportFilter = {
  clientID: string;
  objectType?: TagObjectType;
  groupID?: string;
  tagIDs?: string[];
  match?: "any" | "all" | "none";
  source?: TagSource;
  inheritance?: "all" | "direct" | "inherited";
  technicianID?: string;
  teamID?: string;
  priority?: string;
  status?: string;
  from: string;
  to: string;
  cursor?: string;
  limit?: number;
};

export type ClassificationReportRow = {
  tagId?: string;
  leftTagId?: string;
  rightTagId?: string;
  objectType?: TagObjectType;
  date?: string;
  count: number;
  addedCount?: number;
  removedCount?: number;
  activeCount?: number;
  ageSeconds?: number;
  source?: string;
  objectIds?: string[];
  objectRefs?: Array<{
    objectType: TagObjectType;
    objectId: string;
    label: string;
    clientId?: string;
    parentObjectId?: string;
  }>;
  evidenceCursor?: string;
};

export type ClassificationReport = {
  kind: ClassificationReportKind;
  rows: ClassificationReportRow[];
  nextCursor?: string;
  projectionAsOf: string;
};
export type ClassificationEvidencePage = {
  items: Array<{
    objectType: TagObjectType;
    objectId: string;
    label: string;
    clientId?: string;
    parentObjectId?: string;
  }>;
  nextCursor?: string;
  projectionAsOf: string;
};

export type ClassificationAdminAPI = {
  catalog(signal?: AbortSignal): Promise<ClassificationCatalog>;
  createGroup(input: {
    label: string;
    description: string;
    position: number;
  }): Promise<TagGroup>;
  updateGroup(
    id: string,
    input: {
      label: string;
      description: string;
      position: number;
      state: "active" | "archived";
      expectedVersion: number;
    },
  ): Promise<TagGroup>;
  createTag(input: {
    groupId: string;
    label: string;
    description: string;
    color: string;
    synonyms: string[];
  }): Promise<Tag>;
  updateTag(
    id: string,
    input: {
      groupId: string;
      label: string;
      description: string;
      color: string;
      synonyms: string[];
      expectedVersion: number;
    },
  ): Promise<Tag>;
  impact(
    id: string,
    operation: ClassificationImpact["operation"],
    replacementTagId?: string,
  ): Promise<ClassificationImpact>;
  merge(
    id: string,
    input: { survivorTagId: string; reason: string; expectedVersion: number },
  ): Promise<Tag>;
  archive(
    id: string,
    input: {
      replacementTagId: string;
      reason: string;
      expectedVersion: number;
    },
  ): Promise<Tag>;
  health(clientID: string, signal?: AbortSignal): Promise<ClassificationHealth>;
  migrationHistory(signal?: AbortSignal): Promise<ClassificationMigrationRun[]>;
  report?(
    kind: ClassificationReportKind,
    filter: ClassificationReportFilter,
    signal?: AbortSignal,
  ): Promise<ClassificationReport>;
  reportEvidence?(
    tagID: string,
    filter: ClassificationReportFilter,
    signal?: AbortSignal,
  ): Promise<ClassificationEvidencePage>;
  reportTechnicians?(
    clientID: string,
    signal?: AbortSignal,
  ): Promise<Array<{ id: string; label: string }>>;
  aiPolicy?(signal?: AbortSignal): Promise<ClassificationAIPolicy>;
  updateAIPolicy?(input: {
    automaticApplyEnabled: boolean;
    automaticApplyThreshold: number;
    modelProfileId: string;
    expectedVersion: number;
  }): Promise<ClassificationAIPolicy>;
};

export type ReplaceDirectInput = {
  tagIDs: string[];
  expectedVersion: number;
  reason: string;
  idempotencyKey: string;
};

export type ClassificationAPI = {
  catalog(signal?: AbortSignal): Promise<ClassificationCatalog>;
  object(target: TagTarget, signal?: AbortSignal): Promise<TaggedObject>;
  replaceDirect(
    target: TagTarget,
    input: ReplaceDirectInput,
    signal?: AbortSignal,
  ): Promise<TaggedObject>;
  history(target: TagTarget, signal?: AbortSignal): Promise<TagHistoryEntry[]>;
  requestSuggestions?(
    target: TagTarget,
    signal?: AbortSignal,
  ): Promise<ClassificationSuggestion>;
  suggestion?(
    id: string,
    signal?: AbortSignal,
  ): Promise<ClassificationSuggestion>;
  decideSuggestion?(
    id: string,
    tagId: string,
    decision: "accepted" | "dismissed",
    signal?: AbortSignal,
  ): Promise<ClassificationSuggestion>;
};
