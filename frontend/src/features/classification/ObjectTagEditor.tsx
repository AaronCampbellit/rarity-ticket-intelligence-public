import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Button, TagPicker } from "../../design-system";
import type {
  ClassificationAPI,
  ClassificationCatalog,
  ClassificationSuggestion,
  TagHistoryEntry,
  TagTarget,
  TaggedObject,
} from "./types";

type Conflict = { submitted: string[]; current: string[] };

export function ObjectTagEditor({
  api,
  target,
  clientID = "",
  reason = "Technician updated classification",
  focusRequest = 0,
}: {
  api: ClassificationAPI;
  target: TagTarget;
  clientID?: string;
  reason?: string;
  /** Increments when an adjacent guarded action needs this editor for recovery. */
  focusRequest?: number;
}) {
  const [catalog, setCatalog] = useState<ClassificationCatalog>();
  const [tagged, setTagged] = useState<TaggedObject>();
  const [loadedTargetIdentity, setLoadedTargetIdentity] = useState<string>();
  const [history, setHistory] = useState<TagHistoryEntry[]>([]);
  const [directIDs, setDirectIDs] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [historyWarning, setHistoryWarning] = useState("");
  const [conflict, setConflict] = useState<Conflict>();
  const [suggestion, setSuggestion] = useState<ClassificationSuggestion>();
  const [suggesting, setSuggesting] = useState(false);
  const [deciding, setDeciding] = useState(false);
  const errorRef = useRef<HTMLParagraphElement>(null);
  const editorRef = useRef<HTMLElement>(null);
  const apiRef = useRef(api);
  apiRef.current = api;
  const historyRequestGeneration = useRef(0);
  const loadRequestGeneration = useRef(0);
  const saveRequestGeneration = useRef(0);
  const saveController = useRef<AbortController | undefined>(undefined);
  const suggestionGeneration = useRef(0);
  const suggestionController = useRef<AbortController | undefined>(undefined);
  const targetIdentity = targetKey(target, clientID);
  const currentTargetIdentity = useRef(targetIdentity);
  currentTargetIdentity.current = targetIdentity;

  useEffect(() => {
    const controller = new AbortController();
    const loadGeneration = ++loadRequestGeneration.current;
    const loadTargetIdentity = targetKey(target, clientID);
    const isCurrentLoad = () =>
      !controller.signal.aborted &&
      loadGeneration === loadRequestGeneration.current &&
      loadTargetIdentity === currentTargetIdentity.current;
    saveRequestGeneration.current += 1;
    saveController.current?.abort();
    saveController.current = undefined;
    setLoading(true);
    setSaving(false);
    setSuggesting(false);
    setDeciding(false);
    setStatus("");
    setError("");
    setHistoryWarning("");
    setConflict(undefined);
    setCatalog(undefined);
    setTagged(undefined);
    setLoadedTargetIdentity(undefined);
    setDirectIDs([]);
    setHistory([]);
    suggestionGeneration.current += 1;
    suggestionController.current?.abort();
    suggestionController.current = undefined;
    setSuggestion(undefined);
    Promise.all([
      apiRef.current.catalog(controller.signal),
      apiRef.current.object(target, controller.signal),
    ])
      .then(([nextCatalog, nextTagged]) => {
        if (!isCurrentLoad()) return;
        setCatalog(nextCatalog);
        setTagged(nextTagged);
        setDirectIDs(nextTagged.direct.map((item) => item.tag.id));
        setLoadedTargetIdentity(loadTargetIdentity);
      })
      .catch(() => {
        if (isCurrentLoad()) setError("Classification could not be loaded.");
      })
      .finally(() => {
        if (isCurrentLoad()) setLoading(false);
      });
    const initialHistoryGeneration = ++historyRequestGeneration.current;
    api
      .history(target, controller.signal)
      .then((nextHistory) => {
        if (
          !controller.signal.aborted &&
          initialHistoryGeneration === historyRequestGeneration.current
        ) {
          setHistory(nextHistory);
          setHistoryWarning("");
        }
      })
      .catch(() => {
        if (
          !controller.signal.aborted &&
          initialHistoryGeneration === historyRequestGeneration.current
        )
          setHistoryWarning("History could not be refreshed.");
      });
    return () => {
      controller.abort();
      loadRequestGeneration.current += 1;
      historyRequestGeneration.current += 1;
      saveRequestGeneration.current += 1;
      saveController.current?.abort();
      saveController.current = undefined;
      suggestionController.current?.abort();
      suggestionController.current = undefined;
    };
  }, [clientID, target.objectId, target.objectType]);

  useEffect(() => {
    if (!suggestion || suggestion.status !== "pending" || !api.suggestion)
      return;
    const controller = new AbortController();
    suggestionController.current?.abort();
    suggestionController.current = controller;
    const generation = ++suggestionGeneration.current;
    const identity = targetIdentity;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const next = await api.suggestion!(suggestion.id, controller.signal);
        if (
          controller.signal.aborted ||
          generation !== suggestionGeneration.current ||
          identity !== currentTargetIdentity.current
        )
          return;
        setSuggestion(next);
        if (next.status === "pending")
          timer = setTimeout(() => void poll(), 750);
      } catch {
        if (
          !controller.signal.aborted &&
          generation === suggestionGeneration.current &&
          identity === currentTargetIdentity.current
        )
          setError("AI suggestion status could not be refreshed.");
      }
    };
    timer = setTimeout(() => void poll(), 750);
    return () => {
      controller.abort();
      if (suggestionController.current === controller)
        suggestionController.current = undefined;
      if (timer) clearTimeout(timer);
    };
  }, [api, suggestion?.id, suggestion?.status, targetIdentity]);

  useLayoutEffect(() => {
    if (error === "Select at least one meaningful classification tag.")
      errorRef.current?.focus();
  }, [error]);

  useLayoutEffect(() => {
    if (focusRequest && !loading) editorRef.current?.focus();
  }, [focusRequest, loading]);

  async function save() {
    if (!tagged) return;
    const saveTarget = target;
    const saveTargetIdentity = targetKey(saveTarget, clientID);
    const saveGeneration = ++saveRequestGeneration.current;
    const controller = new AbortController();
    saveController.current?.abort();
    saveController.current = controller;
    const isCurrentSave = () =>
      !controller.signal.aborted &&
      saveGeneration === saveRequestGeneration.current &&
      saveTargetIdentity === currentTargetIdentity.current;
    setSaving(true);
    setStatus("");
    setHistoryWarning("");
    setError("");
    setConflict(undefined);
    const submitted = directIDs.map(
      (id) =>
        catalog?.tags.find((tag) => tag.id === id)?.label ?? "Unknown tag",
    );
    try {
      const updated = await api.replaceDirect(
        saveTarget,
        {
          tagIDs: directIDs,
          expectedVersion: tagged.objectVersion,
          reason,
          idempotencyKey: `${saveTarget.objectType}-${saveTarget.objectId}-${Date.now()}`,
        },
        controller.signal,
      );
      if (!isCurrentSave()) return;
      setTagged(updated);
      setDirectIDs(updated.direct.map((item) => item.tag.id));
      setStatus("Classification saved.");
      const refreshedHistoryGeneration = ++historyRequestGeneration.current;
      try {
        const nextHistory = await api.history(saveTarget, controller.signal);
        if (
          isCurrentSave() &&
          refreshedHistoryGeneration === historyRequestGeneration.current
        ) {
          setHistory(nextHistory);
          setHistoryWarning("");
        }
      } catch {
        if (
          isCurrentSave() &&
          refreshedHistoryGeneration === historyRequestGeneration.current
        )
          setHistoryWarning("History could not be refreshed.");
      }
    } catch (cause) {
      if (!isCurrentSave()) return;
      const code =
        typeof cause === "object" && cause && "code" in cause
          ? String(cause.code)
          : "request_failed";
      if (code === "classification_required") {
        setError("Select at least one meaningful classification tag.");
      } else if (code === "version_conflict") {
        try {
          const current = await api.object(saveTarget, controller.signal);
          if (!isCurrentSave()) return;
          setTagged(current);
          setDirectIDs(current.direct.map((item) => item.tag.id));
          setConflict({
            submitted,
            current: current.direct.map((item) => item.tag.label),
          });
        } catch {
          if (isCurrentSave())
            setError("Classification changed elsewhere. Reload and try again.");
        }
      } else {
        setError("Classification could not be saved. Try again.");
      }
    } finally {
      if (isCurrentSave()) {
        setSaving(false);
        saveController.current = undefined;
      }
    }
  }

  async function requestSuggestions() {
    if (!api.requestSuggestions) return;
    const controller = new AbortController();
    suggestionController.current?.abort();
    suggestionController.current = controller;
    const identity = targetIdentity;
    const generation = ++suggestionGeneration.current;
    const current = () =>
      !controller.signal.aborted &&
      identity === currentTargetIdentity.current &&
      generation === suggestionGeneration.current;
    setDeciding(false);
    setSuggesting(true);
    setError("");
    try {
      const next = await api.requestSuggestions(target, controller.signal);
      if (current()) setSuggestion(next);
    } catch {
      if (current()) setError("AI suggestions could not be requested.");
    } finally {
      if (current()) {
        setSuggesting(false);
        suggestionController.current = undefined;
      }
    }
  }
  async function decideSuggestion(
    tagId: string,
    decision: "accepted" | "dismissed",
  ) {
    if (!suggestion || !api.decideSuggestion) return;
    const controller = new AbortController();
    suggestionController.current?.abort();
    suggestionController.current = controller;
    const identity = targetIdentity;
    const generation = ++suggestionGeneration.current;
    const current = () =>
      !controller.signal.aborted &&
      identity === currentTargetIdentity.current &&
      generation === suggestionGeneration.current;
    setDeciding(true);
    try {
      const updated = await api.decideSuggestion(
        suggestion.id,
        tagId,
        decision,
        controller.signal,
      );
      if (!current()) return;
      setSuggestion(updated);
      if (decision === "accepted") {
        const [nextObject, nextHistory] = await Promise.all([
          api.object(target, controller.signal),
          api.history(target, controller.signal),
        ]);
        if (!current()) return;
        setTagged(nextObject);
        setDirectIDs(nextObject.direct.map((item) => item.tag.id));
        setHistory(nextHistory);
      }
      if (current())
        setStatus(
          decision === "accepted"
            ? "AI tag accepted and applied."
            : "AI suggestion dismissed.",
        );
    } catch {
      if (current()) setError("AI suggestion could not be decided.");
    } finally {
      if (current()) {
        setDeciding(false);
        suggestionController.current = undefined;
      }
    }
  }

  if (loading)
    return (
      <section className="rti-object-tag-editor" aria-busy="true">
        <p>Loading classification…</p>
      </section>
    );
  if (!catalog || !tagged || loadedTargetIdentity !== targetIdentity)
    return (
      <section className="rti-object-tag-editor">
        <p ref={errorRef} tabIndex={-1} role="alert">
          {error || "Classification is unavailable."}
        </p>
      </section>
    );
  const inheritedIDs = tagged.inherited.map((item) => item.tag.id);

  return (
    <section
      ref={editorRef}
      tabIndex={-1}
      className="rti-object-tag-editor"
      aria-labelledby="classification-title"
    >
      <header>
        <div>
          <h2 id="classification-title">Classification</h2>
          <p>Classify this record for routing, reporting, and automation.</p>
        </div>
      </header>
      {error ? (
        <p
          ref={errorRef}
          tabIndex={-1}
          role="alert"
          className="rti-field__error"
        >
          {error}
        </p>
      ) : null}
      {conflict ? (
        <section className="rti-classification-conflict" role="alert">
          <h3>Classification changed elsewhere</h3>
          <p>Your submitted tags: {labels(conflict.submitted)}</p>
          <p>Current tags: {labels(conflict.current)}</p>
          <p>The editor has been refreshed with the current classification.</p>
        </section>
      ) : null}
      <h3>Direct tags</h3>
      <TagPicker
        label="Classification tags"
        groups={catalog.groups}
        tags={catalog.tags}
        selectedIds={directIDs}
        selectedAssignments={tagged.direct}
        inheritedIds={inheritedIDs}
        required
        onChange={setDirectIDs}
      />
      <h3>Inherited tags</h3>
      {tagged.inherited.length ? (
        <ul className="rti-object-tag-editor__inherited">
          {tagged.inherited.map((item) => (
            <li key={item.id}>
              {item.tag.label} — inherited from project{" "}
              {item.sourceObjectId ? (
                <a
                  href={sourceHref(
                    clientID,
                    item.sourceObjectType,
                    item.sourceObjectId,
                  )}
                >
                  View source project
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <p>No inherited tags.</p>
      )}
      {api.requestSuggestions ? (
        <section aria-labelledby="classification-ai-title">
          <h3 id="classification-ai-title">AI suggestions</h3>
          <Button
            loading={suggesting}
            loadingLabel="Requesting suggestions"
            onClick={requestSuggestions}
          >
            Get AI suggestions
          </Button>
          {suggestion?.status === "pending" ? (
            <p aria-live="polite">AI classification is pending.</p>
          ) : suggestion?.status === "failed" ? (
            <p role="alert">AI classification failed. Try again.</p>
          ) : suggestion?.suggestions.length ? (
            <ul>
              {suggestion.suggestions.map((item) => {
                const label =
                  catalog.tags.find((tag) => tag.id === item.tagId)?.label ??
                  "Unknown tag";
                const decided =
                  item.disposition && item.disposition !== "suggested";
                return (
                  <li key={item.id || item.tagId}>
                    <strong>{label}</strong>
                    {item.confidence !== undefined
                      ? ` — ${Math.round(item.confidence * 100)}%`
                      : ""}
                    {item.rationale ? `: ${item.rationale}` : ""}{" "}
                    {decided ? (
                      <span> — {item.disposition}</span>
                    ) : api.decideSuggestion ? (
                      <>
                        <button
                          type="button"
                          disabled={deciding}
                          onClick={() =>
                            void decideSuggestion(item.tagId, "accepted")
                          }
                        >
                          Accept
                        </button>
                        <button
                          type="button"
                          disabled={deciding}
                          onClick={() =>
                            void decideSuggestion(item.tagId, "dismissed")
                          }
                        >
                          Dismiss
                        </button>
                      </>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          ) : suggestion?.status === "completed" ? (
            <p>No AI suggestions were returned.</p>
          ) : null}
        </section>
      ) : null}
      <Button
        intent="primary"
        loading={saving}
        loadingLabel="Saving classification"
        onClick={save}
      >
        Save classification
      </Button>
      <p role="status" aria-live="polite">
        {status}
      </p>
      {historyWarning ? <p role="status">{historyWarning}</p> : null}
      <section aria-labelledby="classification-history-title">
        <h3 id="classification-history-title">Classification history</h3>
        {history.length ? (
          <ol className="rti-object-tag-editor__history">
            {history.map((entry) => (
              <li key={entry.id}>
                <strong>
                  {entry.actorId || entry.assignment.assignedBy || "System"}{" "}
                  {entry.operation} {entry.assignment.tag.label}
                </strong>
                <span>
                  {sourceDescription(entry)} ·{" "}
                  <time dateTime={entry.occurredAt}>
                    {new Date(entry.occurredAt).toLocaleString()}
                  </time>{" "}
                  · Version {entry.targetVersion}
                </span>
                {entry.sourceObjectId ? (
                  <a
                    href={sourceHref(
                      clientID,
                      entry.sourceObjectType,
                      entry.sourceObjectId,
                    )}
                  >
                    View source project
                  </a>
                ) : null}
              </li>
            ))}
          </ol>
        ) : (
          <p>No classification changes yet.</p>
        )}
      </section>
    </section>
  );
}

/** Keeps expensive object/history reads behind an explicit detail disclosure. */
export function DeferredObjectTagEditor(props: {
  api: ClassificationAPI;
  target: TagTarget;
  clientID?: string;
  reason?: string;
  recoveryRequest?: number;
}) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    setOpen(false);
  }, [props.clientID, props.target.objectType, props.target.objectId]);
  useEffect(() => {
    if (props.recoveryRequest) setOpen(true);
  }, [props.recoveryRequest]);
  return (
    <details
      className="rti-object-tag-editor__disclosure"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>Classification</summary>
      {open ? (
        <ObjectTagEditor {...props} focusRequest={props.recoveryRequest} />
      ) : null}
    </details>
  );
}

function labels(items: string[]) {
  return items.length ? items.join(", ") : "None";
}

function sourceDescription(entry: TagHistoryEntry) {
  return entry.inherited
    ? "Inherited from project"
    : entry.assignment.source.replaceAll("_", " ");
}

function sourceHref(clientID: string, type: string | undefined, id: string) {
  return type === "project"
    ? `#/project?recordType=project&recordID=${encodeURIComponent(id)}&clientID=${encodeURIComponent(clientID)}&label=Project`
    : `/work/${encodeURIComponent(id)}`;
}

function targetKey(target: TagTarget, clientID = "") {
  return `${clientID}:${target.objectType}:${target.objectId}`;
}
