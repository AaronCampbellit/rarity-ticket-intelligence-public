import { X } from "lucide-react";
import { useState, type KeyboardEvent } from "react";

import type { NavigationItem } from "../components/navigation/GroupedSidebar";
import type { WorkspaceItem } from "./types";
import {
  blockingDirtyRecord,
  useWorkspace,
  workspaceActions,
} from "./useWorkspace";

export function WorkspaceTabs({
  navigation,
  onActivate,
}: {
  navigation: NavigationItem[];
  onActivate: (item: WorkspaceItem) => void;
}) {
  const { state, dispatch } = useWorkspace();
  const [blockedMessage, setBlockedMessage] = useState("");

  function activate(id: string) {
    const tab = state.tabs.find((candidate) => candidate.id === id);
    if (!tab) return;
    const dirty = blockingDirtyRecord(state, id);
    if (dirty) {
      setBlockedMessage(
        `Save or discard changes in ${dirty.label} before switching records.`,
      );
      return;
    }
    setBlockedMessage("");
    dispatch(workspaceActions.activateTab(id));
    onActivate(tab);
  }

  function reorder(id: string, direction: -1 | 1) {
    dispatch(workspaceActions.moveTab(id, direction));
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, id: string) {
    if (event.altKey && event.shiftKey && event.key === "ArrowLeft") {
      event.preventDefault();
      reorder(id, -1);
    }
    if (event.altKey && event.shiftKey && event.key === "ArrowRight") {
      event.preventDefault();
      reorder(id, 1);
    }
  }

  if (!state.tabs.length) return null;

  return (
    <>
      <nav className="rti-workspace-tabs" aria-label="Open records">
        {state.tabs.map((tab) => (
          <div
            className="rti-workspace-tab"
            data-active={state.activeID === tab.id}
            key={tab.id}
          >
            <button
              type="button"
              aria-current={state.activeID === tab.id ? "page" : undefined}
              onClick={() => activate(tab.id)}
              onKeyDown={(event) => onKeyDown(event, tab.id)}
              title="Shift+Alt+Arrow to reorder"
            >
              <span>{tab.label}</span>
              {tab.dirty ? <i aria-label="Unsaved changes" /> : null}
            </button>
            <button
              type="button"
              className="rti-workspace-tab__close"
              aria-label={`Close ${tab.label}`}
              onClick={() => {
                setBlockedMessage("");
                dispatch(workspaceActions.closeTab(tab.id));
                if (tab.dirty) return;
                if (state.activeID === tab.id) {
                  const remaining = state.tabs.filter(
                    (candidate) => candidate.id !== tab.id,
                  );
                  const next = remaining.at(-1);
                  const item = next
                    ? navigation.find(({ id }) => id === next.routeID)
                    : navigation.find(({ id }) => id === tab.routeID);
                  if (item) item.onSelect?.();
                }
              }}
            >
              <X size={13} aria-hidden="true" />
            </button>
          </div>
        ))}
      </nav>
      {blockedMessage ? (
        <p className="rti-workspace-tabs__notice" role="status">
          {blockedMessage}
        </p>
      ) : null}
    </>
  );
}
