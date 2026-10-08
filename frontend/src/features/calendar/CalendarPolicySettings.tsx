import { useState } from "react";
import { Button, Notice } from "../../design-system";
import {
  Choice,
  Field,
  MutationForm,
  textValue,
  useSourceRows,
  value,
  type Option,
  type SourceRow,
} from "./formSupport";
import { calendarRequest } from "./requests";
import { validTimezone } from "./preferences";

const conflictKinds = [
  "approved_pto",
  "non_working_time",
  "protected_maintenance",
  "ordinary_overbooking",
];
export function CalendarPolicySettings({
  teams,
  technicians,
  onChanged,
}: {
  teams: Option[];
  technicians: Option[];
  onChanged: () => void;
}) {
  const [scope, setScope] = useState("msp"),
    [scopeID, setScopeID] = useState("");
  const query = new URLSearchParams({
    scope_type: scope,
    ...(scope === "team"
      ? { team_id: scopeID }
      : scope === "technician"
        ? { technician_id: scopeID }
        : {}),
  });
  const source = useSourceRows(
    scope === "msp" || scopeID
      ? `calendar/conflict-policies?${query}`
      : undefined,
  );
  return (
    <section>
      <h3>Conflict policy</h3>
      <p>
        Warnings allow confirmation. Overrideable blocks require an authorized
        override and reason. Hard blocks prevent applying the proposed schedule.
      </p>
      <Choice
        label="Policy scope"
        value={scope}
        empty={false}
        options={["msp", "team", "technician"]}
        onChange={(e) => {
          setScope(e.target.value);
          setScopeID("");
        }}
      />
      {scope !== "msp" ? (
        <Choice
          label={scope === "team" ? "Policy team" : "Policy technician"}
          options={scope === "team" ? teams : technicians}
          value={scopeID}
          onChange={(e) => setScopeID(e.target.value)}
        />
      ) : null}
      <Button onClick={source.reload}>Reload conflict policy</Button>
      {source.error ? (
        <Notice tone="danger" title="Policy unavailable">
          {source.error}
        </Notice>
      ) : null}
      {source.ready ? (
        <MutationForm
          key={`${scope}-${scopeID}-${source.rows.map((row) => row.version).join()}`}
          label="Save conflict policy"
          onSaved={() => {
            source.reload();
            onChanged();
          }}
          onSubmit={(data) =>
            calendarRequest("calendar/conflict-policies", {
              method: "PUT",
              body: {
                scope: {
                  type: scope,
                  ...(scope === "team"
                    ? { team_id: scopeID }
                    : scope === "technician"
                      ? { technician_id: scopeID }
                      : {}),
                },
                expected_version: Math.max(
                  0,
                  ...source.rows.map((row) => row.version),
                ),
                rules: conflictKinds.map((kind) => ({
                  kind,
                  severity: value(data, kind),
                })),
              },
            })
          }
        >
          {conflictKinds.map((kind) => (
            <Choice
              key={kind}
              label={kind
                .replaceAll("_", " ")
                .replace(/^./, (s) => s.toUpperCase())}
              name={kind}
              required
              empty={false}
              defaultValue={
                textValue(
                  source.rows.find((row) => row.kind === kind)?.severity,
                ) || "warning"
              }
              options={["warning", "overrideable_block", "hard_block"]}
            />
          ))}
        </MutationForm>
      ) : null}
    </section>
  );
}
export function CustomDateSettings({ onChanged }: { onChanged: () => void }) {
  const source = useSourceRows("calendar/custom-date-fields"),
    [selected, setSelected] = useState("");
  const initial = source.rows.find((row) => row.id === selected);
  return (
    <section>
      <h3>Custom calendar dates</h3>
      <p>
        Configure the typed fields that appear in the calendar. Date fields keep
        their calendar date without an invented clock time.
      </p>
      <Button
        onClick={() => {
          setSelected("");
          source.reload();
        }}
      >
        Reload custom date definitions
      </Button>
      {source.error ? (
        <Notice tone="danger" title="Definitions unavailable">
          {source.error}
        </Notice>
      ) : null}
      {source.ready ? (
        <>
          <Choice
            label="Date definition"
            options={source.rows.map((row) => ({
              id: row.id,
              name: `${textValue(row.object_type)} · ${textValue(row.label)}`,
            }))}
            value={selected}
            empty="New date definition"
            onChange={(e) => setSelected(e.target.value)}
          />
          <CustomDateDefinitionForm
            key={`${selected}-${initial?.version ?? 0}`}
            initial={initial}
            onSaved={() => {
              setSelected("");
              source.reload();
              onChanged();
            }}
          />
        </>
      ) : null}
    </section>
  );
}
function CustomDateDefinitionForm({
  initial,
  onSaved,
}: {
  initial?: SourceRow;
  onSaved: () => void;
}) {
  const [type, setType] = useState(textValue(initial?.field_type) || "date"),
    [capacity, setCapacity] = useState(initial?.capacity_bearing === true),
    [objectType, setObjectType] = useState(
      textValue(initial?.object_type) || "work_record",
    );
  return (
    <MutationForm
      label="Save date definition"
      onSaved={onSaved}
      onSubmit={(data) => {
        const timezone = value(data, "timezone_source");
        if (
          type === "datetime" &&
          !["object", "client", "msp"].includes(timezone) &&
          !validTimezone(timezone)
        )
          throw new Error(
            "Choose an IANA timezone source, object, client, or msp.",
          );
        return calendarRequest("calendar/custom-date-fields", {
          method: "PUT",
          body: {
            ...(initial ? { id: initial.id } : {}),
            object_type: objectType,
            field_id: value(data, "field_id"),
            label: value(data, "label"),
            category: value(data, "category"),
            field_type: type,
            scheduling_mode: value(data, "scheduling_mode"),
            capacity_bearing:
              type === "datetime" &&
              capacity &&
              ["work_record", "task"].includes(objectType),
            timezone_source: type === "datetime" ? timezone : "",
            planned_effort_source:
              type === "datetime" && capacity
                ? value(data, "planned_effort_source")
                : "",
            expected_version: initial?.version ?? 0,
          },
        });
      }}
    >
      <Choice
        label="Object type"
        name="object_type"
        empty={false}
        value={objectType}
        disabled={!!initial}
        onChange={(e) => setObjectType(e.target.value)}
        options={[
          "work_record",
          "task",
          "project",
          "asset",
          "knowledge_article",
          "time_entry",
        ]}
      />
      <Field
        label="Field key"
        name="field_id"
        required
        readOnly={!!initial}
        defaultValue={textValue(initial?.field_id)}
      />
      <Field
        label="Label"
        name="label"
        required
        defaultValue={textValue(initial?.label)}
      />
      <Field
        label="Category"
        name="category"
        required
        defaultValue={textValue(initial?.category)}
      />
      <Choice
        label="Field type"
        name="field_type"
        empty={false}
        value={type}
        onChange={(e) => {
          setType(e.target.value);
          setCapacity(false);
        }}
        options={["date", "datetime"]}
      />
      <Choice
        key={type}
        label="Scheduling mode"
        name="scheduling_mode"
        empty={false}
        defaultValue={textValue(initial?.scheduling_mode) || "informational"}
        options={
          type === "date"
            ? ["informational", "fixed_block"]
            : ["informational", "fixed_block", "effort_allocation"]
        }
      />
      {type === "datetime" ? (
        <>
          <Field
            label="Timezone source"
            name="timezone_source"
            required
            defaultValue={textValue(initial?.timezone_source)}
            placeholder="object, client, msp, or America/New_York"
          />
          {["work_record", "task"].includes(objectType) ? (
            <label>
              <input
                type="checkbox"
                checked={capacity}
                onChange={(e) => setCapacity(e.target.checked)}
              />
              Consumes technician capacity
            </label>
          ) : null}
          {capacity ? (
            <Field
              label="Planned effort field key"
              name="planned_effort_source"
              required
              defaultValue={textValue(initial?.planned_effort_source)}
            />
          ) : null}
        </>
      ) : null}
    </MutationForm>
  );
}
