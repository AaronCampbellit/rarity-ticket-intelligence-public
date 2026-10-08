import type { ReactNode } from "react";

export type FeedbackTone =
  "neutral" | "info" | "success" | "warning" | "danger";

export function StatusBadge({
  tone = "neutral",
  children,
}: {
  tone?: FeedbackTone;
  children: ReactNode;
}) {
  return (
    <span className="rti-status-badge" data-tone={tone}>
      {children}
    </span>
  );
}
