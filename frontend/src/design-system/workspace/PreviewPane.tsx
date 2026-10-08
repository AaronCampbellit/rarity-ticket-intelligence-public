import { ArrowUpRight, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { useWorkspace, workspaceActions } from "./useWorkspace";

export function PreviewPane({ children }: { children?: ReactNode }) {
  const { state, dispatch, openRecord } = useWorkspace();
  const openingControl = useRef<HTMLElement | null>(null);
  const [blockedMessage, setBlockedMessage] = useState("");
  const item = state.preview;

  useEffect(() => {
    setBlockedMessage("");
    if (item && document.activeElement instanceof HTMLElement) {
      openingControl.current = document.activeElement;
    }
  }, [item?.id]);

  if (!item) return null;

  function closePreview() {
    dispatch(workspaceActions.closePreview());
    if (openingControl.current?.isConnected) openingControl.current.focus();
  }

  return (
    <aside
      className="rti-preview-pane"
      data-mode="inspector"
      aria-label={`${item.label} preview`}
    >
      <header>
        <div>
          <span>Preview</span>
          <strong>{item.label}</strong>
        </div>
        <div>
          <button
            type="button"
            aria-label={`Open ${item.label} in workspace`}
            onClick={() => {
              if (!openRecord(item)) {
                setBlockedMessage(
                  "Save or discard changes in the active record before opening this one.",
                );
              }
            }}
          >
            <ArrowUpRight size={16} aria-hidden="true" />
            <span>Open</span>
          </button>
          <button
            type="button"
            aria-label={`Close ${item.label} preview`}
            onClick={closePreview}
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>
      </header>
      {blockedMessage ? (
        <p className="rti-workspace-tabs__notice" role="status">
          {blockedMessage}
        </p>
      ) : null}
      <div className="rti-preview-pane__body">
        {children ?? (
          <>
            <p className="rti-eyebrow">Quick view</p>
            <h2>{item.preview?.title ?? item.label}</h2>
            {item.preview?.status ? (
              <p className="rti-preview-pane__status">{item.preview.status}</p>
            ) : null}
            <p>
              {item.preview?.summary ??
                "Review this record here, then open the complete workspace when you need to make changes."}
            </p>
          </>
        )}
      </div>
    </aside>
  );
}
