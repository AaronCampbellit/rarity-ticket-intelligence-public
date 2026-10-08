import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Notice, Page, StatePanel, TagInput } from "../../design-system";

type Stage = {
  id: string;
  key: string;
  name: string;
  position: number;
  probability: number;
  forecast_category: string;
  required_fields: string[];
  allowed_next_stage_ids: string[];
  requires_proposal: boolean;
  requires_approval: boolean;
};

type Pipeline = {
  id: string;
  key: string;
  name: string;
  stages: Stage[];
};

const categories = [
  "pipeline",
  "weighted",
  "committed",
  "closed_won",
  "closed_lost",
];

function newStage(position: number): Stage {
  return {
    id: crypto.randomUUID(),
    key: "",
    name: "",
    position,
    probability: 0,
    forecast_category: "pipeline",
    required_fields: [],
    allowed_next_stage_ids: [],
    requires_proposal: false,
    requires_approval: false,
  };
}

function defaultStages(): Stage[] {
  return [
    newStage(1),
    newStage(2),
    {
      ...newStage(3),
      key: "closed-won",
      name: "Closed won",
      probability: 100,
      forecast_category: "closed_won",
    },
  ];
}

export function PipelineSettingsPage({
  capabilities,
}: {
  capabilities: ReadonlySet<string>;
}) {
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [stages, setStages] = useState<Stage[]>(defaultStages);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );

  const allowed = capabilities.has("pipeline.create");
  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch("/api/v1/pipelines", {
      credentials: "same-origin",
      signal,
    });
    if (!response.ok) throw new Error("Pipeline settings unavailable");
    setPipelines((await response.json()) as Pipeline[]);
    setState("ready");
  }, []);

  useEffect(() => {
    if (!allowed) {
      setState("ready");
      return;
    }
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [allowed, load]);

  function updateStage(index: number, update: Partial<Stage>) {
    setStages((current) =>
      current.map((stage, stageIndex) =>
        stageIndex === index ? { ...stage, ...update } : stage,
      ),
    );
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    const configured = stages.map((stage, index) => ({
      ...stage,
      position: index + 1,
      allowed_next_stage_ids:
        index + 1 < stages.length ? [stages[index + 1].id] : [],
    }));
    try {
      const response = await fetch("/api/v1/pipelines", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          key: form.get("key"),
          name: form.get("name"),
          stages: configured,
        }),
      });
      if (!response.ok) throw new Error("Pipeline creation failed");
      setStages(defaultStages());
      formElement.reset();
      await load();
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Sales configuration"
      title="Sales pipelines"
      description="Create ordered, MSP-wide sales processes with explicit forecast and proposal gates. Existing pipelines remain immutable audit records."
    >
      <div className="operations-health">
        {!allowed ? (
          <Notice
            tone="warning"
            title="Pipeline administration required"
            urgent
          >
            Pipeline settings require pipeline administration access.
          </Notice>
        ) : null}
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading pipelines"
            description="Retrieving MSP-wide sales processes."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Pipeline settings are unavailable"
            description="Verify the values and permissions."
            supportCode="PIPELINE-SETTINGS"
          />
        ) : null}
        {allowed ? (
          <section className="settings-card" aria-labelledby="create-pipeline">
            <h2 id="create-pipeline">Create pipeline</h2>
            <form onSubmit={create}>
              <div className="settings-grid">
                <label>
                  Name
                  <input name="name" required />
                </label>
                <label>
                  Key
                  <input name="key" required pattern="[a-z0-9_-]+" />
                </label>
              </div>
              {stages.map((stage, index) => (
                <fieldset key={stage.id}>
                  <legend>Stage {index + 1}</legend>
                  <div className="settings-grid">
                    <label>
                      Name
                      <input
                        required
                        value={stage.name}
                        onChange={(event) =>
                          updateStage(index, { name: event.target.value })
                        }
                      />
                    </label>
                    <label>
                      Key
                      <input
                        required
                        pattern="[a-z0-9_-]+"
                        value={stage.key}
                        onChange={(event) =>
                          updateStage(index, { key: event.target.value })
                        }
                      />
                    </label>
                    <label>
                      Probability
                      <input
                        type="number"
                        min="0"
                        max="100"
                        required
                        value={stage.probability}
                        onChange={(event) =>
                          updateStage(index, {
                            probability: Number(event.target.value),
                          })
                        }
                      />
                    </label>
                    <label>
                      Forecast category
                      <select
                        value={stage.forecast_category}
                        onChange={(event) =>
                          updateStage(index, {
                            forecast_category: event.target.value,
                          })
                        }
                      >
                        {categories.map((category) => (
                          <option key={category}>{category}</option>
                        ))}
                      </select>
                    </label>
                    <TagInput
                      label="Required fields"
                      values={stage.required_fields}
                      onChange={(requiredFields) =>
                        updateStage(index, {
                          required_fields: requiredFields,
                        })
                      }
                      placeholder="expected_close_on"
                    />
                    <label>
                      <input
                        type="checkbox"
                        checked={stage.requires_proposal}
                        onChange={(event) =>
                          updateStage(index, {
                            requires_proposal: event.target.checked,
                          })
                        }
                      />{" "}
                      Proposal required
                    </label>
                    <label>
                      <input
                        type="checkbox"
                        checked={stage.requires_approval}
                        onChange={(event) =>
                          updateStage(index, {
                            requires_approval: event.target.checked,
                          })
                        }
                      />{" "}
                      Approval required
                    </label>
                  </div>
                  {stages.length > 1 ? (
                    <button
                      type="button"
                      onClick={() =>
                        setStages((current) =>
                          current.filter(
                            (_, stageIndex) => stageIndex !== index,
                          ),
                        )
                      }
                    >
                      Remove stage
                    </button>
                  ) : null}
                </fieldset>
              ))}
              <button
                type="button"
                onClick={() =>
                  setStages((current) => [
                    ...current,
                    newStage(current.length + 1),
                  ])
                }
              >
                Add stage
              </button>{" "}
              <button type="submit" disabled={state === "saving"}>
                Create immutable pipeline
              </button>
            </form>
          </section>
        ) : null}
        <section aria-labelledby="configured-pipelines">
          <h2 id="configured-pipelines">Configured pipelines</h2>
          {allowed && state === "ready" && !pipelines.length ? (
            <p>No pipelines are configured.</p>
          ) : null}
          <div className="health-grid">
            {pipelines.map((pipeline) => (
              <article key={pipeline.id}>
                <p className="record-id">{pipeline.key}</p>
                <h3>{pipeline.name}</h3>
                <ol>
                  {pipeline.stages.map((stage) => (
                    <li key={stage.id}>
                      <strong>{stage.name}</strong> — {stage.probability}% ·{" "}
                      {stage.forecast_category.replaceAll("_", " ")}
                      {stage.requires_proposal ? " · proposal required" : ""}
                      {stage.requires_approval ? " · approval required" : ""}
                    </li>
                  ))}
                </ol>
              </article>
            ))}
          </div>
        </section>
      </div>
    </Page>
  );
}
