import { useId } from "react";
import { Button, Select } from "../../design-system";
import { filterKeys, type CalendarFilter, type FilterOptions } from "./types";

const labels: Record<(typeof filterKeys)[number], string> = {
  client_ids: "Clients",
  technician_ids: "Technicians",
  owner_ids: "Owners",
  team_ids: "Teams",
  technology_ids: "Technologies",
  project_ids: "Projects",
  phase_ids: "Phases",
  sla_ids: "SLAs",
  ticket_types: "Ticket types",
  tag_ids: "Tags",
  priorities: "Priorities",
  event_roles: "Event roles",
  health_states: "Health",
  source_types: "Source types",
  scheduling_modes: "Scheduling modes",
  terminal_states: "Completion",
};
const optionKeys: Record<string, string> = {
  client_ids: "clients",
  technician_ids: "technicians",
  owner_ids: "owners",
  phase_ids: "phases",
  sla_ids: "slas",
  ticket_types: "ticket_types",
  team_ids: "teams",
  technology_ids: "technologies",
  project_ids: "projects",
  tag_ids: "tags",
  priorities: "priorities",
  event_roles: "event_roles",
};
const fixed: Record<string, string[]> = {
  health_states: ["blocked", "overdue", "at_risk", "on_track", "terminal"],
  scheduling_modes: ["fixed_block", "effort_allocation", "informational"],
  terminal_states: ["active", "completed", "cancelled"],
  source_types: [
    "work_record",
    "task",
    "project",
    "phase",
    "milestone",
    "resource_plan",
    "technician_schedule",
    "pto",
    "maintenance_window",
    "commercial_commitment",
    "custom_date",
  ],
};
export function CalendarFilters({
  filter,
  options,
  names,
  onChange,
}: {
  filter: CalendarFilter;
  options: FilterOptions;
  names: Record<string, string>;
  onChange: (filter: CalendarFilter) => void;
}) {
  const id = useId();
  const count = Object.values(filter).reduce<number>(
    (sum, value) => sum + (Array.isArray(value) ? value.length : value ? 1 : 0),
    0,
  );
  return (
    <details className="calendar-filters">
      <summary>
        Filters · {count ? `${count} selected` : "All authorized clients"}
      </summary>
      <p>
        Client filters are explicit. The active client in the navigation bar
        does not limit this calendar. Busy time remains visible only within your
        workforce authority.
      </p>
      <div className="calendar-form-grid">
        {filterKeys.map((key) => {
          const available = [
            ...new Set([
              ...(fixed[key] ?? Object.keys(options[optionKeys[key]] ?? {})),
              ...(filter[key] ?? []),
            ]),
          ];
          return (
            <label key={key} htmlFor={`${id}-${key}`}>
              <span>{labels[key]}</span>
              <Select
                multiple
                id={`${id}-${key}`}
                value={filter[key] ?? []}
                onChange={(event) => {
                  const values = Array.from(
                    event.target.selectedOptions,
                    (option) => option.value,
                  );
                  const next = { ...filter };
                  if (values.length) next[key] = values;
                  else delete next[key];
                  onChange(next);
                }}
              >
                {available.map((value) => (
                  <option key={value} value={value}>
                    {names[value] ?? value.replaceAll("_", " ")}
                  </option>
                ))}
              </Select>
            </label>
          );
        })}
      </div>
      <label>
        <input
          type="checkbox"
          checked={filter.conflicts_only ?? false}
          onChange={(event) =>
            onChange({ ...filter, conflicts_only: event.target.checked })
          }
        />{" "}
        Conflicts only
      </label>
      <Button onClick={() => onChange({})}>Clear filters</Button>
    </details>
  );
}
