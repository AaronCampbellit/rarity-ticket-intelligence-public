import { useEffect, useState } from "react";

import { Button, Page, StatePanel, StatusBadge } from "../../design-system";
import "./operations.css";

type HealthState = "disabled" | "healthy" | "degraded" | "failed";

type ConnectionHealth = {
  id: string;
  kind: string;
  state: HealthState;
  reason: string;
  last_error_code?: string;
  freshness_lag_nanoseconds: number;
  queue_delay_nanoseconds: number;
  consecutive_failures: number;
  pending_failures: number;
};

type HealthSnapshot = {
  state: HealthState;
  generated_at: string;
  connections: ConnectionHealth[];
};

export function OperationsHealthPage() {
  const [snapshot, setSnapshot] = useState<HealthSnapshot | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/api/v1/integrations/health", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok)
          throw new Error(`integration_health_${response.status}`);
        return (await response.json()) as HealthSnapshot;
      })
      .then((value) => {
        setSnapshot(value);
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [loadAttempt]);

  return (
    <Page
      eyebrow="Operations"
      title="Integration and delivery health"
      description="Authoritative connection freshness, queue delay, and delivery failures from durable platform evidence."
      actions={
        snapshot ? (
          <StatusBadge tone={healthTone(snapshot.state)}>
            {label(snapshot.state)}
          </StatusBadge>
        ) : undefined
      }
    >
      <div className="operations-health">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading integration health"
            description="Retrieving durable connection and delivery evidence."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Integration health is unavailable"
            description="Verify permissions and platform readiness, then retry."
            action={
              <Button
                intent="primary"
                onClick={() => setLoadAttempt((current) => current + 1)}
              >
                Retry
              </Button>
            }
            supportCode="INTEGRATION-HEALTH"
          />
        ) : null}
        {state === "ready" && snapshot?.connections.length === 0 ? (
          <section className="operations-empty">
            <h2>No integration connections</h2>
            <p>
              Configure Graph, forwarding, Datto, Teams, or webhook connections
              to begin collecting health evidence.
            </p>
          </section>
        ) : null}
        {snapshot?.connections.length ? (
          <section aria-label="Connection health" className="health-grid">
            {snapshot.connections.map((connection) => (
              <article key={`${connection.kind}-${connection.id}`}>
                <header>
                  <div>
                    <p className="record-id">{connection.kind}</p>
                    <h2>{connection.id}</h2>
                  </div>
                  <span className={`health-state health-${connection.state}`}>
                    {label(connection.state)}
                  </span>
                </header>
                <dl>
                  <div>
                    <dt>Reason</dt>
                    <dd>{humanize(connection.reason)}</dd>
                  </div>
                  <div>
                    <dt>Freshness lag</dt>
                    <dd>{duration(connection.freshness_lag_nanoseconds)}</dd>
                  </div>
                  <div>
                    <dt>Queue delay</dt>
                    <dd>{duration(connection.queue_delay_nanoseconds)}</dd>
                  </div>
                  <div>
                    <dt>Pending failures</dt>
                    <dd>{connection.pending_failures}</dd>
                  </div>
                  <div>
                    <dt>Consecutive failures</dt>
                    <dd>{connection.consecutive_failures}</dd>
                  </div>
                  <div>
                    <dt>Last error</dt>
                    <dd>{connection.last_error_code || "None"}</dd>
                  </div>
                </dl>
              </article>
            ))}
          </section>
        ) : null}
        {snapshot ? (
          <p className="health-generated">
            Evaluated {new Date(snapshot.generated_at).toLocaleString()}
          </p>
        ) : null}
      </div>
    </Page>
  );
}

function healthTone(state: HealthState) {
  if (state === "healthy") return "success" as const;
  if (state === "degraded") return "warning" as const;
  if (state === "failed") return "danger" as const;
  return "neutral" as const;
}

function label(state: HealthState): string {
  return state.charAt(0).toUpperCase() + state.slice(1);
}

function humanize(value: string): string {
  return value ? value.replaceAll("_", " ") : "No reason reported";
}

function duration(nanoseconds: number): string {
  if (!nanoseconds) return "None";
  const seconds = Math.max(0, Math.round(nanoseconds / 1_000_000_000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.round(minutes / 60)}h`;
}
