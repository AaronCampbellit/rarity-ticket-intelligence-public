import type { RouteID } from "../../app/routes";

export type RecordEntity = "ticket" | "task" | "project";

export type WorkspaceItem = {
  id: string;
  routeID: RouteID;
  recordID: string;
  parentRecordID?: string;
  clientID: string;
  label: string;
  entityType: RecordEntity;
  openedAt: number;
  dirty?: boolean;
  preview?: {
    title: string;
    summary?: string;
    status?: string;
  };
};

export type WorkspaceContext = {
  routeID: RouteID;
  routeLabel: string;
  clientID?: string;
  recordID?: string;
  entityType?: RecordEntity;
};

export type WorkspaceState = {
  tabs: WorkspaceItem[];
  activeID?: string;
  preview?: WorkspaceItem;
  closeRequest?: { id: string };
  dirtySources?: Record<string, string[]>;
};
