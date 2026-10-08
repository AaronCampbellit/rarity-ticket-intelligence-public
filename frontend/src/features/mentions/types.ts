export type MentionTargetType = "staff" | "team";
export type MentionParentType = "work_record" | "task" | "project";
export type MentionSourceKind = "details" | "comment" | "note";
export type MentionItemState = "unread" | "read" | "archived";
export type MentionOrigin = "direct" | "team" | "both";

export type MentionToken = {
  id: string;
  targetType: MentionTargetType;
  targetId: string;
  label: string;
  start: number;
  end: number;
};

export type TeamConfirmation = {
  teamVersion: number;
  eligibleMemberIds: string[];
};

export type MentionDocument = {
  body: string;
  tokens: MentionToken[];
  confirmedTeamSnapshots: Record<string, TeamConfirmation>;
};

export type MentionContext = {
  clientId: string;
  parentType: MentionParentType;
  parentId: string;
  sourceKind: MentionSourceKind;
};

export type MentionParentContext = Pick<
  MentionContext,
  "clientId" | "parentType" | "parentId"
>;

type CandidateBase = {
  id: string;
  label: string;
  version: number;
};

export type StaffMentionCandidate = CandidateBase & {
  targetType: "staff";
};

export type TeamMentionCandidate = CandidateBase & {
  targetType: "team";
  eligibleCount: number;
  excludedCount: number;
  eligibleMemberIds: string[];
};

export type MentionCandidate = StaffMentionCandidate | TeamMentionCandidate;

export type MentionSaveOptions = {
  sourceId?: string;
  expectedVersion: number;
  idempotencyKey: string;
};

export type InternalContentSource = {
  id: string;
  parentType: MentionParentType;
  parentId: string;
  sourceKind: MentionSourceKind;
  document: MentionDocument;
  authorId: string;
  lifecycleState: "active" | "redacted";
  version: number;
  createdAt: string;
  updatedAt: string;
  redactedAt?: string;
  readOnly: boolean;
  legacy: boolean;
};

export type MentionAPI = {
  candidates(
    context: MentionContext,
    query: string,
    signal?: AbortSignal,
    exactTarget?: { targetType: "team"; targetId: string },
  ): Promise<MentionCandidate[]>;
  save(
    context: MentionContext,
    document: MentionDocument,
    options: MentionSaveOptions,
    signal?: AbortSignal,
  ): Promise<InternalContentSource>;
};

export type CollaborationAPI = MentionAPI & {
  list(
    context: MentionParentContext,
    signal?: AbortSignal,
  ): Promise<InternalContentSource[]>;
  redact(
    context: MentionContext,
    sourceId: string,
    expectedVersion: number,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<InternalContentSource>;
};

export type MentionStateCounts = {
  unread: number;
  read: number;
  archived: number;
};

export type MentionWidgetItem = {
  id: string;
  parentType: MentionParentType;
  parentId: string;
  parentDisplayId: string;
  parentSubject: string;
  latestOccurrenceId: string;
  authorLabel: string;
  origin: MentionOrigin;
  preview?: string;
  state: MentionItemState;
  lastMentionedAt: string;
  version: number;
};

export type MentionWidgetPage = {
  counts: MentionStateCounts;
  items: MentionWidgetItem[];
  nextCursor?: string;
};

export type MentionStateResult = {
  id: string;
  state: MentionItemState;
  version: number;
};

export type MentionDeepLink = {
  href: string;
  clientId: string;
  parentType: MentionParentType;
  parentId: string;
  sourceId?: string;
  tokenId?: string;
  sourceAvailable: boolean;
  itemVersion: number;
};

export type MentionWidgetAPI = {
  listWidget(
    state: MentionItemState,
    cursor?: string,
    limit?: number,
    signal?: AbortSignal,
  ): Promise<MentionWidgetPage>;
  changeItemState(
    itemId: string,
    state: MentionItemState,
    expectedVersion: number,
    signal?: AbortSignal,
  ): Promise<MentionStateResult>;
  resolveOccurrence(
    occurrenceId: string,
    itemId: string,
    expectedVersion: number,
    signal?: AbortSignal,
  ): Promise<MentionDeepLink>;
};

export type MentionClientAPI = CollaborationAPI & MentionWidgetAPI;
