export { AIRail } from "./AIRail";
export { DirtyForm } from "./DirtyForm";
export { CommandPalette } from "./CommandPalette";
export { PreviewPane } from "./PreviewPane";
export { PageHistoryStage } from "./PageHistoryStage";
export { WorkspaceTabs } from "./WorkspaceTabs";
export {
  WorkspaceProvider,
  blockingDirtyRecord,
  clearWorkspaceStorage,
  initialWorkspaceState,
  restoreWorkspace,
  serializeWorkspace,
  useOptionalWorkspace,
  useWorkspaceDirtyState,
  workspaceStorageKey,
  useWorkspace,
  workspaceActions,
  workspaceReducer,
} from "./useWorkspace";
export type { WorkspaceContext, WorkspaceItem, WorkspaceState } from "./types";
import "./workspace.css";
