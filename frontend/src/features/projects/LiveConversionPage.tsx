import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import { Page, StatePanel } from "../../design-system";
import {
  getProposalVersion,
  listOpportunities,
  listOpportunityTasks,
  listProposals,
  type OpportunityResponse,
  type OpportunityTaskResponse,
  type ProposalResponse,
  type ProposalVersionResponse,
} from "../sales/api";
import { convertOpportunity, previewOpportunityConversion } from "./api";
import { ConversionPreview } from "./ConversionPreview";
import type { ConversionPreviewModel } from "./types";
import "./projects.css";

type ConversionRequest = {
  expected_version: number;
  accepted_proposal_version_id: string;
  existing_client_id: string;
  project_display_id: string;
  project_name: string;
  planned_start?: string;
  planned_end?: string;
  phases: Array<{
    name: string;
    proposal_line_ids: string[];
    planned_start?: string;
    planned_end?: string;
  }>;
  selected_task_ids: string[];
  task_versions: Record<string, number>;
};

function instant(value: FormDataEntryValue | null): string | undefined {
  const date = String(value ?? "").trim();
  return date ? `${date}T00:00:00Z` : undefined;
}

export function LiveConversionPage({
  clientID,
  onConverted,
}: {
  clientID: string;
  onConverted: (projectID: string) => void;
}) {
  const [proposals, setProposals] = useState<ProposalResponse[]>([]);
  const [opportunities, setOpportunities] = useState<OpportunityResponse[]>([]);
  const [selectedProposalID, setSelectedProposalID] = useState("");
  const [version, setVersion] = useState<ProposalVersionResponse>();
  const [tasks, setTasks] = useState<OpportunityTaskResponse[]>([]);
  const [selectedTaskIDs, setSelectedTaskIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const [taskState, setTaskState] = useState<
    "idle" | "loading" | "ready" | "error"
  >("idle");
  const [previewing, setPreviewing] = useState(false);
  const [preview, setPreview] = useState<ConversionPreviewModel>();
  const [request, setRequest] = useState<ConversionRequest>();
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const previewController = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    const controller = new AbortController();
    setProposals([]);
    setOpportunities([]);
    setSelectedProposalID("");
    setVersion(undefined);
    setPreview(undefined);
    setRequest(undefined);
    setState("loading");
    void Promise.all([
      listProposals(clientID, controller.signal),
      listOpportunities(clientID, controller.signal),
    ])
      .then(([foundProposals, foundOpportunities]) => {
        const accepted = foundProposals.filter(
          (proposal) =>
            proposal.state === "accepted" && proposal.current_version_id,
        );
        setProposals(accepted);
        setOpportunities(foundOpportunities);
        setSelectedProposalID(accepted[0]?.id || "");
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [clientID]);

  const selectedProposal = proposals.find(
    (proposal) => proposal.id === selectedProposalID,
  );
  const opportunity = useMemo(
    () =>
      opportunities.find(
        (candidate) => candidate.id === selectedProposal?.opportunity_id,
      ),
    [opportunities, selectedProposal],
  );

  useEffect(() => {
    if (!selectedProposal?.current_version_id) {
      setVersion(undefined);
      return;
    }
    const controller = new AbortController();
    let active = true;
    setVersion(undefined);
    void getProposalVersion(
      clientID,
      selectedProposal.current_version_id,
      controller.signal,
    )
      .then((foundVersion) => {
        if (active) setVersion(foundVersion);
      })
      .catch(() => {
        if (active && !controller.signal.aborted) setState("error");
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [clientID, selectedProposal]);

  useEffect(() => {
    previewController.current?.abort();
    setPreviewing(false);
    setPreview(undefined);
    setRequest(undefined);
    setTasks([]);
    setSelectedTaskIDs(new Set());
    if (!opportunity) {
      setTaskState("idle");
      return;
    }

    const controller = new AbortController();
    let active = true;
    setTaskState("loading");
    void listOpportunityTasks(clientID, opportunity.id, controller.signal)
      .then((foundTasks) => {
        if (!active) return;
        setTasks(foundTasks);
        setTaskState("ready");
      })
      .catch(() => {
        if (!active || controller.signal.aborted) return;
        setTaskState("error");
        setState("error");
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [clientID, opportunity, selectedProposalID]);

  useEffect(
    () => () => {
      previewController.current?.abort();
    },
    [],
  );

  function toggleTask(task: OpportunityTaskResponse, checked: boolean) {
    if (task.status === "completed") return;
    setSelectedTaskIDs((current) => {
      const next = new Set(current);
      if (checked) next.add(task.id);
      else next.delete(task.id);
      return next;
    });
  }

  async function createPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedProposal || !opportunity || !version || taskState !== "ready")
      return;
    const data = new FormData(event.currentTarget);
    const plannedStart = instant(data.get("plannedStart"));
    const plannedEnd = instant(data.get("plannedEnd"));
    const selectedTasks = tasks.filter(
      (task) => task.status !== "completed" && selectedTaskIDs.has(task.id),
    );
    const nextRequest: ConversionRequest = {
      expected_version: opportunity.version,
      accepted_proposal_version_id: version.id,
      existing_client_id: clientID,
      project_display_id: String(data.get("displayID") ?? ""),
      project_name: String(data.get("name") ?? ""),
      planned_start: plannedStart,
      planned_end: plannedEnd,
      phases: [
        {
          name: String(data.get("phaseName") ?? ""),
          proposal_line_ids: version.lines.map((line) => line.id),
          planned_start: plannedStart,
          planned_end: plannedEnd,
        },
      ],
      selected_task_ids: selectedTasks.map((task) => task.id),
      task_versions: Object.fromEntries(
        selectedTasks.map((task) => [task.id, task.version]),
      ),
    };
    previewController.current?.abort();
    const controller = new AbortController();
    previewController.current = controller;
    setPreviewing(true);
    try {
      const nextPreview = await previewOpportunityConversion(
        clientID,
        opportunity.id,
        nextRequest,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      const taskByID = new Map(selectedTasks.map((task) => [task.id, task]));
      setPreview({
        ...nextPreview,
        tasks: nextPreview.tasks.map((task) => ({
          ...task,
          title: taskByID.get(task.id)?.title ?? task.title,
        })),
      });
      setRequest(nextRequest);
    } catch {
      if (!controller.signal.aborted) setState("error");
    } finally {
      if (previewController.current === controller) setPreviewing(false);
    }
  }

  async function convert(selection: {
    previewHash: string;
    selectedTaskIDs: string[];
    taskVersions: Record<string, number>;
  }) {
    if (!request || !opportunity || !preview) return;
    const exactIDs =
      selection.selectedTaskIDs.length === request.selected_task_ids.length &&
      selection.selectedTaskIDs.every(
        (taskID, index) => taskID === request.selected_task_ids[index],
      );
    const expectedVersionEntries = Object.entries(request.task_versions);
    const exactVersions =
      Object.keys(selection.taskVersions).length ===
        expectedVersionEntries.length &&
      expectedVersionEntries.every(
        ([taskID, version]) => selection.taskVersions[taskID] === version,
      );
    if (!exactIDs || !exactVersions || selection.previewHash !== preview.hash) {
      setState("error");
      return;
    }
    try {
      const result = await convertOpportunity(clientID, opportunity.id, {
        ...request,
        preview_hash: selection.previewHash,
        idempotency_key: crypto.randomUUID(),
      });
      onConverted(result.projectID);
    } catch {
      setState("error");
    }
  }

  if (preview) {
    return (
      <ConversionPreview
        preview={preview}
        onConvert={(selection) => void convert(selection)}
      />
    );
  }

  return (
    <Page
      eyebrow="Accepted proposal handoff"
      title="Create Project"
      description="Preview the immutable commercial baseline before conversion."
    >
      <div className="project-page conversion-launcher">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading accepted proposals"
            description="Retrieving conversion-ready commercial baselines."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Conversion could not be prepared or completed"
            description="Review the selected proposal and try again."
            supportCode="PROJECT-CONVERSION"
          />
        ) : null}
        {state === "ready" && !proposals.length ? (
          <StatePanel
            state="empty"
            title="No accepted proposals"
            description="No accepted proposals are ready for conversion."
          />
        ) : null}
        {proposals.length ? (
          <form
            key={selectedProposalID}
            className="conversion-launcher-form"
            onSubmit={(event) => void createPreview(event)}
          >
            <label htmlFor="conversion-proposal">
              <span>Accepted proposal</span>
              <select
                id="conversion-proposal"
                value={selectedProposalID}
                onChange={(event) => setSelectedProposalID(event.target.value)}
              >
                {proposals.map((proposal) => (
                  <option key={proposal.id} value={proposal.id}>
                    {proposal.display_id} · Version {proposal.current_version}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Project ID</span>
              <input
                name="displayID"
                defaultValue={`PRJ-${selectedProposal?.display_id || ""}`}
                required
              />
            </label>
            <label>
              <span>Project name</span>
              <input
                name="name"
                defaultValue={opportunity?.name || ""}
                required
              />
            </label>
            <label>
              <span>Initial phase</span>
              <input name="phaseName" defaultValue="Delivery" required />
            </label>
            <label>
              <span>Planned start</span>
              <input name="plannedStart" type="date" />
            </label>
            <label>
              <span>Planned end</span>
              <input name="plannedEnd" type="date" />
            </label>
            <fieldset className="conversion-tasks">
              <legend>Opportunity tasks</legend>
              {taskState === "loading" ? (
                <p>Loading opportunity tasks…</p>
              ) : null}
              {taskState === "ready" && !tasks.length ? (
                <p>No opportunity tasks are available.</p>
              ) : null}
              {tasks.map((task) => {
                const completed = task.status === "completed";
                return (
                  <label key={task.id}>
                    <input
                      type="checkbox"
                      aria-label={task.title}
                      disabled={
                        completed || taskState !== "ready" || previewing
                      }
                      checked={selectedTaskIDs.has(task.id)}
                      onChange={(event) =>
                        toggleTask(task, event.target.checked)
                      }
                    />
                    <span>
                      <strong>{task.title}</strong>
                      <small>
                        {completed
                          ? "Completed · retained"
                          : "Available to move"}
                      </small>
                    </span>
                  </label>
                );
              })}
            </fieldset>
            <p>
              {version
                ? `${version.lines.length} proposal lines will be mapped to the initial phase.`
                : "Loading the accepted commercial baseline…"}
            </p>
            <button
              className="project-primary"
              type="submit"
              disabled={
                !version || !opportunity || taskState !== "ready" || previewing
              }
            >
              {previewing ? "Preparing preview…" : "Preview conversion"}
            </button>
          </form>
        ) : null}
      </div>
    </Page>
  );
}
