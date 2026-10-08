import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Notice, Page, StatePanel, TagInput } from "../../design-system";
import "./operations.css";

type Connection = {
  id: string;
  intake_address: string;
  allowed_sender_domains: string[];
  max_message_bytes: number;
  rate_limit_per_minute: number;
  enabled: boolean;
  health_state: string;
  last_received_at?: string;
  last_error_code?: string;
  version: number;
};

export function ForwardingSettingsPage() {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch(
      "/api/v1/integrations/forwarding/connections",
      {
        credentials: "same-origin",
        signal,
      },
    );
    if (!response.ok) throw new Error("forwarding connections unavailable");
    setConnections((await response.json()) as Connection[]);
    setState("ready");
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  async function mutate(
    url: string,
    method: "POST" | "PATCH",
    body: Record<string, unknown>,
  ) {
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(url, {
        method,
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error("forwarding mutation failed");
      await load();
      setMessage("Forwarding connection updated.");
    } catch {
      setState("error");
    }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await mutate(
      "/api/v1/integrations/forwarding/connections",
      "POST",
      forwardingBody(new FormData(event.currentTarget)),
    );
  }

  return (
    <Page
      eyebrow="Email intake"
      title="Forwarding connections"
      description="Configure protected intake addresses, aligned sender domains, message limits, and durable rate controls."
    >
      <div className="operations-health">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading forwarding connections"
            description="Retrieving protected intake addresses."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Forwarding settings are unavailable"
            description="Verify permissions and values."
            supportCode="FORWARDING-CONNECTIONS"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Forwarding connection updated">
            {message}
          </Notice>
        ) : null}
        {state === "ready" ? (
          <>
            <section
              className="settings-card"
              aria-labelledby="create-forwarding-heading"
            >
              <h2 id="create-forwarding-heading">Create connection</h2>
              <ForwardingForm
                onSubmit={create}
                submitLabel="Create forwarding connection"
              />
            </section>
            <section aria-labelledby="forwarding-connections-heading">
              <h2 id="forwarding-connections-heading">Connections</h2>
              {!connections.length ? (
                <p>No forwarding connections are configured.</p>
              ) : null}
              <div className="health-grid">
                {connections.map((connection) => (
                  <article key={connection.id}>
                    <header>
                      <div>
                        <p className="record-id">{connection.health_state}</p>
                        <h3>{connection.intake_address}</h3>
                      </div>
                      <span
                        className={`health-state health-${connection.enabled ? "healthy" : "disabled"}`}
                      >
                        {connection.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </header>
                    <dl>
                      <div>
                        <dt>Allowed domains</dt>
                        <dd>{connection.allowed_sender_domains.join(", ")}</dd>
                      </div>
                      <div>
                        <dt>Message ceiling</dt>
                        <dd>
                          {Math.round(
                            connection.max_message_bytes / (1024 * 1024),
                          )}{" "}
                          MiB
                        </dd>
                      </div>
                      <div>
                        <dt>Rate ceiling</dt>
                        <dd>{connection.rate_limit_per_minute}/minute</dd>
                      </div>
                      <div>
                        <dt>Last received</dt>
                        <dd>
                          {connection.last_received_at
                            ? new Date(
                                connection.last_received_at,
                              ).toLocaleString()
                            : "Never"}
                        </dd>
                      </div>
                    </dl>
                    <form
                      onSubmit={(event) => {
                        event.preventDefault();
                        const form = new FormData(event.currentTarget);
                        void mutate(
                          `/api/v1/integrations/forwarding/connections/${encodeURIComponent(connection.id)}`,
                          "PATCH",
                          {
                            expected_version: connection.version,
                            enabled: !connection.enabled,
                            reason: form.get("reason"),
                          },
                        );
                      }}
                    >
                      <label>
                        Reason
                        <input name="reason" required />
                      </label>
                      <button type="submit">
                        {connection.enabled ? "Disable" : "Enable"} connection
                      </button>
                    </form>
                  </article>
                ))}
              </div>
            </section>
          </>
        ) : null}
      </div>
    </Page>
  );
}

function ForwardingForm({
  onSubmit,
  submitLabel,
}: {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  submitLabel: string;
}) {
  return (
    <form onSubmit={onSubmit}>
      <label>
        Intake address
        <input name="intake_address" type="email" required />
      </label>
      <TagInput
        label="Allowed sender domains"
        name="allowed_sender_domains"
        placeholder="customer.example"
      />
      <label>
        Maximum message size in MiB
        <input
          name="max_message_mib"
          type="number"
          min="1"
          max="50"
          defaultValue="25"
          required
        />
      </label>
      <label>
        Messages per minute
        <input
          name="rate_limit_per_minute"
          type="number"
          min="1"
          max="10000"
          defaultValue="60"
          required
        />
      </label>
      <label>
        Reason
        <input name="reason" required />
      </label>
      <button type="submit">{submitLabel}</button>
    </form>
  );
}

function forwardingBody(form: FormData): Record<string, unknown> {
  return {
    intake_address: form.get("intake_address"),
    allowed_sender_domains: form
      .getAll("allowed_sender_domains")
      .map(String)
      .map((value) => value.trim())
      .filter(Boolean),
    max_message_bytes: Number(form.get("max_message_mib")) * 1024 * 1024,
    rate_limit_per_minute: Number(form.get("rate_limit_per_minute")),
    reason: form.get("reason"),
  };
}
