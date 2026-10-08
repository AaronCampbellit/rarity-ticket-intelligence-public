import type { ClientResourceKind } from "./types";

export type TicketAction = "transition" | "priority" | "note" | "email";
export type ResourceAction =
  | "client_resource_add"
  | "client_resource_list"
  | "client_resource_update"
  | "client_resource_deactivate"
  | "client_resource_reactivate";
export type ReadAction =
  | "ticket_get"
  | "ticket_search"
  | "project_search"
  | "project_get"
  | "knowledge_search"
  | "knowledge_get"
  | "prospect_list"
  | "opportunity_list"
  | "opportunity_get"
  | "proposal_list"
  | "proposal_get";
export type WorkspaceAction =
  | TicketAction
  | ReadAction
  | "ticket_create"
  | "ticket_assign"
  | "ticket_route"
  | "project"
  | "task"
  | "client"
  | "knowledge_create"
  | "knowledge_revise"
  | "knowledge_publish"
  | "prospect_create"
  | "opportunity_transition"
  | "opportunity_activity_create"
  | "proposal_create"
  | ResourceAction;
export type CatalogGroup =
  "Read" | "Work" | "Client resources" | "Knowledge" | "Sales";
export type CatalogAction = {
  value: WorkspaceAction;
  label: string;
  group: CatalogGroup;
  capability: string;
  clientBound: boolean;
  mode: "read" | "write";
};

export const catalogGroups: CatalogGroup[] = [
  "Read",
  "Work",
  "Client resources",
  "Knowledge",
  "Sales",
];

export const workspaceCatalog: CatalogAction[] = [
  {
    value: "ticket_get",
    label: "Get ticket",
    group: "Read",
    capability: "work_record.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "ticket_search",
    label: "Search tickets",
    group: "Read",
    capability: "search.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "project_search",
    label: "List projects",
    group: "Read",
    capability: "project.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "project_get",
    label: "Get project",
    group: "Read",
    capability: "project.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "ticket_create",
    label: "Create ticket",
    group: "Work",
    capability: "work_record.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "ticket_assign",
    label: "Assign ticket",
    group: "Work",
    capability: "work_record.assign",
    clientBound: true,
    mode: "write",
  },
  {
    value: "transition",
    label: "Change status",
    group: "Work",
    capability: "work_record.transition",
    clientBound: true,
    mode: "write",
  },
  {
    value: "priority",
    label: "Change priority",
    group: "Work",
    capability: "work_record.edit",
    clientBound: true,
    mode: "write",
  },
  {
    value: "note",
    label: "Add internal note",
    group: "Work",
    capability: "comment.internal.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "email",
    label: "Add client-visible reply",
    group: "Work",
    capability: "comment.public.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "ticket_route",
    label: "Route ticket",
    group: "Work",
    capability: "work_record.route",
    clientBound: true,
    mode: "write",
  },
  {
    value: "project",
    label: "Create project with tasks",
    group: "Work",
    capability: "project.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "task",
    label: "Add project task",
    group: "Work",
    capability: "task.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "client",
    label: "Create client",
    group: "Client resources",
    capability: "client.create",
    clientBound: false,
    mode: "write",
  },
  {
    value: "client_resource_add",
    label: "Add client resource",
    group: "Client resources",
    capability: "client_resource.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "client_resource_list",
    label: "Look up client resources",
    group: "Client resources",
    capability: "search.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "client_resource_update",
    label: "Update client resource",
    group: "Client resources",
    capability: "client_resource.update",
    clientBound: true,
    mode: "write",
  },
  {
    value: "client_resource_deactivate",
    label: "Deactivate client resource",
    group: "Client resources",
    capability: "client_resource.lifecycle",
    clientBound: true,
    mode: "write",
  },
  {
    value: "client_resource_reactivate",
    label: "Reactivate client resource",
    group: "Client resources",
    capability: "client_resource.lifecycle",
    clientBound: true,
    mode: "write",
  },
  {
    value: "knowledge_search",
    label: "Search knowledge",
    group: "Knowledge",
    capability: "knowledge.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "knowledge_get",
    label: "Get knowledge article",
    group: "Knowledge",
    capability: "knowledge.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "knowledge_create",
    label: "Create knowledge draft",
    group: "Knowledge",
    capability: "knowledge.edit",
    clientBound: true,
    mode: "write",
  },
  {
    value: "knowledge_revise",
    label: "Revise knowledge draft",
    group: "Knowledge",
    capability: "knowledge.edit",
    clientBound: true,
    mode: "write",
  },
  {
    value: "knowledge_publish",
    label: "Publish internal knowledge",
    group: "Knowledge",
    capability: "knowledge.publish",
    clientBound: true,
    mode: "write",
  },
  {
    value: "prospect_list",
    label: "List prospects",
    group: "Sales",
    capability: "opportunity.read",
    clientBound: false,
    mode: "read",
  },
  {
    value: "opportunity_list",
    label: "List opportunities",
    group: "Sales",
    capability: "opportunity.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "opportunity_get",
    label: "Get opportunity",
    group: "Sales",
    capability: "opportunity.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "proposal_list",
    label: "List proposals",
    group: "Sales",
    capability: "proposal.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "proposal_get",
    label: "Get proposal",
    group: "Sales",
    capability: "proposal.read",
    clientBound: true,
    mode: "read",
  },
  {
    value: "opportunity_transition",
    label: "Move opportunity stage",
    group: "Sales",
    capability: "opportunity.transition",
    clientBound: true,
    mode: "write",
  },
  {
    value: "opportunity_activity_create",
    label: "Add opportunity activity",
    group: "Sales",
    capability: "opportunity.activity.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "proposal_create",
    label: "Create proposal draft",
    group: "Sales",
    capability: "proposal.create",
    clientBound: true,
    mode: "write",
  },
  {
    value: "prospect_create",
    label: "Create prospect",
    group: "Sales",
    capability: "prospect.create",
    clientBound: false,
    mode: "write",
  },
];

export const resourceKinds: Array<{
  kind: ClientResourceKind;
  label: string;
  capability: string;
}> = [
  { kind: "location", label: "Location", capability: "location.create" },
  { kind: "contact", label: "Contact", capability: "contact.create" },
  { kind: "asset", label: "Asset", capability: "asset.create" },
  { kind: "service", label: "Service", capability: "service.create" },
  { kind: "contract", label: "Contract", capability: "contract.create" },
];

export function resourceKindsForAction(
  action: WorkspaceAction,
  capabilities?: ReadonlySet<string>,
) {
  if (action === "client_resource_update") {
    return resourceKinds.filter(
      ({ kind }) => !capabilities || capabilities.has(`${kind}.update`),
    );
  }
  if (
    action === "client_resource_deactivate" ||
    action === "client_resource_reactivate"
  ) {
    return resourceKinds.filter(
      ({ kind }) => !capabilities || capabilities.has(`${kind}.lifecycle`),
    );
  }
  return resourceKinds.filter(
    ({ capability }) => !capabilities || capabilities.has(capability),
  );
}

export function availableWorkspaceCatalog(
  capabilities?: ReadonlySet<string>,
): CatalogAction[] {
  return workspaceCatalog.filter((item) => {
    if (item.value === "client_resource_add") {
      return resourceKindsForAction(item.value, capabilities).length > 0;
    }
    if (
      item.value === "client_resource_update" ||
      item.value === "client_resource_deactivate" ||
      item.value === "client_resource_reactivate"
    ) {
      return resourceKindsForAction(item.value, capabilities).length > 0;
    }
    return !capabilities || capabilities.has(item.capability);
  });
}

export function isClientResourceAction(
  action: WorkspaceAction,
): action is ResourceAction {
  return action.startsWith("client_resource_");
}
