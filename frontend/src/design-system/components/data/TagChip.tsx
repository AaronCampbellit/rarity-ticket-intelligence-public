import { X } from "lucide-react";

import type { Tag, TagSource } from "../../../features/classification/types";
import "./data.css";

export type TagChipProps = {
  tag: Pick<Tag, "id" | "label" | "state">;
  source?: TagSource | "ai_suggestion_pending";
  inherited?: boolean;
  onRemove?: () => void;
};

const sourceLabels: Record<TagSource | "ai_suggestion_pending", string> = {
  human: "Selected by a technician",
  ai_confirmed: "AI suggestion confirmed by a technician",
  ai_automatic: "Applied automatically by AI",
  automation: "Applied by automation",
  integration: "Applied by an integration",
  migration: "Migrated classification",
  system_fallback: "System fallback classification",
  ai_suggestion_pending: "AI suggestion pending review",
};

export function TagChip({
  tag,
  source,
  inherited = false,
  onRemove,
}: TagChipProps) {
  const sourceLabel = inherited
    ? "Inherited from project"
    : source
      ? sourceLabels[source]
      : "Classification tag";
  return (
    <span
      className="rti-tag-chip"
      data-state={tag.state}
      data-inherited={inherited || undefined}
    >
      <span aria-label={`${tag.label}. ${sourceLabel}`}>{tag.label}</span>
      <span className="rti-tag-chip__source">{sourceLabel}</span>
      {onRemove && !inherited ? (
        <button
          type="button"
          aria-label={`Remove ${tag.label}`}
          onClick={onRemove}
        >
          <X size={13} aria-hidden="true" />
        </button>
      ) : null}
    </span>
  );
}
