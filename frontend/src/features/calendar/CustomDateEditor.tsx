import { useState } from "react";
import { Button, Notice } from "../../design-system";
import {
  Choice,
  Field,
  MutationForm,
  localInput,
  textValue,
  useSourceRows,
  value,
  type SourceRow,
} from "./formSupport";
import { calendarRequest } from "./requests";
import { zonedTimestamp } from "./dates";
export function CustomDateEditor({
  objectType,
  objectID,
  clientID,
}: {
  objectType:
    | "work_record"
    | "task"
    | "project"
    | "asset"
    | "knowledge_article"
    | "time_entry";
  objectID: string;
  clientID?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>Custom calendar dates</summary>
      {open ? (
        <CustomDates
          key={`${objectType}-${objectID}`}
          objectType={objectType}
          objectID={objectID}
          clientID={clientID}
        />
      ) : null}
    </details>
  );
}
function CustomDates({
  objectType,
  objectID,
  clientID,
}: {
  objectType: string;
  objectID: string;
  clientID?: string;
}) {
  const root = `objects/${objectType}/${encodeURIComponent(objectID)}`,
    definitions = useSourceRows(`${root}/custom-date-fields`, clientID),
    values = useSourceRows(`${root}/custom-date-values`, clientID),
    [selected, setSelected] = useState(""),
    [notice, setNotice] = useState("");
  const definition = definitions.rows.find((row) => row.id === selected),
    current = values.rows.find((row) => row.field_id === selected);
  return (
    <section>
      <Button
        onClick={() => {
          definitions.reload();
          values.reload();
          setNotice("");
        }}
      >
        Reload custom dates
      </Button>
      {definitions.error || values.error ? (
        <Notice tone="danger" title="Custom dates unavailable">
          {definitions.error || values.error}
        </Notice>
      ) : null}
      {notice ? <p role="status">{notice}</p> : null}
      {definitions.ready && values.ready ? (
        <>
          {!definitions.rows.length ? (
            <p>No active custom calendar date fields for this record.</p>
          ) : (
            <>
              <Choice
                label="Custom date field"
                options={definitions.rows.map((row) => ({
                  id: row.id,
                  name: textValue(row.label),
                }))}
                value={selected}
                onChange={(e) => {
                  setSelected(e.target.value);
                  setNotice("");
                }}
              />
              {definition ? (
                <CustomDateForm
                  key={`${definition.id}-${definition.version}-${current?.version ?? 0}`}
                  definition={definition}
                  current={current}
                  path={`${root}/custom-date-values`}
                  clientID={clientID}
                  onSaved={() => {
                    values.reload();
                    setNotice("Custom date saved.");
                  }}
                />
              ) : null}
            </>
          )}
        </>
      ) : null}
    </section>
  );
}
function CustomDateForm({
  definition,
  current,
  path,
  clientID,
  onSaved,
}: {
  definition: SourceRow;
  current?: SourceRow;
  path: string;
  clientID?: string;
  onSaved: () => void;
}) {
  const timed = definition.field_type === "datetime",
    timezone =
      textValue(current?.timezone) ||
      (["object", "client", "msp", ""].includes(
        textValue(definition.timezone_source),
      )
        ? ""
        : textValue(definition.timezone_source));
  return (
    <MutationForm
      label="Save custom date"
      onSaved={onSaved}
      onSubmit={(data, key) => {
        const payload = {
          field_id: definition.id,
          expected_version: current?.version ?? 0,
          ...(timed
            ? {
                timestamp_value: zonedTimestamp(
                  `${value(data, "date")}:00`,
                  value(data, "timezone"),
                ),
                timezone: value(data, "timezone"),
              }
            : { date_value: value(data, "date") }),
        };
        return calendarRequest(path, {
          method: "PUT",
          clientID,
          body: { ...payload, idempotency_key: key(payload) },
        });
      }}
    >
      <Field
        label={textValue(definition.label)}
        name="date"
        type={timed ? "datetime-local" : "date"}
        required
        defaultValue={
          timed
            ? localInput(current?.timestamp_value, timezone || "UTC")
            : textValue(current?.date_value)
        }
      />
      {timed ? (
        <Field
          label="Custom date timezone"
          name="timezone"
          required
          defaultValue={timezone}
        />
      ) : null}
      <p>The source record's edit permission is required to save this field.</p>
    </MutationForm>
  );
}
