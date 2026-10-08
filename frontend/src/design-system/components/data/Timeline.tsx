import type { ReactNode } from "react";

export type TimelineItem = {
  id: string;
  title: string;
  timestamp: string;
  description?: ReactNode;
  actor?: string;
  tone?: "neutral" | "info" | "warning" | "success";
};

export function Timeline({ items }: { items: TimelineItem[] }) {
  return (
    <ol className="rti-timeline">
      {items.map((item) => (
        <li key={item.id} data-tone={item.tone}>
          <span />
          <div>
            <header>
              <strong>{item.title}</strong>
              <time>{item.timestamp}</time>
            </header>
            {item.description ? <p>{item.description}</p> : null}
            {item.actor ? <small>{item.actor}</small> : null}
          </div>
        </li>
      ))}
    </ol>
  );
}
