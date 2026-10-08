import type {
  AIWorkspaceReadTool,
  AIWorkspaceWriteTool,
  ClientResourceKind,
} from "./types";
import type { WorkspaceAction } from "./workspaceCatalog";

export type ResourceFormState = {
  displayID: string;
  name: string;
  email: string;
  phone: string;
  location: string;
  assetType: string;
  sourceSystem: string;
  externalID: string;
  criticality: "" | "low" | "normal" | "high" | "critical";
  startsOn: string;
  endsOn: string;
};

export const emptyResourceForm = (): ResourceFormState => ({
  displayID: "",
  name: "",
  email: "",
  phone: "",
  location: "",
  assetType: "",
  sourceSystem: "",
  externalID: "",
  criticality: "",
  startsOn: "",
  endsOn: "",
});

export type StructuredActionValues = Partial<{
  targetClientID: string;
  clientDisplayID: string;
  clientName: string;
  ticketID: string;
  ticketType: "incident" | "request" | "change" | "problem";
  ticketStatus: string;
  ticketPriority: "low" | "normal" | "high" | "critical";
  serviceReference: string;
  contractReference: string;
  technicianReference: string;
  version: string;
  value: string;
  reason: string;
  projectTitle: string;
  projectTasks: string;
  reference: string;
  query: string;
  limit: string;
  structuredName: string;
  structuredDisplayID: string;
  structuredEmail: string;
  structuredPhone: string;
  structuredBody: string;
  projectID: string;
  queueReference: string;
  pipelineID: string;
  stageID: string;
  opportunityStage: string;
  activityKind: "note" | "call" | "email" | "meeting";
  activitySummary: string;
  activityDetails: string;
  proposalState: "" | "draft" | "issued" | "accepted";
  proposalOpportunityID: string;
  resourceKind: ClientResourceKind;
  resourceForm: ResourceFormState;
  resourceReference: string;
  resourceClearFields: ReadonlySet<string>;
}>;

export type SerializedStructuredAction =
  | {
      mode: "read";
      tool: AIWorkspaceReadTool;
      input: Record<string, unknown>;
    }
  | {
      mode: "write";
      tool: AIWorkspaceWriteTool;
      input: Record<string, unknown>;
    };

export function serializeStructuredAction(
  action: WorkspaceAction,
  values: StructuredActionValues,
): SerializedStructuredAction | undefined {
  const client = values.clientDisplayID?.trim() ?? "";
  const clientName = values.clientName?.trim() ?? "";
  const operationalClient = clientName || client;
  if (secondWaveClientBoundAction(action) && !operationalClient) {
    return undefined;
  }
  const targetClientID = values.targetClientID?.trim() ?? "";
  const reference = values.reference?.trim() ?? "";
  const query = values.query?.trim() ?? "";
  const limit = Number(values.limit);
  const expectedVersion = Number(values.version);
  const ticketID = values.ticketID?.trim() ?? "";
  const reason = values.reason?.trim() ?? "";
  const name = values.structuredName?.trim() ?? "";
  const displayID = values.structuredDisplayID?.trim() ?? "";
  const body = values.structuredBody?.trim() ?? "";
  const kind = values.resourceKind ?? "location";

  switch (action) {
    case "ticket_get":
      return {
        mode: "read",
        tool: "ticket.get",
        input: { client_id: targetClientID, id: reference },
      };
    case "ticket_search":
      return {
        mode: "read",
        tool: "ticket.search",
        input: { client_id: targetClientID, query, limit },
      };
    case "project_search":
      return {
        mode: "read",
        tool: "project.search",
        input: { client, limit },
      };
    case "project_get":
      return {
        mode: "read",
        tool: "project.get",
        input: { client, project: reference },
      };
    case "client_resource_list":
      return {
        mode: "read",
        tool: "client_resource.list",
        input: { client, kind },
      };
    case "knowledge_search":
      return {
        mode: "read",
        tool: "knowledge.search",
        input: { client, query, limit },
      };
    case "knowledge_get":
      return {
        mode: "read",
        tool: "knowledge.get",
        input: { client, article: reference },
      };
    case "prospect_list":
      return {
        mode: "read",
        tool: "prospect.list",
        input: { limit },
      };
    case "opportunity_list":
      return {
        mode: "read",
        tool: "opportunity.list",
        input: {
          client: operationalClient,
          ...nonEmpty("pipeline_id", values.pipelineID),
          ...nonEmpty("stage_id", values.stageID),
          limit,
        },
      };
    case "opportunity_get":
      return {
        mode: "read",
        tool: "opportunity.get",
        input: { client: operationalClient, opportunity: reference },
      };
    case "proposal_list":
      return {
        mode: "read",
        tool: "proposal.list",
        input: {
          client: operationalClient,
          ...nonEmpty("state", values.proposalState),
          ...nonEmpty("opportunity_id", values.proposalOpportunityID),
          limit,
        },
      };
    case "proposal_get":
      return {
        mode: "read",
        tool: "proposal.get",
        input: { client: operationalClient, proposal: reference },
      };
    case "client_resource_add": {
      const input = resourceCreateInput(
        kind,
        values.resourceForm ?? emptyResourceForm(),
        client,
      );
      return input
        ? { mode: "write", tool: `${kind}.create`, input }
        : undefined;
    }
    case "client_resource_update": {
      const patch = resourceUpdatePatch(
        kind,
        values.resourceForm ?? emptyResourceForm(),
        values.resourceClearFields ?? new Set(),
      );
      if (!patch) return undefined;
      return {
        mode: "write",
        tool: `${kind}.update`,
        input: {
          client,
          resource: values.resourceReference?.trim() ?? "",
          patch,
          reason,
        },
      };
    }
    case "client_resource_deactivate":
    case "client_resource_reactivate":
      return {
        mode: "write",
        tool: `${kind}.${
          action === "client_resource_deactivate" ? "deactivate" : "reactivate"
        }`,
        input: {
          client,
          resource: values.resourceReference?.trim() ?? "",
          reason,
        },
      };
    case "project":
      return {
        mode: "write",
        tool: "project.create",
        input: {
          client_id: targetClientID,
          name: values.projectTitle?.trim() ?? "",
          tasks: (values.projectTasks ?? "")
            .split("\n")
            .map((task) => task.trim())
            .filter(Boolean),
        },
      };
    case "task":
      return {
        mode: "write",
        tool: "task.create",
        input: {
          client_id: targetClientID,
          project_id: values.projectID?.trim() ?? "",
          project_ref: reference,
          title: name,
        },
      };
    case "client":
      return {
        mode: "write",
        tool: "client.create",
        input: { display_id: displayID, name },
      };
    case "ticket_route":
      return {
        mode: "write",
        tool: "ticket.route",
        input: {
          client,
          ticket: ticketID,
          queue: values.queueReference?.trim() ?? "",
          expected_version: expectedVersion,
          reason,
        },
      };
    case "ticket_create":
      return {
        mode: "write",
        tool: "ticket.create",
        input: {
          client: operationalClient,
          display_id: displayID,
          type: values.ticketType ?? "",
          title: name,
          description: body,
          status: values.ticketStatus?.trim() ?? "",
          priority: values.ticketPriority ?? "",
          ...nonEmpty("service", values.serviceReference),
          ...nonEmpty("contract", values.contractReference),
        },
      };
    case "ticket_assign":
      return {
        mode: "write",
        tool: "ticket.assign",
        input: {
          client: operationalClient,
          ticket: ticketID,
          technician: values.technicianReference?.trim() ?? "",
          expected_version: expectedVersion,
          reason,
        },
      };
    case "opportunity_transition":
      return {
        mode: "write",
        tool: "opportunity.transition",
        input: {
          client: operationalClient,
          opportunity: reference,
          stage: values.opportunityStage?.trim() ?? "",
          expected_version: expectedVersion,
          reason,
        },
      };
    case "opportunity_activity_create":
      return {
        mode: "write",
        tool: "opportunity.activity.create",
        input: {
          client: operationalClient,
          opportunity: reference,
          kind: values.activityKind ?? "",
          summary: values.activitySummary?.trim() ?? "",
          details: values.activityDetails?.trim() ?? "",
        },
      };
    case "proposal_create":
      return {
        mode: "write",
        tool: "proposal.create",
        input: {
          client: operationalClient,
          opportunity: reference,
          display_id: displayID,
        },
      };
    case "knowledge_publish":
      return {
        mode: "write",
        tool: "knowledge.publish",
        input: {
          client: operationalClient,
          article: reference,
          expected_version: expectedVersion,
          reason,
        },
      };
    case "knowledge_create":
      return {
        mode: "write",
        tool: "knowledge.draft.create",
        input: { client, display_id: displayID, title: name, body },
      };
    case "knowledge_revise":
      return {
        mode: "write",
        tool: "knowledge.draft.revise",
        input: {
          client,
          article: reference,
          expected_version: expectedVersion,
          title: name,
          body,
        },
      };
    case "prospect_create":
      return {
        mode: "write",
        tool: "prospect.create",
        input: {
          display_id: displayID,
          name,
          ...nonEmpty("email", values.structuredEmail),
          ...nonEmpty("phone", values.structuredPhone),
        },
      };
    case "transition":
      return ticketMutation(
        "ticket.transition",
        { status: values.value?.trim() ?? "", reason },
        targetClientID,
        ticketID,
        expectedVersion,
      );
    case "priority":
      return ticketMutation(
        "ticket.priority",
        { priority: values.value?.trim() ?? "", reason },
        targetClientID,
        ticketID,
        expectedVersion,
      );
    case "note":
      return ticketMutation(
        "ticket.note",
        { body: values.value?.trim() ?? "" },
        targetClientID,
        ticketID,
        expectedVersion,
      );
    case "email":
      return ticketMutation(
        "ticket.reply",
        { body: values.value?.trim() ?? "" },
        targetClientID,
        ticketID,
        expectedVersion,
      );
  }
}

function secondWaveClientBoundAction(action: WorkspaceAction): boolean {
  return (
    action === "ticket_create" ||
    action === "ticket_assign" ||
    action === "opportunity_list" ||
    action === "opportunity_get" ||
    action === "proposal_list" ||
    action === "proposal_get" ||
    action === "opportunity_transition" ||
    action === "opportunity_activity_create" ||
    action === "proposal_create" ||
    action === "knowledge_publish"
  );
}

function ticketMutation(
  tool: AIWorkspaceWriteTool,
  values: Record<string, unknown>,
  clientID: string,
  ticketID: string,
  expectedVersion: number,
): SerializedStructuredAction {
  return {
    mode: "write",
    tool,
    input: {
      client_id: clientID,
      id: ticketID,
      expected_version: expectedVersion,
      ...values,
    },
  };
}

function resourceCreateInput(
  kind: ClientResourceKind,
  form: ResourceFormState,
  client: string,
): Record<string, unknown> | undefined {
  const displayID = form.displayID.trim();
  const name = form.name.trim();
  if (!client || !displayID || !name) return undefined;
  const common = { client, display_id: displayID };
  switch (kind) {
    case "location":
      return { ...common, name };
    case "contact":
      return {
        ...common,
        display_name: name,
        ...nonEmpty("email", form.email),
        ...nonEmpty("phone", form.phone),
        ...nonEmpty("location", form.location),
      };
    case "asset": {
      const assetType = form.assetType.trim();
      const sourceSystem = form.sourceSystem.trim();
      const externalID = form.externalID.trim();
      if (!assetType || Boolean(sourceSystem) !== Boolean(externalID)) {
        return undefined;
      }
      return {
        ...common,
        name,
        asset_type: assetType,
        ...nonEmpty("location", form.location),
        ...nonEmpty("source_system", sourceSystem),
        ...nonEmpty("external_id", externalID),
      };
    }
    case "service":
      return {
        ...common,
        name,
        ...nonEmpty("criticality", form.criticality),
      };
    case "contract":
      if (!form.startsOn || (form.endsOn && form.endsOn < form.startsOn)) {
        return undefined;
      }
      return {
        ...common,
        name,
        starts_on: form.startsOn,
        ...nonEmpty("ends_on", form.endsOn),
      };
  }
}

function resourceUpdatePatch(
  kind: ClientResourceKind,
  form: ResourceFormState,
  clearFields: ReadonlySet<string>,
): Record<string, unknown> | undefined {
  const patch: Record<string, unknown> = {};
  const set = (field: string, value: string, clearField?: string) => {
    const normalized = value.trim();
    if (clearField && clearFields.has(clearField)) patch[field] = "";
    else if (normalized) patch[field] = normalized;
  };
  if (kind === "location") set("name", form.name);
  if (kind === "contact") {
    set("display_name", form.name);
    set("email", form.email, "email");
    set("phone", form.phone, "phone");
    set("location", form.location, "location");
  }
  if (kind === "asset") {
    set("name", form.name);
    set("asset_type", form.assetType);
    set("location", form.location, "location");
  }
  if (kind === "service") {
    set("name", form.name);
    set("criticality", form.criticality);
  }
  if (kind === "contract") {
    set("name", form.name);
    set("starts_on", form.startsOn);
    if (clearFields.has("endsOn")) patch.clear_ends_on = true;
    else set("ends_on", form.endsOn);
  }
  return Object.keys(patch).length ? patch : undefined;
}

function nonEmpty(
  key: string,
  value: string | undefined,
): Record<string, string> {
  const normalized = value?.trim() ?? "";
  return normalized ? { [key]: normalized } : {};
}
