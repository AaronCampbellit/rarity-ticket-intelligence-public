import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Notice, Page, StatePanel } from "../../design-system";
import "./operations.css";

type Mailbox = {
  id: string;
  mailbox_address: string;
  tenant_id: string;
  client_id: string;
  credential_configured: boolean;
  client_state_configured: boolean;
  enabled: boolean;
  health_state: string;
  last_success_at?: string;
  last_error_code?: string;
  version: number;
};

export function GraphSettingsPage() {
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch("/api/v1/integrations/graph/mailboxes", {
      credentials: "same-origin",
      signal,
    });
    if (!response.ok) throw new Error("Graph mailboxes unavailable");
    setMailboxes((await response.json()) as Mailbox[]);
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
      if (!response.ok) throw new Error("Graph mailbox mutation failed");
      await load();
      setMessage("Graph mailbox configuration updated.");
    } catch {
      setState("error");
    }
  }
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      "/api/v1/integrations/graph/mailboxes",
      "POST",
      credentialBody(form, true),
    );
  }

  return (
    <Page
      eyebrow="Email intake"
      title="Microsoft Graph mailboxes"
      description="Manage application-authenticated mailbox intake. Client secrets and notification client-state values are write-only."
    >
      <div className="operations-health">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading Graph mailboxes"
            description="Retrieving application-authenticated mailbox connections."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Graph mailbox settings are unavailable"
            description="Verify permissions and values."
            supportCode="GRAPH-MAILBOXES"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Graph mailbox updated">
            {message}
          </Notice>
        ) : null}
        {state === "ready" ? (
          <>
            <section
              className="settings-card"
              aria-labelledby="create-graph-heading"
            >
              <h2 id="create-graph-heading">Add mailbox</h2>
              <CredentialForm
                onSubmit={create}
                submitLabel="Add Graph mailbox"
                includeMailbox
              />
            </section>
            <section aria-labelledby="graph-mailboxes-heading">
              <h2 id="graph-mailboxes-heading">Mailboxes</h2>
              {!mailboxes.length ? (
                <p>No Graph mailboxes are configured.</p>
              ) : null}
              <div className="health-grid">
                {mailboxes.map((mailbox) => (
                  <article key={mailbox.id}>
                    <header>
                      <div>
                        <p className="record-id">{mailbox.health_state}</p>
                        <h3>{mailbox.mailbox_address}</h3>
                      </div>
                      <span
                        className={`health-state health-${mailbox.enabled ? "healthy" : "disabled"}`}
                      >
                        {mailbox.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </header>
                    <dl>
                      <div>
                        <dt>Tenant</dt>
                        <dd>
                          {mailbox.tenant_id ||
                            "Deployment-managed legacy credential"}
                        </dd>
                      </div>
                      <div>
                        <dt>Application</dt>
                        <dd>{mailbox.client_id || "Not exposed"}</dd>
                      </div>
                      <div>
                        <dt>Credential</dt>
                        <dd>
                          {mailbox.credential_configured
                            ? "Configured"
                            : "Missing"}
                        </dd>
                      </div>
                      <div>
                        <dt>Last success</dt>
                        <dd>
                          {mailbox.last_success_at
                            ? new Date(mailbox.last_success_at).toLocaleString()
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
                            `/api/v1/integrations/graph/mailboxes/${encodeURIComponent(mailbox.id)}`,
                            "PATCH",
                            {
                              expected_version: mailbox.version,
                              enabled: !mailbox.enabled,
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
                          {mailbox.enabled ? "Disable" : "Enable"} mailbox
                        </button>
                      </form>
                      <CredentialForm
                        className="settings-action-form"
                        heading="Replace credential"
                        includeMailbox={false}
                        submitLabel="Replace credential"
                        onSubmit={(event) => {
                          event.preventDefault();
                          const form = new FormData(event.currentTarget);
                          void mutate(
                            `/api/v1/integrations/graph/mailboxes/${encodeURIComponent(mailbox.id)}/credential`,
                            "POST",
                            {
                              expected_version: mailbox.version,
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
  includeMailbox,
  className,
  heading,
}: {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  submitLabel: string;
  includeMailbox: boolean;
  className?: string;
  heading?: string;
}) {
  return (
    <form className={className} onSubmit={onSubmit}>
      {heading ? <h4>{heading}</h4> : null}
      {includeMailbox ? (
        <label>
          Mailbox address
          <input name="mailbox_address" type="email" required />
        </label>
      ) : null}
      <label>
        Tenant ID
        <input name="tenant_id" required />
      </label>
      <label>
        Application client ID
        <input name="client_id" required />
      </label>
      <label>
        Application client secret
        <input
          name="client_secret"
          type="password"
          autoComplete="new-password"
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
function credentialBody(
  form: FormData,
  mailbox: boolean,
): Record<string, unknown> {
  return {
    ...(mailbox ? { mailbox_address: form.get("mailbox_address") } : {}),
    tenant_id: form.get("tenant_id"),
    client_id: form.get("client_id"),
    client_secret: form.get("client_secret"),
    reason: form.get("reason"),
  };
}
