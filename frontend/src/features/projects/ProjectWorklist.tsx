import { CalendarAdministration } from "../calendar/CalendarAdministration";
import { CustomDateEditor } from "../calendar/CustomDateEditor";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import {
  Button,
  DirtyForm,
  KanbanBoard,
  Page,
  StatePanel,
  TextListBuilder,
  ViewSwitcher,
  readViewPreference,
  writeViewPreference,
  type KanbanColumn,
  type WorkView,
  type WorkspaceItem,
  useOptionalWorkspace,
} from "../../design-system";
import {
  applyChangeOrder,
  approveChangeOrder,
  createChangeOrder,
  createCostActual,
  createProjectTask,
  createResourcePlan,
  createTaskTimeEntry,
  getProject,
  getProjectTask,
  listProjects,
  issueChangeOrderVersion,
  recognizeBillableWork,
  overrideChangeOrder,
  updatePhase,
  type FinancialSummaryResponse,
  type ProjectSummaryResponse,
  type ProjectTaskDetail,
  type ProjectWorkspaceResponse,
} from "./api";
import { ChangeOrderEditor } from "./ChangeOrderEditor";
import { ProjectPage } from "./ProjectPage";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import {
  createClassificationAPI,
  isClassificationCreateError,
} from "../classification/api";
import { InternalCollaborationPanel } from "../collaboration/InternalCollaborationPanel";
import type { MentionDeepLink } from "../mentions/types";
import type {
  ChangeOrderWorkspace,
  FinancialSummaryModel,
  ProjectWorkspace,
} from "./types";
import "./projects.css";

export function projectModel(
  value: ProjectWorkspaceResponse,
): ProjectWorkspace {
  const currency =
    value.financials?.original_budget.currency ||
    value.financials?.current_budget.currency ||
    "USD";
  const financials = value.financials;
  return {
    id: value.id,
    displayID: value.display_id,
    name: value.name,
    clientName: value.client_name,
    lifecycleState: value.lifecycle_state,
    plannedStart: value.planned_start || "Not scheduled",
    plannedEnd: value.planned_end || "Not scheduled",
    originalProposalVersion: value.original_proposal_version,
    phases: value.phases.map((phase) => ({
      id: phase.id,
      position: phase.position,
      name: phase.name,
      state: phase.state,
      ownerName: phase.owner_id || "Unassigned",
      participatingTeams: phase.participating_teams ?? [],
      plannedMinutes: phase.planned_minutes,
      actualMinutes: phase.actual_minutes,
      deliverables: phase.deliverables ?? [],
      completionCriteria: phase.completion_criteria ?? [],
      financials: phase.financials
        ? financialModel(phase.financials, currency)
        : undefined,
      tasks: phase.tasks.map((task) => ({
        id: task.id,
        title: task.title,
        ownerName: task.owner_name || "Unassigned",
        completed: task.status === "completed",
        subtasks: task.subtasks,
        plannedMinutes: task.estimate_minutes,
        actualMinutes: task.actual_minutes,
      })),
    })),
    projectTasks: value.project_tasks.map((task) => ({
      id: task.id,
      title: task.title,
      status: task.status,
      ownerName: task.owner_name || "Unassigned",
      subtasks: task.subtasks,
      plannedMinutes: task.estimate_minutes,
      actualMinutes: task.actual_minutes,
    })),
    resourcePlans: value.resource_plans.map((plan) => ({
      id: plan.id,
      resourceType: plan.resource_type,
      resourceName: plan.resource_name,
      startsOn: plan.starts_on,
      endsOn: plan.ends_on,
      plannedMinutes: plan.planned_minutes,
    })),
    costActuals: value.cost_actuals.map((cost) => ({
      id: cost.id,
      phaseID: cost.phase_id || "",
      costType: cost.cost_type,
      description: cost.description,
      amountMinor: cost.amount.minor,
      currency: cost.amount.currency,
      committed: cost.committed,
      incurredAt: new Date(cost.incurred_at).toLocaleDateString(),
    })),
    capacity: value.capacity.map((resource) => ({
      id: resource.id,
      name: resource.name,
      role: "Technician",
      availableMinutes: resource.available_minutes,
      scheduledMinutes: resource.scheduled_minutes,
      actualMinutes: resource.actual_minutes,
      overbookedMinutes: resource.overbooked_minutes,
    })),
    financials: financialModel(financials, currency),
    financialsVisible: value.financials_visible,
    changeOrders: value.change_orders.map(
      ({ order, current_version, decision }) => {
        return {
          id: order.id,
          displayID: order.display_id,
          state: order.state,
          version: order.version,
          currentVersion: current_version
            ? {
                id: current_version.id,
                version: current_version.version,
                description: current_version.description,
                currency: current_version.currency,
                revenueDeltaMinor: current_version.revenue_delta_minor,
                costDeltaMinor: current_version.cost_delta_minor,
                laborDeltaMinutes: current_version.labor_delta_minutes,
              }
            : undefined,
          decisions: decision
            ? [
                {
                  id: decision.id,
                  decision: decision.decision,
                  override: decision.override,
                  reason: decision.reason || "",
                  decidedBy: decision.decided_by,
                  decidedAt: new Date(decision.decided_at).toLocaleString(),
                },
              ]
            : [],
        } satisfies ChangeOrderWorkspace;
      },
    ),
  };
}

function financialModel(
  financials: FinancialSummaryResponse | undefined,
  fallbackCurrency: string,
): FinancialSummaryModel {
  return {
    currency:
      financials?.original_budget.currency ||
      financials?.current_budget.currency ||
      fallbackCurrency,
    originalBudgetMinor: financials?.original_budget.minor ?? 0,
    currentBudgetMinor: financials?.current_budget.minor ?? 0,
    plannedLaborMinor: financials?.planned_labor.minor ?? 0,
    actualLaborMinor: financials?.actual_labor.minor ?? 0,
    actualLaborComplete: financials?.actual_labor_complete ?? false,
    costActualsMinor: financials?.cost_actuals.minor ?? 0,
    committedCostMinor: financials?.committed_cost.minor ?? 0,
    billableWorkMinor: financials?.billable_work.minor ?? 0,
    profitMinor: financials?.profit.minor ?? 0,
    profitAvailable: financials?.profit_available ?? false,
    projectedProfitMinor: financials?.projected_profit.minor ?? 0,
    marginBasisPoints: financials?.margin_basis_points ?? 0,
  };
}

export function ProjectWorklist({
  clientID,
  capabilities = new Set<string>(),
  principalID = "anonymous",
  selectedRecord,
  authenticatedMentionLink,
}: {
  clientID: string;
  capabilities?: ReadonlySet<string>;
  principalID?: string;
  selectedRecord?: WorkspaceItem;
  authenticatedMentionLink?: MentionDeepLink;
}) {
  const selectedProjectID =
    selectedRecord?.entityType === "task"
      ? selectedRecord.parentRecordID
      : selectedRecord?.entityType === "project"
        ? selectedRecord.recordID
        : undefined;
  const [items, setItems] = useState<ProjectSummaryResponse[]>([]);
  const [selectedID, setSelectedID] = useState(selectedProjectID ?? "");
  const [workspace, setWorkspace] = useState<ProjectWorkspaceResponse>();
  const [directTask, setDirectTask] = useState<ProjectTaskDetail>();
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [detailState, setDetailState] = useState<
    "idle" | "loading" | "ready" | "error"
  >(selectedRecord ? "loading" : "idle");
  const [detailAttempt, setDetailAttempt] = useState(0);
  const [taskTagIDs, setTaskTagIDs] = useState<string[]>([]);
  const [timeTagIDs, setTimeTagIDs] = useState<string[]>([]);
  const [projectTimeTaskID, setProjectTimeTaskID] = useState("");
  const [taskClassificationError, setTaskClassificationError] = useState("");
  const [timeClassificationError, setTimeClassificationError] = useState("");
  const taskClassificationRef = useRef<HTMLInputElement>(null);
  const timeClassificationRef = useRef<HTMLInputElement>(null);
  const [view, setView] = useState<WorkView>(() =>
    readViewPreference(window.localStorage, principalID, "project"),
  );
  const workspaceController = useOptionalWorkspace();
  const workspaceOwnerID = selectedRecord?.id ?? "route:project";
  const activeProjectID = selectedProjectID ?? selectedID;
  const selectedTaskID =
    selectedRecord?.entityType === "task" ? selectedRecord.recordID : undefined;

  useEffect(() => {
    setTaskTagIDs([]);
    setTimeTagIDs([]);
    setTaskClassificationError("");
    setTimeClassificationError("");
  }, [clientID, activeProjectID, selectedTaskID]);

  useEffect(() => {
    setTimeTagIDs([]);
    setTimeClassificationError("");
  }, [projectTimeTaskID]);
  const projectColumns = useMemo<KanbanColumn<ProjectSummaryResponse>[]>(() => {
    const columns: KanbanColumn<ProjectSummaryResponse>[] = [
      { id: "planned", label: "Planned", tone: "neutral", items: [] },
      { id: "active", label: "Active", tone: "info", items: [] },
      {
        id: "risk",
        label: "At risk / blocked",
        tone: "warning",
        items: [],
      },
      { id: "complete", label: "Complete", tone: "success", items: [] },
    ];
    for (const item of items) {
      const lifecycle = item.lifecycle_state.toLowerCase().replaceAll(" ", "_");
      const column = ["complete", "completed", "closed", "delivered"].includes(
        lifecycle,
      )
        ? columns[3]
        : ["at_risk", "blocked", "on_hold", "paused"].includes(lifecycle)
          ? columns[2]
          : ["active", "in_progress", "delivery"].includes(lifecycle)
            ? columns[1]
            : columns[0];
      column.items.push(item);
    }
    return columns;
  }, [items]);

  useEffect(() => {
    const controller = new AbortController();
    setState("loading");
    void listProjects(clientID, controller.signal)
      .then((found) => {
        setItems(found);
        setSelectedID(selectedProjectID ?? found[0]?.id ?? "");
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [clientID, loadAttempt, selectedProjectID]);

  useEffect(() => {
    if (!activeProjectID) {
      setWorkspace(undefined);
      setDetailState("idle");
      return;
    }
    const controller = new AbortController();
    setWorkspace(undefined);
    setDetailState("loading");
    void getProject(clientID, activeProjectID, controller.signal)
      .then((found) => {
        setWorkspace(found);
        setDetailState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setDetailState("error");
      });
    return () => controller.abort();
  }, [activeProjectID, clientID, detailAttempt]);

  useEffect(() => {
    setDirectTask(undefined);
    if (!selectedTaskID || selectedProjectID) return;
    const controller = new AbortController();
    setDetailState("loading");
    void getProjectTask(clientID, selectedTaskID, controller.signal)
      .then((found) => {
        if (!controller.signal.aborted) {
          setDirectTask(found);
          setDetailState("ready");
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setDetailState("error");
      });
    return () => controller.abort();
  }, [clientID, detailAttempt, selectedProjectID, selectedTaskID]);

  async function refresh() {
    if (activeProjectID) {
      setWorkspace(await getProject(clientID, activeProjectID));
    }
  }

  async function approve(order: ChangeOrderWorkspace) {
    if (!order.currentVersion) return;
    try {
      await approveChangeOrder(
        clientID,
        order.currentVersion.id,
        order.version,
      );
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function override(
    order: ChangeOrderWorkspace,
    evidence: { versionID: string; expectedVersion: number; reason: string },
  ) {
    try {
      await overrideChangeOrder(
        clientID,
        evidence.versionID,
        evidence.expectedVersion,
        evidence.reason,
      );
      await refresh();
      return true;
    } catch {
      setState("error");
      return false;
    }
  }

  async function apply(order: ChangeOrderWorkspace) {
    if (!order.currentVersion) return;
    try {
      await applyChangeOrder(clientID, order.currentVersion.id, order.version);
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function addChangeOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workspace) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await createChangeOrder(
        clientID,
        workspace.id,
        String(form.get("display_id") ?? ""),
      );
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function addProjectTask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workspace) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const selectedParentTask = String(form.get("parent_task_id") ?? "");
    const selectedLocation = String(form.get("location") ?? "project");
    const [parentType, parentID, parentTaskID] = selectedParentTask
      ? selectedParentTask.split(":")
      : selectedLocation === "project"
        ? ["project", workspace.id, ""]
        : ["phase", selectedLocation.slice("phase:".length), ""];
    try {
      await createProjectTask(
        clientID,
        {
          type: parentType === "phase" ? "phase" : "project",
          id: parentID,
        },
        {
          title: String(form.get("title") ?? ""),
          parentTaskID: parentTaskID || undefined,
          ownerID: String(form.get("owner_id") ?? "") || undefined,
          estimateMinutes: Number(form.get("estimate_minutes") ?? 0),
          tagIDs: taskTagIDs,
        },
      );
      formElement.reset();
      setTaskTagIDs([]);
      await refresh();
    } catch (cause) {
      if (isClassificationCreateError(cause)) {
        setTaskClassificationError(
          "Classification changed. Confirm at least one current classification tag.",
        );
        taskClassificationRef.current?.focus();
      } else {
        setState("error");
      }
    }
  }

  async function issueChangeOrder(
    event: FormEvent<HTMLFormElement>,
    order: ChangeOrderWorkspace,
  ) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await issueChangeOrderVersion(clientID, order.id, {
        expectedVersion: order.version,
        description: String(form.get("description") ?? ""),
        currency: String(form.get("currency") ?? ""),
        revenueDeltaMinor: Number(form.get("revenue_delta_minor") ?? 0),
        costDeltaMinor: Number(form.get("cost_delta_minor") ?? 0),
        laborDeltaMinutes: Number(form.get("labor_delta_minutes") ?? 0),
      });
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function addTaskTime(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await createTaskTimeEntry(clientID, String(form.get("task_id") ?? ""), {
        technicianID: String(form.get("technician_id") ?? ""),
        minutes: Number(form.get("minutes") ?? 0),
        billable: form.get("billable") === "on",
        note: String(form.get("note") ?? ""),
        tagIDs: timeTagIDs,
      });
      formElement.reset();
      setTimeTagIDs([]);
      await refresh();
    } catch (cause) {
      if (isClassificationCreateError(cause)) {
        setTimeClassificationError(
          "Classification changed. Confirm at least one current classification tag.",
        );
        timeClassificationRef.current?.focus();
      } else {
        setState("error");
      }
    }
  }

  async function addCostActual(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workspace) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await createCostActual(clientID, workspace.id, {
        phaseID: String(form.get("phase_id") ?? "") || undefined,
        costType: String(form.get("cost_type") ?? ""),
        description: String(form.get("description") ?? ""),
        amountMinor: Math.round(Number(form.get("amount") ?? 0) * 100),
        currency: String(form.get("currency") ?? ""),
        committed: form.get("committed") === "on",
        incurredAt: String(form.get("incurred_at") ?? ""),
      });
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function addBillableWork(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workspace) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await recognizeBillableWork(clientID, workspace.id, {
        phaseID: String(form.get("phase_id") ?? "") || undefined,
        description: String(form.get("description") ?? ""),
        amountMinor: Math.round(Number(form.get("amount") ?? 0) * 100),
        currency: String(form.get("currency") ?? ""),
        recognizedAt: String(form.get("recognized_at") ?? ""),
      });
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function planResource(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workspace) return;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      await createResourcePlan(clientID, workspace.id, {
        phaseID: String(form.get("phase_id") ?? ""),
        resourceType: form.get("resource_type") === "team" ? "team" : "role",
        resourceID: String(form.get("resource_id") ?? ""),
        startsOn: String(form.get("starts_on") ?? ""),
        endsOn: String(form.get("ends_on") ?? ""),
        plannedMinutes: Number(form.get("planned_minutes") ?? 0),
      });
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  async function editPhase(
    event: FormEvent<HTMLFormElement>,
    phase: ProjectWorkspaceResponse["phases"][number],
  ) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const values = (name: string) =>
      form
        .getAll(name)
        .map(String)
        .map((value) => value.trim())
        .filter(Boolean);
    try {
      await updatePhase(clientID, phase, {
        name: String(form.get("name") ?? ""),
        ownerID: String(form.get("owner_id") ?? ""),
        participatingTeams: values("participating_teams"),
        plannedStart: String(form.get("planned_start") ?? ""),
        plannedEnd: String(form.get("planned_end") ?? ""),
        plannedMinutes: Number(form.get("planned_minutes") ?? 0),
        budgetMinor: Math.round(Number(form.get("budget") ?? 0) * 100),
        currency: String(form.get("currency") ?? "USD"),
        deliverables: values("deliverables"),
        completionCriteria: values("completion_criteria"),
      });
      formElement.reset();
      await refresh();
    } catch {
      setState("error");
    }
  }

  if (!selectedRecord && state === "loading")
    return (
      <Page
        eyebrow="Delivery"
        title="Projects"
        description="Project delivery, resources, financials, and change control."
      >
        <StatePanel
          state="loading"
          title="Loading projects"
          description="Retrieving the selected Client’s project portfolio."
        />
      </Page>
    );
  if (!selectedRecord && state === "error")
    return (
      <Page
        eyebrow="Delivery"
        title="Projects"
        description="Project delivery, resources, financials, and change control."
      >
        <StatePanel
          state="error"
          title="Projects could not be loaded"
          description="Verify Client access and platform readiness, then try again."
          action={
            <Button
              intent="primary"
              onClick={() => setLoadAttempt((current) => current + 1)}
            >
              Retry
            </Button>
          }
          supportCode="PROJECT-LIST-UNAVAILABLE"
        />
      </Page>
    );
  if (!selectedRecord && !items.length)
    return (
      <Page
        eyebrow="Delivery"
        title="Projects"
        description="Project delivery, resources, financials, and change control."
      >
        <StatePanel
          state="empty"
          title="No projects"
          description="No projects exist for the selected Client."
        />
      </Page>
    );
  const model = workspace ? projectModel(workspace) : undefined;
  const projectWorkspaceItem = (
    item: ProjectSummaryResponse,
  ): WorkspaceItem => ({
    id: `project:${item.id}`,
    routeID: "project",
    recordID: item.id,
    clientID,
    label: item.display_id,
    entityType: "project",
    openedAt: Date.now(),
    preview: {
      title: item.name,
      summary: `${item.display_id} · ${item.planned_start || "Start not scheduled"} to ${item.planned_end || "End not scheduled"}`,
      status: item.lifecycle_state,
    },
  });
  const taskWorkspaceItem = (taskID: string, title: string): WorkspaceItem => ({
    id: `task:${taskID}`,
    routeID: "project",
    recordID: taskID,
    parentRecordID: workspace?.id ?? activeProjectID,
    clientID,
    label: title,
    entityType: "task",
    openedAt: Date.now(),
    preview: {
      title,
      summary: `Delivery task in ${model?.displayID ?? "the selected project"}.`,
    },
  });
  if (!selectedRecord) {
    return (
      <Page
        eyebrow="Delivery"
        title="Projects"
        description="Project delivery, resources, financials, and change control."
      >
        <div className="project-portfolio">
          <header>
            <div>
              <strong>{items.length} projects</strong>
              <span>Authorized client portfolio</span>
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
                  "project",
                  next,
                );
              }}
            />
          </header>
          {view === "kanban" ? (
            <KanbanBoard
              columns={projectColumns}
              getID={(item) => item.id}
              getLabel={(item) => `${item.display_id} ${item.name}`}
              onOpen={(item) => {
                setSelectedID(item.id);
                workspaceController?.openPreview(projectWorkspaceItem(item));
              }}
              onOpenFull={(item) =>
                workspaceController?.openRecord(projectWorkspaceItem(item))
              }
              renderCard={(item) => (
                <span className="project-portfolio-card">
                  <b>{item.display_id}</b>
                  <strong>{item.name}</strong>
                  <small>{item.lifecycle_state}</small>
                  <ObjectTagSummary
                    clientID={clientID}
                    target={{ objectType: "project", objectId: item.id }}
                  />
                </span>
              )}
            />
          ) : (
            <div className="project-portfolio-list" aria-label="Project list">
              {items.map((item) => (
                <article key={item.id}>
                  <button
                    type="button"
                    aria-label={`${item.display_id} ${item.name}`}
                    onClick={() => {
                      setSelectedID(item.id);
                      workspaceController?.openPreview(
                        projectWorkspaceItem(item),
                      );
                    }}
                    onDoubleClick={() =>
                      workspaceController?.openRecord(
                        projectWorkspaceItem(item),
                      )
                    }
                  >
                    <b>{item.display_id}</b>
                    <strong>{item.name}</strong>
                    <span>{item.lifecycle_state}</span>
                    <small>
                      {item.planned_start || "Start not scheduled"} —{" "}
                      {item.planned_end || "End not scheduled"}
                    </small>
                    <ObjectTagSummary
                      clientID={clientID}
                      target={{ objectType: "project", objectId: item.id }}
                    />
                  </button>
                  <Button
                    onClick={() =>
                      workspaceController?.openRecord(
                        projectWorkspaceItem(item),
                      )
                    }
                  >
                    Open
                  </Button>
                </article>
              ))}
            </div>
          )}
        </div>
      </Page>
    );
  }

  if (detailState === "error") {
    return (
      <Page eyebrow="Delivery" title={selectedRecord.label}>
        <StatePanel
          state="error"
          title="Delivery record could not be loaded"
          description="Verify Client access and try loading this record again."
          action={
            <Button
              intent="primary"
              onClick={() => setDetailAttempt((current) => current + 1)}
            >
              Retry
            </Button>
          }
          supportCode="PROJECT-DETAIL-UNAVAILABLE"
        />
      </Page>
    );
  }

  if (
    detailState === "loading" ||
    (selectedProjectID && workspace?.id !== selectedProjectID) ||
    (selectedTaskID && !selectedProjectID && directTask?.id !== selectedTaskID)
  ) {
    return (
      <Page eyebrow="Delivery" title={selectedRecord.label}>
        <StatePanel
          state="loading"
          title={`Loading ${selectedRecord.entityType}`}
          description="Retrieving the selected delivery record and its parent context."
        />
      </Page>
    );
  }

  if (selectedRecord.entityType === "task") {
    if (directTask) {
      return (
        <Page
          eyebrow="Task"
          title={directTask.title}
          description={`${directTask.parent.type.replaceAll("_", " ")} · ${directTask.parent.id}`}
        >
          <section className="project-task-workspace">
            <dl>
              <div>
                <dt>Status</dt>
                <dd>{directTask.status}</dd>
              </div>
              <div>
                <dt>Owner</dt>
                <dd>{directTask.owner_id || "Unassigned"}</dd>
              </div>
              <div>
                <dt>Planned</dt>
                <dd>{directTask.estimate_minutes} minutes</dd>
              </div>
            </dl>
            <DeferredObjectTagEditor
              api={createClassificationAPI(globalThis.fetch, clientID)}
              clientID={clientID}
              target={{ objectType: "task", objectId: directTask.id }}
            />
            <CustomDateEditor
              objectType="task"
              objectID={directTask.id}
              clientID={clientID}
            />
            <InternalCollaborationPanel
              clientId={clientID}
              parentType="task"
              parentId={directTask.id}
              authorId={principalID}
              capabilities={capabilities}
              canEditParent={capabilities.has("project.edit")}
              authenticatedMentionLink={authenticatedMentionLink}
            />
          </section>
        </Page>
      );
    }
    const task =
      workspace?.project_tasks.find(
        ({ id }) => id === selectedRecord.recordID,
      ) ??
      workspace?.phases
        .flatMap((phase) => phase.tasks)
        .find(({ id }) => id === selectedRecord.recordID);
    if (!task || !model) {
      return (
        <Page eyebrow="Delivery task" title={selectedRecord.label}>
          <StatePanel
            state="error"
            title="Task could not be found"
            description="The task may have been removed or is no longer available in this Client scope."
            action={
              <Button
                intent="primary"
                onClick={() => setDetailAttempt((current) => current + 1)}
              >
                Retry
              </Button>
            }
            supportCode="PROJECT-TASK-NOT-FOUND"
          />
        </Page>
      );
    }
    const parentPhase = workspace?.phases.find((phase) =>
      phase.tasks.some(({ id }) => id === task.id),
    );
    return (
      <Page
        eyebrow="Delivery task"
        title={task.title}
        description={`${model.displayID} · ${model.name}`}
      >
        <section className="project-task-workspace">
          <dl>
            <div>
              <dt>Status</dt>
              <dd>{task.status}</dd>
            </div>
            <div>
              <dt>Owner</dt>
              <dd>{task.owner_name || "Unassigned"}</dd>
            </div>
            <div>
              <dt>Planned</dt>
              <dd>{task.estimate_minutes} minutes</dd>
            </div>
            <div>
              <dt>Actual</dt>
              <dd>{task.actual_minutes} minutes</dd>
            </div>
            <div>
              <dt>Schedule</dt>
              <dd>
                {parentPhase?.planned_start || "Start not scheduled"} —{" "}
                {parentPhase?.planned_end || "End not scheduled"}
              </dd>
            </div>
          </dl>
          <DeferredObjectTagEditor
            api={createClassificationAPI(globalThis.fetch, clientID)}
            clientID={clientID}
            target={{ objectType: "task", objectId: task.id }}
          />
          <CustomDateEditor
            objectType="task"
            objectID={task.id}
            clientID={clientID}
          />
          <InternalCollaborationPanel
            clientId={clientID}
            parentType="task"
            parentId={task.id}
            authorId={principalID}
            capabilities={capabilities}
            canEditParent={capabilities.has("project.edit")}
            authenticatedMentionLink={authenticatedMentionLink}
          />
          {capabilities.has("time_entry.create") ? (
            <DirtyForm
              key={selectedRecord.id}
              ownerID={selectedRecord.id}
              className="settings-grid"
              onSubmit={addTaskTime}
            >
              <input
                type="hidden"
                name="task_id"
                value={selectedRecord.recordID}
              />
              <label>
                Technician ID
                <input name="technician_id" required />
              </label>
              <label>
                Actual minutes
                <input name="minutes" type="number" min="1" required />
              </label>
              <label>
                <input name="billable" type="checkbox" /> Billable
              </label>
              <label>
                Task note
                <textarea
                  name="note"
                  placeholder="Record delivery context with this time entry"
                />
              </label>
              <RequiredClassificationPicker
                clientID={clientID}
                selectedIDs={timeTagIDs}
                inputRef={timeClassificationRef}
                error={timeClassificationError}
                onChange={(ids) => {
                  setTimeTagIDs(ids);
                  setTimeClassificationError("");
                }}
              />
              <button type="submit" disabled={!timeTagIDs.length}>
                Record task time
              </button>
            </DirtyForm>
          ) : null}
        </section>
      </Page>
    );
  }

  return (
    <>
      {model ? (
        <ProjectPage
          actions={
            <CalendarAdministration
              principalID={principalID}
              capabilities={capabilities}
              clients={[{ id: clientID, name: "Current client" }]}
              defaultClientID={clientID}
              defaultProjectID={model.id}
              initialTab="milestone"
              onChanged={() => setDetailAttempt((value) => value + 1)}
            />
          }
          project={model}
          clientID={clientID}
          onPreviewTask={(taskID, title) =>
            workspaceController?.openPreview(taskWorkspaceItem(taskID, title))
          }
          onOpenTask={(taskID, title) =>
            workspaceController?.openRecord(taskWorkspaceItem(taskID, title))
          }
        >
          <DeferredObjectTagEditor
            api={createClassificationAPI(globalThis.fetch, clientID)}
            clientID={clientID}
            target={{ objectType: "project", objectId: model.id }}
          />
          <CustomDateEditor
            objectType="project"
            objectID={model.id}
            clientID={clientID}
          />
          <InternalCollaborationPanel
            clientId={clientID}
            parentType="project"
            parentId={model.id}
            authorId={principalID}
            capabilities={capabilities}
            canEditParent={capabilities.has("project.edit")}
            authenticatedMentionLink={authenticatedMentionLink}
          />
        </ProjectPage>
      ) : (
        <p role="status">Loading project detail…</p>
      )}
      {workspace && capabilities.has("project.resource.plan") ? (
        <details className="change-order-workspace">
          <summary>Add phase resource plan</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            onSubmit={planResource}
            className="settings-grid"
          >
            <label>
              Phase
              <select name="phase_id" required>
                <option value="">Select phase</option>
                {workspace.phases.map((phase) => (
                  <option key={phase.id} value={phase.id}>
                    {phase.position}. {phase.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Resource type
              <select name="resource_type">
                <option value="role">Role</option>
                <option value="team">Team</option>
              </select>
            </label>
            <label>
              Role or team ID
              <input name="resource_id" required />
            </label>
            <label>
              Starts on
              <input name="starts_on" type="date" required />
            </label>
            <label>
              Ends on
              <input name="ends_on" type="date" required />
            </label>
            <label>
              Planned minutes
              <input name="planned_minutes" type="number" min="0" required />
            </label>
            <button type="submit">Add resource plan</button>
          </DirtyForm>
        </details>
      ) : null}
      {workspace && capabilities.has("project.edit")
        ? workspace.phases.map((phase) => (
            <details className="change-order-workspace" key={phase.id}>
              <summary>Edit phase: {phase.name}</summary>
              <DirtyForm
                ownerID={workspaceOwnerID}
                onSubmit={(event) => void editPhase(event, phase)}
                className="settings-grid"
              >
                <label>
                  Phase name
                  <input name="name" defaultValue={phase.name} required />
                </label>
                <label>
                  Owner ID
                  <input name="owner_id" defaultValue={phase.owner_id} />
                </label>
                <TextListBuilder
                  label="Participating teams"
                  name="participating_teams"
                  itemLabel="Team ID"
                  placeholder="Team identifier"
                  initialValues={phase.participating_teams}
                />
                <label>
                  Planned start
                  <input
                    name="planned_start"
                    type="date"
                    defaultValue={phase.planned_start}
                  />
                </label>
                <label>
                  Planned end
                  <input
                    name="planned_end"
                    type="date"
                    defaultValue={phase.planned_end}
                  />
                </label>
                <label>
                  Planned minutes
                  <input
                    name="planned_minutes"
                    type="number"
                    min="0"
                    defaultValue={phase.planned_minutes}
                    required
                  />
                </label>
                <label>
                  Budget
                  <input
                    name="budget"
                    type="number"
                    min="0"
                    step="0.01"
                    defaultValue={phase.budget.minor / 100}
                    required
                  />
                </label>
                <label>
                  Currency
                  <input
                    name="currency"
                    minLength={3}
                    maxLength={3}
                    defaultValue={phase.budget.currency || "USD"}
                    required
                  />
                </label>
                <TextListBuilder
                  label="Deliverables"
                  name="deliverables"
                  itemLabel="Deliverable"
                  placeholder="Describe a delivered outcome"
                  initialValues={phase.deliverables}
                />
                <TextListBuilder
                  label="Completion criteria"
                  name="completion_criteria"
                  itemLabel="Criterion"
                  placeholder="Define a verifiable completion check"
                  initialValues={phase.completion_criteria}
                />
                <button type="submit">Save phase</button>
              </DirtyForm>
            </details>
          ))
        : null}
      {workspace && capabilities.has("change_order.update") ? (
        <details className="change-order-workspace">
          <summary>Create Change Order</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            className="settings-grid"
            onSubmit={addChangeOrder}
          >
            <label>
              Change Order ID
              <input name="display_id" placeholder="CO-1042" required />
            </label>
            <button type="submit">Create draft</button>
          </DirtyForm>
        </details>
      ) : null}
      {workspace && capabilities.has("task.create") ? (
        <details className="change-order-workspace">
          <summary>Add Project task or subtask</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            className="settings-grid"
            onSubmit={addProjectTask}
          >
            <label>
              Task title
              <input name="title" required />
            </label>
            <label>
              Location
              <select name="location">
                <option value="project">Project backlog</option>
                {workspace.phases.map((phase) => (
                  <option key={phase.id} value={`phase:${phase.id}`}>
                    Phase {phase.position}: {phase.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Task owner ID
              <input name="owner_id" />
            </label>
            <label>
              Planned minutes
              <input
                name="estimate_minutes"
                type="number"
                min="0"
                defaultValue="0"
                required
              />
            </label>
            <label>
              Parent task
              <select name="parent_task_id">
                <option value="">No parent task</option>
                {workspace.project_tasks.map((task) => (
                  <option
                    key={task.id}
                    value={`project:${workspace.id}:${task.id}`}
                  >
                    Project: {task.title}
                  </option>
                ))}
                {workspace.phases.flatMap((phase) =>
                  phase.tasks.map((task) => (
                    <option
                      key={task.id}
                      value={`phase:${phase.id}:${task.id}`}
                    >
                      {phase.name}: {task.title}
                    </option>
                  )),
                )}
              </select>
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
            <button type="submit" disabled={!taskTagIDs.length}>
              Add Project task
            </button>
          </DirtyForm>
        </details>
      ) : null}
      {workspace && capabilities.has("time_entry.create") ? (
        <details className="change-order-workspace">
          <summary>Record Project task time</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            className="settings-grid"
            onSubmit={addTaskTime}
          >
            <label>
              Delivery task
              <select
                name="task_id"
                required
                value={projectTimeTaskID}
                onChange={(event) => setProjectTimeTaskID(event.target.value)}
              >
                <option value="">Select task</option>
                {workspace.project_tasks.map((task) => (
                  <option key={task.id} value={task.id}>
                    Project: {task.title}
                  </option>
                ))}
                {workspace.phases.flatMap((phase) =>
                  phase.tasks.map((task) => (
                    <option key={task.id} value={task.id}>
                      {phase.name}: {task.title}
                    </option>
                  )),
                )}
              </select>
            </label>
            <label>
              Technician ID
              <input name="technician_id" required />
            </label>
            <label>
              Actual minutes
              <input name="minutes" type="number" min="1" required />
            </label>
            <label>
              <input name="billable" type="checkbox" /> Billable
            </label>
            <label>
              Time note
              <input name="note" />
            </label>
            <RequiredClassificationPicker
              clientID={clientID}
              selectedIDs={timeTagIDs}
              inputRef={timeClassificationRef}
              error={timeClassificationError}
              onChange={(ids) => {
                setTimeTagIDs(ids);
                setTimeClassificationError("");
              }}
            />
            <button type="submit" disabled={!timeTagIDs.length}>
              Record task time
            </button>
          </DirtyForm>
        </details>
      ) : null}
      {workspace && capabilities.has("project.edit") ? (
        <details className="change-order-workspace">
          <summary>Record Project cost</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            className="settings-grid"
            onSubmit={addCostActual}
          >
            <label>
              Cost Phase
              <select name="phase_id">
                <option value="">Project-wide</option>
                {workspace.phases.map((phase) => (
                  <option key={phase.id} value={phase.id}>
                    {phase.position}. {phase.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Cost type
              <input name="cost_type" required />
            </label>
            <label>
              Cost description
              <input name="description" required />
            </label>
            <label>
              Cost amount
              <input name="amount" type="number" min="0" step="0.01" required />
            </label>
            <label>
              Cost currency
              <input
                name="currency"
                defaultValue={
                  workspace.current_baseline.currency ||
                  workspace.original_baseline.currency ||
                  "USD"
                }
                minLength={3}
                maxLength={3}
                required
              />
            </label>
            <label>
              Incurred or committed on
              <input name="incurred_at" type="date" required />
            </label>
            <label>
              <input name="committed" type="checkbox" /> Committed, not yet
              incurred
            </label>
            <button type="submit">Record Project cost</button>
          </DirtyForm>
        </details>
      ) : null}
      {workspace && capabilities.has("project.edit") ? (
        <details className="change-order-workspace">
          <summary>Recognize billable work</summary>
          <DirtyForm
            ownerID={workspaceOwnerID}
            className="settings-grid"
            onSubmit={addBillableWork}
          >
            <label>
              Recognition Phase
              <select name="phase_id">
                <option value="">Project-wide</option>
                {workspace.phases.map((phase) => (
                  <option key={phase.id} value={phase.id}>
                    {phase.position}. {phase.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Recognition description
              <input name="description" required />
            </label>
            <label>
              Recognized amount
              <input name="amount" type="number" min="0" step="0.01" required />
            </label>
            <label>
              Recognition currency
              <input
                name="currency"
                defaultValue={
                  workspace.current_baseline.currency ||
                  workspace.original_baseline.currency ||
                  "USD"
                }
                minLength={3}
                maxLength={3}
                required
              />
            </label>
            <label>
              Recognized on
              <input name="recognized_at" type="date" required />
            </label>
            <button type="submit">Recognize billable work</button>
          </DirtyForm>
        </details>
      ) : null}
      {model?.changeOrders.map((order) => (
        <div className="change-order-workspace" key={order.id}>
          {order.currentVersion ? (
            <ChangeOrderEditor
              changeOrder={order}
              workspaceOwnerID={selectedRecord?.id ?? "route:project"}
              onApprove={() => void approve(order)}
              onOverride={(evidence) => override(order, evidence)}
            />
          ) : (
            <p>
              <strong>{order.displayID}</strong> · {order.state}
            </p>
          )}
          {capabilities.has("change_order.update") &&
          (order.state === "draft" || order.state === "rejected") ? (
            <details open={!order.currentVersion}>
              <summary>Issue Change Order version</summary>
              <DirtyForm
                ownerID={workspaceOwnerID}
                className="settings-grid"
                onSubmit={(event) => void issueChangeOrder(event, order)}
              >
                <label>
                  Description
                  <textarea name="description" required />
                </label>
                <label>
                  Currency
                  <input
                    name="currency"
                    defaultValue={
                      order.currentVersion?.currency ||
                      workspace?.current_baseline.currency ||
                      "USD"
                    }
                    minLength={3}
                    maxLength={3}
                    required
                  />
                </label>
                <label>
                  Revenue delta
                  <input name="revenue_delta_minor" type="number" required />
                </label>
                <label>
                  Cost delta
                  <input name="cost_delta_minor" type="number" required />
                </label>
                <label>
                  Labor delta (minutes)
                  <input name="labor_delta_minutes" type="number" required />
                </label>
                <button type="submit">Issue version</button>
              </DirtyForm>
            </details>
          ) : null}
          {order.state === "approved" ? (
            <button
              className="apply-change-order"
              type="button"
              onClick={() => void apply(order)}
            >
              Apply approved Change Order
            </button>
          ) : null}
        </div>
      ))}
    </>
  );
}
