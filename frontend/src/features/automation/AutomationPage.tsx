import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import {
  AutomationStepBuilder,
  Notice,
  Page,
  ScopeBuilder,
  StatePanel,
  automationStepsFromForm,
} from "../../design-system";
import { useClientClassificationCatalog } from "../classification/useClientCatalog";
import type { Tag, TagGroup } from "../classification/types";
import "./automation.css";

type Step = Record<string, unknown>;

type ManagedDefinition = {
  id: string;
  name: string;
  version: number;
  state: "draft" | "published";
  record_version: number;
  trigger: { event_type: string };
  capabilities: string[];
  steps: Step[];
};

type DeadLetter = {
  id: string;
  automation_id: string;
  automation_version: number;
  created_at: string;
  error_code: string;
  safe_message: string;
  state: "open" | "retrying" | "replayed" | "dismissed";
};

type ExternalConnection = {
  id: string;
  msp_id: string;
  client_id: string;
  name: string;
  endpoint: string;
};

const defaultSteps: Step[] = [
  {
    id: "acknowledge",
    kind: "action",
    action: {
      kind: "add_comment",
      parameters: { body: "Automation acknowledged this work record." },
    },
  },
];

export function AutomationPage({
  clientID,
  capabilities,
}: {
  clientID: string;
  capabilities: ReadonlySet<string>;
}) {
  const canManage = capabilities.has("automation.manage");
  const canManageDeadLetters = capabilities.has(
    "automation.dead_letter.manage",
  );
  const { catalog } = useClientClassificationCatalog(canManage ? clientID : "");
  const [definitions, setDefinitions] = useState<ManagedDefinition[]>([]);
  const [deadLetters, setDeadLetters] = useState<DeadLetter[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!clientID) return;
      const options = {
        credentials: "same-origin" as const,
        headers: clientContextHeaders(clientID),
        signal,
      };
      const [foundDefinitions, foundDeadLetters] = await Promise.all([
        canManage
          ? fetch("/api/v1/automation/definitions", options).then(
              parse<ManagedDefinition[]>,
            )
          : Promise.resolve([]),
        canManageDeadLetters
          ? fetch("/api/v1/automation/dead-letters", options).then(
              parse<DeadLetter[]>,
            )
          : Promise.resolve([]),
      ]);
      setDefinitions(foundDefinitions);
      setDeadLetters(foundDeadLetters);
      setState("ready");
    },
    [canManage, canManageDeadLetters, clientID],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate("/api/v1/automation/definitions", {
      name: form.get("name"),
      trigger: { event_type: form.get("event_type") },
      capabilities: form.getAll("capabilities").map(String),
      steps: automationStepsFromForm(form),
    });
  }

  async function revise(
    event: FormEvent<HTMLFormElement>,
    definition: ManagedDefinition,
  ) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      `/api/v1/automation/definitions/${encodeURIComponent(definition.id)}/versions/${definition.version}/revisions`,
      {
        expected_version: definition.record_version,
        trigger: { event_type: form.get("event_type") },
        capabilities: form.getAll("capabilities").map(String),
        steps: automationStepsFromForm(form),
      },
    );
  }

  async function publish(definition: ManagedDefinition) {
    await mutate(
      `/api/v1/automation/definitions/${encodeURIComponent(definition.id)}/versions/${definition.version}/publish`,
      { expected_version: definition.record_version },
    );
  }

  async function act(event: FormEvent<HTMLFormElement>, letter: DeadLetter) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      `/api/v1/automation/dead-letters/${encodeURIComponent(letter.id)}/actions`,
      { action: form.get("action"), reason: form.get("reason") },
    );
  }

  async function createExternalConnection(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch("/api/v1/automation/connections", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...clientContextHeaders(clientID),
        },
        body: JSON.stringify({
          name: form.get("name"),
          endpoint: form.get("endpoint"),
          signing_secret_ref: form.get("signing_secret_ref"),
        }),
      });
      const connection = await parse<ExternalConnection>(response);
      await load();
      setMessage(`External connection created: ${connection.id}`);
    } catch {
      setState("error");
    }
  }

  async function mutate(endpoint: string, body: Record<string, unknown>) {
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...clientContextHeaders(clientID),
        },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error(`automation_${response.status}`);
      await load();
      setMessage("Automation configuration updated.");
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Automation administration"
      title="Definitions and dead letters"
      description="Publish bounded typed automation and resolve failed runs with explicit reasoned actions."
    >
      <div className="automation-page">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading automation"
            description="Retrieving definitions and dead letters."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Automation could not be loaded or changed"
            description="Verify scope, workflow steps, version, permissions, and action reason."
            supportCode="AUTOMATION-UNAVAILABLE"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Automation updated">
            {message}
          </Notice>
        ) : null}
        {canManage ? (
          <section aria-labelledby="external-connections-heading">
            <div className="section-heading">
              <div>
                <p className="record-id">Scoped provider configuration</p>
                <h2 id="external-connections-heading">
                  External HTTP connections
                </h2>
              </div>
            </div>
            <form
              className="definition-form automation-connection"
              onSubmit={(event) => void createExternalConnection(event)}
            >
              <label>
                <span>Connection name</span>
                <input name="name" required />
              </label>
              <label>
                <span>HTTPS endpoint</span>
                <input name="endpoint" type="url" required />
              </label>
              <label>
                <span>Signing secret environment reference</span>
                <input
                  name="signing_secret_ref"
                  placeholder="env://RARITY_AUTOMATION_HTTP_SECRET_PRIMARY"
                  pattern="env://RARITY_AUTOMATION_HTTP_SECRET_[A-Z0-9_]+"
                  required
                />
              </label>
              <button type="submit" disabled={state === "saving"}>
                {state === "saving" ? "Saving…" : "Create external connection"}
              </button>
            </form>
          </section>
        ) : null}
        {canManage ? (
          <section>
            <div className="section-heading">
              <div>
                <p className="record-id">Versioned configuration</p>
                <h2>Automation definitions</h2>
              </div>
            </div>
            <details className="automation-create">
              <summary>Create automation definition</summary>
              <DefinitionForm
                groups={catalog?.groups ?? []}
                tags={catalog?.tags ?? []}
                saving={state === "saving"}
                submitLabel="Create draft"
                onSubmit={create}
              />
            </details>
            <div className="automation-grid">
              {definitions.map((definition) => (
                <article key={`${definition.id}-${definition.version}`}>
                  <header>
                    <div>
                      <p className="record-id">Version {definition.version}</p>
                      <h3>{definition.name}</h3>
                    </div>
                    <span>{definition.state}</span>
                  </header>
                  {definition.state === "published" ? (
                    <DefinitionForm
                      groups={catalog?.groups ?? []}
                      tags={catalog?.tags ?? []}
                      definition={definition}
                      saving={state === "saving"}
                      submitLabel="Create revision"
                      onSubmit={(event) => revise(event, definition)}
                    />
                  ) : (
                    <dl>
                      <dt>Trigger</dt>
                      <dd>{definition.trigger.event_type}</dd>
                      <dt>Required capabilities</dt>
                      <dd>{definition.capabilities.join(", ")}</dd>
                      <dt>Steps</dt>
                      <dd>{definition.steps.length}</dd>
                    </dl>
                  )}
                  {definition.state === "draft" ? (
                    <div>
                      <p>
                        Publishing verifies every referenced tag and group is
                        active. Inherited tags cannot be removed, and minimum
                        classification is preserved.
                      </p>
                      <button
                        type="button"
                        disabled={state === "saving"}
                        onClick={() => void publish(definition)}
                      >
                        Publish this version
                      </button>
                    </div>
                  ) : null}
                </article>
              ))}
              {!definitions.length && state === "ready" ? (
                <p>No automation definitions exist for this client.</p>
              ) : null}
            </div>
          </section>
        ) : null}
        {canManageDeadLetters ? (
          <section>
            <p className="record-id">Failure recovery</p>
            <h2>Dead letters</h2>
            <div className="dead-letter-list">
              {deadLetters.map((letter) => (
                <article key={letter.id}>
                  <div>
                    <strong>{letter.error_code}</strong>
                    <p>{letter.safe_message}</p>
                    <small>
                      Automation {letter.automation_id} v
                      {letter.automation_version} · {letter.state}
                    </small>
                  </div>
                  {letter.state === "open" ? (
                    <form onSubmit={(event) => void act(event, letter)}>
                      <label>
                        <span>Action</span>
                        <select name="action">
                          <option value="retry">Retry failed step</option>
                          <option value="replay">Replay event</option>
                          <option value="dismiss">Dismiss</option>
                        </select>
                      </label>
                      <label>
                        <span>Reason</span>
                        <input name="reason" required />
                      </label>
                      <button type="submit" disabled={state === "saving"}>
                        Apply action
                      </button>
                    </form>
                  ) : null}
                </article>
              ))}
              {!deadLetters.length && state === "ready" ? (
                <p>No automation dead letters exist for this client.</p>
              ) : null}
            </div>
          </section>
        ) : null}
      </div>
    </Page>
  );
}

function DefinitionForm({
  definition,
  groups,
  tags,
  saving,
  submitLabel,
  onSubmit,
}: {
  definition?: ManagedDefinition;
  groups: TagGroup[];
  tags: Tag[];
  saving: boolean;
  submitLabel: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  return (
    <form
      className="definition-form"
      onSubmit={(event) => void onSubmit(event)}
    >
      {!definition ? (
        <label>
          <span>Name</span>
          <input name="name" required />
        </label>
      ) : null}
      <label>
        <span>Trigger event type</span>
        <select
          name="event_type"
          defaultValue={definition?.trigger.event_type ?? "work_record.created"}
          required
        >
          {Array.from(
            new Set([
              "work_record.created",
              "work_record.updated",
              "tag.added",
              "tag.removed",
              definition?.trigger.event_type ?? "",
            ]),
          )
            .filter(Boolean)
            .map((eventType) => (
              <option key={eventType} value={eventType}>
                {eventType}
              </option>
            ))}
        </select>
      </label>
      <ScopeBuilder
        label="Required capabilities"
        name="capabilities"
        initialValues={definition?.capabilities ?? ["work_record.comment"]}
        options={Array.from(
          new Set([
            "work_record.read",
            "work_record.comment",
            "work_record.update",
            "classification.apply",
            "notification.send",
            ...(definition?.capabilities ?? []),
          ]),
        ).map((value) => ({
          value,
          label: value.replaceAll("_", " ").replaceAll(".", " · "),
        }))}
      />
      <AutomationStepBuilder
        steps={definition?.steps ?? defaultSteps}
        groups={groups}
        tags={tags}
      />
      <button type="submit" disabled={saving}>
        {saving ? "Saving…" : submitLabel}
      </button>
    </form>
  );
}

async function parse<T>(response: Response): Promise<T> {
  if (!response.ok) throw new Error(`automation_${response.status}`);
  return (await response.json()) as T;
}
