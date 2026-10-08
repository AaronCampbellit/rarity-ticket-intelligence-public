import { Bot, ChevronRight, MessageSquareText, Sparkles } from "lucide-react";
import { useState } from "react";

import { AIAssistPanel } from "../../features/ai/AIAssistPanel";
import type { AIAssistAPI } from "../../features/ai/types";
import type { WorkspaceContext } from "./types";

export function AIRail({
  context,
  api,
  compact = false,
}: {
  context: WorkspaceContext;
  api?: AIAssistAPI;
  compact?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const isDeliveryRecord =
    context.entityType === "project" || context.entityType === "task";
  return (
    <aside
      className="rti-ai-rail"
      data-mode="dock"
      data-compact={compact}
      data-open={open}
      aria-label="Rarity AI"
    >
      <button
        type="button"
        className="rti-ai-rail__toggle"
        aria-expanded={open}
        aria-label={
          compact
            ? open
              ? "Close page assistant"
              : "Open page assistant"
            : undefined
        }
        title={compact ? "Page assistant" : undefined}
        onClick={() => setOpen((current) => !current)}
      >
        {open ? (
          <ChevronRight size={19} aria-hidden="true" />
        ) : compact ? (
          <MessageSquareText size={18} aria-hidden="true" />
        ) : (
          <Sparkles size={19} aria-hidden="true" />
        )}
        <span className={compact ? "sr-only" : undefined}>
          {open ? "Close Rarity AI" : "Ask Rarity"}
        </span>
      </button>
      {open ? (
        <div className="rti-ai-rail__panel">
          <header>
            <span>
              <Bot size={18} aria-hidden="true" />
            </span>
            <div>
              <strong>Rarity AI</strong>
              <small>Context-aware workspace assistant</small>
            </div>
          </header>
          <div className="rti-ai-context">
            <span>Attached context</span>
            <button type="button">{context.routeLabel}</button>
            {context.clientID ? (
              <button type="button">Active client</button>
            ) : null}
            {context.recordID ? (
              <button type="button">Current record</button>
            ) : null}
          </div>
          <div className="rti-ai-conversation" aria-live="polite">
            {context.recordID && api && !isDeliveryRecord ? (
              <AIAssistPanel
                workRecordID={context.recordID}
                supportedFeatures={[
                  "summary",
                  "reply_draft",
                  "similar_suggestions",
                ]}
                api={api}
                embedded
              />
            ) : isDeliveryRecord ? (
              <div className="rti-ai-empty">
                <Sparkles size={16} aria-hidden="true" />
                <div>
                  <strong>Delivery record assistance is coming next</strong>
                  <p>
                    This project or task stays attached for context, but
                    work-record generation is disabled so Rarity cannot target
                    an incompatible resource.
                  </p>
                </div>
              </div>
            ) : (
              <div className="rti-ai-empty">
                <Sparkles size={16} aria-hidden="true" />
                <div>
                  <strong>Open a work record to begin</strong>
                  <p>
                    The assistant only generates recommendations from an
                    authorized record context. Nothing is applied or sent
                    without your review.
                  </p>
                </div>
              </div>
            )}
          </div>
          <small className="rti-ai-rail__disclaimer">
            AI can make mistakes. Review proposed actions before applying them.
          </small>
        </div>
      ) : null}
    </aside>
  );
}
