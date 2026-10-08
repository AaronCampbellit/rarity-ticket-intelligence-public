import { useEffect, useRef, useState, type FormEvent } from "react";

import type { DirectoryClient } from "../../api/browserSession";
import {
  AIProposal,
  proposalLocationIdentity,
  proposalTargetIdentity,
} from "./AIProposal";
import {
  ClientResourceFields,
  ClientResourceUpdateFields,
} from "./ClientResourceActionFields";
import { StructuredActionForm } from "./StructuredActionForm";
import type {
  AIWorkspaceAPI,
  AIWorkspaceConversation,
  AIWorkspaceMessage,
  AIWorkspaceProposal,
  ClientResourceKind,
  ClientResourceSummary,
} from "./types";
import { aiWorkspaceAPI } from "./workspaceApi";
import { useStructuredActionController } from "./useStructuredActionController";
import { resourceKinds } from "./workspaceCatalog";
import "./workspace.css";

type ClientResourceCatalog = {
  clientID: string;
  kind: ClientResourceKind;
  resources: ClientResourceSummary[];
};

export function AIWorkspace({
  open,
  clients,
  pageClientID,
  capabilities,
  onClientCreated,
  onClientResourcesChanged,
  onClose,
  api = aiWorkspaceAPI,
}: {
  open: boolean;
  clients: DirectoryClient[];
  pageClientID: string;
  capabilities?: ReadonlySet<string>;
  onClientCreated?: () => Promise<void>;
  onClientResourcesChanged?: () => Promise<void>;
  onClose: () => void;
  api?: AIWorkspaceAPI;
}) {
  const [conversations, setConversations] = useState<AIWorkspaceConversation[]>(
    [],
  );
  const [active, setActive] = useState<AIWorkspaceConversation>();
  const [messages, setMessages] = useState<AIWorkspaceMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [actionOpen, setActionOpen] = useState(false);
  const [locationCatalog, setLocationCatalog] =
    useState<ClientResourceCatalog>();
  const [locationError, setLocationError] = useState("");
  const [proposal, setProposal] = useState<AIWorkspaceProposal>();
  const [outcome, setOutcome] = useState("");
  const closeButton = useRef<HTMLButtonElement>(null);
  const locationRequest = useRef(0);
  const listConversations = api.list;
  const getConversation = api.get;
  const structured = useStructuredActionController({
    clients,
    pageClientID,
    capabilities,
    api,
    ensureConversation,
    setBusy,
    setError,
    setOutcome,
    setProposal,
    clearLocationCatalog,
  });
  const {
    action,
    canReadResources,
    clearResourceResults,
    resourceClearFields,
    resourceForm,
    resourceKind,
    resourceResultContext,
    resourceResults,
    resourceResultSummary,
    selectedTargetClient,
    setResourceClearFields,
    setResourceForm,
    structuredReadContext,
    structuredReadResult,
    targetClientID,
  } = structured;
  const proposalTargetClient = clients.find(
    (client) => client.id === proposal?.target_client_id,
  );
  const proposalLocationReference = proposal?.preview.changes?.location?.after;
  const locations =
    locationCatalog?.clientID === targetClientID &&
    locationCatalog.kind === "location"
      ? locationCatalog.resources
      : [];
  const proposalHasCanonicalTarget =
    Boolean(
      proposal && proposalTargetIdentity(proposal, proposalTargetClient),
    ) &&
    (!proposalLocationReference ||
      Boolean(proposal && proposalLocationIdentity(proposal)));

  function clearLocationCatalog(clearSelection = false) {
    locationRequest.current += 1;
    setLocationCatalog(undefined);
    setLocationError("");
    if (clearSelection) {
      setResourceForm((current) =>
        current.location ? { ...current, location: "" } : current,
      );
    }
  }

  useEffect(() => {
    setConversations([]);
    setActive(undefined);
    setMessages([]);
    setProposal(undefined);
    setOutcome("");
    clearLocationCatalog();
    clearResourceResults();
    if (!open) return;
    const controller = new AbortController();
    setBusy(true);
    setError("");
    void listConversations(controller.signal)
      .then(async (items) => {
        if (controller.signal.aborted) return;
        setConversations(items);
        if (!items[0]) return;
        const loaded = await getConversation(items[0].id, controller.signal);
        if (controller.signal.aborted) return;
        setActive(loaded.conversation);
        setMessages(loaded.messages);
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setError("The AI workspace could not be loaded.");
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [getConversation, listConversations, open]);

  useEffect(() => {
    if (!open) return;
    closeButton.current?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [onClose, open]);

  useEffect(() => {
    if (!open) {
      clearLocationCatalog();
      return;
    }
    const needsLocation =
      canReadResources &&
      action === "client_resource_add" &&
      (resourceKind === "contact" || resourceKind === "asset");
    const client = needsLocation ? selectedTargetClient : undefined;
    clearLocationCatalog();
    const requestID = locationRequest.current;
    if (needsLocation) {
      setResourceForm((current) =>
        current.location ? { ...current, location: "" } : current,
      );
    }
    if (!client) return;

    const controller = new AbortController();
    void (async () => {
      try {
        const conversation =
          active ?? (await ensureConversation(controller.signal));
        const result = await api.runRead(
          conversation.id,
          "client_resource.list",
          { client: client.display_id, kind: "location" },
          controller.signal,
        );
        if (controller.signal.aborted || requestID !== locationRequest.current)
          return;
        setLocationCatalog({
          clientID: client.id,
          kind: "location",
          resources: resourceSummaries(result.data),
        });
      } catch {
        if (
          !controller.signal.aborted &&
          requestID === locationRequest.current
        ) {
          setLocationError("Active Locations could not be loaded.");
        }
      }
    })();
    return () => controller.abort();
  }, [
    action,
    active,
    api,
    canReadResources,
    open,
    resourceKind,
    selectedTargetClient,
  ]);

  async function ensureConversation(
    signal?: AbortSignal,
  ): Promise<AIWorkspaceConversation> {
    if (active) return active;
    const created = await api.create("New conversation", signal);
    setConversations((current) => [created, ...current]);
    setActive(created);
    return created;
  }

  async function send(event: FormEvent) {
    event.preventDefault();
    const text = draft.trim();
    if (!text || busy) return;
    setBusy(true);
    setError("");
    try {
      const conversation = await ensureConversation();
      const response = await api.sendMessage(conversation.id, text);
      setMessages((current) => [
        ...current,
        response.user_message,
        response.assistant_message,
      ]);
      if (response.proposal) {
        setProposal(response.proposal);
        setOutcome("");
      }
      setDraft("");
    } catch {
      setError("Rarity AI could not answer that request.");
    } finally {
      setBusy(false);
    }
  }

  async function confirmProposal() {
    if (!proposal || !proposalHasCanonicalTarget) return;
    const confirmedProposal = proposal;
    setBusy(true);
    setError("");
    try {
      const result = await api.confirm(
        confirmedProposal.id,
        confirmedProposal.version,
      );
      setOutcome(result.summary);
      setProposal(undefined);
      setActionOpen(false);
      if (confirmedProposal.tool_name === "client.create" && onClientCreated) {
        try {
          await onClientCreated();
        } catch {
          setError(
            "The Client was created, but the authorized Client directory could not be refreshed. Reload before targeting it.",
          );
        }
      }
      if (
        isClientResourceMutationTool(confirmedProposal.tool_name) &&
        onClientResourcesChanged
      ) {
        try {
          await onClientResourcesChanged();
        } catch {
          setError(
            "The resource changed, but the Client resource catalog could not be refreshed. Reload before acting on it again.",
          );
        }
      }
    } catch {
      setError("The target or your access changed. Prepare a fresh preview.");
    } finally {
      setBusy(false);
    }
  }

  async function rejectProposal() {
    if (!proposal) return;
    setBusy(true);
    try {
      await api.reject(proposal.id, proposal.version);
      setProposal(undefined);
      setOutcome("Action rejected.");
    } catch {
      setError("The action could not be rejected.");
    } finally {
      setBusy(false);
    }
  }

  if (!open) return null;

  return (
    <>
      <button
        className="ai-workspace__backdrop"
        type="button"
        aria-label="Close AI workspace"
        onClick={onClose}
      />
      <aside
        className="ai-workspace"
        role="dialog"
        aria-modal="false"
        aria-label="AI workspace"
      >
        <header className="ai-workspace__header">
          <div>
            <p className="ai-workspace__eyebrow">Rarity intelligence</p>
            <h2>AI workspace</h2>
          </div>
          <button ref={closeButton} type="button" onClick={onClose}>
            Close
          </button>
        </header>

        <p className="ai-workspace__scope">All authorized clients.</p>

        {conversations.length > 1 ? (
          <label className="ai-workspace__conversation">
            Conversation
            <select
              value={active?.id ?? ""}
              onChange={(event) => {
                const selected = conversations.find(
                  (item) => item.id === event.target.value,
                );
                if (!selected) return;
                setBusy(true);
                void getConversation(selected.id)
                  .then((loaded) => {
                    setActive(loaded.conversation);
                    setMessages(loaded.messages);
                  })
                  .catch(() =>
                    setError("The conversation could not be loaded."),
                  )
                  .finally(() => setBusy(false));
              }}
            >
              {conversations.map((conversation) => (
                <option key={conversation.id} value={conversation.id}>
                  {conversation.title}
                </option>
              ))}
            </select>
          </label>
        ) : null}

        <div className="ai-workspace__messages" aria-live="polite">
          {!messages.length && !busy ? (
            <div className="ai-workspace__empty">
              <h3>Ask about RTI or prepare an action</h3>
              <p>
                Guidance includes its documentation source. Changes always stop
                for your review.
              </p>
            </div>
          ) : null}
          {messages.map((message) => (
            <article
              key={message.id}
              className={`ai-workspace__message ai-workspace__message--${message.role}`}
            >
              <strong>{message.role === "user" ? "You" : "Rarity AI"}</strong>
              <p>{message.text}</p>
              <MessageReferences references={message.referenced_objects} />
            </article>
          ))}
          {busy ? <p role="status">Working…</p> : null}
          {error ? (
            <p className="ai-workspace__error" role="alert">
              {error}
            </p>
          ) : null}
          {outcome ? <p className="ai-workspace__outcome">{outcome}</p> : null}
          {structuredReadResult ? (
            <section
              className="ai-workspace__resource-results"
              aria-label="Structured read results"
            >
              <strong>{structuredReadResult.summary}</strong>
              {structuredReadContext ? (
                <p>
                  {structuredReadContext.action.label}
                  {structuredReadContext.client
                    ? ` · ${structuredReadContext.client.name} (${structuredReadContext.client.display_id})`
                    : " · All authorized MSP data"}
                </p>
              ) : null}
              {structuredReadResult.data ? (
                <StructuredReadValue value={structuredReadResult.data} />
              ) : (
                <p>No matching results.</p>
              )}
            </section>
          ) : null}
          {resourceResultSummary ? (
            <section
              className="ai-workspace__resource-results"
              aria-label="Client resource results"
            >
              <strong>{resourceResultSummary}</strong>
              {resourceResultContext ? (
                <p>
                  {resourceResultContext.client.name} (
                  {resourceResultContext.client.display_id}) ·{" "}
                  {resourceKindLabel(resourceResultContext.kind)}s
                </p>
              ) : null}
              {resourceResults.length ? (
                <ul>
                  {resourceResults.map((resource) => (
                    <li key={resource.id}>
                      <strong>{resource.name}</strong>
                      <span>
                        {resource.display_id}
                        {resource.detail ? ` · ${resource.detail}` : ""}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>No matching active resources.</p>
              )}
            </section>
          ) : null}
        </div>

        {proposal ? (
          <AIProposal
            proposal={proposal}
            targetClient={proposalTargetClient}
            busy={busy}
            onConfirm={() => void confirmProposal()}
            onReject={() => void rejectProposal()}
          />
        ) : actionOpen ? (
          <StructuredActionForm
            controller={structured}
            clients={clients}
            busy={busy}
            clientResourceCreateFields={
              <ClientResourceFields
                kind={resourceKind}
                form={resourceForm}
                locations={locations}
                locationError={locationError}
                showLocation={canReadResources}
                onChange={(field, nextValue) =>
                  setResourceForm((current) => ({
                    ...current,
                    [field]: nextValue,
                  }))
                }
              />
            }
            clientResourceUpdateFields={
              <ClientResourceUpdateFields
                kind={resourceKind}
                form={resourceForm}
                clearFields={resourceClearFields}
                onChange={(field, nextValue) => {
                  setResourceForm((current) => ({
                    ...current,
                    [field]: nextValue,
                  }));
                  setResourceClearFields((current) => {
                    if (!current.has(field)) return current;
                    const next = new Set(current);
                    next.delete(field);
                    return next;
                  });
                }}
                onClear={(field, checked) => {
                  setResourceClearFields((current) => {
                    const next = new Set(current);
                    if (checked) next.add(field);
                    else next.delete(field);
                    return next;
                  });
                  if (checked) {
                    setResourceForm((current) => ({
                      ...current,
                      [field]: "",
                    }));
                  }
                }}
              />
            }
            onCancel={() => setActionOpen(false)}
          />
        ) : (
          <button
            className="ai-workspace__prepare"
            type="button"
            onClick={() => setActionOpen(true)}
          >
            Prepare action
          </button>
        )}

        <form className="ai-workspace__composer" onSubmit={send}>
          <label htmlFor="ai-workspace-message">Ask Rarity AI</label>
          <div>
            <textarea
              id="ai-workspace-message"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              placeholder="How do queues work?"
              maxLength={16_000}
            />
            <button type="submit" disabled={busy || !draft.trim()}>
              Send
            </button>
          </div>
        </form>
      </aside>
    </>
  );
}

function resourceSummaries(
  data: Record<string, unknown> | undefined,
): ClientResourceSummary[] {
  const resources = data?.resources;
  if (!Array.isArray(resources)) return [];
  return resources.flatMap((value) => {
    const resource = value as Partial<ClientResourceSummary>;
    if (
      !resource ||
      typeof resource.id !== "string" ||
      !isClientResourceKind(resource.kind) ||
      typeof resource.display_id !== "string" ||
      typeof resource.name !== "string" ||
      typeof resource.version !== "number"
    ) {
      return [];
    }
    return [resource as ClientResourceSummary];
  });
}

function isClientResourceKind(value: unknown): value is ClientResourceKind {
  return resourceKinds.some((item) => item.kind === value);
}

function resourceKindLabel(kind: ClientResourceKind): string {
  return resourceKinds.find((item) => item.kind === kind)?.label ?? kind;
}

function isClientResourceMutationTool(name: string): boolean {
  return /^(location|contact|asset|service|contract)\.(create|update|deactivate|reactivate)$/.test(
    name,
  );
}

function StructuredReadValue({ value }: { value: unknown }) {
  if (value == null || value === "") return <>Not provided</>;
  if (typeof value === "boolean") return <>{value ? "Yes" : "No"}</>;
  if (typeof value === "string" || typeof value === "number")
    return <>{value}</>;
  if (Array.isArray(value)) {
    if (!value.length) return <>No matching results.</>;
    return (
      <ol className="ai-workspace__structured-list">
        {value.map((item, index) => (
          <li key={index}>
            <StructuredReadValue value={item} />
          </li>
        ))}
      </ol>
    );
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (!entries.length) return <>No matching results.</>;
    return (
      <dl className="ai-workspace__structured-data">
        {entries.map(([key, item]) => (
          <div key={key}>
            <dt>{structuredLabel(key)}</dt>
            <dd>
              <StructuredReadValue value={item} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return <>Not provided</>;
}

function structuredLabel(value: string): string {
  const normalized = value.replaceAll("_", " ").trim();
  return normalized
    ? normalized[0].toUpperCase() + normalized.slice(1)
    : "Value";
}

function MessageReferences({
  references,
}: {
  references?: Record<string, unknown>;
}) {
  const citations = references?.citations;
  if (!Array.isArray(citations)) return null;
  return (
    <ul className="ai-workspace__citations" aria-label="Sources">
      {citations.map((value, index) => {
        const citation = value as Record<string, unknown>;
        return (
          <li key={`${String(citation.source_key)}-${index}`}>
            <strong>{String(citation.section ?? "RTI documentation")}</strong>
            {citation.excerpt ? <span>{String(citation.excerpt)}</span> : null}
            <small>{String(citation.source_key ?? "")}</small>
          </li>
        );
      })}
    </ul>
  );
}
