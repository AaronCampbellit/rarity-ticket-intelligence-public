import type { WorkspaceItem } from "../../design-system";
import type { WorkRecord } from "./api";

export function ticketWorkspaceItem(
  record: WorkRecord,
  clientID: string,
): WorkspaceItem {
  return {
    id: `ticket:${record.id}`,
    routeID: "work",
    recordID: record.id,
    clientID,
    label: record.displayID,
    entityType: "ticket",
    openedAt: Date.now(),
    preview: {
      title: record.title,
      summary: record.description || "No description provided.",
      status: record.status,
    },
  };
}
