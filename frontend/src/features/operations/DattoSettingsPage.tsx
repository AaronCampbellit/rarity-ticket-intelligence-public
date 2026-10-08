import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Notice, Page, StatePanel } from "../../design-system";
import "./operations.css";

type DattoConnection = {
  id: string;
  name: string;
  api_url: string;
  credential_configured: boolean;
  sync_interval_seconds: number;
  enabled: boolean;
  health_state: string;
  last_completed_at?: string;
  last_error_code?: string;
  version: number;
};

export function DattoSettingsPage() {
  const [connections, setConnections] = useState<DattoConnection[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch("/api/v1/integrations/datto/connections", {
      credentials: "same-origin",
      signal,
    });
    if (!response.ok) throw new Error("Datto connections unavailable");
    setConnections((await response.json()) as DattoConnection[]);
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
      if (!response.ok) throw new Error("Datto connection mutation failed");
      await load();
      setMessage("Datto connection configuration updated.");
    } catch {
      setState("error");
    }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      "/api/v1/integrations/datto/connections",
      "POST",
      credentialBody(form, true),
    );
  }

  return (
    <Page
      eyebrow="RMM integration"
      title="Datto RMM connections"
      description="Configure scheduled inventory synchronization. API secrets are encrypted at rest and remain write-only."
    >
      <div className="operations-health">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading Datto connections"
            description="Retrieving configured RMM connections."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Datto connection settings are unavailable"
            description="Verify permissions and values."
            supportCode="DATTO-CONNECTIONS"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Datto connection updated">
            {message}
          </Notice>
        ) : null}
        {state === "ready" ? (
          <>
            <section
              className="settings-card"
              aria-labelledby="create-datto-heading"
            >
              <h2 id="create-datto-heading">Add connection</h2>
              <CredentialForm
                onSubmit={create}
                submitLabel="Add Datto connection"
                includeName
              />
            </section>
            <section aria-labelledby="datto-connections-heading">
              <h2 id="datto-connections-heading">Connections</h2>
              {!connections.length ? (
                <p>No Datto connections are configured.</p>
              ) : null}
              <div className="health-grid">
                {connections.map((connection) => (
                  <article key={connection.id}>
                    <header>
                      <div>
                        <p className="record-id">{connection.health_state}</p>
                        <h3>{connection.name}</h3>
                      </div>
                      <span
                        className={`health-state health-${
                          connection.enabled ? "healthy" : "disabled"
                        }`}
                      >
                        {connection.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </header>
                    <dl>
                      <div>
                        <dt>API endpoint</dt>
                        <dd>
                          {connection.api_url ||
                            "Deployment-managed legacy credential"}
                        </dd>
                      </div>
                      <div>
                        <dt>Credential</dt>
                        <dd>
                          {connection.credential_configured
                            ? "Configured"
                            : "Missing"}
                        </dd>
                      </div>
                      <div>
                        <dt>Sync interval</dt>
                        <dd>{connection.sync_interval_seconds / 60} minutes</dd>
                      </div>
                      <div>
                        <dt>Last completed</dt>
                        <dd>
                          {connection.last_completed_at
                            ? new Date(
                                connection.last_completed_at,
                              ).toLocaleString()
                            : "Never"}
                        </dd>
                      </div>
                    </dl>
                    <div className="settings-action-grid">
                      <form
                        className="settings-action-form"
                        onSubmit={(event) => {
                          event.preventDefault();
                          const form = new FormData(event.currentTarget);
                          void mutate(
                            `/api/v1/integrations/datto/connections/${encodeURIComponent(
                              connection.id,
                            )}`,
                            "PATCH",
                            {
                              expected_version: connection.version,
                              enabled: !connection.enabled,
                              reason: form.get("reason"),
                            },
                          );
                        }}
                      >
                        <h4>Availability</h4>
                        <label>
                          Reason
                          <input name="reason" required />
                        </label>
                        <button type="submit">
                          {connection.enabled ? "Disable" : "Enable"} connection
                        </button>
                      </form>
                      <form
                        className="settings-action-form"
                        onSubmit={(event) => {
                          event.preventDefault();
                          const form = new FormData(event.currentTarget);
                          void mutate(
                            `/api/v1/integrations/datto/connections/${encodeURIComponent(
                              connection.id,
                            )}`,
                            "PATCH",
                            {
                              expected_version: connection.version,
                              name: form.get("name"),
                              sync_interval_seconds:
                                Number(form.get("sync_interval_minutes")) * 60,
                              reason: form.get("reason"),
                            },
                          );
                        }}
                      >
                        <h4>Scheduling</h4>
                        <label>
                          Display name
                          <input
                            name="name"
                            defaultValue={connection.name}
                            required
                          />
                        </label>
                        <label>
                          Sync interval (minutes)
                          <input
                            name="sync_interval_minutes"
                            type="number"
                            min="1"
                            max="1440"
                            defaultValue={connection.sync_interval_seconds / 60}
                            required
                          />
                        </label>
                        <label>
                          Reason
                          <input name="reason" required />
                        </label>
                        <button type="submit">Update scheduling</button>
                      </form>
                      <CredentialForm
                        className="settings-action-form"
                        heading="Replace credential"
                        includeName={false}
                        submitLabel="Replace credential"
                        onSubmit={(event) => {
                          event.preventDefault();
                          const form = new FormData(event.currentTarget);
                          void mutate(
                            `/api/v1/integrations/datto/connections/${encodeURIComponent(
                              connection.id,
                            )}/credential`,
                            "POST",
                            {
                              expected_version: connection.version,
                              ...credentialBody(form, false),
                            },
                          );
                        }}
                      />
                    </div>
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

function CredentialForm({
  onSubmit,
  submitLabel,
  includeName,
  className,
  heading,
}: {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  submitLabel: string;
  includeName: boolean;
  className?: string;
  heading?: string;
}) {
  return (
    <form className={className} onSubmit={onSubmit}>
      {heading ? <h4>{heading}</h4> : null}
      {includeName ? (
        <label>
          Display name
          <input name="name" required />
        </label>
      ) : null}
      <label>
        Datto API URL
        <input
          name="api_url"
          type="url"
          placeholder="https://example.centrastage.net"
          required
        />
      </label>
      <label>
        API key
        <input name="api_key" autoComplete="off" required />
      </label>
      <label>
        API secret
        <input
          name="api_secret"
          type="password"
          autoComplete="new-password"
          required
        />
      </label>
      {includeName ? (
        <label>
          Sync interval (minutes)
          <input
            name="sync_interval_minutes"
            type="number"
            min="1"
            max="1440"
            defaultValue="15"
            required
          />
        </label>
      ) : null}
      <label>
        Reason
        <input name="reason" required />
      </label>
      <button type="submit">{submitLabel}</button>
    </form>
  );
}

function credentialBody(
  form: FormData,
  includeName: boolean,
): Record<string, unknown> {
  return {
    ...(includeName
      ? {
          name: form.get("name"),
          sync_interval_seconds: Number(form.get("sync_interval_minutes")) * 60,
        }
      : {}),
    api_url: form.get("api_url"),
    api_key: form.get("api_key"),
    api_secret: form.get("api_secret"),
    reason: form.get("reason"),
  };
}
