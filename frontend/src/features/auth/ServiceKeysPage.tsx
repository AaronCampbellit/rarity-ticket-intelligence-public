import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import { Notice, Page, ScopeBuilder, StatePanel } from "../../design-system";

type ServiceKey = {
  id: string;
  name: string;
  prefix: string;
  capabilities: string[];
  data_scopes: string[];
  created_at: string;
  expires_at: string;
  revoked_at?: string;
};

type IssuedServiceKey = ServiceKey & { token: string };

export function ServiceKeysPage({ clientID }: { clientID: string }) {
  const [keys, setKeys] = useState<ServiceKey[]>([]);
  const [secret, setSecret] = useState<IssuedServiceKey | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!clientID) return;
      const response = await fetch("/api/v1/admin/service-keys", {
        credentials: "same-origin",
        headers: clientContextHeaders(clientID),
        signal,
      });
      if (!response.ok) throw new Error("service keys unavailable");
      setKeys((await response.json()) as ServiceKey[]);
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

  async function issue(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    await mutate("/api/v1/admin/service-keys", {
      name: form.get("name"),
      capabilities: form.getAll("capabilities").map(String),
      data_scopes: form.getAll("data_scopes").map(String),
      ttl_seconds: daysToSeconds(form.get("ttl_days")),
    });
    formElement.reset();
  }

  async function rotate(event: FormEvent<HTMLFormElement>, key: ServiceKey) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      `/api/v1/admin/service-keys/${encodeURIComponent(key.id)}/rotate`,
      {
        ttl_seconds: daysToSeconds(form.get("ttl_days")),
        reason: form.get("reason"),
      },
    );
  }

  async function revoke(event: FormEvent<HTMLFormElement>, key: ServiceKey) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate(
      `/api/v1/admin/service-keys/${encodeURIComponent(key.id)}/revoke`,
      {
        reason: form.get("reason"),
      },
      true,
    );
  }

  async function mutate(
    url: string,
    body: Record<string, unknown>,
    noContent = false,
  ) {
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(url, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...clientContextHeaders(clientID),
          ...csrfHeaders(),
        },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error("service key change failed");
      if (!noContent) setSecret((await response.json()) as IssuedServiceKey);
      else setSecret(null);
      await load();
      setMessage(
        noContent
          ? "Service key revoked."
          : "Copy the new token now. Rarity will not display it again.",
      );
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      className="settings-page"
      eyebrow="Integration security"
      title="Service API keys"
      description="Issue, rotate, and revoke client-scoped integration credentials."
    >
      {!clientID ? (
        <StatePanel
          state="empty"
          title="Select a client"
          description="Select a client to administer service keys."
        />
      ) : null}
      {state === "error" ? (
        <StatePanel
          state="error"
          title="Service keys could not be loaded or changed"
          description="Verify scope and values."
        />
      ) : null}
      {message ? <Notice title="Service key updated">{message}</Notice> : null}
      {secret ? (
        <section className="settings-card" aria-labelledby="new-token-heading">
          <h2 id="new-token-heading">One-time token</h2>
          <p>
            Store this token in the consuming system&apos;s secret manager. Only
            the prefix remains visible after leaving this page.
          </p>
          <label>
            Token
            <input
              readOnly
              value={secret.token}
              onFocus={(event) => event.currentTarget.select()}
            />
          </label>
        </section>
      ) : null}
      {clientID ? (
        <>
          <section
            className="settings-card"
            aria-labelledby="issue-key-heading"
          >
            <h2 id="issue-key-heading">Issue key</h2>
            <form onSubmit={(event) => void issue(event)}>
              <label>
                Name
                <input name="name" required />
              </label>
              <ScopeBuilder
                label="Capabilities"
                name="capabilities"
                options={[
                  { value: "work_record.read", label: "Read work records" },
                  { value: "work_record.create", label: "Create work records" },
                  { value: "work_record.update", label: "Update work records" },
                  { value: "work_record.comment", label: "Add work comments" },
                  { value: "asset.read", label: "Read assets" },
                  { value: "integration.read", label: "Read integrations" },
                ]}
              />
              <ScopeBuilder
                label="Data scopes"
                name="data_scopes"
                options={[
                  { value: "work_records", label: "Work records" },
                  { value: "assets", label: "Assets" },
                  { value: "clients", label: "Client directory" },
                  { value: "knowledge", label: "Knowledge" },
                  { value: "billing", label: "Billing and time" },
                ]}
              />
              <label>
                Lifetime in days
                <input
                  name="ttl_days"
                  type="number"
                  min="1"
                  max="365"
                  defaultValue="90"
                  required
                />
              </label>
              <button type="submit" disabled={state === "saving"}>
                Issue service key
              </button>
            </form>
          </section>
          <section
            className="settings-card"
            aria-labelledby="existing-keys-heading"
          >
            <h2 id="existing-keys-heading">Existing keys</h2>
            {state === "loading" ? (
              <StatePanel state="loading" title="Loading service keys…" />
            ) : null}
            {state !== "loading" && !keys.length ? (
              <StatePanel
                state="empty"
                title="No service keys"
                description="No service keys exist for this client."
              />
            ) : null}
            {keys.map((key) => (
              <article className="settings-card" key={key.id}>
                <h3>{key.name}</h3>
                <p>
                  <code>{key.prefix}</code> ·{" "}
                  {key.revoked_at
                    ? "Revoked"
                    : new Date(key.expires_at) <= new Date()
                      ? "Expired"
                      : "Active"}
                </p>
                <p>Capabilities: {key.capabilities.join(", ")}</p>
                <p>Data scopes: {key.data_scopes.join(", ")}</p>
                {!key.revoked_at ? (
                  <div className="settings-action-grid">
                    <form
                      className="settings-action-form"
                      onSubmit={(event) => void rotate(event, key)}
                    >
                      <h4>Rotate</h4>
                      <label>
                        New lifetime in days
                        <input
                          name="ttl_days"
                          type="number"
                          min="1"
                          max="365"
                          defaultValue="90"
                          required
                        />
                      </label>
                      <label>
                        Reason
                        <input name="reason" required />
                      </label>
                      <button type="submit" disabled={state === "saving"}>
                        Rotate key
                      </button>
                    </form>
                    <form
                      className="settings-action-form"
                      onSubmit={(event) => void revoke(event, key)}
                    >
                      <h4>Revoke</h4>
                      <label>
                        Reason
                        <input name="reason" required />
                      </label>
                      <button type="submit" disabled={state === "saving"}>
                        Revoke key
                      </button>
                    </form>
                  </div>
                ) : null}
              </article>
            ))}
          </section>
        </>
      ) : null}
    </Page>
  );
}

function daysToSeconds(value: FormDataEntryValue | null): number {
  return Number(value) * 24 * 60 * 60;
}
