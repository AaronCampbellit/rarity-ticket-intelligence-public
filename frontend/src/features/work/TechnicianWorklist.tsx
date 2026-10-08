import { CustomDateEditor } from "../calendar/CustomDateEditor";
import {
  type FormEvent,
  useId,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Maximize2 } from "lucide-react";

import {
  Button,
  Dialog,
  DirtyForm,
  FilterBar,
  FormActions,
  Notice,
  Page,
  KanbanBoard,
  Select,
  StatePanel,
  Switch,
  TextInput,
  ViewSwitcher,
  type KanbanColumn,
  type WorkspaceItem,
  type WorkView,
  useOptionalWorkspace,
  readViewPreference,
  writeViewPreference,
} from "../../design-system";
import {
  changeWorkPriority,
  claimWorkRecord,
  createWorkComment,
  createWorkTimeEntry,
  createWorkTimeEntryFromCapture,
  currentPrincipalID,
  getWorkTask,
  getWorkRecord,
  listLaborRoles,
  listWorkRecords,
  listSavedWorkViews,
  saveWorkView,
  transitionWorkRecord,
  uploadWorkAttachment,
  type WorkRecord,
  type WorkTask,
  type LaborRole,
  type SavedWorkView,
  type TicketTimeCapture,
  type WorkViewQuery,
} from "./api";
import { TicketTimer } from "./TicketTimer";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import {
  createClassificationAPI,
  isClassificationCreateError,
} from "../classification/api";
import { InternalCollaborationPanel } from "../collaboration/InternalCollaborationPanel";
import type { MentionDeepLink } from "../mentions/types";
import "./work.css";

export { ticketWorkspaceItem } from "./workspaceItem";
import { ticketWorkspaceItem } from "./workspaceItem";

export function TechnicianWorklist({
  clientID,
  capabilities = new Set<string>(),
  selectedWorkRecordID,
  selectedTaskID,
  preferencePrincipalID = "anonymous",
  authenticatedMentionLink,
}: {
  clientID: string;
  capabilities?: ReadonlySet<string>;
  selectedWorkRecordID?: string;
  selectedTaskID?: string;
  preferencePrincipalID?: string;
  authenticatedMentionLink?: MentionDeepLink;
}) {
  const [records, setRecords] = useState<WorkRecord[]>([]);
  const [nextPage, setNextPage] = useState<{ updatedAt: string; id: string }>();
  const [pageState, setPageState] = useState<"idle" | "loading" | "error">(
    "idle",
  );
  const pageRequest = useRef<AbortController | undefined>(undefined);
  const requestGeneration = useRef(0);
  const [selected, setSelected] = useState<WorkRecord>();
  const [selectedTask, setSelectedTask] = useState<WorkTask>();
  const [principalID, setPrincipalID] = useState("");
  const [views, setViews] = useState<SavedWorkView[]>([]);
  const [filters, setFilters] = useState<WorkViewQuery>({ ownership: "all" });
  const [state, setState] = useState<"idle" | "loading" | "ready" | "error">(
    clientID ? "loading" : "idle",
  );
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [view, setView] = useState<WorkView>(() =>
    readViewPreference(window.localStorage, preferencePrincipalID, "work"),
  );
  const workspace = useOptionalWorkspace();

  useEffect(() => {
    const generation = ++requestGeneration.current;
    pageRequest.current?.abort();
    setNextPage(undefined);
    setPageState("idle");
    if (selectedTaskID) return;
    if (!clientID) {
      setState("idle");
      return;
    }
    const controller = new AbortController();
    setRecords([]);
    setSelected(undefined);
    setState("loading");
    void listWorkRecords(clientID, controller.signal, { filters })
      .then(async (found) => {
        if (
          controller.signal.aborted ||
          generation !== requestGeneration.current
        )
          return;
        const last = found.at(-1);
        const next =
          found.length === 100 && last
            ? { updatedAt: last.updatedAt, id: last.id }
            : undefined;
        let restored = selectedWorkRecordID
          ? found.find(({ id }) => id === selectedWorkRecordID)
          : undefined;
        if (selectedWorkRecordID && !restored) {
          restored = await getWorkRecord(
            clientID,
            selectedWorkRecordID,
            controller.signal,
          );
          found = [restored, ...found.filter(({ id }) => id !== restored?.id)];
        }
        if (
          controller.signal.aborted ||
          generation !== requestGeneration.current
        )
          return;
        setNextPage(next);
        setRecords(found);
        setSelected(selectedWorkRecordID ? restored : found[0]);
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => {
      controller.abort();
      pageRequest.current?.abort();
    };
  }, [clientID, loadAttempt, selectedTaskID, selectedWorkRecordID, filters]);

  async function loadMore() {
    if (!nextPage || pageState === "loading") return;
    const generation = requestGeneration.current;
    const controller = new AbortController();
    pageRequest.current?.abort();
    pageRequest.current = controller;
    setPageState("loading");
    try {
      const found = await listWorkRecords(clientID, controller.signal, {
        filters,
        before: nextPage,
      });
      if (controller.signal.aborted || generation !== requestGeneration.current)
        return;
      setRecords((current) => {
        const seen = new Set(current.map((record) => record.id));
        return [...current, ...found.filter((record) => !seen.has(record.id))];
      });
      const last = found.at(-1);
      setNextPage(
        found.length === 100 && last && last.id !== nextPage.id
          ? { updatedAt: last.updatedAt, id: last.id }
          : undefined,
      );
      setPageState("idle");
    } catch {
      if (
        !controller.signal.aborted &&
        generation === requestGeneration.current
      )
        setPageState("error");
    }
  }

  useEffect(() => {
    setSelectedTask(undefined);
    if (!clientID || !selectedTaskID) return;
    const controller = new AbortController();
    setState("loading");
    void getWorkTask(clientID, selectedTaskID, controller.signal)
      .then((task) => {
        if (controller.signal.aborted) return;
        setSelectedTask(task);
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [clientID, loadAttempt, selectedTaskID]);

  useEffect(() => {
    if (!clientID) return;
    const controller = new AbortController();
    void listSavedWorkViews(clientID, controller.signal)
      .then(setViews)
      .catch(() => {
        if (!controller.signal.aborted) setViews([]);
      });
    return () => controller.abort();
  }, [clientID]);

  useEffect(() => {
    if (!clientID) return;
    void currentPrincipalID()
      .then(setPrincipalID)
      .catch(() => setState("error"));
  }, [clientID]);

  function updateRecord(record: WorkRecord) {
    setSelected(record);
    setRecords((current) =>
      current.map((candidate) =>
        candidate.id === record.id ? record : candidate,
      ),
    );
  }

  function selectRecord(record: WorkRecord) {
    setSelected(record);
    workspace?.openPreview(ticketWorkspaceItem(record, clientID));
  }

  function openRecord(record: WorkRecord) {
    setSelected(record);
    workspace?.openRecord(ticketWorkspaceItem(record, clientID));
  }

  const visibleRecords = useMemo(() => {
    const text = filters.text?.trim().toLowerCase() ?? "";
    return records.filter((record) => {
      if (
        text &&
        !`${record.displayID} ${record.title} ${record.description}`
          .toLowerCase()
          .includes(text)
      ) {
        return false;
      }
      if (filters.status && record.status !== filters.status) return false;
      if (filters.priority && record.priority !== filters.priority)
        return false;
      if (filters.ownership === "assigned" && !record.primaryOwnerID)
        return false;
      if (filters.ownership === "unassigned" && record.primaryOwnerID)
        return false;
      return true;
    });
  }, [filters, records]);
  const assigned = useMemo(
    () => visibleRecords.filter((record) => record.primaryOwnerID),
    [visibleRecords],
  );
  const unassigned = useMemo(
    () => visibleRecords.filter((record) => !record.primaryOwnerID),
    [visibleRecords],
  );
  const kanbanColumns = useMemo<KanbanColumn<WorkRecord>[]>(() => {
    const column = (
      id: string,
      label: string,
      tone: KanbanColumn<WorkRecord>["tone"],
    ) => ({
      id,
      label,
      tone,
      items: [] as WorkRecord[],
    });
    const columns = [
      column("new", "New & unassigned", "neutral"),
      column("active", "In progress", "info"),
      column("waiting", "Waiting", "warning"),
      column("done", "Resolved", "success"),
    ];
    for (const record of visibleRecords) {
      const status = record.status.toLowerCase().replaceAll(" ", "_");
      const target = ["resolved", "closed", "complete", "completed"].includes(
        status,
      )
        ? columns[3]
        : ["waiting", "pending", "on_hold", "blocked"].includes(status)
          ? columns[2]
          : ["new", "open", "queued"].includes(status) && !record.primaryOwnerID
            ? columns[0]
            : columns[1];
      target.items.push(record);
    }
    return columns;
  }, [visibleRecords]);

  if (
    selectedTaskID &&
    selectedTask?.id === selectedTaskID &&
    state === "ready"
  ) {
    return (
      <Page
        eyebrow="Task"
        title={selectedTask.title}
        description={`${selectedTask.parent.type.replaceAll("_", " ")} · ${selectedTask.parent.id}`}
      >
        <section className="work-record-workspace">
          <dl>
            <div>
              <dt>Status</dt>
              <dd>{selectedTask.status}</dd>
            </div>
            <div>
              <dt>Owner</dt>
              <dd>{selectedTask.ownerID || "Unassigned"}</dd>
            </div>
            <div>
              <dt>Planned</dt>
              <dd>{selectedTask.estimateMinutes} minutes</dd>
            </div>
          </dl>
          <DeferredObjectTagEditor
            api={createClassificationAPI(globalThis.fetch, clientID)}
            clientID={clientID}
            target={{ objectType: "task", objectId: selectedTask.id }}
          />
          <CustomDateEditor
            objectType="task"
            objectID={selectedTask.id}
            clientID={clientID}
          />
          <InternalCollaborationPanel
            clientId={clientID}
            parentType="task"
            parentId={selectedTask.id}
            authorId={principalID || preferencePrincipalID}
            capabilities={capabilities}
            canEditParent={capabilities.has(
              selectedTask.parent.type === "work_record"
                ? "work_record.edit"
                : "project.edit",
            )}
            authenticatedMentionLink={authenticatedMentionLink}
          />
        </section>
      </Page>
    );
  }

  if (selectedTaskID) {
    return (
      <Page eyebrow="Task" title="Task">
        <StatePanel
          state={state === "error" ? "error" : "loading"}
          title={
            state === "error" ? "Task could not be loaded" : "Loading task"
          }
          description={
            state === "error"
              ? "Retry the task request and verify Client access."
              : "Retrieving the selected task and its internal collaboration context."
          }
          action={
            state === "error" ? (
              <Button
                intent="primary"
                onClick={() => setLoadAttempt((current) => current + 1)}
              >
                Retry
              </Button>
            ) : undefined
          }
          supportCode={state === "error" ? "WORK-TASK-UNAVAILABLE" : undefined}
        />
      </Page>
    );
  }

  return (
    <Page
      eyebrow="Service operations"
      title="Technician work"
      description="Assigned work and unassigned queue items for the selected Client."
    >
      <div className="technician-worklist">
        {!selectedWorkRecordID ? (
          <WorkViewControls
            clientID={clientID}
            filters={filters}
            views={views}
            records={records}
            onChange={setFilters}
            onSaved={(view) => setViews((current) => [...current, view])}
          />
        ) : null}
        {!selectedWorkRecordID ? (
          <div className="technician-worklist__view">
            <div>
              <strong>{visibleRecords.length} records</strong>
              <span>{nextPage ? "More records available" : "Live queue"}</span>
            </div>
            <ViewSwitcher
              value={view}
              available={["list", "kanban"]}
              onChange={(next) => {
                if (next !== "list" && next !== "kanban") return;
                setView(next);
                writeViewPreference(
                  window.localStorage,
                  preferencePrincipalID,
                  "work",
                  next,
                );
              }}
            />
          </div>
        ) : null}
        {state === "idle" ? (
          <StatePanel
            state="empty"
            title="Select a Client"
            description="Select an available Client to load live work. If no Clients are available, ask an administrator to review your access."
          />
        ) : null}
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading work"
            description="Retrieving assigned work and unassigned queues."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Technician work could not be loaded"
            description="Try the request again. If it continues to fail, verify Client access."
            action={
              <Button
                intent="primary"
                onClick={() => setLoadAttempt((current) => current + 1)}
              >
                Retry
              </Button>
            }
            supportCode="WORK-LIST-UNAVAILABLE"
          />
        ) : null}
        {!selectedWorkRecordID && nextPage && state === "ready" ? (
          <div>
            {pageState === "error" ? (
              <Notice tone="danger" title="More work unavailable">
                More work could not be loaded. Your current results are
                retained.
              </Notice>
            ) : null}
            <Button
              disabled={pageState === "loading"}
              onClick={() => void loadMore()}
            >
              {pageState === "loading"
                ? "Loading more work…"
                : pageState === "error"
                  ? "Retry loading more work"
                  : "Load more work"}
            </Button>
          </div>
        ) : null}
        {selectedWorkRecordID && selected ? (
          <div className="work-record-workspace">
            <WorkDetail
              clientID={clientID}
              capabilities={capabilities}
              principalID={principalID}
              record={selected}
              workspaceOwnerID={`ticket:${selected.id}`}
              onUpdate={updateRecord}
              authenticatedMentionLink={authenticatedMentionLink}
            />
          </div>
        ) : !selectedWorkRecordID ? (
          <div className="technician-work-grid" data-view={view}>
            {view === "kanban" ? (
              <KanbanBoard
                columns={kanbanColumns}
                getID={(record) => record.id}
                getLabel={(record) => `${record.displayID} ${record.title}`}
                onOpen={selectRecord}
                onOpenFull={openRecord}
                renderCard={(record) => (
                  <span className="work-kanban-card">
                    <span>
                      <b>{record.displayID}</b>
                      <em data-priority={record.priority}>{record.priority}</em>
                    </span>
                    <strong>{record.title}</strong>
                    <small>
                      {record.type} ·{" "}
                      {record.primaryOwnerID ? "Assigned" : "Unassigned"}
                    </small>
                    <ObjectTagSummary
                      clientID={clientID}
                      target={{
                        objectType: "work_record",
                        objectId: record.id,
                      }}
                    />
                  </span>
                )}
              />
            ) : (
              <div className="work-groups">
                <WorkGroup
                  clientID={clientID}
                  title="Assigned work"
                  records={assigned}
                  selectedID={selected?.id}
                  onSelect={selectRecord}
                  onOpenFull={openRecord}
                />
                <WorkGroup
                  clientID={clientID}
                  title="Unassigned queues"
                  records={unassigned}
                  selectedID={selected?.id}
                  onSelect={selectRecord}
                  onOpenFull={openRecord}
                />
              </div>
            )}
            {selected ? (
              <WorkDetail
                clientID={clientID}
                capabilities={capabilities}
                principalID={principalID}
                record={selected}
                workspaceOwnerID="route:work"
                onUpdate={updateRecord}
                authenticatedMentionLink={authenticatedMentionLink}
              />
            ) : null}
          </div>
        ) : null}
      </div>
    </Page>
  );
}

function WorkViewControls({
  clientID,
  filters,
  views,
  records,
  onChange,
  onSaved,
}: {
  clientID: string;
  filters: WorkViewQuery;
  views: SavedWorkView[];
  records: WorkRecord[];
  onChange: (query: WorkViewQuery) => void;
  onSaved: (view: SavedWorkView) => void;
}) {
  const filterID = useId();
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveError, setSaveError] = useState(false);
  const [shared, setShared] = useState(false);
  const statuses = [
    ...new Set(
      [filters.status, ...records.map((record) => record.status)].filter(
        Boolean,
      ),
    ),
  ].sort();
  const priorities = [
    ...new Set(
      [filters.priority, ...records.map((record) => record.priority)].filter(
        Boolean,
      ),
    ),
  ].sort();

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setSaveError(false);
    try {
      const saved = await saveWorkView(
        clientID,
        String(data.get("name") ?? ""),
        filters,
        data.get("shared") === "on",
      );
      onSaved(saved);
      form.reset();
      setShared(false);
      setSaveOpen(false);
    } catch {
      setSaveError(true);
    }
  }

  return (
    <>
      <FilterBar
        activeCount={
          [
            filters.text,
            filters.status,
            filters.priority,
            filters.ownership !== "all" ? filters.ownership : "",
          ].filter(Boolean).length
        }
        onReset={() => onChange({ ownership: "all" })}
      >
        <label>
          <span>Saved view</span>
          <Select
            defaultValue=""
            onChange={(event) => {
              const view = views.find(
                (candidate) => candidate.id === event.target.value,
              );
              if (view) onChange(view.query);
            }}
          >
            <option value="">Current filters</option>
            {views.map((view) => (
              <option key={view.id} value={view.id}>
                {view.name}
                {view.audience.type === "msp" ? " · shared" : ""}
              </option>
            ))}
          </Select>
        </label>
        <label>
          <span>Search</span>
          <TextInput
            value={filters.text ?? ""}
            onChange={(event) =>
              onChange({ ...filters, text: event.target.value })
            }
          />
        </label>
        <label>
          <span>Status</span>
          <TextInput
            list={`${filterID}-status`}
            placeholder="All"
            value={filters.status ?? ""}
            onChange={(event) =>
              onChange({ ...filters, status: event.target.value })
            }
          />
          <datalist id={`${filterID}-status`}>
            {statuses.map((status) => (
              <option key={status} value={status} />
            ))}
          </datalist>
        </label>
        <label>
          <span>Priority</span>
          <TextInput
            list={`${filterID}-priority`}
            placeholder="All"
            value={filters.priority ?? ""}
            onChange={(event) =>
              onChange({ ...filters, priority: event.target.value })
            }
          />
          <datalist id={`${filterID}-priority`}>
            {priorities.map((priority) => (
              <option key={priority} value={priority} />
            ))}
          </datalist>
        </label>
        <label>
          <span>Ownership</span>
          <Select
            value={filters.ownership ?? "all"}
            onChange={(event) =>
              onChange({
                ...filters,
                ownership: event.target.value as WorkViewQuery["ownership"],
              })
            }
          >
            <option value="all">All</option>
            <option value="assigned">Assigned</option>
            <option value="unassigned">Unassigned</option>
          </Select>
        </label>
        <Button disabled={!clientID} onClick={() => setSaveOpen(true)}>
          Save view
        </Button>
      </FilterBar>
      <Dialog
        open={saveOpen}
        title="Save work view"
        description="Keep these filters for reuse or share them across the MSP."
        onClose={() => setSaveOpen(false)}
        actions={
          <FormActions>
            <Button intent="tertiary" onClick={() => setSaveOpen(false)}>
              Cancel
            </Button>
            <Button form="save-work-view-form" type="submit" intent="primary">
              Save view
            </Button>
          </FormActions>
        }
      >
        <form
          id="save-work-view-form"
          className="work-save-view"
          onSubmit={(event) => void save(event)}
        >
          <label>
            <span>View name</span>
            <TextInput name="name" required />
          </label>
          <Switch
            name="shared"
            label="Share across the MSP"
            checked={shared}
            onChange={(event) => setShared(event.target.checked)}
          />
          {saveError ? (
            <Notice tone="danger" title="View could not be saved" urgent>
              Verify your permission to save or share this view.
            </Notice>
          ) : null}
        </form>
      </Dialog>
    </>
  );
}

function WorkDetail({
  clientID,
  capabilities,
  principalID,
  record,
  workspaceOwnerID,
  onUpdate,
  authenticatedMentionLink,
}: {
  clientID: string;
  capabilities: ReadonlySet<string>;
  principalID: string;
  record: WorkRecord;
  workspaceOwnerID: string;
  onUpdate: (record: WorkRecord) => void;
  authenticatedMentionLink?: MentionDeepLink;
}) {
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [timeCapture, setTimeCapture] = useState<TicketTimeCapture>();
  const [laborRoles, setLaborRoles] = useState<LaborRole[]>([]);
  const [captureLaborRoleID, setCaptureLaborRoleID] = useState("");
  const [captureBillable, setCaptureBillable] = useState(false);
  const [includeCapture, setIncludeCapture] = useState(false);
  const [timerRefresh, setTimerRefresh] = useState(0);
  const [capturedTimeTagIDs, setCapturedTimeTagIDs] = useState<string[]>([]);
  const [manualTimeTagIDs, setManualTimeTagIDs] = useState<string[]>([]);
  const [capturedTimeClassificationError, setCapturedTimeClassificationError] =
    useState("");
  const [manualTimeClassificationError, setManualTimeClassificationError] =
    useState("");
  const [terminalRecoveryRequest, setTerminalRecoveryRequest] = useState(0);
  const capturedTimeClassificationRef = useRef<HTMLInputElement>(null);
  const manualTimeClassificationRef = useRef<HTMLInputElement>(null);
  const workTargetRef = useRef("");
  workTargetRef.current = `${clientID}:${record.id}`;

  useEffect(() => {
    setCapturedTimeTagIDs([]);
    setManualTimeTagIDs([]);
    setCapturedTimeClassificationError("");
    setManualTimeClassificationError("");
    setTerminalRecoveryRequest(0);
    setError("");
  }, [clientID, record.id]);

  useEffect(() => {
    if (!timeCapture) return;
    const controller = new AbortController();
    void listLaborRoles(clientID, controller.signal)
      .then(setLaborRoles)
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === "AbortError")) {
          setError("Labor roles could not be loaded.");
        }
      });
    return () => controller.abort();
  }, [clientID, timeCapture]);

  function clearCapture() {
    setTimeCapture(undefined);
    setCapturedTimeTagIDs([]);
    setCapturedTimeClassificationError("");
    setCaptureLaborRoleID("");
    setCaptureBillable(false);
    setIncludeCapture(false);
    setTimerRefresh((current) => current + 1);
  }

  async function action<T>(
    work: () => Promise<T>,
    success: string,
    apply?: (result: T) => void,
  ) {
    const actionTarget = workTargetRef.current;
    setError("");
    try {
      const result = await work();
      if (actionTarget !== workTargetRef.current) return false;
      apply?.(result);
      setMessage(success);
      return true;
    } catch (cause) {
      if (actionTarget !== workTargetRef.current) return false;
      setMessage("");
      if (isClassificationCreateError(cause)) {
        setError(
          "Classification is required before this terminal ticket action. Update the ticket classification and try again.",
        );
        setTerminalRecoveryRequest((request) => request + 1);
        return false;
      }
      setError(
        "The action could not be completed. Refresh and verify permissions or workflow requirements.",
      );
      return false;
    }
  }

  async function timeAction(
    source: "captured" | "manual",
    work: () => Promise<void>,
    success: string,
  ) {
    const actionTarget = workTargetRef.current;
    setError("");
    try {
      await work();
      if (actionTarget !== workTargetRef.current) return false;
      setMessage(success);
      return true;
    } catch (cause) {
      if (actionTarget !== workTargetRef.current) return false;
      setMessage("");
      if (isClassificationCreateError(cause)) {
        const message =
          "Classification changed. Confirm at least one current classification tag.";
        if (source === "captured") {
          setCapturedTimeClassificationError(message);
          capturedTimeClassificationRef.current?.focus();
        } else {
          setManualTimeClassificationError(message);
          manualTimeClassificationRef.current?.focus();
        }
        return false;
      }
      setError(
        "The action could not be completed. Refresh and verify permissions or workflow requirements.",
      );
      return false;
    }
  }

  async function submitTransition(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    if (
      await action(
        () =>
          transitionWorkRecord(
            clientID,
            record,
            String(data.get("status") ?? ""),
            String(data.get("reason") ?? ""),
          ),
        "Status updated.",
        onUpdate,
      )
    ) {
      form.reset();
    }
  }

  async function submitPriority(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    if (
      await action(
        () =>
          changeWorkPriority(
            clientID,
            record,
            String(data.get("priority") ?? ""),
            String(data.get("reason") ?? ""),
          ),
        "Priority updated.",
        onUpdate,
      )
    ) {
      form.reset();
    }
  }

  async function submitComment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const commentTarget = workTargetRef.current;
    if (includeCapture && !capturedTimeTagIDs.length) {
      setCapturedTimeClassificationError(
        "Select at least one meaningful classification tag.",
      );
      capturedTimeClassificationRef.current?.focus();
      return;
    }
    setError("");
    try {
      await createWorkComment(
        clientID,
        record.id,
        "client",
        String(data.get("body") ?? ""),
        includeCapture && timeCapture
          ? {
              capture: timeCapture,
              laborRoleID: captureLaborRoleID,
              billable: captureBillable,
              tagIDs: capturedTimeTagIDs,
            }
          : undefined,
      );
      if (commentTarget !== workTargetRef.current) return;
      form.reset();
      if (includeCapture) clearCapture();
      setMessage("Client reply recorded.");
    } catch (cause) {
      if (commentTarget !== workTargetRef.current) return;
      setMessage("");
      if (isClassificationCreateError(cause)) {
        setCapturedTimeClassificationError(
          "Classification changed. Confirm at least one current classification tag.",
        );
        capturedTimeClassificationRef.current?.focus();
      } else {
        setError(
          "The action could not be completed. Refresh and verify permissions or workflow requirements.",
        );
      }
    }
  }

  async function submitTime(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const recorded = await timeAction(
      "manual",
      async () => {
        await createWorkTimeEntry(
          clientID,
          record.id,
          principalID,
          Number(data.get("minutes")),
          data.get("billable") === "on",
          String(data.get("note") ?? ""),
          manualTimeTagIDs,
        );
      },
      "Time entry recorded.",
    );
    if (recorded) {
      form.reset();
      setManualTimeTagIDs([]);
    }
  }

  async function submitCapturedTime(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!timeCapture) return;
    const data = new FormData(event.currentTarget);
    const recorded = await timeAction(
      "captured",
      async () => {
        await createWorkTimeEntryFromCapture(
          clientID,
          record.id,
          timeCapture,
          captureLaborRoleID,
          captureBillable,
          String(data.get("note") ?? ""),
          capturedTimeTagIDs,
        );
      },
      "Captured time recorded.",
    );
    if (recorded) clearCapture();
    if (recorded) setCapturedTimeTagIDs([]);
  }

  return (
    <aside
      className="work-preview work-detail"
      aria-label="Selected work record"
    >
      <header>
        <div>
          <p className="record-id">{record.displayID}</p>
          <h2>{record.title}</h2>
        </div>
        {!record.primaryOwnerID ? (
          <button
            type="button"
            disabled={!principalID}
            onClick={() =>
              void action(
                () => claimWorkRecord(clientID, record, principalID),
                "Work claimed.",
                onUpdate,
              )
            }
          >
            Claim
          </button>
        ) : null}
      </header>
      <p>{record.description || "No description provided."}</p>
      <dl>
        <div>
          <dt>Type</dt>
          <dd>{record.type}</dd>
        </div>
        <div>
          <dt>Status</dt>
          <dd>{record.status}</dd>
        </div>
        <div>
          <dt>Priority</dt>
          <dd>{record.priority}</dd>
        </div>
        <div>
          <dt>Queue</dt>
          <dd>{record.queueID || "Unrouted"}</dd>
        </div>
      </dl>
      <DeferredObjectTagEditor
        api={createClassificationAPI(globalThis.fetch, clientID)}
        clientID={clientID}
        target={{ objectType: "work_record", objectId: record.id }}
        recoveryRequest={terminalRecoveryRequest}
      />
      <CustomDateEditor
        objectType="work_record"
        objectID={record.id}
        clientID={clientID}
      />
      <InternalCollaborationPanel
        clientId={clientID}
        parentType="work_record"
        parentId={record.id}
        authorId={principalID}
        capabilities={capabilities}
        canEditParent={capabilities.has("work_record.edit")}
        authenticatedMentionLink={authenticatedMentionLink}
      />
      <TicketTimer
        key={`${record.id}-${timerRefresh}`}
        clientID={clientID}
        workRecordID={record.id}
        onCapture={(capture) => {
          setTimeCapture(capture);
          setCapturedTimeTagIDs([]);
          setCapturedTimeClassificationError("");
          setCaptureLaborRoleID("");
          setCaptureBillable(false);
          setIncludeCapture(false);
        }}
      />
      {timeCapture ? (
        <DirtyForm
          ownerID={workspaceOwnerID}
          className="ticket-time-object"
          aria-label="Selected time capture"
          onSubmit={(event) => void submitCapturedTime(event)}
        >
          <strong>Time capture</strong>
          <span>
            {Math.max(1, Math.round(timeCapture.durationSeconds / 60))} minutes
            ready for this ticket
          </span>
          <label>
            <span>Labor role</span>
            <select
              name="labor_role_id"
              required
              value={captureLaborRoleID}
              onChange={(event) => setCaptureLaborRoleID(event.target.value)}
            >
              <option value="" disabled>
                Select a labor role
              </option>
              {laborRoles.map((role) => (
                <option key={role.id} value={role.id}>
                  {role.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Time note</span>
            <input name="note" />
          </label>
          <label className="work-check">
            <input
              name="billable"
              type="checkbox"
              checked={captureBillable}
              onChange={(event) => setCaptureBillable(event.target.checked)}
            />{" "}
            Billable
          </label>
          <RequiredClassificationPicker
            clientID={clientID}
            selectedIDs={capturedTimeTagIDs}
            inputRef={capturedTimeClassificationRef}
            error={capturedTimeClassificationError}
            onChange={(ids) => {
              setCapturedTimeTagIDs(ids);
              setCapturedTimeClassificationError("");
            }}
          />
          <div className="ticket-time-object__actions">
            <button
              type="submit"
              disabled={
                laborRoles.length === 0 ||
                !captureLaborRoleID ||
                !capturedTimeTagIDs.length
              }
            >
              Record captured time
            </button>
            <button type="button" onClick={clearCapture}>
              Remove time capture
            </button>
          </div>
        </DirtyForm>
      ) : null}
      {message ? (
        <p className="work-action-message" role="status">
          {message}
        </p>
      ) : null}
      {error ? (
        <p className="work-action-error" role="alert">
          {error}
        </p>
      ) : null}
      <section aria-labelledby="workflow-action-title">
        <h3 id="workflow-action-title">Workflow</h3>
        <DirtyForm
          ownerID={workspaceOwnerID}
          className="work-action-form"
          onSubmit={(event) => void submitTransition(event)}
        >
          <label>
            <span>Move to status</span>
            <input name="status" list="work-statuses" required />
          </label>
          <datalist id="work-statuses">
            <option value="in_progress" />
            <option value="pending" />
            <option value="resolved" />
            <option value="closed" />
          </datalist>
          <label>
            <span>Reason</span>
            <input name="reason" />
          </label>
          <button type="submit">Update status</button>
        </DirtyForm>
        <DirtyForm
          ownerID={workspaceOwnerID}
          className="work-action-form"
          onSubmit={(event) => void submitPriority(event)}
        >
          <label>
            <span>Priority</span>
            <select name="priority" defaultValue={record.priority}>
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="urgent">Urgent</option>
            </select>
          </label>
          <label>
            <span>Reason</span>
            <input name="reason" required />
          </label>
          <button type="submit">Change priority</button>
        </DirtyForm>
      </section>
      <section aria-labelledby="communication-title">
        <h3 id="communication-title">Communication</h3>
        <DirtyForm
          ownerID={workspaceOwnerID}
          className="work-action-form"
          onSubmit={(event) => void submitComment(event)}
        >
          <p>Public client reply</p>
          <label>
            <span>Message</span>
            <textarea name="body" required />
          </label>
          {timeCapture ? (
            <label className="work-check">
              <input
                name="include_time_capture"
                type="checkbox"
                checked={includeCapture}
                onChange={(event) => setIncludeCapture(event.target.checked)}
              />{" "}
              Include time capture
            </label>
          ) : null}
          <button
            type="submit"
            disabled={
              includeCapture &&
              (!captureLaborRoleID || !capturedTimeTagIDs.length)
            }
          >
            Record message
          </button>
        </DirtyForm>
      </section>
      <section aria-labelledby="labor-title">
        <h3 id="labor-title">Labor and files</h3>
        <DirtyForm
          ownerID={workspaceOwnerID}
          className="work-action-form"
          onSubmit={(event) => void submitTime(event)}
        >
          <label>
            <span>Minutes worked</span>
            <input name="minutes" type="number" min="1" required />
          </label>
          <label>
            <span>Work note</span>
            <input name="note" />
          </label>
          <label className="work-check">
            <input name="billable" type="checkbox" /> Manual entry billable
          </label>
          <RequiredClassificationPicker
            clientID={clientID}
            selectedIDs={manualTimeTagIDs}
            inputRef={manualTimeClassificationRef}
            error={manualTimeClassificationError}
            onChange={(ids) => {
              setManualTimeTagIDs(ids);
              setManualTimeClassificationError("");
            }}
          />
          <button
            type="submit"
            disabled={!principalID || !manualTimeTagIDs.length}
          >
            Record time
          </button>
        </DirtyForm>
        <label className="work-file">
          <span>Attach file</span>
          <input
            type="file"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) {
                void action(
                  () => uploadWorkAttachment(clientID, record.id, file),
                  "Attachment uploaded.",
                );
              }
            }}
          />
        </label>
      </section>
    </aside>
  );
}

function WorkGroup({
  clientID,
  title,
  records,
  selectedID,
  onSelect,
  onOpenFull,
}: {
  clientID: string;
  title: string;
  records: WorkRecord[];
  selectedID?: string;
  onSelect: (record: WorkRecord) => void;
  onOpenFull: (record: WorkRecord) => void;
}) {
  return (
    <section aria-label={title}>
      <h2>{title}</h2>
      {records.map((record) => (
        <div className="work-row-shell" key={record.id}>
          <button
            type="button"
            className={
              record.id === selectedID ? "work-row active" : "work-row"
            }
            onClick={() => onSelect(record)}
            onDoubleClick={() => onOpenFull(record)}
          >
            <span className="work-row__identity">
              <small>{record.displayID}</small>
              <strong>{record.title}</strong>
              <ObjectTagSummary
                clientID={clientID}
                target={{ objectType: "work_record", objectId: record.id }}
              />
            </span>
            <span className="work-row__status">
              <span>{record.status}</span>
              <small>{record.priority}</small>
            </span>
          </button>
          <button
            type="button"
            className="work-row-open"
            aria-label={`Open full record ${record.displayID} ${record.title}`}
            title="Open full record"
            onClick={() => onOpenFull(record)}
          >
            <Maximize2 size={15} aria-hidden="true" />
          </button>
        </div>
      ))}
    </section>
  );
}
