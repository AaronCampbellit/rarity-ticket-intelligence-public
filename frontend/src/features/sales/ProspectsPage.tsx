import { type FormEvent, useCallback, useEffect, useState } from "react";

import { Notice, Page, StatePanel } from "../../design-system";
import {
  createProspect,
  createProspectOpportunity,
  listPipelines,
  listProspects,
  type PipelineResponse,
  type ProspectResponse,
} from "./api";
import "./sales.css";

export function ProspectsPage({
  capabilities,
}: {
  capabilities: ReadonlySet<string>;
}) {
  const [prospects, setProspects] = useState<ProspectResponse[]>([]);
  const [pipelines, setPipelines] = useState<PipelineResponse[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const selected =
    prospects.find((prospect) => prospect.id === selectedID) ?? prospects[0];

  const reload = useCallback(async (signal?: AbortSignal) => {
    const [found, configured] = await Promise.all([
      listProspects(signal),
      listPipelines(signal),
    ]);
    setProspects(found);
    setPipelines(configured);
    setSelectedID((current) =>
      found.some((prospect) => prospect.id === current)
        ? current
        : (found[0]?.id ?? ""),
    );
    setState("ready");
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void reload(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [reload]);

  async function addProspect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setMessage("");
    try {
      const created = await createProspect({
        displayID: String(form.get("display_id") ?? ""),
        name: String(form.get("name") ?? ""),
        email: String(form.get("email") ?? ""),
        phone: String(form.get("phone") ?? ""),
      });
      formElement.reset();
      await reload();
      setSelectedID(created.id);
      setMessage("Prospect created.");
    } catch {
      setState("error");
    }
  }

  async function addOpportunity(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const pipeline = pipelines.find(
      (candidate) => candidate.id === form.get("pipeline_id"),
    );
    const initialStage = pipeline?.stages[0];
    if (!pipeline || !initialStage) return;
    setState("saving");
    setMessage("");
    try {
      await createProspectOpportunity({
        prospectID: selected.id,
        pipelineID: pipeline.id,
        stageID: initialStage.id,
        displayID: String(form.get("display_id") ?? ""),
        name: String(form.get("name") ?? ""),
        description: String(form.get("description") ?? ""),
        amountMinor: Math.round(Number(form.get("amount") ?? 0) * 100),
        currency: String(form.get("currency") ?? "USD"),
        ownerID: String(form.get("owner_id") ?? ""),
        expectedCloseOn: String(form.get("expected_close_on") ?? ""),
      });
      formElement.reset();
      setState("ready");
      setMessage("Opportunity created from the Prospect.");
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Pre-client sales"
      title="Prospects"
      description="Qualify organizations before Client conversion while preserving lineage into Opportunities, Proposals, and Projects."
    >
      <div className="sales-worklist">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading prospects"
            description="Retrieving pre-client sales records."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Prospects are unavailable"
            description="Verify access and values."
            supportCode="PROSPECTS-UNAVAILABLE"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Prospect updated">
            {message}
          </Notice>
        ) : null}
        {capabilities.has("prospect.create") ? (
          <details className="detail-card">
            <summary>Create prospect</summary>
            <form onSubmit={addProspect} className="settings-grid">
              <label>
                Display ID
                <input name="display_id" required />
              </label>
              <label>
                Name
                <input name="name" required />
              </label>
              <label>
                Email
                <input name="email" type="email" />
              </label>
              <label>
                Phone
                <input name="phone" type="tel" />
              </label>
              <button type="submit" disabled={state === "saving"}>
                Create prospect
              </button>
            </form>
          </details>
        ) : null}
        {state === "ready" && !prospects.length ? (
          <StatePanel
            state="empty"
            title="No active prospects"
            description="No prospects are currently being qualified."
          />
        ) : null}
        <div className="sales-worklist-grid">
          <section aria-label="Prospect list">
            {prospects.map((prospect) => (
              <button
                type="button"
                className={
                  selected?.id === prospect.id
                    ? "worklist-row active"
                    : "worklist-row"
                }
                key={prospect.id}
                onClick={() => setSelectedID(prospect.id)}
              >
                <span>
                  <strong>{prospect.display_id}</strong>
                  <small>{prospect.name}</small>
                </span>
                <span>
                  {prospect.email || prospect.phone || "No contact details"}
                </span>
              </button>
            ))}
          </section>
          {selected ? (
            <section className="detail-card" aria-label="Selected prospect">
              <p className="record-id">{selected.display_id}</p>
              <h2>{selected.name}</h2>
              <p>
                {selected.email || "No email"} · {selected.phone || "No phone"}
              </p>
              {capabilities.has("opportunity.create") ? (
                <details>
                  <summary>Create opportunity from Prospect</summary>
                  <form onSubmit={addOpportunity} className="settings-grid">
                    <label>
                      Display ID
                      <input name="display_id" required />
                    </label>
                    <label>
                      Name
                      <input name="name" required />
                    </label>
                    <label>
                      Pipeline
                      <select name="pipeline_id" required>
                        <option value="">Select pipeline</option>
                        {pipelines.map((pipeline) => (
                          <option key={pipeline.id} value={pipeline.id}>
                            {pipeline.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Amount
                      <input
                        name="amount"
                        type="number"
                        min="0"
                        step="0.01"
                        required
                      />
                    </label>
                    <label>
                      Currency
                      <input
                        name="currency"
                        defaultValue="USD"
                        minLength={3}
                        maxLength={3}
                        required
                      />
                    </label>
                    <label>
                      Expected close
                      <input name="expected_close_on" type="date" />
                    </label>
                    <label>
                      Owner ID
                      <input name="owner_id" />
                    </label>
                    <label>
                      Description
                      <textarea name="description" />
                    </label>
                    <button
                      type="submit"
                      disabled={state === "saving" || !pipelines.length}
                    >
                      Create linked opportunity
                    </button>
                  </form>
                </details>
              ) : null}
            </section>
          ) : null}
        </div>
      </div>
    </Page>
  );
}
