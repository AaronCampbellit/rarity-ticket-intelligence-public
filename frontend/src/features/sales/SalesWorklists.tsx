import {
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  Button,
  KanbanBoard,
  KeyValueBuilder,
  Notice,
  Page,
  recordFromForm,
  StatePanel,
  TextListBuilder,
  ViewSwitcher,
  type KanbanColumn,
  type WorkView,
  readViewPreference,
  writeViewPreference,
} from "../../design-system";
import {
  APIError,
  createOpportunity,
  createOpportunityActivity,
  createOpportunityTask,
  createProposal,
  decideInternalApproval,
  getInternalApproval,
  getProposalVersion,
  issueProposalVersion,
  listOpportunities,
  listOpportunityForecast,
  listOpportunityActivities,
  listOpportunityAttachments,
  listOpportunityTasks,
  listPipelines,
  listProposals,
  recordOfflineAcceptance,
  replaceOpportunityCustomFields,
  replaceOpportunityParticipants,
  transitionOpportunity,
  uploadOpportunityAttachment,
  type OpportunityAttachmentResponse,
  type OpportunityActivityResponse,
  type OpportunityResponse,
  type OpportunityTaskResponse,
  type ForecastBucketResponse,
  type InternalApprovalResponse,
  type PipelineResponse,
  type ProposalResponse,
  type ProposalVersionResponse,
} from "./api";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import {
  createClassificationAPI,
  isClassificationCreateError,
} from "../classification/api";
import "./sales.css";

function money(value: { minor: number; currency: string }): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: value.currency,
  }).format(value.minor / 100);
}

function messageFor(error: unknown): string {
  if (error instanceof APIError) return error.message;
  return "The request could not be completed.";
}

export function OpportunityWorklist({
  clientID,
  capabilities,
  principalID = "anonymous",
}: {
  clientID: string;
  capabilities: ReadonlySet<string>;
  principalID?: string;
}) {
  const [items, setItems] = useState<OpportunityResponse[]>([]);
  const [pipelines, setPipelines] = useState<PipelineResponse[]>([]);
  const [forecast, setForecast] = useState<ForecastBucketResponse[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [activities, setActivities] = useState<OpportunityActivityResponse[]>(
    [],
  );
  const [tasks, setTasks] = useState<OpportunityTaskResponse[]>([]);
  const [attachments, setAttachments] = useState<
    OpportunityAttachmentResponse[]
  >([]);
  const [attachmentFile, setAttachmentFile] = useState<File>();
  const [taskTagIDs, setTaskTagIDs] = useState<string[]>([]);
  const [taskClassificationError, setTaskClassificationError] = useState("");
  const taskClassificationRef = useRef<HTMLInputElement>(null);
  const [transitionReason, setTransitionReason] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [view, setView] = useState<WorkView>(() =>
    readViewPreference(window.localStorage, principalID, "sales"),
  );
  const [error, setError] = useState("");
  const selected = items.find((item) => item.id === selectedID) ?? items[0];

  useEffect(() => {
    setTaskTagIDs([]);
    setTaskClassificationError("");
  }, [clientID, selected?.id]);

  const reload = useCallback(
    async (signal?: AbortSignal) => {
      const [found, configured, buckets] = await Promise.all([
        listOpportunities(clientID, signal),
        listPipelines(signal),
        listOpportunityForecast(clientID, signal),
      ]);
      setItems(found);
      setPipelines(configured);
      setForecast(buckets);
      setSelectedID((current) =>
        found.some((item) => item.id === current)
          ? current
          : (found[0]?.id ?? ""),
      );
      setState("ready");
    },
    [clientID],
  );

  useEffect(() => {
    const controller = new AbortController();
    setState("loading");
    void reload(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [reload]);

  useEffect(() => {
    if (!selected) {
      setActivities([]);
      setTasks([]);
      setAttachments([]);
      setAttachmentFile(undefined);
      return;
    }
    const controller = new AbortController();
    void Promise.all([
      listOpportunityActivities(clientID, selected.id, controller.signal),
      listOpportunityTasks(clientID, selected.id, controller.signal),
      listOpportunityAttachments(clientID, selected.id, controller.signal),
    ])
      .then(([foundActivities, foundTasks, foundAttachments]) => {
        setActivities(foundActivities);
        setTasks(foundTasks);
        setAttachments(foundAttachments);
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setError("Activity history could not be loaded.");
      });
    return () => controller.abort();
  }, [clientID, selected?.id]);

  const pipeline = pipelines.find((item) => item.id === selected?.pipeline_id);
  const stage = pipeline?.stages.find((item) => item.id === selected?.stage_id);
  const nextStages = useMemo(
    () =>
      stage?.allowed_next_stage_ids
        .map((id) => pipeline?.stages.find((candidate) => candidate.id === id))
        .filter((candidate) => candidate !== undefined) ?? [],
    [pipeline, stage],
  );
  const boardColumns = useMemo<KanbanColumn<OpportunityResponse>[]>(() => {
    const configured = pipelines.flatMap((configuredPipeline) =>
      configuredPipeline.stages.map((configuredStage) => ({
        id: configuredStage.id,
        label:
          pipelines.length > 1
            ? `${configuredPipeline.name} · ${configuredStage.name}`
            : configuredStage.name,
        tone:
          configuredStage.forecast_category === "closed_won"
            ? ("success" as const)
            : configuredStage.probability >= 70
              ? ("info" as const)
              : configuredStage.probability >= 35
                ? ("warning" as const)
                : ("neutral" as const),
        items: items.filter((item) => item.stage_id === configuredStage.id),
      })),
    );
    const configuredStageIDs = new Set(configured.map(({ id }) => id));
    const uncategorized = items.filter(
      (item) => !configuredStageIDs.has(item.stage_id),
    );
    return uncategorized.length
      ? [
          ...configured,
          {
            id: "uncategorized",
            label: "Needs placement",
            tone: "warning",
            items: uncategorized,
          },
        ]
      : configured;
  }, [items, pipelines]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const selectedPipeline = pipelines.find(
      (item) => item.id === form.get("pipeline_id"),
    );
    const firstStage = selectedPipeline?.stages[0];
    if (!selectedPipeline || !firstStage) return;
    setState("saving");
    setError("");
    try {
      const created = await createOpportunity(clientID, {
        pipelineID: selectedPipeline.id,
        stageID: firstStage.id,
        displayID: String(form.get("display_id") ?? ""),
        name: String(form.get("name") ?? ""),
        description: String(form.get("description") ?? ""),
        amountMinor: Math.round(Number(form.get("amount")) * 100),
        currency: String(form.get("currency") ?? "USD"),
        ownerID: String(form.get("owner_id") ?? ""),
        expectedCloseOn: String(form.get("expected_close_on") ?? ""),
      });
      formElement.reset();
      await reload();
      setSelectedID(created.id);
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function move(stageID: string) {
    const reason = transitionReason.trim();
    if (!selected || !reason) return;
    setState("saving");
    setError("");
    try {
      const updated = await transitionOpportunity(
        clientID,
        selected.id,
        selected.version,
        stageID,
        reason,
      );
      setTransitionReason("");
      setItems((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
      await reload();
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function addActivity(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setError("");
    try {
      const created = await createOpportunityActivity(clientID, selected.id, {
        kind: String(form.get("kind") ?? "note"),
        summary: String(form.get("summary") ?? ""),
        details: String(form.get("details") ?? ""),
      });
      setActivities((current) => [created, ...current]);
      formElement.reset();
      setState("ready");
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function addTask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    if (!taskTagIDs.length) {
      setTaskClassificationError(
        "Select at least one meaningful classification tag.",
      );
      taskClassificationRef.current?.focus();
      return;
    }
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setError("");
    try {
      const created = await createOpportunityTask(clientID, selected.id, {
        title: String(form.get("title") ?? ""),
        ownerID: String(form.get("owner_id") ?? "") || undefined,
        estimateMinutes: Number(form.get("estimate_minutes") ?? 0),
        tagIDs: taskTagIDs,
      });
      setTasks((current) => [...current, created]);
      formElement.reset();
      setTaskTagIDs([]);
      setState("ready");
    } catch (caught) {
      if (isClassificationCreateError(caught)) {
        setTaskClassificationError(
          "Classification changed. Confirm at least one current classification tag.",
        );
        taskClassificationRef.current?.focus();
      } else {
        setError(messageFor(caught));
      }
      setState("ready");
    }
  }

  async function saveCustomFields(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const form = new FormData(event.currentTarget);
    const fields = recordFromForm(form, "custom_fields");
    setState("saving");
    setError("");
    try {
      const updated = await replaceOpportunityCustomFields(
        clientID,
        selected.id,
        selected.version,
        fields,
      );
      setItems((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
      setState("ready");
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function uploadAttachment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const formElement = event.currentTarget;
    if (!attachmentFile) return;
    setState("saving");
    setError("");
    try {
      const created = await uploadOpportunityAttachment(
        clientID,
        selected.id,
        attachmentFile,
      );
      setAttachments((current) => [created, ...current]);
      formElement.reset();
      setAttachmentFile(undefined);
      setState("ready");
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function saveParticipants(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const form = new FormData(event.currentTarget);
    const contacts = form
      .getAll("contact_ids")
      .map(String)
      .map((value) => value.trim())
      .filter(Boolean);
    setState("saving");
    setError("");
    try {
      const updated = await replaceOpportunityParticipants(
        clientID,
        selected.id,
        selected.version,
        String(form.get("team_id") ?? ""),
        contacts,
      );
      setItems((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
      setState("ready");
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  return (
    <Page
      eyebrow="Native PSA sales"
      title="Opportunities"
      description="Operate the selected Client’s sales pipeline and activity history."
    >
      <div className="sales-worklist">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading opportunities"
            description="Retrieving pipeline, forecast, and activity data."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Opportunities could not be loaded"
            description="Verify Client access and retry the request."
            action={
              <Button intent="primary" onClick={() => void reload()}>
                Retry
              </Button>
            }
            supportCode="OPPORTUNITY-LIST-UNAVAILABLE"
          />
        ) : null}
        {error ? (
          <Notice tone="danger" title="Opportunity action failed" urgent>
            {error}
          </Notice>
        ) : null}
        {forecast.length ? (
          <section className="detail-card" aria-labelledby="live-forecast">
            <h2 id="live-forecast">Revenue forecast</h2>
            <div className="proposal-table-wrap">
              <table className="proposal-table">
                <thead>
                  <tr>
                    <th>Stage</th>
                    <th>Category</th>
                    <th>Opportunities</th>
                    <th>Pipeline amount</th>
                    <th>Weighted amount</th>
                  </tr>
                </thead>
                <tbody>
                  {forecast.map((bucket) => (
                    <tr
                      key={`${bucket.pipeline_id}-${bucket.stage_id}-${bucket.amount.currency}`}
                    >
                      <th scope="row">{bucket.stage_name}</th>
                      <td>{bucket.forecast_category.replaceAll("_", " ")}</td>
                      <td>{bucket.opportunity_count}</td>
                      <td>{money(bucket.amount)}</td>
                      <td>{money(bucket.weighted_amount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        ) : null}
        {capabilities.has("opportunity.create") ? (
          <details className="detail-card">
            <summary>Create opportunity</summary>
            <form onSubmit={create} className="settings-grid">
              <label>
                Display ID
                <input name="display_id" required />
              </label>
              <label>
                Name
                <input name="name" required />
              </label>
              <label>
                Pipeline
                <select name="pipeline_id" required>
                  <option value="">Select pipeline</option>
                  {pipelines.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Amount
                <input
                  name="amount"
                  type="number"
                  min="0"
                  step="0.01"
                  required
                />
              </label>
              <label>
                Currency
                <input
                  name="currency"
                  defaultValue="USD"
                  minLength={3}
                  maxLength={3}
                  required
                />
              </label>
              <label>
                Expected close
                <input name="expected_close_on" type="date" />
              </label>
              <label>
                Owner ID
                <input name="owner_id" />
              </label>
              <label>
                Description
                <textarea name="description" />
              </label>
              <button
                type="submit"
                disabled={state === "saving" || !pipelines.length}
              >
                Create opportunity
              </button>
            </form>
          </details>
        ) : null}
        {state === "ready" && items.length === 0 ? (
          <StatePanel
            state="empty"
            title="No active opportunities"
            description="No active opportunities for this Client."
          />
        ) : null}
        <div className="sales-worklist__view">
          <div>
            <strong>{items.length} opportunities</strong>
            <span>Live pipeline</span>
          </div>
          <ViewSwitcher
            value={view}
            available={["list", "kanban"]}
            onChange={(next) => {
              if (next !== "list" && next !== "kanban") return;
              setView(next);
              writeViewPreference(
                window.localStorage,
                principalID,
                "sales",
                next,
              );
            }}
          />
        </div>
        <div className="sales-worklist-grid" data-view={view}>
          <section aria-label="Opportunity list">
            {view === "kanban" ? (
              <KanbanBoard
                columns={boardColumns}
                getID={(item) => item.id}
                getLabel={(item) => `${item.display_id} ${item.name}`}
                onOpen={(item) => setSelectedID(item.id)}
                renderCard={(item) => (
                  <span className="sales-kanban-card">
                    <span>
                      <b>{item.display_id}</b>
                      <em>{money(item.amount)}</em>
                    </span>
                    <strong>{item.name}</strong>
                    <small>
                      {item.fields.expected_close_on
                        ? `Close ${item.fields.expected_close_on}`
                        : "Close date not set"}
                    </small>
                  </span>
                )}
              />
            ) : (
              items.map((item) => (
                <button
                  type="button"
                  className={
                    selected?.id === item.id
                      ? "worklist-row active"
                      : "worklist-row"
                  }
                  key={item.id}
                  onClick={() => setSelectedID(item.id)}
                >
                  <span>
                    <strong>{item.display_id}</strong>
                    <small>{item.name}</small>
                  </span>
                  <span>{money(item.amount)}</span>
                </button>
              ))
            )}
          </section>
          {selected ? (
            <section className="detail-card" aria-label="Selected opportunity">
              <p className="record-id">{selected.display_id}</p>
              <h2>{selected.name}</h2>
              <dl>
                <div>
                  <dt>Amount</dt>
                  <dd>{money(selected.amount)}</dd>
                </div>
                <div>
                  <dt>Pipeline</dt>
                  <dd>{pipeline?.name ?? selected.pipeline_id}</dd>
                </div>
                <div>
                  <dt>Stage</dt>
                  <dd>{stage?.name ?? selected.stage_id}</dd>
                </div>
                <div>
                  <dt>Probability</dt>
                  <dd>{stage ? `${stage.probability}%` : "Unavailable"}</dd>
                </div>
                <div>
                  <dt>Expected close</dt>
                  <dd>{selected.fields.expected_close_on || "Not set"}</dd>
                </div>
              </dl>
              {capabilities.has("opportunity.transition") &&
              nextStages.length ? (
                <div aria-label="Stage actions">
                  <label>
                    Transition reason
                    <input
                      value={transitionReason}
                      onChange={(event) =>
                        setTransitionReason(event.currentTarget.value)
                      }
                      required
                    />
                  </label>
                  {nextStages.map((next) => (
                    <button
                      key={next.id}
                      type="button"
                      disabled={state === "saving" || !transitionReason.trim()}
                      onClick={() => void move(next.id)}
                    >
                      Move to {next.name}
                    </button>
                  ))}
                </div>
              ) : null}
              <h3>Custom fields</h3>
              {capabilities.has("opportunity.update") ? (
                <form key={selected.id} onSubmit={saveCustomFields}>
                  <KeyValueBuilder
                    label="Custom fields"
                    name="custom_fields"
                    initialValue={selected.custom_fields}
                  />
                  <button type="submit" disabled={state === "saving"}>
                    Save custom fields
                  </button>
                </form>
              ) : Object.keys(selected.custom_fields ?? {}).length ? (
                <dl>
                  {Object.entries(selected.custom_fields ?? {}).map(
                    ([key, value]) => (
                      <div key={key}>
                        <dt>{key.replaceAll("_", " ")}</dt>
                        <dd>{value}</dd>
                      </div>
                    ),
                  )}
                </dl>
              ) : (
                <p>No custom fields recorded.</p>
              )}
              <h3>Sales participants</h3>
              {capabilities.has("opportunity.update") ? (
                <form
                  key={`${selected.id}-participants`}
                  onSubmit={saveParticipants}
                >
                  <label>
                    Participating team ID
                    <input
                      name="team_id"
                      defaultValue={selected.team_id ?? ""}
                    />
                  </label>
                  <TextListBuilder
                    label="Contacts"
                    name="contact_ids"
                    itemLabel="Contact ID"
                    placeholder="Contact identifier"
                    initialValues={selected.contact_ids}
                  />
                  <button type="submit" disabled={state === "saving"}>
                    Save participants
                  </button>
                </form>
              ) : (
                <p>
                  {(selected.contact_ids ?? []).length} linked contact
                  {(selected.contact_ids ?? []).length === 1 ? "" : "s"}
                  {selected.team_id ? ` · team ${selected.team_id}` : ""}
                </p>
              )}
              <h3>Activity</h3>
              {capabilities.has("opportunity.activity.create") ? (
                <form onSubmit={addActivity}>
                  <label>
                    Type
                    <select name="kind">
                      <option>note</option>
                      <option>call</option>
                      <option>email</option>
                      <option>meeting</option>
                    </select>
                  </label>
                  <label>
                    Summary
                    <input name="summary" required />
                  </label>
                  <label>
                    Details
                    <textarea name="details" />
                  </label>
                  <button type="submit" disabled={state === "saving"}>
                    Add activity
                  </button>
                </form>
              ) : null}
              {!activities.length ? (
                <p>No activity recorded.</p>
              ) : (
                <ol className="activity-list">
                  {activities.map((activity) => (
                    <li key={activity.id}>
                      <time dateTime={activity.occurred_at}>
                        {new Date(activity.occurred_at).toLocaleString()}
                      </time>
                      <p>
                        <strong>{activity.kind}</strong> — {activity.summary}
                      </p>
                      {activity.details ? (
                        <small>{activity.details}</small>
                      ) : null}
                    </li>
                  ))}
                </ol>
              )}
              <h3>Attachments</h3>
              {capabilities.has("attachment.create") ? (
                <form onSubmit={uploadAttachment}>
                  <label>
                    Add attachment
                    <input
                      name="attachment"
                      type="file"
                      accept=".csv,.docx,.gif,.jpeg,.jpg,.pdf,.png,.txt,.webp,.xlsx,.zip"
                      onChange={(event) =>
                        setAttachmentFile(event.currentTarget.files?.[0])
                      }
                      required
                    />
                  </label>
                  <button type="submit" disabled={state === "saving"}>
                    Upload attachment
                  </button>
                </form>
              ) : null}
              {!attachments.length ? (
                <p>No attachments recorded.</p>
              ) : (
                <ul className="task-list">
                  {attachments.map((attachment) => (
                    <li key={attachment.id}>
                      <span>{attachment.filename}</span>
                      <small>
                        {(attachment.size_bytes / 1024).toFixed(1)} KiB ·{" "}
                        {attachment.content_type}
                      </small>
                    </li>
                  ))}
                </ul>
              )}
              <h3>Tasks</h3>
              {capabilities.has("task.create") ? (
                <form onSubmit={addTask}>
                  <label>
                    Task title
                    <input name="title" required />
                  </label>
                  <label>
                    Task owner ID
                    <input name="owner_id" />
                  </label>
                  <label>
                    Task planned minutes
                    <input
                      name="estimate_minutes"
                      type="number"
                      min="0"
                      defaultValue="0"
                      required
                    />
                  </label>
                  <RequiredClassificationPicker
                    clientID={clientID}
                    selectedIDs={taskTagIDs}
                    inputRef={taskClassificationRef}
                    error={taskClassificationError}
                    onChange={(ids) => {
                      setTaskTagIDs(ids);
                      setTaskClassificationError("");
                    }}
                  />
                  <button
                    type="submit"
                    disabled={state === "saving" || !taskTagIDs.length}
                  >
                    Add task
                  </button>
                </form>
              ) : null}
              {!tasks.length ? (
                <p>No tasks recorded.</p>
              ) : (
                <ol className="task-list">
                  {tasks.map((task) => (
                    <li key={task.id}>
                      <span aria-hidden="true">
                        {task.status === "completed" ? "✓" : "○"}
                      </span>
                      <span>{task.title}</span>
                      <ObjectTagSummary
                        clientID={clientID}
                        target={{ objectType: "task", objectId: task.id }}
                      />
                      <DeferredObjectTagEditor
                        api={createClassificationAPI(
                          globalThis.fetch,
                          clientID,
                        )}
                        clientID={clientID}
                        target={{ objectType: "task", objectId: task.id }}
                      />
                    </li>
                  ))}
                </ol>
              )}
            </section>
          ) : null}
        </div>
      </div>
    </Page>
  );
}

export function ProposalWorklist({
  clientID,
  capabilities,
}: {
  clientID: string;
  capabilities: ReadonlySet<string>;
}) {
  const [items, setItems] = useState<ProposalResponse[]>([]);
  const [opportunities, setOpportunities] = useState<OpportunityResponse[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [lineKeys, setLineKeys] = useState(() => [crypto.randomUUID()]);
  const [currentVersion, setCurrentVersion] =
    useState<ProposalVersionResponse>();
  const [approval, setApproval] = useState<InternalApprovalResponse>();
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [error, setError] = useState("");
  const selected = items.find((item) => item.id === selectedID) ?? items[0];

  const reload = useCallback(
    async (signal?: AbortSignal) => {
      const [proposals, foundOpportunities] = await Promise.all([
        listProposals(clientID, signal),
        listOpportunities(clientID, signal),
      ]);
      setItems(proposals);
      setOpportunities(foundOpportunities);
      setSelectedID((current) =>
        proposals.some((item) => item.id === current)
          ? current
          : (proposals[0]?.id ?? ""),
      );
      setState("ready");
    },
    [clientID],
  );

  useEffect(() => {
    const controller = new AbortController();
    setState("loading");
    void reload(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [reload]);

  useEffect(() => {
    if (!selected?.current_version_id) {
      setCurrentVersion(undefined);
      setApproval(undefined);
      return;
    }
    const controller = new AbortController();
    void getProposalVersion(
      clientID,
      selected.current_version_id,
      controller.signal,
    )
      .then(async (version) => {
        setCurrentVersion(version);
        if (version.requires_internal_approval) {
          setApproval(
            await getInternalApproval(clientID, version.id, controller.signal),
          );
        } else {
          setApproval(undefined);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setError("Proposal version evidence could not be loaded.");
        }
      });
    return () => controller.abort();
  }, [clientID, selected?.current_version_id]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setError("");
    try {
      const created = await createProposal(
        clientID,
        String(form.get("opportunity_id") ?? ""),
        String(form.get("display_id") ?? ""),
      );
      formElement.reset();
      await reload();
      setSelectedID(created.id);
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function issue(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const currency = String(form.get("currency") ?? "USD");
    const cents = (name: string) =>
      Math.round(Number(form.get(name) ?? 0) * 100);
    setState("saving");
    setError("");
    try {
      await issueProposalVersion(clientID, selected.id, {
        expectedVersion: selected.current_version,
        currency,
        lines: lineKeys.map((key) => ({
          type: String(form.get(`type_${key}`) ?? "fixed_fee"),
          description: String(form.get(`description_${key}`) ?? ""),
          quantity: Number(form.get(`quantity_${key}`) ?? 1),
          unitPriceMinor: cents(`unit_price_${key}`),
          unitCostMinor: cents(`unit_cost_${key}`),
          discountMinor: cents(`discount_${key}`),
          taxMinor: cents(`tax_${key}`),
          taxTreatment: String(
            form.get(`tax_treatment_${key}`) ?? "tax_exempt",
          ),
          recurrence: String(form.get(`recurrence_${key}`) ?? ""),
          plannedMinutes: Number(form.get(`planned_minutes_${key}`) ?? 0),
        })),
        maximumWithoutApprovalMinor: cents("approval_maximum"),
        minimumMarginBasisPoints: Number(
          form.get("minimum_margin_basis_points") ?? 0,
        ),
        expiresAt: String(form.get("expires_at") ?? "")
          ? new Date(String(form.get("expires_at"))).toISOString()
          : undefined,
      });
      formElement.reset();
      setLineKeys([crypto.randomUUID()]);
      await reload();
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function decide(
    event: FormEvent<HTMLFormElement>,
    decision: "approved" | "rejected",
  ) {
    event.preventDefault();
    if (!currentVersion || !approval) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setError("");
    try {
      setApproval(
        await decideInternalApproval(
          clientID,
          currentVersion.id,
          approval.version,
          decision,
          String(form.get("reason") ?? ""),
        ),
      );
      formElement.reset();
      setState("ready");
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  async function acceptOffline(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!currentVersion) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    setError("");
    try {
      await recordOfflineAcceptance(
        clientID,
        currentVersion.id,
        String(form.get("signer_name") ?? ""),
        String(form.get("signer_email") ?? ""),
        new Date(String(form.get("accepted_at") ?? "")).toISOString(),
      );
      formElement.reset();
      await reload();
    } catch (caught) {
      setError(messageFor(caught));
      setState("ready");
    }
  }

  return (
    <Page
      eyebrow="Commercial baseline"
      title="Proposals"
      description="Live proposal versions and acceptance state."
    >
      <div className="sales-worklist">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading proposals"
            description="Retrieving proposal versions and acceptance evidence."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Proposals could not be loaded"
            description="Verify Client access and retry the request."
            action={
              <Button intent="primary" onClick={() => void reload()}>
                Retry
              </Button>
            }
            supportCode="PROPOSAL-LIST-UNAVAILABLE"
          />
        ) : null}
        {error ? (
          <Notice tone="danger" title="Proposal action failed" urgent>
            {error}
          </Notice>
        ) : null}
        {capabilities.has("proposal.create") ? (
          <details className="detail-card">
            <summary>Create proposal</summary>
            <form onSubmit={create} className="settings-grid">
              <label>
                Opportunity
                <select name="opportunity_id" required>
                  <option value="">Select opportunity</option>
                  {opportunities.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.display_id} · {item.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Proposal display ID
                <input name="display_id" required />
              </label>
              <button
                type="submit"
                disabled={state === "saving" || !opportunities.length}
              >
                Create proposal
              </button>
            </form>
          </details>
        ) : null}
        {state === "ready" && items.length === 0 ? (
          <StatePanel
            state="empty"
            title="No proposals"
            description="No proposals exist for the selected Client."
          />
        ) : null}
        <div className="sales-worklist-grid">
          <section className="proposal-list" aria-label="Proposal list">
            {items.map((item) => (
              <button
                type="button"
                className={
                  selected?.id === item.id
                    ? "worklist-row active"
                    : "worklist-row"
                }
                key={item.id}
                onClick={() => setSelectedID(item.id)}
              >
                <span>
                  <strong>{item.display_id}</strong>
                  <small>{item.state}</small>
                </span>
                <span>Version {item.current_version || "Draft"}</span>
              </button>
            ))}
          </section>
          {selected ? (
            <section className="detail-card" aria-label="Selected proposal">
              <p className="record-id">{selected.display_id}</p>
              <h2>Version {selected.current_version || "Draft"}</h2>
              <p>
                <span className="status-pill">{selected.state}</span>
              </p>
              <p>Opportunity {selected.opportunity_id}</p>
              {currentVersion ? (
                <dl>
                  <div>
                    <dt>Total</dt>
                    <dd>{money(currentVersion.total)}</dd>
                  </div>
                  <div>
                    <dt>Margin</dt>
                    <dd>{money(currentVersion.margin)}</dd>
                  </div>
                  <div>
                    <dt>Internal approval</dt>
                    <dd>
                      {currentVersion.requires_internal_approval
                        ? (approval?.state ?? "Loading")
                        : "Not required"}
                    </dd>
                  </div>
                </dl>
              ) : null}
              {capabilities.has("proposal.approve") &&
              approval?.state === "pending" ? (
                <form
                  onSubmit={(event) => {
                    const submitter = (event.nativeEvent as SubmitEvent)
                      .submitter as HTMLButtonElement | null;
                    void decide(
                      event,
                      submitter?.value === "rejected" ? "rejected" : "approved",
                    );
                  }}
                  className="settings-grid"
                >
                  <label>
                    Approval reason
                    <input name="reason" required />
                  </label>
                  <button
                    type="submit"
                    name="decision"
                    value="approved"
                    disabled={state === "saving"}
                  >
                    Approve version
                  </button>
                  <button
                    type="submit"
                    name="decision"
                    value="rejected"
                    disabled={state === "saving"}
                  >
                    Reject version
                  </button>
                </form>
              ) : null}
              {capabilities.has("proposal.acceptance.record") &&
              currentVersion &&
              (!currentVersion.requires_internal_approval ||
                approval?.state === "approved") ? (
                <details>
                  <summary>Record offline customer acceptance</summary>
                  <form onSubmit={acceptOffline} className="settings-grid">
                    <label>
                      Signer name
                      <input name="signer_name" required />
                    </label>
                    <label>
                      Signer email
                      <input name="signer_email" type="email" required />
                    </label>
                    <label>
                      Accepted at
                      <input
                        name="accepted_at"
                        type="datetime-local"
                        required
                      />
                    </label>
                    <button type="submit" disabled={state === "saving"}>
                      Record accepted commercial baseline
                    </button>
                  </form>
                </details>
              ) : null}
              {capabilities.has("proposal.issue") ? (
                <details>
                  <summary>Issue new immutable version</summary>
                  <form onSubmit={issue} className="settings-grid">
                    {lineKeys.map((key, index) => (
                      <fieldset key={key}>
                        <legend>Commercial line {index + 1}</legend>
                        <label>
                          Line type
                          <select name={`type_${key}`}>
                            <option value="fixed_fee">Fixed fee</option>
                            <option value="time_and_materials">
                              Time and materials
                            </option>
                            <option value="product_license">
                              Product/license
                            </option>
                            <option value="recurring_service">
                              Recurring service
                            </option>
                          </select>
                        </label>
                        <label>
                          Description
                          <input name={`description_${key}`} required />
                        </label>
                        <label>
                          Quantity
                          <input
                            name={`quantity_${key}`}
                            type="number"
                            min="0"
                            defaultValue="1"
                            required
                          />
                        </label>
                        <label>
                          Unit price
                          <input
                            name={`unit_price_${key}`}
                            type="number"
                            min="0"
                            step="0.01"
                            required
                          />
                        </label>
                        <label>
                          Unit cost
                          <input
                            name={`unit_cost_${key}`}
                            type="number"
                            min="0"
                            step="0.01"
                            defaultValue="0"
                            required
                          />
                        </label>
                        <label>
                          Discount
                          <input
                            name={`discount_${key}`}
                            type="number"
                            min="0"
                            step="0.01"
                            defaultValue="0"
                          />
                        </label>
                        <label>
                          Tax
                          <input
                            name={`tax_${key}`}
                            type="number"
                            min="0"
                            step="0.01"
                            defaultValue="0"
                          />
                        </label>
                        <label>
                          Tax treatment
                          <input
                            name={`tax_treatment_${key}`}
                            defaultValue="tax_exempt"
                            required
                          />
                        </label>
                        <label>
                          Recurrence
                          <input
                            name={`recurrence_${key}`}
                            placeholder="Required for recurring service"
                          />
                        </label>
                        <label>
                          Planned minutes
                          <input
                            name={`planned_minutes_${key}`}
                            type="number"
                            min="0"
                            defaultValue="0"
                          />
                        </label>
                        {lineKeys.length > 1 ? (
                          <button
                            type="button"
                            onClick={() =>
                              setLineKeys((current) =>
                                current.filter(
                                  (candidate) => candidate !== key,
                                ),
                              )
                            }
                          >
                            Remove line
                          </button>
                        ) : null}
                      </fieldset>
                    ))}
                    <button
                      type="button"
                      onClick={() =>
                        setLineKeys((current) => [
                          ...current,
                          crypto.randomUUID(),
                        ])
                      }
                    >
                      Add commercial line
                    </button>
                    <label>
                      Currency
                      <input
                        name="currency"
                        defaultValue="USD"
                        minLength={3}
                        maxLength={3}
                        required
                      />
                    </label>
                    <label>
                      Approval above amount
                      <input
                        name="approval_maximum"
                        type="number"
                        min="0"
                        step="0.01"
                        defaultValue="0"
                      />
                    </label>
                    <label>
                      Minimum margin (basis points)
                      <input
                        name="minimum_margin_basis_points"
                        type="number"
                        min="0"
                        max="10000"
                        defaultValue="0"
                      />
                    </label>
                    <label>
                      Expires at
                      <input name="expires_at" type="datetime-local" />
                    </label>
                    <button type="submit" disabled={state === "saving"}>
                      Issue version
                    </button>
                  </form>
                </details>
              ) : null}
            </section>
          ) : null}
        </div>
      </div>
    </Page>
  );
}
