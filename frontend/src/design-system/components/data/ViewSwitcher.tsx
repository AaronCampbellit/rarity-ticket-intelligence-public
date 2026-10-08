import { Columns3, List, Table2 } from "lucide-react";

export type WorkView = "list" | "table" | "kanban";

const views = [
  { id: "list" as const, label: "List", icon: List },
  { id: "table" as const, label: "Table", icon: Table2 },
  { id: "kanban" as const, label: "Kanban", icon: Columns3 },
];

export function ViewSwitcher({
  value,
  onChange,
  available = views.map(({ id }) => id),
}: {
  value: WorkView;
  onChange: (view: WorkView) => void;
  available?: WorkView[];
}) {
  return (
    <div className="rti-view-switcher" role="group" aria-label="View as">
      {views
        .filter(({ id }) => available.includes(id))
        .map(({ id, label, icon: Icon }) => (
          <button
            type="button"
            key={id}
            aria-pressed={value === id}
            aria-label={`${label} view`}
            onClick={() => onChange(id)}
          >
            <Icon size={15} aria-hidden="true" />
            <span>{label}</span>
          </button>
        ))}
    </div>
  );
}
