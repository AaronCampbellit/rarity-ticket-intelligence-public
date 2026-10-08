import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button, Notice, Page } from "../../design-system";
import { AISettingsAPIError } from "./api";
import type {
  AIAssistAPI,
  AIAssistJob,
  AIAssistRecommendation,
  AIFeature,
} from "./types";

type StoredJob = { version: 1; jobID: string };

const POLL_INTERVAL_MS = 2_000;
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const retryableFailureCodes = new Set([
  "provider_unavailable",
  "provider_timeout",
]);
const featureLabels: Record<AIFeature, string> = {
  summary: "summary",
  reply_draft: "reply draft",
  similar_suggestions: "similar suggestions",
  classification: "classification",
  calendar_recommendation: "calendar recommendation",
};

function storageKey(workRecordID: string) {
  return `rarity.ai-assist.v1:${workRecordID}`;
}

function readStoredJob(key: string): StoredJob | undefined {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? "");
    if (
      value &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      (value as { version?: unknown }).version === 1 &&
      typeof (value as { jobID?: unknown }).jobID === "string" &&
      (value as { jobID: string }).jobID.trim()
    ) {
      return value as StoredJob;
    }
  } catch {
    // Corrupt or older session data is intentionally ignored.
  }
  return undefined;
}

function isActive(job?: AIAssistJob) {
  return job?.state === "queued" || job?.state === "running";
}

function safeErrorCode(error: unknown) {
  return error instanceof AISettingsAPIError ? error.code : "request_failed";
}

function isDefinitivelyUnavailable(error: unknown) {
  return (
    error instanceof AISettingsAPIError &&
    [401, 403, 404].includes(error.status)
  );
}

function elapsed(job: AIAssistJob, now: number) {
  const started = Date.parse(job.createdAt);
  return Number.isNaN(started)
    ? 0
    : Math.max(0, Math.floor((now - started) / 1_000));
}

export function AIAssistPanel({
  workRecordID,
  supportedFeatures,
  api,
  generationEnabled = true,
  embedded = false,
}: {
  workRecordID: string;
  supportedFeatures: AIFeature[];
  api: AIAssistAPI;
  generationEnabled?: boolean;
  embedded?: boolean;
}) {
  const key = useMemo(() => storageKey(workRecordID), [workRecordID]);
  const [job, setJob] = useState<AIAssistJob>();
  const [savedJobID, setSavedJobID] = useState<string>();
  const [recommendation, setRecommendation] =
    useState<AIAssistRecommendation>();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [operation, setOperation] = useState<
    "submit" | "cancel" | "retry" | "decision" | "refresh"
  >();
  const [cancelReason, setCancelReason] = useState("");
  const [retryReason, setRetryReason] = useState("");
  const [decisionReason, setDecisionReason] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const operationRef = useRef(operation);
  const requestVersion = useRef(0);
  const jobLoadInFlight = useRef(false);
  const statusController = useRef<AbortController | undefined>(undefined);
  const actionController = useRef<AbortController | undefined>(undefined);
  const errorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    operationRef.current = operation;
  }, [operation]);

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  const clearStoredJob = useCallback(() => {
    sessionStorage.removeItem(key);
    setSavedJobID(undefined);
  }, [key]);
  const retainJob = useCallback(
    (next: AIAssistJob) => {
      if (next.workRecordID !== workRecordID) {
        clearStoredJob();
        return;
      }
      sessionStorage.setItem(
        key,
        JSON.stringify({ version: 1, jobID: next.id } satisfies StoredJob),
      );
      setSavedJobID(next.id);
    },
    [clearStoredJob, key, workRecordID],
  );

  const loadRecommendation = useCallback(
    async (recommendationID: string, signal: AbortSignal, version: number) => {
      try {
        const next = await api.getRecommendation(recommendationID, signal);
        if (
          signal.aborted ||
          requestVersion.current !== version ||
          next.workRecordID !== workRecordID
        )
          return;
        setRecommendation(next);
      } catch (cause) {
        if (!signal.aborted && requestVersion.current === version) {
          setError(
            `Recommendation review is unavailable (${safeErrorCode(cause)}).`,
          );
        }
      }
    },
    [api, workRecordID],
  );

  const loadJob = useCallback(
    async (jobID: string, mode: "mount" | "poll" | "refresh" = "refresh") => {
      if (mode === "poll" && (jobLoadInFlight.current || operationRef.current))
        return;
      if (jobLoadInFlight.current) statusController.current?.abort();
      jobLoadInFlight.current = true;
      const controller = new AbortController();
      statusController.current = controller;
      const version = ++requestVersion.current;
      if (mode === "refresh") {
        operationRef.current = "refresh";
        setOperation("refresh");
      }
      try {
        const next = await api.getJob(jobID, controller.signal);
        if (
          controller.signal.aborted ||
          requestVersion.current !== version ||
          next.workRecordID !== workRecordID
        ) {
          if (next.workRecordID !== workRecordID) clearStoredJob();
          return;
        }
        setError("");
        setJob(next);
        retainJob(next);
        if (next.recommendationID)
          void loadRecommendation(
            next.recommendationID,
            controller.signal,
            version,
          );
      } catch (cause) {
        if (!controller.signal.aborted && requestVersion.current === version) {
          if (isDefinitivelyUnavailable(cause)) {
            clearStoredJob();
            setJob(undefined);
            setRecommendation(undefined);
          }
          setError(
            `Generation status is unavailable (${safeErrorCode(cause)}).`,
          );
        }
      } finally {
        if (statusController.current === controller) {
          jobLoadInFlight.current = false;
          statusController.current = undefined;
        }
        if (requestVersion.current === version) {
          if (mode === "refresh") {
            operationRef.current = undefined;
            setOperation(undefined);
          }
        }
      }
    },
    [api, clearStoredJob, loadRecommendation, retainJob, workRecordID],
  );

  useEffect(() => {
    setJob(undefined);
    setRecommendation(undefined);
    setError("");
    const saved = readStoredJob(key);
    setSavedJobID(saved?.jobID);
    if (saved) void loadJob(saved.jobID, "mount");
    return () => {
      statusController.current?.abort();
      statusController.current = undefined;
      jobLoadInFlight.current = false;
      actionController.current?.abort();
    };
  }, [key, loadJob]);

  useEffect(() => {
    if (!job || !isActive(job)) return;
    const tick = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(tick);
  }, [job?.id, job?.state]);

  useEffect(() => {
    if (!job || !isActive(job)) return;
    const delay = Math.max(
      POLL_INTERVAL_MS,
      (job.retryAfterSeconds ?? 0) * 1_000,
    );
    const timer = window.setTimeout(() => void loadJob(job.id, "poll"), delay);
    return () => window.clearTimeout(timer);
  }, [job?.id, job?.retryAfterSeconds, job?.state, loadJob]);

  const runAction = useCallback(
    async (
      name: NonNullable<typeof operation>,
      action: (signal: AbortSignal) => Promise<AIAssistJob>,
    ) => {
      if (
        operationRef.current &&
        !(name === "cancel" && operationRef.current === "refresh")
      )
        return;
      statusController.current?.abort();
      statusController.current = undefined;
      jobLoadInFlight.current = false;
      const controller = new AbortController();
      actionController.current = controller;
      const version = ++requestVersion.current;
      operationRef.current = name;
      setOperation(name);
      setError("");
      try {
        const next = await action(controller.signal);
        if (controller.signal.aborted || requestVersion.current !== version)
          return;
        setJob(next);
        if (name === "cancel" && next.state === "cancelled") clearStoredJob();
        else retainJob(next);
        if (next.recommendationID)
          void loadRecommendation(
            next.recommendationID,
            controller.signal,
            version,
          );
      } catch (cause) {
        if (!controller.signal.aborted && requestVersion.current === version) {
          setError(
            `Generation request could not be completed (${safeErrorCode(cause)}).`,
          );
        }
      } finally {
        if (requestVersion.current === version) {
          operationRef.current = undefined;
          setOperation(undefined);
        }
      }
    },
    [clearStoredJob, loadRecommendation, retainJob],
  );

  const submit = (feature: AIFeature) => {
    if (!generationEnabled) return;
    const idempotencyKey = crypto.randomUUID();
    void runAction("submit", (signal) =>
      api.submit(workRecordID, { feature, idempotencyKey }, signal),
    );
  };
  const cancel = () => {
    if (!job || !cancelReason.trim()) return;
    void runAction("cancel", (signal) =>
      api.cancel(
        job.id,
        { expectedVersion: job.version, reason: cancelReason.trim() },
        signal,
      ),
    );
  };
  const retry = () => {
    if (!job || !retryReason.trim()) return;
    void runAction("retry", (signal) =>
      api.retry(
        job.id,
        { expectedVersion: job.version, reason: retryReason.trim() },
        signal,
      ),
    );
  };
  const decide = async (decision: "accepted" | "rejected") => {
    if (!recommendation || !decisionReason.trim() || operationRef.current)
      return;
    statusController.current?.abort();
    statusController.current = undefined;
    jobLoadInFlight.current = false;
    const controller = new AbortController();
    actionController.current = controller;
    const version = ++requestVersion.current;
    operationRef.current = "decision";
    setOperation("decision");
    setError("");
    try {
      const result = await api.decide(
        recommendation.id,
        { decision, reason: decisionReason.trim() },
        controller.signal,
      );
      if (controller.signal.aborted || requestVersion.current !== version)
        return;
      setRecommendation((current) =>
        current ? { ...current, state: result.state } : current,
      );
      setNotice(
        `${result.state === "accepted" ? "Accepted" : "Rejected"} recorded. No changes were applied or sent.`,
      );
    } catch (cause) {
      if (!controller.signal.aborted && requestVersion.current === version) {
        setError(
          `Recommendation decision could not be recorded (${safeErrorCode(cause)}).`,
        );
      }
    } finally {
      if (requestVersion.current === version) {
        operationRef.current = undefined;
        setOperation(undefined);
      }
    }
  };

  const retryable =
    job?.state === "failed" &&
    !!job.safeErrorCode &&
    retryableFailureCodes.has(job.safeErrorCode);
  const busy = !!operation;

  const refreshAction =
    job || savedJobID ? (
      <Button
        disabled={busy}
        onClick={() => void loadJob(job?.id ?? savedJobID ?? "")}
      >
        Refresh status
      </Button>
    ) : undefined;

  const content = (
    <div className="ai-assist-page" data-embedded={embedded}>
      {embedded && refreshAction ? (
        <div className="ai-assist-embedded__actions">{refreshAction}</div>
      ) : null}
      <section
        className="ai-assist-panel"
        aria-labelledby="assist-actions-title"
      >
        <h2 id="assist-actions-title">Create a recommendation</h2>
        <p>
          Only approved field names are disclosed here. Values, attachments,
          credentials, and provider request details stay out of this workspace.
        </p>
        <p>
          AI output is a technician-reviewed recommendation. It never applies a
          record change or sends a message.
        </p>
        {generationEnabled ? (
          <p>Work Record ID: {workRecordID}</p>
        ) : (
          <p>
            Provide a Work Record UUID in the AI assist route before live
            generation is available.
          </p>
        )}
        <div className="ai-assist-actions">
          {supportedFeatures.map((feature) => (
            <button
              key={feature}
              type="button"
              className="ai-primary"
              disabled={!generationEnabled || busy || isActive(job)}
              onClick={() => submit(feature)}
            >
              Generate {featureLabels[feature]}
            </button>
          ))}
        </div>
      </section>

      {error ? (
        <div ref={errorRef} tabIndex={-1}>
          <Notice tone="danger" title="AI assistance failed" urgent>
            {error}
          </Notice>
        </div>
      ) : null}
      <div className="ai-status" aria-live="polite">
        {notice}
      </div>

      {job ? (
        <section
          className="ai-assist-panel ai-generation-status"
          aria-labelledby="generation-status-title"
        >
          <div className="panel-heading">
            <div>
              <h2 id="generation-status-title">Generation status</h2>
              {isActive(job) ? (
                <p>Generating {featureLabels[job.feature]}…</p>
              ) : (
                <p>
                  {job.state === "completed"
                    ? "Generation completed."
                    : job.state === "cancelled"
                      ? "Generation cancelled."
                      : `Generation could not complete (${job.safeErrorCode ?? "request_failed"}).`}
                </p>
              )}
            </div>
            <span className="ai-health">
              {isActive(job)
                ? `${job.state} • ${elapsed(job, now)}s`
                : job.state}
            </span>
          </div>
          {job.retryAfterSeconds ? (
            <p>Next status check respects the server retry interval.</p>
          ) : null}
          {isActive(job) ? (
            <div className="ai-inline-form">
              <label>
                Cancellation reason
                <input
                  aria-label="Cancellation reason"
                  value={cancelReason}
                  onChange={(event) => setCancelReason(event.target.value)}
                />
              </label>
              <button
                type="button"
                className="ai-secondary"
                disabled={
                  (!!operation && operation !== "refresh") ||
                  !cancelReason.trim()
                }
                onClick={cancel}
              >
                Cancel generation
              </button>
            </div>
          ) : null}
          {retryable ? (
            <div className="ai-inline-form">
              <label>
                Retry reason
                <input
                  aria-label="Retry reason"
                  value={retryReason}
                  onChange={(event) => setRetryReason(event.target.value)}
                />
              </label>
              <button
                type="button"
                className="ai-secondary"
                disabled={busy || !retryReason.trim()}
                onClick={retry}
              >
                Retry generation
              </button>
            </div>
          ) : null}
        </section>
      ) : null}

      {!job && savedJobID ? (
        <section
          className="ai-assist-panel"
          aria-labelledby="saved-generation-title"
        >
          <h2 id="saved-generation-title">Saved generation</h2>
          <p>
            The durable job is still saved in this browser session. Refresh
            status to reconnect without resubmitting it.
          </p>
        </section>
      ) : null}

      {recommendation ? (
        <section
          className="ai-assist-panel ai-recommendation"
          aria-labelledby="recommendation-title"
        >
          <div className="panel-heading">
            <div>
              <h2 id="recommendation-title">Recommendation</h2>
              <p
                className={
                  recommendation.state === "pending_human"
                    ? "ai-pending-human"
                    : ""
                }
              >
                {recommendation.state === "pending_human"
                  ? "Pending human review"
                  : `${recommendation.state === "accepted" ? "Accepted" : "Rejected"} recommendation`}
              </p>
            </div>
          </div>
          <p className="ai-recommendation-text">{recommendation.text}</p>
          <dl className="ai-review-facts">
            <div>
              <dt>Approved input fields</dt>
              <dd>{recommendation.relevantInputs.join(", ") || "None"}</dd>
            </div>
            <div>
              <dt>Authorized related records</dt>
              <dd>
                {recommendation.candidateIDs.length ? (
                  <ul>
                    {recommendation.candidateIDs.map((candidateID) => (
                      <li key={candidateID}>
                        {uuidPattern.test(candidateID) ? (
                          <a
                            href={`#/ai-assist?workRecordID=${encodeURIComponent(candidateID)}`}
                          >
                            {candidateID}
                          </a>
                        ) : (
                          candidateID
                        )}
                      </li>
                    ))}
                  </ul>
                ) : (
                  "None"
                )}
              </dd>
            </div>
          </dl>
          {recommendation.state === "pending_human" ? (
            <div className="ai-decision-form">
              <label>
                Decision reason
                <textarea
                  aria-label="Decision reason"
                  value={decisionReason}
                  onChange={(event) => setDecisionReason(event.target.value)}
                />
              </label>
              <div className="ai-assist-actions">
                <button
                  type="button"
                  className="ai-primary"
                  disabled={busy || !decisionReason.trim()}
                  onClick={() => void decide("accepted")}
                >
                  Accept recommendation
                </button>
                <button
                  type="button"
                  className="ai-secondary"
                  disabled={busy || !decisionReason.trim()}
                  onClick={() => void decide("rejected")}
                >
                  Reject recommendation
                </button>
              </div>
            </div>
          ) : (
            <p>
              Recorded for human review history. No changes were applied or
              sent.
            </p>
          )}
        </section>
      ) : null}
    </div>
  );

  if (embedded) return content;

  return (
    <Page
      eyebrow="Technician workspace"
      title="AI assistance"
      description="Generate a durable, review-only recommendation for this work record."
      actions={refreshAction}
    >
      {content}
    </Page>
  );
}
