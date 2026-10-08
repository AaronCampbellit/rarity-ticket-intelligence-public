import { useEffect, useRef, useState, type FormEvent } from "react";

import type { DirectoryClient } from "../../api/browserSession";
import {
  emptyResourceForm,
  serializeStructuredAction,
  type ResourceFormState,
} from "./structuredAction";
import type {
  AIWorkspaceAPI,
  AIWorkspaceConversation,
  AIWorkspaceProposal,
  AIWorkspaceReadResult,
  ClientResourceKind,
  ClientResourceSummary,
} from "./types";
import { AIWorkspaceClarificationError } from "./workspaceApi";
import {
  availableWorkspaceCatalog,
  isClientResourceAction,
  resourceKinds,
  resourceKindsForAction,
  type CatalogAction,
  type WorkspaceAction,
} from "./workspaceCatalog";

type StructuredRequestSnapshot = {
  action: WorkspaceAction;
  targetClientID: string;
  kind: ClientResourceKind;
  requestID: number;
};

export type StructuredReadContext = {
  action: CatalogAction;
  client?: DirectoryClient;
};

export type ClientResourceResultContext = {
  client: DirectoryClient;
  kind: ClientResourceKind;
};

export type StructuredActionController = ReturnType<
  typeof useStructuredActionController
>;

export function useStructuredActionController({
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
}: {
  clients: DirectoryClient[];
  pageClientID: string;
  capabilities?: ReadonlySet<string>;
  api: AIWorkspaceAPI;
  ensureConversation: (
    signal?: AbortSignal,
  ) => Promise<AIWorkspaceConversation>;
  setBusy: (busy: boolean) => void;
  setError: (error: string) => void;
  setOutcome: (outcome: string) => void;
  setProposal: (proposal: AIWorkspaceProposal | undefined) => void;
  clearLocationCatalog: (clearSelection?: boolean) => void;
}) {
  const [action, setAction] = useState<WorkspaceAction>("transition");
  const [explicitTargetClientID, setExplicitTargetClientID] = useState("");
  const [ticketID, setTicketID] = useState("");
  const [ticketType, setTicketType] = useState<
    "" | "incident" | "request" | "change" | "problem"
  >("");
  const [ticketStatus, setTicketStatus] = useState("");
  const [ticketPriority, setTicketPriority] = useState<
    "" | "low" | "normal" | "high" | "critical"
  >("");
  const [serviceReference, setServiceReference] = useState("");
  const [contractReference, setContractReference] = useState("");
  const [technicianReference, setTechnicianReference] = useState("");
  const [version, setVersion] = useState("");
  const [value, setValue] = useState("");
  const [reason, setReason] = useState("");
  const [projectTitle, setProjectTitle] = useState("");
  const [projectTasks, setProjectTasks] = useState("");
  const [reference, setReference] = useState("");
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState("25");
  const [structuredName, setStructuredName] = useState("");
  const [structuredDisplayID, setStructuredDisplayID] = useState("");
  const [structuredEmail, setStructuredEmail] = useState("");
  const [structuredPhone, setStructuredPhone] = useState("");
  const [structuredBody, setStructuredBody] = useState("");
  const [projectID, setProjectID] = useState("");
  const [queueReference, setQueueReference] = useState("");
  const [pipelineID, setPipelineID] = useState("");
  const [stageID, setStageID] = useState("");
  const [opportunityStage, setOpportunityStage] = useState("");
  const [activityKind, setActivityKind] = useState<
    "" | "note" | "call" | "email" | "meeting"
  >("");
  const [activitySummary, setActivitySummary] = useState("");
  const [activityDetails, setActivityDetails] = useState("");
  const [proposalState, setProposalState] = useState<
    "" | "draft" | "issued" | "accepted"
  >("");
  const [proposalOpportunityID, setProposalOpportunityID] = useState("");
  const [resourceKind, setResourceKindState] =
    useState<ClientResourceKind>("location");
  const [resourceForm, setResourceForm] =
    useState<ResourceFormState>(emptyResourceForm);
  const [resourceReference, setResourceReference] = useState("");
  const [resourceClearFields, setResourceClearFields] = useState<Set<string>>(
    () => new Set(),
  );
  const [resourceResults, setResourceResults] = useState<
    ClientResourceSummary[]
  >([]);
  const [resourceResultSummary, setResourceResultSummary] = useState("");
  const [resourceResultContext, setResourceResultContext] =
    useState<ClientResourceResultContext>();
  const [structuredReadResult, setStructuredReadResult] =
    useState<AIWorkspaceReadResult>();
  const [structuredReadContext, setStructuredReadContext] =
    useState<StructuredReadContext>();

  const lookupRequest = useRef(0);
  const proposalRequest = useRef(0);
  const lookupAction = useRef(action);
  const lookupTargetClientID = useRef("");
  const lookupResourceKind = useRef(resourceKind);
  const targetClientID = clients.some(
    (client) => client.id === explicitTargetClientID,
  )
    ? explicitTargetClientID
    : initialTargetClientID(clients, pageClientID);
  const selectedTargetClient = clients.find(
    (client) => client.id === targetClientID,
  );
  const createResourceKinds = resourceKindsForAction(
    "client_resource_add",
    capabilities,
  );
  const availableResourceKinds = resourceKindsForAction(action, capabilities);
  const canReadResources = !capabilities || capabilities.has("search.read");
  const availableCatalog = availableWorkspaceCatalog(capabilities);
  const selectedCatalogAction =
    availableCatalog.find((item) => item.value === action) ??
    availableCatalog[0];
  const actionNeedsClient = selectedCatalogAction?.clientBound ?? true;
  const actionIsRead = selectedCatalogAction?.mode === "read";
  const assetExternalIdentityIncomplete =
    action === "client_resource_add" &&
    resourceKind === "asset" &&
    Boolean(resourceForm.sourceSystem.trim()) !==
      Boolean(resourceForm.externalID.trim());

  lookupAction.current = action;
  lookupTargetClientID.current = actionNeedsClient ? targetClientID : "";
  lookupResourceKind.current = resourceKind;

  function clearResourceResults() {
    lookupRequest.current += 1;
    setResourceResults([]);
    setResourceResultSummary("");
    setResourceResultContext(undefined);
    setStructuredReadResult(undefined);
    setStructuredReadContext(undefined);
  }

  function beginStructuredRequest(): StructuredRequestSnapshot {
    const requestID = ++lookupRequest.current;
    setResourceResults([]);
    setResourceResultSummary("");
    setResourceResultContext(undefined);
    setStructuredReadResult(undefined);
    setStructuredReadContext(undefined);
    return {
      action,
      targetClientID: actionNeedsClient ? targetClientID : "",
      kind: resourceKind,
      requestID,
    };
  }

  function structuredRequestIsCurrent(snapshot: StructuredRequestSnapshot) {
    return (
      lookupRequest.current === snapshot.requestID &&
      lookupAction.current === snapshot.action &&
      lookupTargetClientID.current === snapshot.targetClientID &&
      lookupResourceKind.current === snapshot.kind
    );
  }

  function beginProposalRequest(): StructuredRequestSnapshot {
    return {
      action,
      targetClientID: actionNeedsClient ? targetClientID : "",
      kind: resourceKind,
      requestID: ++proposalRequest.current,
    };
  }

  function proposalRequestIsCurrent(snapshot: StructuredRequestSnapshot) {
    return (
      proposalRequest.current === snapshot.requestID &&
      lookupAction.current === snapshot.action &&
      lookupTargetClientID.current === snapshot.targetClientID &&
      lookupResourceKind.current === snapshot.kind
    );
  }

  useEffect(() => {
    clearResourceResults();
  }, [action, resourceKind, targetClientID]);

  useEffect(() => {
    if (
      availableCatalog.length &&
      !availableCatalog.some((item) => item.value === action)
    ) {
      const nextAction = availableCatalog[0].value;
      lookupAction.current = nextAction;
      setAction(nextAction);
    }
  }, [action, availableCatalog]);

  function selectAction(nextAction: WorkspaceAction) {
    lookupAction.current = nextAction;
    proposalRequest.current += 1;
    setAction(nextAction);
    clearLocationCatalog(true);
    clearResourceResults();
    setResourceReference("");
    setResourceForm(emptyResourceForm());
    setResourceClearFields(new Set());
    setReason("");
    setReference("");
    setQuery("");
    setLimit(nextAction === "ticket_search" ? "20" : "25");
    setStructuredName("");
    setStructuredDisplayID("");
    setStructuredEmail("");
    setStructuredPhone("");
    setStructuredBody("");
    setProjectID("");
    setQueueReference("");
    setTicketID("");
    setTicketType("");
    setTicketStatus("");
    setTicketPriority("");
    setServiceReference("");
    setContractReference("");
    setTechnicianReference("");
    setVersion("");
    setValue("");
    setProjectTitle("");
    setProjectTasks("");
    setPipelineID("");
    setStageID("");
    setOpportunityStage("");
    setActivityKind("");
    setActivitySummary("");
    setActivityDetails("");
    setProposalState("");
    setProposalOpportunityID("");
    if (isClientResourceAction(nextAction)) {
      const nextKinds =
        nextAction === "client_resource_list"
          ? resourceKinds
          : resourceKindsForAction(nextAction, capabilities);
      if (!nextKinds.some(({ kind }) => kind === resourceKind)) {
        setResourceKindState(nextKinds[0]?.kind ?? "location");
      }
    }
  }

  function selectTargetClient(nextClientID: string) {
    proposalRequest.current += 1;
    clearLocationCatalog(true);
    clearResourceResults();
    setExplicitTargetClientID(nextClientID);
  }

  function selectResourceKind(nextKind: ClientResourceKind) {
    proposalRequest.current += 1;
    clearLocationCatalog(true);
    clearResourceResults();
    setResourceKindState(nextKind);
    setResourceForm(emptyResourceForm());
    setResourceReference("");
    setResourceClearFields(new Set());
  }

  async function reviewAction(event: FormEvent) {
    event.preventDefault();
    if (actionNeedsClient && !targetClientID) {
      setError("Choose an authorized client before preparing an action.");
      return;
    }
    const readSnapshot = actionIsRead ? beginStructuredRequest() : undefined;
    const writeSnapshot = actionIsRead ? undefined : beginProposalRequest();
    setBusy(true);
    setError("");
    setOutcome("");
    try {
      const conversation = await ensureConversation();
      if (
        (action === "client_resource_update" ||
          action === "client_resource_deactivate" ||
          action === "client_resource_reactivate") &&
        (!selectedTargetClient || !resourceReference.trim() || !reason.trim())
      ) {
        setError(
          "Choose an exact Client resource and enter a reason before review.",
        );
        return;
      }
      const serialized = serializeStructuredAction(action, {
        targetClientID,
        clientDisplayID: selectedTargetClient?.display_id ?? "",
        clientName: selectedTargetClient?.name ?? "",
        ticketID,
        ticketType: ticketType || undefined,
        ticketStatus,
        ticketPriority: ticketPriority || undefined,
        serviceReference,
        contractReference,
        technicianReference,
        version,
        value,
        reason,
        projectTitle,
        projectTasks,
        reference,
        query,
        limit,
        structuredName,
        structuredDisplayID,
        structuredEmail,
        structuredPhone,
        structuredBody,
        projectID,
        queueReference,
        pipelineID,
        stageID,
        opportunityStage,
        activityKind: activityKind || undefined,
        activitySummary,
        activityDetails,
        proposalState,
        proposalOpportunityID,
        resourceKind,
        resourceForm,
        resourceReference,
        resourceClearFields,
      });
      if (!serialized) {
        setError(
          action === "client_resource_update"
            ? "Change at least one approved business field or choose an explicit clear."
            : "Complete the required Client resource fields before review.",
        );
        return;
      }
      if (serialized.mode === "read" && readSnapshot && selectedCatalogAction) {
        if (!structuredRequestIsCurrent(readSnapshot)) return;
        const result = await api.runRead(
          conversation.id,
          serialized.tool,
          serialized.input,
          new AbortController().signal,
        );
        if (!structuredRequestIsCurrent(readSnapshot)) return;
        if (action === "client_resource_list" && selectedTargetClient) {
          setResourceResultSummary(result.summary);
          setResourceResults(resourceSummaries(result.data));
          setResourceResultContext({
            client: selectedTargetClient,
            kind: resourceKind,
          });
        } else {
          setStructuredReadResult(result);
          setStructuredReadContext({
            action: selectedCatalogAction,
            ...(actionNeedsClient && selectedTargetClient
              ? { client: selectedTargetClient }
              : {}),
          });
        }
        return;
      }
      if (!writeSnapshot || serialized.mode !== "write") return;
      const nextProposal = await api.propose(
        conversation.id,
        serialized.tool,
        serialized.input,
      );
      if (proposalRequestIsCurrent(writeSnapshot)) setProposal(nextProposal);
    } catch (error) {
      if (readSnapshot && !structuredRequestIsCurrent(readSnapshot)) return;
      if (writeSnapshot && !proposalRequestIsCurrent(writeSnapshot)) return;
      setError(
        error instanceof AIWorkspaceClarificationError
          ? error.message
          : actionIsRead
            ? "The read could not be completed. Check the supplied references."
            : "The action preview could not be prepared. Check the supplied values.",
      );
    } finally {
      setBusy(false);
    }
  }

  return {
    action,
    actionIsRead,
    actionNeedsClient,
    activityDetails,
    activityKind,
    activitySummary,
    assetExternalIdentityIncomplete,
    availableCatalog,
    availableResourceKinds,
    canReadResources,
    clearResourceResults,
    createResourceKinds,
    contractReference,
    explicitTargetClientID,
    limit,
    opportunityStage,
    pipelineID,
    projectID,
    projectTasks,
    projectTitle,
    proposalOpportunityID,
    proposalState,
    query,
    queueReference,
    reason,
    reference,
    resourceClearFields,
    resourceForm,
    resourceKind,
    resourceReference,
    resourceResultContext,
    resourceResults,
    resourceResultSummary,
    reviewAction,
    selectAction,
    selectedCatalogAction,
    selectedTargetClient,
    serviceReference,
    selectResourceKind,
    selectTargetClient,
    setLimit,
    setActivityDetails,
    setActivityKind,
    setActivitySummary,
    setContractReference,
    setOpportunityStage,
    setPipelineID,
    setProjectID,
    setProjectTasks,
    setProjectTitle,
    setProposalOpportunityID,
    setProposalState,
    setQuery,
    setQueueReference,
    setReason,
    setReference,
    setResourceClearFields,
    setResourceForm,
    setResourceReference,
    setServiceReference,
    setStageID,
    setStructuredBody,
    setStructuredDisplayID,
    setStructuredEmail,
    setStructuredName,
    setStructuredPhone,
    setTechnicianReference,
    setTicketID,
    setTicketPriority,
    setTicketStatus,
    setTicketType,
    setValue,
    setVersion,
    structuredBody,
    structuredDisplayID,
    structuredEmail,
    structuredName,
    structuredPhone,
    structuredReadContext,
    structuredReadResult,
    targetClientID,
    technicianReference,
    ticketID,
    ticketPriority,
    ticketStatus,
    ticketType,
    value,
    version,
    stageID,
  };
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
    return [
      {
        id: resource.id,
        kind: resource.kind,
        display_id: resource.display_id,
        name: resource.name,
        ...(typeof resource.detail === "string"
          ? { detail: resource.detail }
          : {}),
        version: resource.version,
      },
    ];
  });
}

function isClientResourceKind(value: unknown): value is ClientResourceKind {
  return resourceKinds.some(({ kind }) => kind === value);
}

function initialTargetClientID(
  clients: DirectoryClient[],
  pageClientID: string,
) {
  if (clients.some((client) => client.id === pageClientID)) return pageClientID;
  return clients[0]?.id ?? "";
}
