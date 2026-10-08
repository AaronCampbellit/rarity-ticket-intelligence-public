import type { ReactNode } from "react";

import type { FeedbackTone } from "./StatusBadge";
import "./feedback.css";

export function Notice({
  tone = "info",
  title,
  children,
  urgent = false,
}: {
  tone?: FeedbackTone;
  title: string;
  children: ReactNode;
  urgent?: boolean;
}) {
  return (
    <div
      className="rti-notice"
      data-tone={tone}
      role={urgent ? "alert" : "status"}
    >
      <strong>{title}</strong>
      <div>{children}</div>
    </div>
  );
}
