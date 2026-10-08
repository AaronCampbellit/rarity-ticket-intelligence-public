import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import { Page, StatePanel, TagInput } from "../../design-system";
import "./operations.css";

type Connection = {
  id: string;
  name: string;
  direction: string;
  endpoint_url?: string;
  credential_configured: boolean;
  enabled: boolean;
  event_types: string[];
  retry_window_seconds: number;
  version: number;
};

type Delivery = {
  connection_id: string;
  connection_name: string;
  event_id: string;
  event_type: string;
  state: string;
  attempt_count: number;
  next_attempt_at?: string;
  delivered_at?: string;
  failed_at?: string;
  error_code?: string;
  last_http_status?: number;
};

export function WebhooksPage({
  clientID,
  capabilities,
}: {
  clientID: string;
  capabilities: ReadonlySet<string>;
}) {
  const canManage = capabilities.has("integration.manage");
  const canRead = capabilities.has("integration.read");
  const [connections, setConnections] = useState<Connection[]>([]);
  const [deliveries, setDeliveries] = useState<Delivery[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!clientID) return;
      const options = {
        credentials: "same-origin" as const,
        headers: clientContextHeaders(clientID),
        signal,
      };
      const [foundConnections, foundDeliveries] = await Promise.all([
        canManage
          ? fetch("/api/v1/integrations/webhooks/connections", options).then(
              parse<Connection[]>,
            )
          : Promise.resolve([]),
        canRead
          ? fetch("/api/v1/integrations/webhooks/deliveries", options).then(
              parse<Delivery[]>,
            )
          : Promise.resolve([]),
      ]);
      setConnections(foundConnections);
      setDeliveries(foundDeliveries);
      setState("ready");
    },
    [canManage, canRead, clientID],
  );

  useEffect(() => {
    if (!clientID) return;
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [clientID, load]);

  async function mutate(
    url: string,
    method: "POST" | "PATCH",
    body: Record<string, unknown>,
  ) {
    setState("loading");
    try {
      const response = await fetch(url, {
        method,
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...clientContextHeaders(clientID),
          ...csrfHeaders(),
        },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error("webhook mutation failed");
      await load();
    } catch {
      setState("error");
    }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await mutate("/api/v1/integrations/webhooks/connections", "POST", {
      name: form.get("name"),
      direction: form.get("direction"),
      endpoint_url: form.get("endpoint_url"),
      event_types: form.getAll("event_types").map(String),
      retry_window_seconds: Number(form.get("retry_hours")) * 3600,
      signing_secret: form.get("signing_secret"),
      reason: form.get("reason"),
    });
  }

  return (
    <Page
      eyebrow="Integrations"
      title="Webhooks"
      description="Scoped connection configuration and durable outbound delivery evidence. Signing credentials are never returned."
    >
      <div className="operations-health">
        {!clientID ? (
          <StatePanel
            state="empty"
            title="Select a Client"
            description="Select a Client to view webhook evidence."
          />
        ) : null}
        {state === "loading" && clientID ? (
          <StatePanel
            state="loading"
            title="Loading webhooks"
            description="Retrieving connections and delivery evidence."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Webhook evidence is unavailable"
            description="Verify permissions and platform readiness."
            supportCode="WEBHOOK-EVIDENCE"
          />
        ) : null}
        {canManage && state === "ready" ? (
          <section aria-labelledby="webhook-connections-heading">
            <h2 id="webhook-connections-heading">Connections</h2>
            <form
              className="settings-card"
              onSubmit={(event) => void create(event)}
            >
              <h3>Create connection</h3>
              <label>
                Name
                <input name="name" required />
              </label>
              <label>
                Direction
                <select name="direction" defaultValue="outbound">
                  <option value="outbound">Outbound</option>
                  <option value="inbound">Inbound</option>
                  <option value="bidirectional">Bidirectional</option>
                </select>
              </label>
              <label>
                Endpoint URL
                <input
                  name="endpoint_url"
                  type="url"
                  placeholder="https://hooks.example.com/rarity"
                />
              </label>
              <TagInput
                label="Event types"
                name="event_types"
                placeholder="work_record.created"
              />
              <label>
                Retry window in hours
                <input
                  name="retry_hours"
                  type="number"
                  min="1"
                  max="24"
                  defaultValue="24"
                  required
                />
              </label>
              <label>
                Signing secret
                <input
                  name="signing_secret"
                  type="password"
                  minLength={32}
                  autoComplete="new-password"
                  required
                />
              </label>
              <label>
                Reason
                <input name="reason" required />
              </label>
              <button type="submit">Create webhook connection</button>
            </form>
            {!connections.length ? (
              <p>No webhook connections exist for this client.</p>
            ) : null}
            <div className="health-grid">
              {connections.map((connection) => (
                <article key={connection.id}>
                  <header>
                    <div>
                      <p className="record-id">{connection.direction}</p>
                      <h3>{connection.name}</h3>
                    </div>
                    <span
                      className={`health-state health-${connection.enabled ? "healthy" : "disabled"}`}
                    >
                      {connection.enabled ? "Enabled" : "Disabled"}
                    </span>
                  </header>
                  <dl>
                    <div>
                      <dt>Endpoint</dt>
                      <dd>{connection.endpoint_url || "Inbound only"}</dd>
                    </div>
                    <div>
                      <dt>Signing credential</dt>
                      <dd>
                        {connection.credential_configured
                          ? "Configured"
                          : "Missing"}
                      </dd>
                    </div>
                    <div>
                      <dt>Events</dt>
                      <dd>
                        {connection.event_types.join(", ") || "Inbound only"}
                      </dd>
                    </div>
                    <div>
                      <dt>Retry window</dt>
                      <dd>
                        {Math.round(connection.retry_window_seconds / 3600)}h
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
                          `/api/v1/integrations/webhooks/connections/${encodeURIComponent(connection.id)}`,
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
                          `/api/v1/integrations/webhooks/connections/${encodeURIComponent(connection.id)}/credential`,
                          "POST",
                          {
                            expected_version: connection.version,
                            signing_secret: form.get("signing_secret"),
                            reason: form.get("reason"),
                          },
                        );
                      }}
                    >
                      <h4>Signing credential</h4>
                      <label>
                        Replacement signing secret
                        <input
                          name="signing_secret"
                          type="password"
                          minLength={32}
                          autoComplete="new-password"
                          required
                        />
                      </label>
                      <label>
                        Reason
                        <input name="reason" required />
                      </label>
                      <button type="submit">Replace credential</button>
                    </form>
                  </div>
                </article>
              ))}
            </div>
          </section>
        ) : null}
        {canRead && state === "ready" ? (
          <section aria-labelledby="webhook-deliveries-heading">
            <h2 id="webhook-deliveries-heading">Recent deliveries</h2>
            {!deliveries.length ? (
              <p>No outbound webhook deliveries exist for this client.</p>
            ) : null}
            {deliveries.length ? (
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Connection</th>
                      <th>Event</th>
                      <th>State</th>
                      <th>Attempts</th>
                      <th>Last result</th>
                      {canManage ? <th>Recovery</th> : null}
                    </tr>
                  </thead>
                  <tbody>
                    {deliveries.map((delivery) => (
                      <tr
                        key={`${delivery.connection_id}-${delivery.event_id}`}
                      >
                        <td>{delivery.connection_name}</td>
                        <td>
                          <code>{delivery.event_type}</code>
                          <br />
                          <small>{delivery.event_id}</small>
                        </td>
                        <td>{delivery.state}</td>
                        <td>{delivery.attempt_count}</td>
                        <td>
                          {delivery.error_code ||
                            (delivery.last_http_status
                              ? `HTTP ${delivery.last_http_status}`
                              : "Pending")}
                        </td>
                        {canManage ? (
                          <td>
                            {delivery.state === "failed" ? (
                              <form
                                onSubmit={(event) => {
                                  event.preventDefault();
                                  const form = new FormData(
                                    event.currentTarget,
                                  );
                                  void mutate(
                                    `/api/v1/integrations/webhooks/connections/${encodeURIComponent(delivery.connection_id)}/deliveries/${encodeURIComponent(delivery.event_id)}/retry`,
                                    "POST",
                                    {
                                      reason: form.get("reason"),
                                    },
                                  );
                                }}
                              >
                                <label>
                                  <span className="sr-only">
                                    Retry reason for {delivery.event_id}
                                  </span>
                                  <input
                                    aria-label={`Retry reason for ${delivery.event_id}`}
                                    name="reason"
                                    required
                                  />
                                </label>
                                <button type="submit">Retry delivery</button>
                              </form>
                            ) : (
                              "—"
                            )}
                          </td>
                        ) : null}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </section>
        ) : null}
      </div>
    </Page>
  );
}

async function parse<T>(response: Response): Promise<T> {
  if (!response.ok) throw new Error(`webhook_${response.status}`);
  return (await response.json()) as T;
}
