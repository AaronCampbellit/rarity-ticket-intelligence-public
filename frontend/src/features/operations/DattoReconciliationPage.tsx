import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import { Notice, Page, StatePanel } from "../../design-system";
import "./operations.css";

type Candidate = {
  id: string;
  snapshot_id: string;
  client_id: string;
  asset_id: string;
  evidence: Record<string, unknown>;
  state: string;
};

export function DattoReconciliationPage({ clientID }: { clientID: string }) {
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!clientID) return;
      const response = await fetch(
        "/api/v1/integrations/datto/reconciliation?limit=100",
        {
          credentials: "same-origin",
          headers: clientContextHeaders(clientID),
          signal,
        },
      );
      if (!response.ok) throw new Error("datto reconciliation unavailable");
      const body = (await response.json()) as { items: Candidate[] };
      setCandidates(body.items);
      setState("ready");
    },
    [clientID],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  async function decide(
    event: FormEvent<HTMLFormElement>,
    candidate: Candidate,
  ) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/integrations/datto/reconciliation/${encodeURIComponent(candidate.id)}/decide`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(clientID),
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            decision: form.get("decision"),
            reason: form.get("reason"),
          }),
        },
      );
      if (!response.ok) throw new Error("Datto decision failed");
      await load();
      setMessage("Reconciliation decision recorded.");
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Asset management"
      title="Datto reconciliation"
      description="Review ambiguous asset matches. Every decision is scoped and auditable; missing assets are never silently deleted."
    >
      <div className="operations-health">
        {!clientID ? (
          <StatePanel
            state="empty"
            title="Select a Client"
            description="Select a Client to review Datto candidates."
          />
        ) : null}
        {state === "loading" && clientID ? (
          <StatePanel
            state="loading"
            title="Loading reconciliation candidates"
            description="Retrieving unresolved asset evidence."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Datto reconciliation is unavailable"
            description="Verify permissions and integration health."
            supportCode="DATTO-RECONCILIATION"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Decision recorded">
            {message}
          </Notice>
        ) : null}
        {state === "ready" && !candidates.length ? (
          <section className="operations-empty">
            <h2>No candidates need review</h2>
            <p>
              Datto and Rarity asset evidence has no unresolved ambiguity for
              this client.
            </p>
          </section>
        ) : null}
        {candidates.length ? (
          <section
            className="health-grid"
            aria-label="Datto reconciliation candidates"
          >
            {candidates.map((candidate) => (
              <article key={candidate.id}>
                <header>
                  <div>
                    <p className="record-id">{candidate.state}</p>
                    <h2>Asset {candidate.asset_id}</h2>
                  </div>
                </header>
                <dl>
                  {Object.entries(candidate.evidence).map(([key, value]) => (
                    <div key={key}>
                      <dt>{humanize(key)}</dt>
                      <dd>
                        <EvidenceValue value={value} />
                      </dd>
                    </div>
                  ))}
                </dl>
                <form onSubmit={(event) => void decide(event, candidate)}>
                  <label>
                    Decision
                    <select name="decision" defaultValue="link">
                      <option value="link">Link records</option>
                      <option value="choose_rarity">Use Rarity values</option>
                      <option value="choose_datto">Use Datto values</option>
                      <option value="keep_separate">Keep separate</option>
                    </select>
                  </label>
                  <label>
                    Reason
                    <input name="reason" required />
                  </label>
                  <button type="submit" disabled={state === "saving"}>
                    Record decision
                  </button>
                </form>
              </article>
            ))}
          </section>
        ) : null}
      </div>
    </Page>
  );
}

function humanize(value: string): string {
  return value.replaceAll("_", " ");
}

function EvidenceValue({ value }: { value: unknown }) {
  if (value == null) return <>Not provided</>;
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  )
    return <>{typeof value === "boolean" ? (value ? "Yes" : "No") : value}</>;
  if (Array.isArray(value)) {
    return (
      <ul className="evidence-values">
        {value.map((item, index) => (
          <li key={index}>
            <EvidenceValue value={item} />
          </li>
        ))}
      </ul>
    );
  }
  if (typeof value === "object") {
    return (
      <dl className="evidence-details">
        {Object.entries(value as Record<string, unknown>).map(([key, item]) => (
          <div key={key}>
            <dt>{humanize(key)}</dt>
            <dd>
              <EvidenceValue value={item} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return <>Not provided</>;
}
