import { useEffect, useMemo, useRef, useState } from "react";

import { Combobox, Notice } from "../../design-system";
import { createMentionAPI } from "./api";
import type {
  MentionAPI,
  MentionCandidate,
  MentionContext,
  TeamConfirmation,
  TeamMentionCandidate,
} from "./types";
import "./mentions.css";

const searchDelayMilliseconds = 150;
const maximumCandidates = 50;
const defaultMentionAPI = createMentionAPI();

export type MentionPickerProps = {
  context: MentionContext;
  api?: MentionAPI;
  initialQuery?: string;
  onSelect(
    candidate: MentionCandidate,
    confirmation: TeamConfirmation | undefined,
  ): void;
  onDismiss?(): void;
  onCandidates?(candidates: MentionCandidate[]): void;
};

export function MentionPicker({
  context,
  api = defaultMentionAPI,
  initialQuery = "",
  onSelect,
  onDismiss,
  onCandidates,
}: MentionPickerProps) {
  const [query, setQuery] = useState(initialQuery);
  const scopeKey = [
    context.clientId,
    context.parentType,
    context.parentId,
    context.sourceKind,
    query,
  ].join("\u0000");
  const [candidateResult, setCandidateResult] = useState<{
    scopeKey: string;
    values: MentionCandidate[];
  }>({ scopeKey: "", values: [] });
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [pendingTeam, setPendingTeam] = useState<{
    scopeKey: string;
    candidate: TeamMentionCandidate;
  }>();
  const requestSequence = useRef(0);
  const currentScopeKey = useRef(scopeKey);
  const inputRef = useRef<HTMLInputElement>(null);
  currentScopeKey.current = scopeKey;
  const onCandidatesRef = useRef(onCandidates);
  onCandidatesRef.current = onCandidates;
  const candidates =
    candidateResult.scopeKey === scopeKey ? candidateResult.values : [];
  const visibleStatus =
    candidateResult.scopeKey === scopeKey ? status : "loading";
  const currentPendingTeam =
    pendingTeam?.scopeKey === scopeKey ? pendingTeam.candidate : undefined;

  useEffect(() => setQuery(initialQuery), [initialQuery]);

  useEffect(() => {
    const controller = new AbortController();
    const sequence = ++requestSequence.current;
    setStatus("loading");
    setCandidateResult({ scopeKey, values: [] });
    setPendingTeam(undefined);
    const timer = window.setTimeout(() => {
      void api
        .candidates(context, query, controller.signal)
        .then((values) => {
          if (controller.signal.aborted || sequence !== requestSequence.current)
            return;
          const bounded = values.slice(0, maximumCandidates);
          setCandidateResult({ scopeKey, values: bounded });
          setStatus("ready");
          onCandidatesRef.current?.(bounded);
        })
        .catch((cause: unknown) => {
          if (
            controller.signal.aborted ||
            sequence !== requestSequence.current ||
            (cause instanceof DOMException && cause.name === "AbortError")
          )
            return;
          setCandidateResult({ scopeKey, values: [] });
          setStatus("error");
        });
    }, searchDelayMilliseconds);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [
    api,
    context.clientId,
    context.parentId,
    context.parentType,
    context.sourceKind,
    query,
    scopeKey,
  ]);

  const ordered = useMemo(
    () => [
      ...candidates.filter((value) => value.targetType === "staff"),
      ...candidates.filter((value) => value.targetType === "team"),
    ],
    [candidates],
  );
  const byValue = useMemo(
    () =>
      new Map(
        ordered.map((candidate) => [
          `${candidate.targetType}:${candidate.id}`,
          candidate,
        ]),
      ),
    [ordered],
  );
  const options = ordered.map((candidate) => ({
    value: `${candidate.targetType}:${candidate.id}`,
    label: candidate.label,
    group: candidate.targetType === "staff" ? "People" : "Teams",
    description:
      candidate.targetType === "team"
        ? `${candidate.eligibleCount} eligible, ${candidate.excludedCount} excluded`
        : undefined,
  }));

  function choose(candidate: MentionCandidate) {
    if (candidate.targetType === "team" && candidate.excludedCount > 0) {
      setPendingTeam({ scopeKey, candidate });
      return;
    }
    onSelect(candidate, undefined);
  }

  const announcement =
    visibleStatus === "loading"
      ? "Loading mention suggestions."
      : visibleStatus === "error"
        ? "Mention suggestions are unavailable."
        : candidates.length === 0
          ? "No matching people or teams."
          : `${candidates.length} mention suggestions available.`;

  return (
    <div
      className="rti-mention-picker"
      onKeyDownCapture={(event) => {
        if (event.key !== "Escape") return;
        event.preventDefault();
        event.stopPropagation();
        onDismiss?.();
      }}
    >
      <Combobox
        label="Mention a person or team"
        options={options}
        value=""
        query={query}
        onQueryChange={setQuery}
        onChange={(selected) => {
          if (currentScopeKey.current !== scopeKey) return;
          const candidate = byValue.get(selected);
          if (candidate) choose(candidate);
        }}
        placeholder="Search people and teams"
        autoFocus
        inputRef={inputRef}
        closeOnSelect={false}
        filterOptions={false}
        alwaysOpen
        updateQueryOnSelect={false}
      />

      <p className="sr-only" role="status" aria-live="polite">
        {announcement}
      </p>
      {visibleStatus === "error" ? (
        <Notice
          tone="danger"
          title="Mention suggestions are unavailable"
          urgent
        >
          Check your connection and try searching again.
        </Notice>
      ) : visibleStatus === "ready" && candidates.length === 0 ? (
        <p className="rti-mention-picker__empty">
          No matching people or teams.
        </p>
      ) : null}

      {currentPendingTeam ? (
        <Notice
          tone="warning"
          title={`Confirm ${currentPendingTeam.label} mention`}
        >
          <p>
            {currentPendingTeam.eligibleCount} eligible members will be
            mentioned; {currentPendingTeam.excludedCount} member
            {currentPendingTeam.excludedCount === 1 ? "" : "s"} will be
            excluded.
          </p>
          <div className="rti-mention-picker__confirmation-actions">
            <button
              type="button"
              onClick={() => {
                onSelect(currentPendingTeam, {
                  teamVersion: currentPendingTeam.version,
                  eligibleMemberIds: [...currentPendingTeam.eligibleMemberIds],
                });
                setPendingTeam(undefined);
              }}
              aria-label={`Confirm ${currentPendingTeam.label} mention`}
            >
              Confirm
            </button>
            <button
              type="button"
              onClick={() => {
                setPendingTeam(undefined);
                queueMicrotask(() => inputRef.current?.focus());
              }}
            >
              Cancel
            </button>
          </div>
        </Notice>
      ) : null}
    </div>
  );
}
