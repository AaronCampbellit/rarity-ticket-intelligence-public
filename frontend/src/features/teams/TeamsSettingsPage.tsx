import { useEffect, useMemo, useRef, useState } from "react";

import { Button, Notice, Page, StatePanel } from "../../design-system";
import { createTeamsSettingsAPI, TeamsAPIError } from "./api";
import type { TeamsConnection, TeamsSettingsAPI } from "./types";
import "./teams.css";

type Draft = { name: string; webhookURL: string; reason: string };
const emptyDraft: Draft = { name: "", webhookURL: "", reason: "" };

export function TeamsSettingsPage({
  api,
  clientID = "",
}: {
  api?: TeamsSettingsAPI;
  clientID?: string;
}) {
  const activeAPI = useMemo(
    () => api ?? createTeamsSettingsAPI(fetch, clientID),
    [api, clientID],
  );
  const [connections, setConnections] = useState<TeamsConnection[]>([]);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [createDraft, setCreateDraft] = useState<Draft>(emptyDraft);
  const [showCreate, setShowCreate] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const errorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    setBusy("load");
    activeAPI
      .list(controller.signal)
      .then((items) => {
        setConnections(items);
        setDrafts(
          Object.fromEntries(
            items.map((item) => [
              item.id,
              { name: item.name, webhookURL: "", reason: "" },
            ]),
          ),
        );
      })
      .catch((cause) => {
        if (!controller.signal.aborted) showError(cause);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy("");
      });
    return () => controller.abort();
  }, [activeAPI]);

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  function showError(cause: unknown) {
    const code = cause instanceof TeamsAPIError ? cause.code : "request_failed";
    setError(`Teams connection request failed (${code}).`);
    setStatus("");
  }

  function replace(connection: TeamsConnection, message: string) {
    setConnections((current) =>
      current.map((item) => (item.id === connection.id ? connection : item)),
    );
    setDrafts((current) => ({
      ...current,
      [connection.id]: {
        name: connection.name,
        webhookURL: "",
        reason: "",
      },
    }));
    setError("");
    setStatus(message);
  }

  async function createConnection() {
    if (
      !createDraft.name.trim() ||
      !createDraft.webhookURL.trim() ||
      !createDraft.reason.trim()
    )
      return;
    setBusy("create");
    try {
      const created = await activeAPI.create({
        name: createDraft.name.trim(),
        webhookURL: createDraft.webhookURL.trim(),
        reason: createDraft.reason.trim(),
      });
      setConnections((current) => [...current, created]);
      setDrafts((current) => ({
        ...current,
        [created.id]: { name: created.name, webhookURL: "", reason: "" },
      }));
      setCreateDraft(emptyDraft);
      setShowCreate(false);
      setError("");
      setStatus("Teams connection created. Test it before enabling delivery.");
    } catch (cause) {
      showError(cause);
    } finally {
      setBusy("");
    }
  }

  async function run(
    connection: TeamsConnection,
    operation: "name" | "credential" | "enabled" | "test",
  ) {
    const draft = drafts[connection.id] ?? {
      name: connection.name,
      webhookURL: "",
      reason: "",
    };
    if (!draft.reason.trim()) return;
    setBusy(`${operation}:${connection.id}`);
    try {
      let updated: TeamsConnection;
      if (operation === "name") {
        if (!draft.name.trim()) return;
        updated = await activeAPI.updateName(connection.id, {
          expectedVersion: connection.version,
          name: draft.name.trim(),
          reason: draft.reason.trim(),
        });
      } else if (operation === "credential") {
        if (!draft.webhookURL.trim()) return;
        updated = await activeAPI.replaceCredential(connection.id, {
          expectedVersion: connection.version,
          webhookURL: draft.webhookURL.trim(),
          reason: draft.reason.trim(),
        });
      } else if (operation === "enabled") {
        updated = await activeAPI.setEnabled(connection.id, {
          expectedVersion: connection.version,
          enabled: !connection.enabled,
          reason: draft.reason.trim(),
        });
      } else {
        updated = await activeAPI.test(connection.id, {
          reason: draft.reason.trim(),
        });
      }
      replace(
        updated,
        operation === "test"
          ? updated.health === "healthy"
            ? "Teams connection test succeeded."
            : "Teams connection test failed. Review the safe health code before retrying."
          : "Teams connection updated.",
      );
    } catch (cause) {
      showError(cause);
    } finally {
      setBusy("");
    }
  }

  return (
    <Page
      className="teams-settings-page"
      eyebrow="Integration administration"
      title="Microsoft Teams connections"
      description="Configure named Incoming Webhook destinations without exposing webhook URLs after save."
      actions={
        <Button
          type="button"
          intent={showCreate ? "tertiary" : "primary"}
          onClick={() => setShowCreate((value) => !value)}
        >
          {showCreate ? "Cancel" : "Add connection"}
        </Button>
      }
    >
      <div tabIndex={-1} ref={errorRef} hidden={!error}>
        <Notice title="Request failed" tone="danger" urgent>
          {error}
        </Notice>
      </div>
      {status ? (
        <Notice title="Teams connection updated">{status}</Notice>
      ) : null}

      {showCreate ? (
        <section className="teams-panel" aria-labelledby="teams-create-title">
          <h2 id="teams-create-title">Add Teams connection</h2>
          <p>
            The webhook URL is write-only and purpose-encrypted. Remote delivery
            requires public HTTPS and refuses redirects.
          </p>
          <div className="teams-form-grid">
            <label>
              Connection name
              <input
                value={createDraft.name}
                onChange={(event) =>
                  setCreateDraft((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              Incoming Webhook URL
              <input
                type="password"
                autoComplete="new-password"
                value={createDraft.webhookURL}
                onChange={(event) =>
                  setCreateDraft((current) => ({
                    ...current,
                    webhookURL: event.target.value,
                  }))
                }
              />
            </label>
            <label>
              Audit reason
              <input
                value={createDraft.reason}
                onChange={(event) =>
                  setCreateDraft((current) => ({
                    ...current,
                    reason: event.target.value,
                  }))
                }
              />
            </label>
          </div>
          <button
            type="button"
            disabled={
              busy !== "" ||
              !createDraft.name.trim() ||
              !createDraft.webhookURL.trim() ||
              !createDraft.reason.trim()
            }
            onClick={() => void createConnection()}
          >
            Save connection
          </button>
        </section>
      ) : null}

      <section className="teams-connections" aria-labelledby="teams-list-title">
        <div className="teams-section-heading">
          <h2 id="teams-list-title">Configured destinations</h2>
          <span>{connections.length} connections</span>
        </div>
        {busy === "load" ? (
          <StatePanel state="loading" title="Loading Teams connections…" />
        ) : null}
        {!busy && connections.length === 0 ? (
          <StatePanel
            state="empty"
            title="No Teams connections"
            description="No Teams connections are configured for this scope."
          />
        ) : null}
        {connections.map((connection) => {
          const draft = drafts[connection.id] ?? {
            name: connection.name,
            webhookURL: "",
            reason: "",
          };
          const connectionBusy = busy.endsWith(`:${connection.id}`);
          return (
            <article className="teams-card" key={connection.id}>
              <div className="teams-card-heading">
                <div>
                  <h3>{connection.name}</h3>
                  <p>
                    Credential{" "}
                    {connection.credentialConfigured ? "configured" : "missing"}{" "}
                    · Version {connection.version}
                  </p>
                </div>
                <span className={`teams-health ${connection.health}`}>
                  {connection.health}
                </span>
              </div>
              {connection.lastErrorCode ? (
                <p>Safe error: {connection.lastErrorCode}</p>
              ) : null}
              <div className="teams-form-grid">
                <label>
                  Connection name
                  <input
                    value={draft.name}
                    onChange={(event) =>
                      setDrafts((current) => ({
                        ...current,
                        [connection.id]: {
                          ...draft,
                          name: event.target.value,
                        },
                      }))
                    }
                  />
                </label>
                <label>
                  Replacement webhook URL
                  <input
                    type="password"
                    autoComplete="new-password"
                    value={draft.webhookURL}
                    onChange={(event) =>
                      setDrafts((current) => ({
                        ...current,
                        [connection.id]: {
                          ...draft,
                          webhookURL: event.target.value,
                        },
                      }))
                    }
                  />
                </label>
                <label>
                  Audit reason for next action
                  <input
                    value={draft.reason}
                    onChange={(event) =>
                      setDrafts((current) => ({
                        ...current,
                        [connection.id]: {
                          ...draft,
                          reason: event.target.value,
                        },
                      }))
                    }
                  />
                </label>
              </div>
              <div className="teams-actions">
                <button
                  type="button"
                  disabled={
                    connectionBusy ||
                    !draft.reason.trim() ||
                    !draft.name.trim() ||
                    draft.name.trim() === connection.name
                  }
                  onClick={() => void run(connection, "name")}
                >
                  Save name
                </button>
                <button
                  type="button"
                  disabled={
                    connectionBusy ||
                    !draft.reason.trim() ||
                    !draft.webhookURL.trim()
                  }
                  onClick={() => void run(connection, "credential")}
                >
                  Replace webhook
                </button>
                <button
                  type="button"
                  disabled={connectionBusy || !draft.reason.trim()}
                  onClick={() => void run(connection, "test")}
                >
                  Test connection
                </button>
                <button
                  type="button"
                  disabled={connectionBusy || !draft.reason.trim()}
                  onClick={() => void run(connection, "enabled")}
                >
                  {connection.enabled ? "Disable delivery" : "Enable delivery"}
                </button>
              </div>
            </article>
          );
        })}
      </section>
    </Page>
  );
}
