import {
  createContext,
  useContext,
  useEffect,
  useId,
  useMemo,
  useReducer,
  useState,
  type Dispatch,
  type ReactNode,
} from "react";

import type { RouteID } from "../../app/routes";
import type { RecordEntity, WorkspaceItem, WorkspaceState } from "./types";

export function workspaceStorageKey(principalID: string) {
  return `rti:workspace:v2:${encodeURIComponent(principalID)}`;
}

export function clearWorkspaceStorage(principalID: string) {
  try {
    sessionStorage.removeItem(workspaceStorageKey(principalID));
  } catch {
    // Workspace restoration is optional and must never block sign-out.
  }
}

export const initialWorkspaceState: WorkspaceState = { tabs: [] };

type Action =
  | { type: "open-record"; item: WorkspaceItem }
  | { type: "activate-tab"; id: string }
  | { type: "deactivate-tab" }
  | { type: "open-preview"; item: WorkspaceItem }
  | { type: "close-tab"; id: string; force?: boolean }
  | { type: "confirm-close" }
  | { type: "cancel-close" }
  | {
      type: "mark-dirty-source";
      id: string;
      sourceID: string;
      dirty: boolean;
    }
  | { type: "move-tab"; id: string; direction: -1 | 1 }
  | { type: "hydrate"; state: WorkspaceState }
  | { type: "close-preview" };

function uniqueTab(tabs: WorkspaceItem[], item: WorkspaceItem) {
  const existing = tabs.findIndex((tab) => tab.id === item.id);
  if (existing === -1) return [...tabs, item];
  return tabs.map((tab, index) =>
    index === existing ? { ...tab, ...item } : tab,
  );
}

const recordEntities = new Set<RecordEntity>(["ticket", "task", "project"]);

export function blockingDirtyRecord(state: WorkspaceState, targetID: string) {
  const active = state.tabs.find(({ id }) => id === state.activeID);
  const dirty = active?.dirty
    ? active
    : state.tabs.find((candidate) => candidate.dirty);
  return dirty && dirty.id !== targetID ? dirty : undefined;
}

function isRecordItem(item: WorkspaceItem) {
  return (
    typeof item.id === "string" &&
    item.id.length > 0 &&
    typeof item.routeID === "string" &&
    typeof item.recordID === "string" &&
    item.recordID.length > 0 &&
    item.recordID.length <= 128 &&
    typeof item.clientID === "string" &&
    item.clientID.length > 0 &&
    typeof item.label === "string" &&
    item.label.length > 0 &&
    item.label.length <= 80 &&
    recordEntities.has(item.entityType) &&
    typeof item.openedAt === "number"
  );
}

export function workspaceReducer(
  state: WorkspaceState,
  action: Action,
): WorkspaceState {
  switch (action.type) {
    case "open-record": {
      if (!isRecordItem(action.item)) return state;
      if (blockingDirtyRecord(state, action.item.id)) return state;
      return {
        ...state,
        tabs: uniqueTab(state.tabs, action.item),
        activeID: action.item.id,
        preview: undefined,
      };
    }
    case "activate-tab": {
      if (blockingDirtyRecord(state, action.id)) return state;
      return { ...state, activeID: action.id };
    }
    case "deactivate-tab":
      return { ...state, activeID: undefined };
    case "open-preview":
      return isRecordItem(action.item)
        ? { ...state, preview: action.item }
        : state;
    case "close-tab": {
      const target = state.tabs.find((tab) => tab.id === action.id);
      if (target?.dirty && !action.force) {
        return { ...state, closeRequest: { id: action.id } };
      }
      const tabs = state.tabs.filter((tab) => tab.id !== action.id);
      return {
        ...state,
        tabs,
        activeID:
          state.activeID === action.id ? tabs.at(-1)?.id : state.activeID,
        closeRequest: undefined,
        dirtySources: Object.fromEntries(
          Object.entries(state.dirtySources ?? {}).filter(
            ([id]) => id !== action.id,
          ),
        ),
      };
    }
    case "confirm-close":
      return state.closeRequest
        ? workspaceReducer(state, {
            type: "close-tab",
            id: state.closeRequest.id,
            force: true,
          })
        : state;
    case "cancel-close":
      return { ...state, closeRequest: undefined };
    case "mark-dirty-source": {
      const current = state.dirtySources?.[action.id] ?? [];
      const sources = action.dirty
        ? [...new Set([...current, action.sourceID])]
        : current.filter((sourceID) => sourceID !== action.sourceID);
      const dirtySources = { ...(state.dirtySources ?? {}) };
      if (sources.length) dirtySources[action.id] = sources;
      else delete dirtySources[action.id];
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === action.id ? { ...tab, dirty: sources.length > 0 } : tab,
        ),
        dirtySources,
      };
    }
    case "move-tab": {
      const index = state.tabs.findIndex((tab) => tab.id === action.id);
      const target = index + action.direction;
      if (index < 0 || target < 0 || target >= state.tabs.length) return state;
      const tabs = [...state.tabs];
      [tabs[index], tabs[target]] = [tabs[target], tabs[index]];
      return { ...state, tabs };
    }
    case "hydrate":
      return {
        tabs: action.state.tabs.filter(isRecordItem),
        activeID: action.state.activeID,
      };
    case "close-preview":
      return { ...state, preview: undefined };
  }
}

export const workspaceActions = {
  openRecord: (item: WorkspaceItem): Action => ({ type: "open-record", item }),
  activateTab: (id: string): Action => ({ type: "activate-tab", id }),
  deactivateTab: (): Action => ({ type: "deactivate-tab" }),
  openPreview: (item: WorkspaceItem): Action => ({
    type: "open-preview",
    item,
  }),
  closeTab: (id: string): Action => ({ type: "close-tab", id }),
  confirmClose: (): Action => ({ type: "confirm-close" }),
  cancelClose: (): Action => ({ type: "cancel-close" }),
  markDirtySource: (id: string, sourceID: string, dirty: boolean): Action => ({
    type: "mark-dirty-source",
    id,
    sourceID,
    dirty,
  }),
  markDirty: (id: string, dirty: boolean): Action =>
    workspaceActions.markDirtySource(id, "manual", dirty),
  moveTab: (id: string, direction: -1 | 1): Action => ({
    type: "move-tab",
    id,
    direction,
  }),
  hydrate: (state: WorkspaceState): Action => ({ type: "hydrate", state }),
  closePreview: (): Action => ({ type: "close-preview" }),
};

export function serializeWorkspace(
  state: WorkspaceState,
  allowedClientIDs: ReadonlySet<string> = new Set(),
): string {
  const tabs = state.tabs
    .filter((item) => isRecordItem(item) && allowedClientIDs.has(item.clientID))
    .map(
      ({
        id,
        routeID,
        recordID,
        parentRecordID,
        clientID,
        label,
        entityType,
        openedAt,
      }) => ({
        id,
        routeID,
        recordID,
        ...(parentRecordID ? { parentRecordID } : {}),
        clientID,
        label,
        entityType,
        openedAt,
      }),
    );
  return JSON.stringify({
    version: 4,
    tabs,
    activeID: tabs.some(({ id }) => id === state.activeID)
      ? state.activeID
      : undefined,
  });
}

export function restoreWorkspace(
  principalID: string,
  allowedRouteIDs: ReadonlySet<RouteID>,
  allowedClientIDs: ReadonlySet<string> = new Set(),
): WorkspaceState {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(workspaceStorageKey(principalID)) ?? "",
    );
    if (!parsed || parsed.version !== 4 || !Array.isArray(parsed.tabs)) {
      return initialWorkspaceState;
    }
    const tabs: WorkspaceItem[] = parsed.tabs.filter(
      (item: unknown): item is WorkspaceItem =>
        !!item &&
        typeof item === "object" &&
        isRecordItem(item as WorkspaceItem) &&
        allowedClientIDs.has((item as WorkspaceItem).clientID) &&
        typeof (item as WorkspaceItem).routeID === "string" &&
        allowedRouteIDs.has((item as WorkspaceItem).routeID as RouteID) &&
        (!(item as WorkspaceItem).parentRecordID ||
          (typeof (item as WorkspaceItem).parentRecordID === "string" &&
            (item as WorkspaceItem).parentRecordID!.length <= 128)),
    );
    const activeID =
      typeof parsed.activeID === "string" &&
      tabs.some(({ id }) => id === parsed.activeID)
        ? parsed.activeID
        : undefined;
    return {
      tabs,
      activeID,
    };
  } catch {
    return initialWorkspaceState;
  }
}

type WorkspaceController = {
  state: WorkspaceState;
  dispatch: Dispatch<Action>;
  openPreview: (item: WorkspaceItem) => void;
  openRecord: (item: WorkspaceItem) => boolean;
  closeTab: (id: string) => void;
  markDirty: (id: string, dirty: boolean) => void;
};

const WorkspaceContextValue = createContext<WorkspaceController | undefined>(
  undefined,
);

export function WorkspaceProvider({
  principalID,
  allowedRouteIDs,
  allowedClientIDs,
  onOpenRecord,
  children,
}: {
  principalID: string;
  allowedRouteIDs: ReadonlySet<RouteID>;
  allowedClientIDs?: ReadonlySet<string>;
  onOpenRecord?: (item: WorkspaceItem) => void;
  children: ReactNode;
}) {
  const [state, dispatch] = useReducer(
    workspaceReducer,
    { principalID, allowedRouteIDs },
    ({ principalID: id, allowedRouteIDs: allowed }) =>
      restoreWorkspace(id, allowed),
  );
  const [storageReady, setStorageReady] = useState(false);

  useEffect(() => {
    if (!allowedClientIDs) return;
    dispatch(
      workspaceActions.hydrate(
        restoreWorkspace(principalID, allowedRouteIDs, allowedClientIDs),
      ),
    );
    setStorageReady(true);
  }, [allowedClientIDs, allowedRouteIDs, principalID]);

  useEffect(() => {
    if (!storageReady || !allowedClientIDs) return;
    try {
      sessionStorage.setItem(
        workspaceStorageKey(principalID),
        serializeWorkspace(state, allowedClientIDs),
      );
    } catch {
      // Private browsing and storage quotas must not interrupt active work.
    }
  }, [allowedClientIDs, principalID, state, storageReady]);

  const controller = useMemo<WorkspaceController>(
    () => ({
      state,
      dispatch,
      openPreview: (item) => dispatch(workspaceActions.openPreview(item)),
      openRecord: (item) => {
        if (blockingDirtyRecord(state, item.id)) return false;
        dispatch(workspaceActions.openRecord(item));
        onOpenRecord?.(item);
        return true;
      },
      closeTab: (id) => dispatch(workspaceActions.closeTab(id)),
      markDirty: (id, dirty) => dispatch(workspaceActions.markDirty(id, dirty)),
    }),
    [onOpenRecord, state],
  );

  return (
    <WorkspaceContextValue.Provider value={controller}>
      {children}
    </WorkspaceContextValue.Provider>
  );
}

export function useWorkspace() {
  const value = useContext(WorkspaceContextValue);
  if (!value)
    throw new Error("useWorkspace must be used within WorkspaceProvider");
  return value;
}

export function useOptionalWorkspace() {
  return useContext(WorkspaceContextValue);
}

export function useWorkspaceDirtyState(dirty: boolean, ownerID: string) {
  const workspace = useOptionalWorkspace();
  const dispatch = workspace?.dispatch;
  const sourceID = useId();

  useEffect(() => {
    if (!dispatch) return;
    dispatch(workspaceActions.markDirtySource(ownerID, sourceID, dirty));
    return () => {
      dispatch(workspaceActions.markDirtySource(ownerID, sourceID, false));
    };
  }, [dirty, dispatch, ownerID, sourceID]);
}
