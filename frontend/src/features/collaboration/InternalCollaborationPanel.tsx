import { type ReactNode, useEffect, useId, useRef, useState } from "react";

import { MentionAPIError, createMentionAPI } from "../mentions/api";
import { mentionNavigationFromHash } from "../../navigation";
import { MentionEditor } from "../mentions/MentionEditor";
import type {
  CollaborationAPI,
  InternalContentSource,
  MentionContext,
  MentionDeepLink,
  MentionDocument,
  MentionParentType,
  MentionSourceKind,
  TeamMentionCandidate,
} from "../mentions/types";
import "./collaboration.css";

const defaultAPI = createMentionAPI();
const noCapabilities = new Set<string>();

const blankDocument = (): MentionDocument => ({
  body: "",
  tokens: [],
  confirmedTeamSnapshots: {},
});

function copyDocument(document: MentionDocument): MentionDocument {
  return {
    body: document.body,
    tokens: document.tokens.map((token) => ({ ...token })),
    confirmedTeamSnapshots: Object.fromEntries(
      Object.entries(document.confirmedTeamSnapshots).map(
        ([teamID, snapshot]) => [
          teamID,
          {
            teamVersion: snapshot.teamVersion,
            eligibleMemberIds: [...snapshot.eligibleMemberIds],
          },
        ],
      ),
    ),
  };
}

function sourceLabel(kind: MentionSourceKind) {
  if (kind === "comment") return "comment";
  if (kind === "note") return "note";
  return "details";
}

function draftKey(
  clientId: string,
  parentType: MentionParentType,
  parentId: string,
  sourceKind: MentionSourceKind,
) {
  return `${clientId}:${parentType}:${parentId}:${sourceKind}`;
}

type Drafts = Record<MentionSourceKind, MentionDocument>;
type EditSessions = Partial<Record<"comment" | "note", string>>;
type EditOperation = { sourceId: string; expectedVersion: number };
type EditOperations = Partial<Record<"comment" | "note", EditOperation>>;

type VersionConflict = {
  kind: MentionSourceKind;
  sourceId?: string;
  draft: MentionDocument;
  server?: InternalContentSource;
};

type TeamRecovery = {
  kind: MentionSourceKind;
  draft: MentionDocument;
  teams: Array<{
    id: string;
    label: string;
    previousEligible: number;
    current?: TeamMentionCandidate;
  }>;
};

export type InternalCollaborationPanelProps = {
  clientId: string;
  parentType: MentionParentType;
  parentId: string;
  authorId: string;
  api?: CollaborationAPI;
  authenticatedMentionLink?: MentionDeepLink;
  capabilities?: ReadonlySet<string>;
  canEditParent?: boolean;
};

function documentContent(document: MentionDocument) {
  const tokens = [...document.tokens].sort(
    (left, right) => left.start - right.start || left.end - right.end,
  );
  const content: ReactNode[] = [];
  let cursor = 0;
  for (const token of tokens) {
    if (
      !Number.isInteger(token.start) ||
      !Number.isInteger(token.end) ||
      token.start < cursor ||
      token.end <= token.start ||
      token.end > document.body.length ||
      document.body.slice(token.start, token.end) !== token.label
    ) {
      return document.body;
    }
    if (token.start > cursor) {
      content.push(document.body.slice(cursor, token.start));
    }
    content.push(
      <span
        key={token.id}
        className="internal-collaboration__mention-token"
        data-mention-token-id={token.id}
        tabIndex={-1}
      >
        {document.body.slice(token.start, token.end)}
      </span>,
    );
    cursor = token.end;
  }
  if (cursor < document.body.length) content.push(document.body.slice(cursor));
  return content.length ? content : document.body;
}

function SourceCard({
  source,
  onEdit,
  onRedact,
}: {
  source: InternalContentSource;
  onEdit?: () => void;
  onRedact?: () => void;
}) {
  const label = sourceLabel(source.sourceKind);
  if (source.lifecycleState === "redacted") {
    return (
      <article className="internal-collaboration__source is-redacted">
        <p>Redacted internal {label}</p>
        <small>Content is unavailable · Version {source.version}</small>
      </article>
    );
  }
  return (
    <article
      className="internal-collaboration__source"
      data-mention-source-id={source.id}
      tabIndex={-1}
    >
      <p>{documentContent(source.document)}</p>
      <footer>
        <small>
          {source.legacy
            ? `Legacy internal ${label} · Read only`
            : source.version > 1
              ? `Edited · Version ${source.version}`
              : `Version ${source.version}`}
        </small>
        {!source.readOnly && !source.legacy ? (
          <span>
            {onEdit ? (
              <button type="button" onClick={onEdit}>
                Edit internal {label}
              </button>
            ) : null}
            {onRedact ? (
              <button type="button" onClick={onRedact}>
                Redact internal {label}
              </button>
            ) : null}
          </span>
        ) : null}
      </footer>
    </article>
  );
}

export function InternalCollaborationPanel({
  clientId,
  parentType,
  parentId,
  authorId,
  api = defaultAPI,
  authenticatedMentionLink,
  capabilities = noCapabilities,
  canEditParent = false,
}: InternalCollaborationPanelProps) {
  const panelID = useId();
  const parentKey = `${clientId}:${parentType}:${parentId}`;
  const draftStore = useRef(new Map<string, MentionDocument>());
  const editStore = useRef(new Map<string, EditOperation>());
  const activeController = useRef<AbortController | undefined>(undefined);
  const [sources, setSources] = useState<InternalContentSource[]>([]);
  const [sourcesContextKey, setSourcesContextKey] = useState("");
  const [draftContextKey, setDraftContextKey] = useState(parentKey);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [drafts, setDrafts] = useState<Drafts>({
    details: blankDocument(),
    comment: blankDocument(),
    note: blankDocument(),
  });
  const [editing, setEditing] = useState<EditSessions>({});
  const [editOperations, setEditOperations] = useState<EditOperations>({});
  const [savingKind, setSavingKind] = useState<MentionSourceKind>();
  const [errors, setErrors] = useState<
    Partial<Record<MentionSourceKind, string>>
  >({});
  const [versionConflict, setVersionConflict] = useState<VersionConflict>();
  const [teamRecovery, setTeamRecovery] = useState<TeamRecovery>();
  const unavailableMentionRef = useRef<HTMLDivElement>(null);
  const canMutate = capabilities.has("mention.create") && canEditParent;
  const mentionNavigation =
    authenticatedMentionLink?.href === window.location.hash
      ? mentionNavigationFromHash(authenticatedMentionLink.href)
      : undefined;
  const expectedRoute = parentType === "project" ? "project" : "work";
  const targetsThisParent =
    mentionNavigation?.routeID === expectedRoute &&
    mentionNavigation.parentID === parentId &&
    authenticatedMentionLink?.clientId === clientId &&
    authenticatedMentionLink.parentType === parentType &&
    authenticatedMentionLink.parentId === parentId;
  const exactMentionSource =
    targetsThisParent &&
    authenticatedMentionLink?.sourceAvailable &&
    authenticatedMentionLink.sourceId === mentionNavigation?.sourceID &&
    authenticatedMentionLink.tokenId
      ? sources.find(
          (source) =>
            source.id === authenticatedMentionLink.sourceId &&
            source.lifecycleState === "active" &&
            source.document.tokens.some(
              (token) => token.id === authenticatedMentionLink.tokenId,
            ),
        )
      : undefined;

  function storedDraft(kind: MentionSourceKind) {
    const stored = draftStore.current.get(
      draftKey(clientId, parentType, parentId, kind),
    );
    return stored ? copyDocument(stored) : blankDocument();
  }

  function storedEdit(kind: "comment" | "note") {
    return editStore.current.get(
      draftKey(clientId, parentType, parentId, kind),
    );
  }

  function setEditOperation(
    kind: "comment" | "note",
    operation?: EditOperation,
  ) {
    const key = draftKey(clientId, parentType, parentId, kind);
    if (operation) editStore.current.set(key, operation);
    else editStore.current.delete(key);
    setEditOperations((current) => ({ ...current, [kind]: operation }));
    setEditing((current) => ({
      ...current,
      [kind]: operation?.sourceId,
    }));
  }

  useEffect(() => {
    activeController.current?.abort();
    const controller = new AbortController();
    activeController.current = controller;
    setSources([]);
    setSourcesContextKey("");
    setDraftContextKey(parentKey);
    setLoadState("loading");
    const restoredOperations: EditOperations = {
      comment: storedEdit("comment"),
      note: storedEdit("note"),
    };
    setEditOperations(restoredOperations);
    setEditing({
      comment: restoredOperations.comment?.sourceId,
      note: restoredOperations.note?.sourceId,
    });
    setSavingKind(undefined);
    setErrors({});
    setVersionConflict(undefined);
    setTeamRecovery(undefined);
    setDrafts({
      details: storedDraft("details"),
      comment: storedDraft("comment"),
      note: storedDraft("note"),
    });
    void api
      .list({ clientId, parentType, parentId }, controller.signal)
      .then((found) => {
        if (controller.signal.aborted) return;
        setSources(found);
        setSourcesContextKey(parentKey);
        const details = found.find(
          (source) =>
            source.sourceKind === "details" &&
            source.lifecycleState === "active",
        );
        const key = draftKey(clientId, parentType, parentId, "details");
        if (details && !draftStore.current.has(key)) {
          setDrafts((current) => ({
            ...current,
            details: copyDocument(details.document),
          }));
        }
        setLoadState("ready");
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted && !isAbort(error))
          setLoadState("error");
      });
    return () => controller.abort();
  }, [api, clientId, parentId, parentKey, parentType]);

  useEffect(() => {
    if (!targetsThisParent || loadState !== "ready") return;
    const sourceID = exactMentionSource?.id;
    const sourceElement = sourceID
      ? Array.from(
          document.querySelectorAll<HTMLElement>("[data-mention-source-id]"),
        ).find((element) => element.dataset.mentionSourceId === sourceID)
      : undefined;
    const tokenElement = sourceElement
      ? Array.from(
          sourceElement.querySelectorAll<HTMLElement>(
            "[data-mention-token-id]",
          ),
        ).find(
          (element) =>
            element.dataset.mentionTokenId ===
            authenticatedMentionLink?.tokenId,
        )
      : undefined;
    const target = tokenElement ?? unavailableMentionRef.current;
    if (!target) return;
    target.focus({ preventScroll: true });
    target.scrollIntoView?.({
      behavior:
        typeof window.matchMedia === "function" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "auto"
          : "smooth",
      block: "center",
    });
    if (!tokenElement) return;
    tokenElement.classList.add("mention-token-focus");
    const timer = window.setTimeout(
      () => tokenElement.classList.remove("mention-token-focus"),
      2_000,
    );
    return () => window.clearTimeout(timer);
  }, [
    loadState,
    mentionNavigation?.sourceID,
    mentionNavigation?.mentionOccurrenceID,
    exactMentionSource,
    sources,
    targetsThisParent,
  ]);

  function context(kind: MentionSourceKind): MentionContext {
    return { clientId, parentType, parentId, sourceKind: kind };
  }

  function updateDraft(kind: MentionSourceKind, document: MentionDocument) {
    const copy = copyDocument(document);
    draftStore.current.set(
      draftKey(clientId, parentType, parentId, kind),
      copy,
    );
    setDrafts((current) => ({ ...current, [kind]: copy }));
    setErrors((current) => ({ ...current, [kind]: "" }));
  }

  function replaceSource(saved: InternalContentSource) {
    if (sourcesContextKey !== parentKey) return;
    setSources((current) => {
      const existing = current.some((source) => source.id === saved.id);
      return existing
        ? current.map((source) => (source.id === saved.id ? saved : source))
        : [...current, saved];
    });
  }

  function sourceFor(kind: MentionSourceKind) {
    const currentSources = sourcesContextKey === parentKey ? sources : [];
    if (kind === "details") {
      return currentSources.find((source) => source.sourceKind === "details");
    }
    const sourceID = editOperations[kind]?.sourceId;
    return sourceID
      ? currentSources.find((source) => source.id === sourceID)
      : undefined;
  }

  function validatedEditSource(kind: "comment" | "note") {
    const operation = editOperations[kind];
    if (!operation || sourcesContextKey !== parentKey) return undefined;
    return sources.find(
      (source) =>
        source.id === operation.sourceId &&
        source.sourceKind === kind &&
        source.lifecycleState === "active" &&
        !source.legacy &&
        !source.readOnly,
    );
  }

  function editValidation(kind: "comment" | "note") {
    if (!editOperations[kind]) return undefined;
    if (draftContextKey !== parentKey || sourcesContextKey !== parentKey) {
      return loadState === "error" ? "unavailable" : "validating";
    }
    return validatedEditSource(kind) ? "validated" : "unavailable";
  }

  async function reloadVersionConflict(
    kind: MentionSourceKind,
    draft: MentionDocument,
    sourceId: string | undefined,
    signal: AbortSignal,
  ) {
    const current = await api.list({ clientId, parentType, parentId }, signal);
    if (signal.aborted) return;
    setSources(current);
    setSourcesContextKey(parentKey);
    const server = sourceId
      ? current.find((source) => source.id === sourceId)
      : current.find((source) => source.sourceKind === "details");
    setVersionConflict({ kind, sourceId, draft: copyDocument(draft), server });
  }

  async function recoverTeams(
    kind: MentionSourceKind,
    draft: MentionDocument,
    signal: AbortSignal,
  ) {
    const teamTokens = Array.from(
      new Map(
        draft.tokens
          .filter((token) => token.targetType === "team")
          .map((token) => [token.targetId, token]),
      ).values(),
    );
    const teams = await Promise.all(
      teamTokens.map(async (token) => {
        const candidates = await api.candidates(context(kind), "", signal, {
          targetType: "team",
          targetId: token.targetId,
        });
        const current = candidates.find(
          (candidate): candidate is TeamMentionCandidate =>
            candidate.targetType === "team" && candidate.id === token.targetId,
        );
        return {
          id: token.targetId,
          label: token.label.replace(/^@/u, ""),
          previousEligible:
            draft.confirmedTeamSnapshots[token.targetId]?.eligibleMemberIds
              .length ?? 0,
          current,
        };
      }),
    );
    if (!signal.aborted) {
      setTeamRecovery({ kind, draft: copyDocument(draft), teams });
    }
  }

  async function save(kind: MentionSourceKind) {
    if (!canMutate) return;
    const operation = kind === "details" ? undefined : editOperations[kind];
    if (
      savingKind ||
      versionConflict?.kind === kind ||
      teamRecovery?.kind === kind ||
      (kind !== "details" && operation && editValidation(kind) !== "validated")
    ) {
      return;
    }
    const draft = copyDocument(drafts[kind]);
    if (!draft.body.trim()) {
      setErrors((current) => ({
        ...current,
        [kind]: `Enter internal ${sourceLabel(kind)} content before saving.`,
      }));
      return;
    }
    const existing =
      kind !== "details" && operation
        ? validatedEditSource(kind)
        : sourceFor(kind);
    const controller = new AbortController();
    activeController.current?.abort();
    activeController.current = controller;
    setSavingKind(kind);
    setErrors((current) => ({ ...current, [kind]: "" }));
    try {
      const saved = await api.save(
        context(kind),
        draft,
        {
          ...(operation ? { sourceId: operation.sourceId } : {}),
          expectedVersion: operation?.expectedVersion ?? existing?.version ?? 0,
          idempotencyKey: crypto.randomUUID(),
        },
        controller.signal,
      );
      if (controller.signal.aborted) return;
      replaceSource(saved);
      if (kind === "details") {
        updateDraft(kind, saved.document);
      } else {
        updateDraft(kind, blankDocument());
        setEditOperation(kind, undefined);
      }
    } catch (error: unknown) {
      if (controller.signal.aborted || isAbort(error)) return;
      if (
        error instanceof MentionAPIError &&
        error.code === "mention_team_confirmation_stale"
      ) {
        try {
          await recoverTeams(kind, draft, controller.signal);
        } catch (refreshError: unknown) {
          if (!isAbort(refreshError)) {
            setErrors((current) => ({
              ...current,
              [kind]:
                "Current team recipients could not be loaded. Your draft was preserved.",
            }));
          }
        }
      } else if (
        error instanceof MentionAPIError &&
        error.status === 409 &&
        error.code === "version_conflict"
      ) {
        try {
          await reloadVersionConflict(
            kind,
            draft,
            operation?.sourceId,
            controller.signal,
          );
        } catch (refreshError: unknown) {
          if (!isAbort(refreshError)) {
            setErrors((current) => ({
              ...current,
              [kind]:
                "The current server version could not be loaded. Your draft was preserved.",
            }));
          }
        }
      } else {
        setErrors((current) => ({
          ...current,
          [kind]:
            "Internal content could not be saved. Your draft was preserved.",
        }));
      }
    } finally {
      if (!controller.signal.aborted) setSavingKind(undefined);
    }
  }

  async function redact(source: InternalContentSource) {
    if (!canMutate) return;
    const controller = new AbortController();
    activeController.current?.abort();
    activeController.current = controller;
    setSavingKind(source.sourceKind);
    try {
      const redacted = await api.redact(
        context(source.sourceKind),
        source.id,
        source.version,
        crypto.randomUUID(),
        controller.signal,
      );
      if (!controller.signal.aborted) {
        replaceSource(redacted);
        if (source.sourceKind === "details") {
          updateDraft("details", blankDocument());
        } else if (editing[source.sourceKind] === source.id) {
          updateDraft(source.sourceKind, blankDocument());
          setEditOperation(source.sourceKind, undefined);
        }
      }
    } catch (error: unknown) {
      if (!controller.signal.aborted && !isAbort(error)) {
        setErrors((current) => ({
          ...current,
          [source.sourceKind]: "Internal content could not be redacted.",
        }));
      }
    } finally {
      if (!controller.signal.aborted) setSavingKind(undefined);
    }
  }

  function beginEdit(source: InternalContentSource) {
    if (!canMutate) return;
    if (source.sourceKind === "details") return;
    updateDraft(source.sourceKind, source.document);
    setEditOperation(source.sourceKind, {
      sourceId: source.id,
      expectedVersion: source.version,
    });
  }

  function resolveVersionConflict(loadServer: boolean) {
    if (!versionConflict) return;
    const { kind, server, draft } = versionConflict;
    if (loadServer) {
      updateDraft(
        kind,
        server?.lifecycleState === "active" ? server.document : blankDocument(),
      );
      if (kind !== "details") {
        if (!server || server.lifecycleState !== "active") {
          updateDraft(kind, blankDocument());
          setEditOperation(kind, undefined);
        } else {
          setEditOperation(kind, {
            sourceId: server.id,
            expectedVersion: server.version,
          });
        }
      }
    } else {
      updateDraft(kind, draft);
      if (kind !== "details" && server?.lifecycleState === "active") {
        setEditOperation(kind, {
          sourceId: server.id,
          expectedVersion: server.version,
        });
      }
    }
    setVersionConflict(undefined);
  }

  function confirmTeamRecovery() {
    if (!teamRecovery || teamRecovery.teams.some((team) => !team.current))
      return;
    const confirmations = {
      ...teamRecovery.draft.confirmedTeamSnapshots,
    };
    for (const team of teamRecovery.teams) {
      if (!team.current) continue;
      confirmations[team.id] = {
        teamVersion: team.current.version,
        eligibleMemberIds: [...team.current.eligibleMemberIds],
      };
    }
    updateDraft(teamRecovery.kind, {
      ...teamRecovery.draft,
      confirmedTeamSnapshots: confirmations,
    });
    setTeamRecovery(undefined);
  }

  function discardUnavailableEdit(kind: "comment" | "note") {
    updateDraft(kind, blankDocument());
    setEditOperation(kind, undefined);
  }

  const visibleSources = sourcesContextKey === parentKey ? sources : [];
  const visibleDrafts =
    draftContextKey === parentKey
      ? drafts
      : {
          details: blankDocument(),
          comment: blankDocument(),
          note: blankDocument(),
        };
  const visibleEditing = draftContextKey === parentKey ? editing : {};
  const commentValidation = editValidation("comment");
  const noteValidation = editValidation("note");
  const comments = visibleSources.filter(
    (source) => source.sourceKind === "comment",
  );
  const notes = visibleSources.filter((source) => source.sourceKind === "note");
  const detailsSource = visibleSources.find(
    (source) => source.sourceKind === "details",
  );

  return (
    <section
      className="internal-collaboration"
      aria-labelledby={`${panelID}-title`}
      data-parent-context={parentKey}
      data-author-context={authorId}
    >
      <header>
        <div>
          <h2 id={`${panelID}-title`}>Internal collaboration</h2>
          <p>Internal only</p>
        </div>
        {loadState === "loading" ? (
          <span role="status">Loading internal content…</span>
        ) : null}
        {loadState === "error" ? (
          <span role="alert">Internal content is unavailable.</span>
        ) : null}
      </header>

      {targetsThisParent && loadState === "ready" && !exactMentionSource ? (
        <div
          ref={unavailableMentionRef}
          className="internal-collaboration__mention-unavailable"
          role="status"
          aria-label="Mention source unavailable"
          tabIndex={-1}
        >
          The original internal source was removed, redacted, or no longer
          contains the mention.
        </div>
      ) : null}

      <CollaborationRegion
        id={`${panelID}-details`}
        title="Internal details"
        emptyMessage="No internal details yet."
        sources={detailsSource ? [detailsSource] : []}
        renderSource={(source) => (
          <SourceCard
            key={source.id}
            source={source}
            onRedact={canMutate ? () => void redact(source) : undefined}
          />
        )}
      >
        {canMutate ? (
          <>
            <MentionEditor
              value={visibleDrafts.details}
              context={context("details")}
              api={api}
              onChange={(document) => updateDraft("details", document)}
              label="Internal details"
              validationError={errors.details}
              disabled={detailsSource?.lifecycleState === "redacted"}
            />
            <button
              type="button"
              disabled={
                savingKind === "details" ||
                versionConflict?.kind === "details" ||
                teamRecovery?.kind === "details" ||
                detailsSource?.lifecycleState === "redacted"
              }
              onClick={() => void save("details")}
            >
              Save internal details
            </button>
            <ConflictRecovery
              kind="details"
              versionConflict={versionConflict}
              teamRecovery={teamRecovery}
              onLoadServer={() => resolveVersionConflict(true)}
              onReapply={() => resolveVersionConflict(false)}
              onConfirmTeams={confirmTeamRecovery}
            />
          </>
        ) : null}
      </CollaborationRegion>

      <CollaborationRegion
        id={`${panelID}-comments`}
        title="Internal comments"
        emptyMessage="No internal comments yet."
        sources={comments}
        renderSource={(source) => (
          <SourceCard
            key={source.id}
            source={source}
            onEdit={canMutate ? () => beginEdit(source) : undefined}
            onRedact={canMutate ? () => void redact(source) : undefined}
          />
        )}
      >
        {canMutate ? (
          <>
            <p>Internal only</p>
            <MentionEditor
              value={visibleDrafts.comment}
              context={context("comment")}
              api={api}
              onChange={(document) => updateDraft("comment", document)}
              label={
                visibleEditing.comment
                  ? "Edit internal comment"
                  : "New internal comment"
              }
              validationError={errors.comment}
            />
            <button
              type="button"
              disabled={
                savingKind === "comment" ||
                versionConflict?.kind === "comment" ||
                teamRecovery?.kind === "comment" ||
                (Boolean(editOperations.comment) &&
                  commentValidation !== "validated")
              }
              onClick={() => void save("comment")}
            >
              {visibleEditing.comment
                ? "Save edited comment"
                : "Add internal comment"}
            </button>
            {commentValidation === "validating" ? (
              <ValidatingEdit kind="comment" />
            ) : null}
            {commentValidation === "unavailable" ? (
              <UnavailableEdit
                kind="comment"
                onDiscard={() => discardUnavailableEdit("comment")}
              />
            ) : null}
            <ConflictRecovery
              kind="comment"
              versionConflict={versionConflict}
              teamRecovery={teamRecovery}
              onLoadServer={() => resolveVersionConflict(true)}
              onReapply={() => resolveVersionConflict(false)}
              onConfirmTeams={confirmTeamRecovery}
            />
          </>
        ) : null}
      </CollaborationRegion>

      <CollaborationRegion
        id={`${panelID}-notes`}
        title="Internal notes"
        emptyMessage="No internal notes yet."
        sources={notes}
        renderSource={(source) => (
          <SourceCard
            key={source.id}
            source={source}
            onEdit={canMutate ? () => beginEdit(source) : undefined}
            onRedact={canMutate ? () => void redact(source) : undefined}
          />
        )}
      >
        {canMutate ? (
          <>
            <p>Internal only</p>
            <MentionEditor
              value={visibleDrafts.note}
              context={context("note")}
              api={api}
              onChange={(document) => updateDraft("note", document)}
              label={
                visibleEditing.note ? "Edit internal note" : "New internal note"
              }
              validationError={errors.note}
            />
            <button
              type="button"
              disabled={
                savingKind === "note" ||
                versionConflict?.kind === "note" ||
                teamRecovery?.kind === "note" ||
                (Boolean(editOperations.note) && noteValidation !== "validated")
              }
              onClick={() => void save("note")}
            >
              {visibleEditing.note ? "Save edited note" : "Add internal note"}
            </button>
            {noteValidation === "validating" ? (
              <ValidatingEdit kind="note" />
            ) : null}
            {noteValidation === "unavailable" ? (
              <UnavailableEdit
                kind="note"
                onDiscard={() => discardUnavailableEdit("note")}
              />
            ) : null}
            <ConflictRecovery
              kind="note"
              versionConflict={versionConflict}
              teamRecovery={teamRecovery}
              onLoadServer={() => resolveVersionConflict(true)}
              onReapply={() => resolveVersionConflict(false)}
              onConfirmTeams={confirmTeamRecovery}
            />
          </>
        ) : null}
      </CollaborationRegion>
    </section>
  );
}

function ValidatingEdit({ kind }: { kind: "comment" | "note" }) {
  return (
    <p
      role="status"
      aria-label={`Validating restored internal ${kind}`}
    >{`Validating the original internal ${kind} before this edit can be saved…`}</p>
  );
}

function UnavailableEdit({
  kind,
  onDiscard,
}: {
  kind: "comment" | "note";
  onDiscard: () => void;
}) {
  return (
    <div
      className="internal-collaboration__conflict"
      role="alert"
      aria-label={`Unavailable restored internal ${kind}`}
    >
      <strong>The original internal {kind} is unavailable.</strong>
      <p>{`Your edit draft is preserved, but it cannot be submitted as a new ${kind}.`}</p>
      <button type="button" onClick={onDiscard}>
        Discard unavailable edit
      </button>
    </div>
  );
}

function CollaborationRegion({
  id,
  title,
  emptyMessage,
  sources,
  renderSource,
  children,
}: {
  id: string;
  title: string;
  emptyMessage: string;
  sources: InternalContentSource[];
  renderSource?: (source: InternalContentSource) => ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="internal-collaboration__region" aria-labelledby={id}>
      <h3 id={id}>{title}</h3>
      <div className="internal-collaboration__history">
        {sources.length ? (
          sources.map((source) =>
            renderSource ? (
              renderSource(source)
            ) : (
              <SourceCard key={source.id} source={source} />
            ),
          )
        ) : (
          <p>{emptyMessage}</p>
        )}
      </div>
      {children ? (
        <div className="internal-collaboration__composer">{children}</div>
      ) : null}
    </section>
  );
}

function ConflictRecovery({
  kind,
  versionConflict,
  teamRecovery,
  onLoadServer,
  onReapply,
  onConfirmTeams,
}: {
  kind: MentionSourceKind;
  versionConflict?: VersionConflict;
  teamRecovery?: TeamRecovery;
  onLoadServer: () => void;
  onReapply: () => void;
  onConfirmTeams: () => void;
}) {
  if (versionConflict?.kind === kind) {
    const serverBody =
      versionConflict.server?.lifecycleState === "active"
        ? versionConflict.server.document.body
        : "Content unavailable";
    return (
      <div className="internal-collaboration__conflict" role="alert">
        <strong>Internal content changed on the server.</strong>
        <p>Current server text: {serverBody}</p>
        <p>Your draft: {versionConflict.draft.body}</p>
        <div>
          <button type="button" onClick={onLoadServer}>
            Load server version
          </button>
          <button
            type="button"
            onClick={onReapply}
            disabled={
              Boolean(versionConflict.sourceId) &&
              (!versionConflict.server ||
                versionConflict.server.lifecycleState !== "active")
            }
          >
            Reapply my draft
          </button>
        </div>
      </div>
    );
  }
  if (teamRecovery?.kind === kind) {
    const unavailable = teamRecovery.teams.some((team) => !team.current);
    return (
      <div className="internal-collaboration__conflict" role="alert">
        <strong>Team mention recipients changed.</strong>
        {teamRecovery.teams.map((team) => (
          <p key={team.id}>
            {team.label}:{" "}
            {team.current
              ? `Eligible ${team.previousEligible} → ${team.current.eligibleCount} · Excluded ${team.current.excludedCount}`
              : "Current recipient snapshot is unavailable"}
          </p>
        ))}
        <p>
          Your text and mention tokens were preserved. Review and confirm before
          saving again.
        </p>
        <button type="button" disabled={unavailable} onClick={onConfirmTeams}>
          Confirm updated recipients
        </button>
      </div>
    );
  }
  return null;
}

function isAbort(error: unknown) {
  return error instanceof DOMException && error.name === "AbortError";
}
