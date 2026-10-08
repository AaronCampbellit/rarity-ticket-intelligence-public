import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";

import { Button, Dialog, Notice, Page, StatePanel } from "../../design-system";
import { classificationAdminAPI } from "./api";
import type {
  ClassificationAdminAPI,
  ClassificationAIPolicy,
  ClassificationCatalog,
  ClassificationHealth,
  ClassificationImpact,
  ClassificationMigrationRun,
  Tag,
} from "./types";
import "./classification.css";

type Tab = "catalog" | "groups" | "ai" | "health" | "migration";
type TagDraft = {
  label: string;
  groupId: string;
  description: string;
  color: string;
  synonyms: string;
};
type Lifecycle = {
  tag: Tag;
  operation: "merge" | "archive";
  replacementTagId: string;
  reason: string;
  impact?: ClassificationImpact;
  previewKey?: string;
  generation: number;
};
const tabs: Tab[] = ["catalog", "groups", "ai", "health", "migration"];

const emptyTagDraft: TagDraft = {
  label: "",
  groupId: "",
  description: "",
  color: "",
  synonyms: "",
};

export function ClassificationSettingsPage({
  api = classificationAdminAPI,
  clientID,
}: {
  api?: ClassificationAdminAPI;
  clientID?: string;
}) {
  const [catalog, setCatalog] = useState<ClassificationCatalog>();
  const [tab, setTab] = useState<Tab>("catalog");
  const [search, setSearch] = useState("");
  const [tagDraft, setTagDraft] = useState<TagDraft>();
  const [editing, setEditing] = useState<Tag>();
  const [lifecycle, setLifecycle] = useState<Lifecycle>();
  const [health, setHealth] = useState<ClassificationHealth>();
  const [migrations, setMigrations] = useState<ClassificationMigrationRun[]>();
  const [aiPolicy, setAIPolicy] = useState<ClassificationAIPolicy>();
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState("");
  const lifecycleTriggerRef = useRef<HTMLElement | null>(null);
  const lifecycleGeneration = useRef(0);

  const reload = () =>
    api
      .catalog()
      .then(setCatalog)
      .catch(() => setError("Classification catalog could not be loaded."));
  useEffect(() => {
    void reload();
  }, [api]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    setHealth(undefined);
  }, [clientID]);
  useEffect(() => {
    if (tab !== "health" || !clientID) return;
    const controller = new AbortController();
    api
      .health(clientID, controller.signal)
      .then((nextHealth) => {
        if (!controller.signal.aborted) setHealth(nextHealth);
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setError("Classification health could not be loaded.");
      });
    return () => controller.abort();
  }, [api, clientID, tab]);
  useEffect(() => {
    if (tab === "migration" && !migrations)
      api
        .migrationHistory()
        .then(setMigrations)
        .catch(() => setError("Migration history could not be loaded."));
  }, [api, migrations, tab]);
  useEffect(() => {
    if (tab !== "ai" || aiPolicy || !api.aiPolicy) return;
    const controller = new AbortController();
    api
      .aiPolicy(controller.signal)
      .then((policy) => {
        if (!controller.signal.aborted) setAIPolicy(policy);
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setError("AI classification policy could not be loaded.");
      });
    return () => controller.abort();
  }, [aiPolicy, api, tab]);

  async function saveAIPolicy() {
    if (!aiPolicy || !api.updateAIPolicy) return;
    setBusy("ai");
    setError("");
    setStatus("");
    try {
      setAIPolicy(
        await api.updateAIPolicy({
          automaticApplyEnabled: aiPolicy.automaticApplyEnabled,
          automaticApplyThreshold: aiPolicy.automaticApplyThreshold,
          modelProfileId: aiPolicy.modelProfileId,
          expectedVersion: aiPolicy.version,
        }),
      );
      setStatus("AI classification policy saved.");
    } catch {
      setError("AI classification policy could not be saved.");
    } finally {
      setBusy("");
    }
  }

  const orderedGroups = useMemo(
    () =>
      [...(catalog?.groups ?? [])].sort(
        (a, b) =>
          (a.position ?? 0) - (b.position ?? 0) ||
          a.label.localeCompare(b.label),
      ),
    [catalog],
  );
  const visibleTags = useMemo(
    () =>
      (catalog?.tags ?? [])
        .filter((tag) => {
          const terms = [tag.label, ...tag.synonyms].join(" ").toLowerCase();
          return terms.includes(search.trim().toLowerCase());
        })
        .sort(
          (left, right) =>
            orderedGroups.findIndex((group) => group.id === left.groupId) -
              orderedGroups.findIndex((group) => group.id === right.groupId) ||
            left.label.localeCompare(right.label),
        ),
    [catalog, orderedGroups, search],
  );
  const tagGroups = useMemo(
    () =>
      orderedGroups.filter(
        (group) => group.state !== "archived" && !group.systemManaged,
      ),
    [orderedGroups],
  );

  function onTabKeyDown(current: Tab, event: KeyboardEvent<HTMLButtonElement>) {
    const currentIndex = tabs.indexOf(current);
    const nextIndex =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? tabs.length - 1
          : event.key === "ArrowRight"
            ? (currentIndex + 1) % tabs.length
            : event.key === "ArrowLeft"
              ? (currentIndex - 1 + tabs.length) % tabs.length
              : -1;
    if (nextIndex < 0) return;
    event.preventDefault();
    const next = tabs[nextIndex];
    setTab(next);
    requestAnimationFrame(() =>
      document.getElementById(`classification-tab-${next}`)?.focus(),
    );
  }
  function beginLifecycle(
    tag: Tag,
    operation: Lifecycle["operation"],
    trigger: HTMLElement,
  ) {
    lifecycleTriggerRef.current = trigger;
    setLifecycle({
      tag,
      operation,
      replacementTagId: "",
      reason: "",
      generation: ++lifecycleGeneration.current,
    });
  }
  function closeLifecycle() {
    lifecycleGeneration.current += 1;
    setLifecycle(undefined);
    requestAnimationFrame(() => lifecycleTriggerRef.current?.focus());
  }

  async function saveTag() {
    if (!tagDraft?.label.trim() || !tagDraft.groupId) return;
    const input = {
      label: tagDraft.label.trim(),
      groupId: tagDraft.groupId,
      description: tagDraft.description.trim(),
      color: tagDraft.color.trim(),
      synonyms: tagDraft.synonyms
        .split(",")
        .map((term) => term.trim())
        .filter(Boolean),
    };
    setError("");
    setStatus("");
    setBusy("tag");
    try {
      if (editing)
        await api.updateTag(editing.id, {
          ...input,
          expectedVersion: editing.version,
        });
      else await api.createTag(input);
      setTagDraft(undefined);
      setEditing(undefined);
      setStatus(editing ? "Tag updated." : "Tag created.");
      await reload();
    } catch (cause) {
      await handleMutationError(cause, "tag");
    } finally {
      setBusy("");
    }
  }

  async function previewLifecycle() {
    if (
      !lifecycle ||
      (lifecycle.operation === "merge" && !lifecycle.replacementTagId)
    )
      return;
    const previewKey = lifecycleKey(lifecycle);
    try {
      setError("");
      setStatus("");
      setBusy("preview");
      setLifecycle((current) =>
        current ? { ...current, impact: undefined } : current,
      );
      const impact = await api.impact(
        lifecycle.tag.id,
        lifecycle.operation,
        lifecycle.replacementTagId,
      );
      const replacementMatches =
        (impact.replacementTagId ?? "") === lifecycle.replacementTagId;
      if (
        impact.tagId !== lifecycle.tag.id ||
        impact.operation !== lifecycle.operation ||
        !replacementMatches
      ) {
        setError(
          "Impact preview did not match the selected lifecycle action. Review the tag and try again.",
        );
        return;
      }
      setLifecycle((current) =>
        current && lifecycleKey(current) === previewKey
          ? { ...current, impact, previewKey }
          : current,
      );
    } catch {
      setError("Impact preview could not be loaded.");
    } finally {
      setBusy("");
    }
  }

  async function applyLifecycle() {
    if (!lifecycle) return;
    if (lifecycle.previewKey !== lifecycleKey(lifecycle)) {
      setError("Preview the current lifecycle impact before confirming.");
      return;
    }
    if (!lifecycle.reason.trim()) {
      setError("Enter a reason.");
      return;
    }
    setError("");
    setStatus("");
    setBusy("lifecycle");
    try {
      if (lifecycle.operation === "merge")
        await api.merge(lifecycle.tag.id, {
          survivorTagId: lifecycle.replacementTagId,
          reason: lifecycle.reason.trim(),
          expectedVersion: lifecycle.tag.version,
        });
      else
        await api.archive(lifecycle.tag.id, {
          replacementTagId: lifecycle.replacementTagId,
          reason: lifecycle.reason.trim(),
          expectedVersion: lifecycle.tag.version,
        });
      closeLifecycle();
      setStatus(`Tag ${lifecycle.operation}d.`);
      await reload();
    } catch (cause) {
      await handleMutationError(cause, "lifecycle");
    } finally {
      setBusy("");
    }
  }

  async function handleMutationError(
    cause: unknown,
    editor?: "tag" | "lifecycle",
  ) {
    const code =
      cause && typeof cause === "object" && "code" in cause
        ? String(cause.code)
        : "";
    if (code === "version_conflict") {
      setError(
        "Classification changed elsewhere. The catalog was refreshed; review the impact again.",
      );
      if (editor === "tag") {
        setEditing(undefined);
        setTagDraft(undefined);
      }
      if (editor === "lifecycle") closeLifecycle();
      await reload();
    } else setError("Classification change could not be saved.");
  }

  return (
    <Page
      className="classification-settings-page"
      eyebrow="Global administration"
      title="Classification settings"
      description="Maintain the governed MSP-wide taxonomy used for routing, reporting, and automation."
    >
      {error ? (
        <Notice title="Action needed" tone="danger" urgent>
          {error}
        </Notice>
      ) : null}
      {status ? <Notice title="Classification updated">{status}</Notice> : null}
      <div
        className="classification-tabs"
        role="tablist"
        aria-label="Classification settings"
      >
        <TabButton
          id="catalog"
          active={tab}
          onSelect={setTab}
          onKeyDown={onTabKeyDown}
        >
          Catalog
        </TabButton>
        <TabButton
          id="groups"
          active={tab}
          onSelect={setTab}
          onKeyDown={onTabKeyDown}
        >
          Groups
        </TabButton>
        <TabButton
          id="ai"
          active={tab}
          onSelect={setTab}
          onKeyDown={onTabKeyDown}
        >
          AI classification
        </TabButton>
        <TabButton
          id="health"
          active={tab}
          onSelect={setTab}
          onKeyDown={onTabKeyDown}
        >
          Classification health
        </TabButton>
        <TabButton
          id="migration"
          active={tab}
          onSelect={setTab}
          onKeyDown={onTabKeyDown}
        >
          Migration history
        </TabButton>
      </div>
      {tab === "catalog" ? (
        <section
          id="classification-panel-catalog"
          aria-labelledby="classification-tab-catalog"
          role="tabpanel"
          className="classification-panel"
        >
          <div className="classification-toolbar">
            <label>
              {" "}
              <span className="sr-only">Search tags</span>
              <input
                type="search"
                aria-label="Search tags"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Search labels and synonyms"
              />
            </label>
            <Button
              disabled={!tagGroups.length}
              onClick={() => {
                setEditing(undefined);
                setTagDraft({
                  ...emptyTagDraft,
                  groupId: tagGroups[0]?.id ?? "",
                });
              }}
            >
              Add tag
            </Button>
          </div>
          {!catalog ? (
            <StatePanel
              state="loading"
              title="Loading classification catalog"
            />
          ) : (
            <>
              {!tagGroups.length ? (
                <Notice title="Classification groups required" tone="warning">
                  Create an active, non-system classification group before
                  adding or regrouping tags.
                </Notice>
              ) : null}
              <div className="classification-tag-grid">
                {visibleTags.map((tag) => (
                  <article key={tag.id} className="classification-tag-card">
                    <div>
                      <h2>{tag.label}</h2>
                      <p>
                        {orderedGroups.find((group) => group.id === tag.groupId)
                          ?.label ?? "Unknown group"}
                        {tag.synonyms.length
                          ? ` · ${tag.synonyms.join(", ")}`
                          : ""}
                      </p>
                    </div>
                    <div className="classification-card-actions">
                      <Button
                        size="compact"
                        disabled={tag.systemManaged}
                        aria-label={
                          tag.systemManaged
                            ? `System tag: ${tag.label}`
                            : `Edit ${tag.label}`
                        }
                        onClick={() => {
                          setEditing(tag);
                          setTagDraft({
                            label: tag.label,
                            groupId: tag.groupId,
                            description: tag.description ?? "",
                            color: tag.color ?? "",
                            synonyms: tag.synonyms.join(", "),
                          });
                        }}
                      >
                        Edit
                      </Button>
                      <Button
                        size="compact"
                        disabled={tag.systemManaged}
                        aria-label={`Merge ${tag.label}`}
                        onClick={(event) =>
                          beginLifecycle(tag, "merge", event.currentTarget)
                        }
                      >
                        Merge
                      </Button>
                      <Button
                        size="compact"
                        disabled={tag.systemManaged}
                        aria-label={`Archive ${tag.label}`}
                        onClick={(event) =>
                          beginLifecycle(tag, "archive", event.currentTarget)
                        }
                      >
                        Archive
                      </Button>
                    </div>
                  </article>
                ))}
              </div>
            </>
          )}
        </section>
      ) : null}
      {tab === "groups" ? (
        <GroupsPanel
          groups={orderedGroups}
          api={api}
          onChanged={reload}
          onError={setError}
          onStatus={setStatus}
        />
      ) : null}
      {tab === "ai" ? (
        <section
          id="classification-panel-ai"
          aria-labelledby="classification-tab-ai"
          role="tabpanel"
          className="classification-panel"
        >
          {!api.aiPolicy ? (
            <StatePanel
              state="empty"
              title="AI classification is unavailable"
            />
          ) : !aiPolicy ? (
            <StatePanel
              state="loading"
              title="Loading AI classification policy"
            />
          ) : (
            <>
              <label>
                <input
                  type="checkbox"
                  checked={aiPolicy.automaticApplyEnabled}
                  onChange={(event) =>
                    setAIPolicy({
                      ...aiPolicy,
                      automaticApplyEnabled: event.target.checked,
                    })
                  }
                />{" "}
                Apply high-confidence tags automatically
              </label>
              <label>
                Classification model
                <select
                  aria-label="Classification model"
                  value={aiPolicy.modelProfileId}
                  onChange={(event) =>
                    setAIPolicy({
                      ...aiPolicy,
                      modelProfileId: event.target.value,
                    })
                  }
                >
                  <option value="">Select a model</option>
                  {aiPolicy.modelOptions.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Automatic apply threshold
                <input
                  aria-label="Automatic apply threshold"
                  type="number"
                  min="0.5"
                  max="1"
                  step="0.001"
                  value={aiPolicy.automaticApplyThreshold}
                  onChange={(event) =>
                    setAIPolicy({
                      ...aiPolicy,
                      automaticApplyThreshold: Number(event.target.value),
                    })
                  }
                />
              </label>
              <p>
                Retained rate:{" "}
                {aiPolicy.retainedRate === undefined
                  ? "No data"
                  : `${Math.round(aiPolicy.retainedRate * 100)}%`}
              </p>
              <p>
                Technician change rate:{" "}
                {aiPolicy.changeRate === undefined
                  ? "No data"
                  : `${Math.round(aiPolicy.changeRate * 100)}%`}
              </p>
              <p>Provider health: {aiPolicy.providerFailureHealth}</p>
              <Button
                intent="primary"
                loading={busy === "ai"}
                loadingLabel="Saving AI policy"
                disabled={
                  !aiPolicy.modelProfileId ||
                  aiPolicy.automaticApplyThreshold < 0.5 ||
                  aiPolicy.automaticApplyThreshold > 1
                }
                onClick={() => void saveAIPolicy()}
              >
                Save AI policy
              </Button>
            </>
          )}
        </section>
      ) : null}
      {tab === "health" ? (
        <section
          id="classification-panel-health"
          aria-labelledby="classification-tab-health"
          role="tabpanel"
          className="classification-panel"
        >
          {!clientID ? (
            <StatePanel
              state="empty"
              title="Select an active Client to view classification health"
            />
          ) : !health ? (
            <StatePanel state="loading" title="Loading classification health" />
          ) : (
            Object.entries(health.byObjectType).map(([type, values]) => (
              <article className="classification-health-card" key={type}>
                <h2>{type.replace("_", " ")}</h2>
                <p>{values?.meaningful ?? 0} meaningful</p>
                <p>
                  {values?.unclassified ?? 0} unclassified ·{" "}
                  {values?.archiveFallback ?? 0} archive fallback
                </p>
              </article>
            ))
          )}
        </section>
      ) : null}
      {tab === "migration" ? (
        <section
          id="classification-panel-migration"
          aria-labelledby="classification-tab-migration"
          role="tabpanel"
          className="classification-panel"
        >
          {!migrations ? (
            <StatePanel state="loading" title="Loading migration history" />
          ) : (
            <div className="classification-table">
              {migrations.map((run) => (
                <article key={run.id}>
                  <strong>{run.id}</strong>
                  <span>{run.status}</span>
                  <span>
                    {run.rowsMigrated} of {run.rowsDiscovered} migrated ·{" "}
                    {run.fallbackAssignments} fallbacks
                  </span>
                </article>
              ))}
            </div>
          )}
        </section>
      ) : null}
      {tagDraft ? (
        <TagForm
          draft={tagDraft}
          groups={tagGroups}
          editing={editing}
          onChange={setTagDraft}
          onSave={saveTag}
          busy={busy === "tag"}
          onCancel={() => {
            setTagDraft(undefined);
            setEditing(undefined);
          }}
        />
      ) : null}
      {lifecycle ? (
        <LifecycleDialog
          lifecycle={lifecycle}
          tags={catalog?.tags ?? []}
          onChange={setLifecycle}
          onPreview={previewLifecycle}
          onConfirm={applyLifecycle}
          onClose={closeLifecycle}
          busy={busy === "preview" || busy === "lifecycle"}
        />
      ) : null}
    </Page>
  );
}

function TabButton({
  id,
  active,
  onSelect,
  onKeyDown,
  children,
}: {
  id: Tab;
  active: Tab;
  onSelect: (tab: Tab) => void;
  onKeyDown: (tab: Tab, event: KeyboardEvent<HTMLButtonElement>) => void;
  children: string;
}) {
  return (
    <button
      type="button"
      role="tab"
      id={`classification-tab-${id}`}
      aria-controls={`classification-panel-${id}`}
      tabIndex={active === id ? 0 : -1}
      aria-selected={active === id}
      onClick={() => onSelect(id)}
      onKeyDown={(event) => onKeyDown(id, event)}
    >
      {children}
    </button>
  );
}

function TagForm({
  draft,
  groups,
  editing,
  onChange,
  onSave,
  busy,
  onCancel,
}: {
  draft: TagDraft;
  groups: ClassificationCatalog["groups"];
  editing?: Tag;
  onChange: (draft: TagDraft) => void;
  onSave: () => void;
  busy: boolean;
  onCancel: () => void;
}) {
  const change =
    (name: keyof TagDraft) =>
    (event: ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
      onChange({ ...draft, [name]: event.target.value });
  return (
    <section className="classification-form" aria-labelledby="tag-form-title">
      <h2 id="tag-form-title">
        {editing ? `Edit ${editing.label}` : "Add tag"}
      </h2>
      <label>
        Tag label
        <input
          aria-label="Tag label"
          value={draft.label}
          onChange={change("label")}
        />
      </label>
      <label>
        Tag group
        <select
          aria-label="Tag group"
          value={draft.groupId}
          onChange={change("groupId")}
        >
          {groups.map((group) => (
            <option key={group.id} value={group.id}>
              {group.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        Description
        <input value={draft.description} onChange={change("description")} />
      </label>
      <label>
        Color
        <input value={draft.color} onChange={change("color")} />
      </label>
      <label>
        Synonyms
        <input
          aria-label="Synonyms"
          value={draft.synonyms}
          onChange={change("synonyms")}
          placeholder="Comma separated"
        />
      </label>
      <div>
        <Button onClick={onSave} disabled={busy}>
          {busy ? "Saving…" : editing ? "Save tag" : "Create tag"}
        </Button>
        <Button onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </div>
    </section>
  );
}

function LifecycleDialog({
  lifecycle,
  tags,
  onChange,
  onPreview,
  onConfirm,
  onClose,
  busy,
}: {
  lifecycle: Lifecycle;
  tags: Tag[];
  onChange: (value: Lifecycle) => void;
  onPreview: () => void;
  onConfirm: () => void;
  onClose: () => void;
  busy: boolean;
}) {
  useEffect(() => {
    const background = document.getElementById("main-content");
    background?.setAttribute("inert", "");
    return () => background?.removeAttribute("inert");
  }, []);
  const noun =
    lifecycle.operation === "merge" ? "Surviving tag" : "Replacement tag";
  const fallback = Object.entries(
    lifecycle.impact?.fallbackByObjectType ?? {},
  ).map(
    ([type, count]) =>
      `${objectTypeLabel(type)} without another meaningful tag will receive Unclassified (${count}).`,
  );
  const replacement = tags.find((tag) => tag.id === lifecycle.replacementTagId);
  return createPortal(
    <Dialog
      open
      title={`${capitalize(lifecycle.operation)} ${lifecycle.tag.label}`}
      description="Review the authorized impact before confirming this catalog change."
      onClose={onClose}
    >
      <div className="classification-dialog">
        <label>
          {noun}
          <select
            aria-label={noun}
            value={lifecycle.replacementTagId}
            onChange={(event) =>
              onChange({
                ...lifecycle,
                replacementTagId: event.target.value,
                impact: undefined,
              })
            }
          >
            <option value="">
              {lifecycle.operation === "archive"
                ? "No replacement — use fallback where needed"
                : "Choose a tag"}
            </option>
            {tags
              .filter(
                (tag) =>
                  tag.id !== lifecycle.tag.id &&
                  tag.state === "active" &&
                  !tag.systemManaged,
              )
              .map((tag) => (
                <option key={tag.id} value={tag.id}>
                  {tag.label}
                </option>
              ))}
          </select>
        </label>
        <Button
          onClick={onPreview}
          disabled={
            busy ||
            (lifecycle.operation === "merge" && !lifecycle.replacementTagId)
          }
        >
          Preview {lifecycle.operation} impact
        </Button>
        {lifecycle.impact ? (
          <div className="classification-impact">
            <p>{lifecycle.impact.affectedObjects} authorized objects</p>
            <p>
              {lifecycle.impact.affectedSavedViews} saved views ·{" "}
              {lifecycle.impact.affectedReports} report ·{" "}
              {lifecycle.impact.affectedAutomations} automations
            </p>
            {replacement ? (
              <p>
                {lifecycle.impact.affectedObjects} authorized objects will move
                to {replacement.label}.{" "}
                {fallback.length
                  ? `Without that replacement, ${fallback.join(" ")}`
                  : ""}
              </p>
            ) : (
              fallback.map((item) => <p key={item}>Archive fallback: {item}</p>)
            )}
            <p>Expected tag version: {lifecycle.tag.version}</p>
            <label>
              Reason
              <textarea
                aria-label="Reason"
                value={lifecycle.reason}
                onChange={(event) =>
                  onChange({ ...lifecycle, reason: event.target.value })
                }
              />
            </label>
            <Button onClick={onConfirm} disabled={busy}>
              Confirm {lifecycle.operation}
            </Button>
          </div>
        ) : null}
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
      </div>
    </Dialog>,
    document.body,
  );
}

function GroupsPanel({
  groups,
  api,
  onChanged,
  onError,
  onStatus,
}: {
  groups: ClassificationCatalog["groups"];
  api: ClassificationAdminAPI;
  onChanged: () => Promise<void>;
  onError: (error: string) => void;
  onStatus: (status: string) => void;
}) {
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [editing, setEditing] =
    useState<ClassificationCatalog["groups"][number]>();
  const [draft, setDraft] = useState({
    label: "",
    description: "",
    position: "",
  });
  const [busy, setBusy] = useState(false);
  async function create() {
    if (!label.trim()) return;
    onError("");
    onStatus("");
    setBusy(true);
    try {
      await api.createGroup({
        label: label.trim(),
        description: description.trim(),
        position:
          Math.max(0, ...groups.map((group) => group.position ?? 0)) + 1,
      });
      setLabel("");
      setDescription("");
      await onChanged();
      onStatus("Classification group created.");
    } catch {
      onError("Classification group could not be saved.");
    } finally {
      setBusy(false);
    }
  }
  async function updateGroup(
    group: ClassificationCatalog["groups"][number],
    values: {
      label: string;
      description: string;
      position: number;
      state: "active" | "archived";
    },
  ) {
    if (group.systemManaged) return;
    onError("");
    onStatus("");
    setBusy(true);
    try {
      await api.updateGroup(group.id, {
        ...values,
        expectedVersion: group.version ?? 1,
      });
      setEditing(undefined);
      await onChanged();
      onStatus("Classification group updated.");
    } catch (cause) {
      const code =
        cause && typeof cause === "object" && "code" in cause
          ? String(cause.code)
          : "";
      onError(
        code === "version_conflict"
          ? "Classification group changed elsewhere. The catalog was refreshed."
          : "Classification group could not be saved.",
      );
      setEditing(undefined);
      await onChanged();
    } finally {
      setBusy(false);
    }
  }
  async function update(
    next: Partial<{ position: number; state: "active" | "archived" }>,
  ) {
    if (!editing) return;
    await updateGroup(editing, {
      label: draft.label.trim() || editing.label,
      description: draft.description,
      position:
        next.position ?? Number(draft.position || editing.position || 1),
      state: next.state ?? editing.state ?? "active",
    });
  }
  return (
    <section
      id="classification-panel-groups"
      aria-labelledby="classification-tab-groups"
      role="tabpanel"
      className="classification-panel"
    >
      <div className="classification-table">
        {groups.map((group) => (
          <article key={group.id}>
            <strong>{group.label}</strong>
            <span>Position {group.position ?? 0}</span>
            <span>{group.state ?? "active"}</span>
            <div className="classification-card-actions">
              <Button
                size="compact"
                aria-label={
                  group.systemManaged
                    ? `System group: ${group.label}`
                    : `Edit ${group.label}`
                }
                disabled={group.systemManaged}
                onClick={() => {
                  setEditing(group);
                  setDraft({
                    label: group.label,
                    description: group.description,
                    position: String(group.position ?? 1),
                  });
                }}
              >
                Edit
              </Button>
              <Button
                size="compact"
                aria-label={`${group.state === "archived" ? "Reactivate" : "Archive"} ${group.label}`}
                disabled={busy || group.systemManaged}
                onClick={() =>
                  void updateGroup(group, {
                    label: group.label,
                    description: group.description,
                    position: group.position ?? 1,
                    state: group.state === "archived" ? "active" : "archived",
                  })
                }
              >
                {group.state === "archived" ? "Reactivate" : "Archive"}
              </Button>
            </div>
          </article>
        ))}
      </div>
      {editing ? (
        <section
          className="classification-form"
          aria-labelledby="group-form-title"
        >
          <h2 id="group-form-title">Edit {editing.label}</h2>
          <label>
            Group label
            <input
              aria-label="Edit group label"
              value={draft.label}
              onChange={(event) =>
                setDraft({ ...draft, label: event.target.value })
              }
            />
          </label>
          <label>
            Description
            <input
              value={draft.description}
              onChange={(event) =>
                setDraft({ ...draft, description: event.target.value })
              }
            />
          </label>
          <label>
            Position
            <input
              type="number"
              min="1"
              value={draft.position}
              onChange={(event) =>
                setDraft({ ...draft, position: event.target.value })
              }
            />
          </label>
          <div>
            <Button onClick={() => void update({})} disabled={busy}>
              {busy ? "Saving…" : "Save group"}
            </Button>
            <Button onClick={() => setEditing(undefined)} disabled={busy}>
              Cancel
            </Button>
          </div>
        </section>
      ) : null}
      <section className="classification-form">
        <h2>Add group</h2>
        <label>
          Group label
          <input
            value={label}
            onChange={(event) => setLabel(event.target.value)}
          />
        </label>
        <label>
          Description
          <input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>
        <Button onClick={create} disabled={busy}>
          {busy ? "Saving…" : "Create group"}
        </Button>
      </section>
    </section>
  );
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
function lifecycleKey(lifecycle: Lifecycle) {
  return [
    lifecycle.generation,
    lifecycle.tag.id,
    lifecycle.operation,
    lifecycle.replacementTagId,
    lifecycle.tag.version,
  ].join(":");
}
function objectTypeLabel(value: string) {
  return (
    (
      {
        task: "Tasks",
        project: "Projects",
        work_record: "Tickets",
        asset: "Assets",
        knowledge_article: "Documentation",
        time_entry: "Time entries",
      } as Record<string, string>
    )[value] ?? capitalize(value.replace("_", " "))
  );
}
