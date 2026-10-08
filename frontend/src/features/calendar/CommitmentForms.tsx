import { useState } from "react";
import { Notice } from "../../design-system";
import {
  Choice,
  Field,
  IntervalFields,
  MutationForm,
  RecurrenceEditor,
  intervalPayload,
  recurrencePayload,
  textValue,
  value,
  type Option,
  type SourceRow,
} from "./formSupport";
import { calendarRequest } from "./requests";
import { useOptions } from "./useCalendarDirectory";
import type { Recurrence } from "./types";

export type CommitmentKind =
  "milestone" | "maintenance" | "renewal" | "license";
export function scaledDecimal(input: string, scale: number) {
  if (!/^\d+(\.\d+)?$/.test(input))
    throw new Error("Enter a non-negative decimal number.");
  const digits = Math.log10(scale),
    [whole, fraction = ""] = input.split(".");
  if (fraction.length > digits)
    throw new Error(`Use at most ${digits} decimal places.`);
  const result = Number(whole) * scale + Number(fraction.padEnd(digits, "0"));
  if (!Number.isSafeInteger(result))
    throw new Error("This amount is too large.");
  return result;
}
export function CommitmentForm({
  kind,
  clientID,
  projectID,
  technicians,
  clients,
  initial,
  onSaved,
}: {
  kind: CommitmentKind;
  clientID: string;
  projectID?: string;
  technicians: Option[];
  clients: Option[];
  initial?: SourceRow;
  onSaved: () => void;
}) {
  const current: Record<string, unknown> = initial ?? {};
  const commercial = kind === "renewal" || kind === "license";
  const [hasCost, setHasCost] = useState(current.has_cost === true);
  const resources = useOptions(
    clientID && kind !== "milestone" ? "client-resources" : undefined,
    clientID,
  );
  const phases = useOptions(
    projectID ? `projects/${encodeURIComponent(projectID)}` : undefined,
    clientID,
    "phases",
  );
  const [scopeType, setScopeType] = useState("client");
  const scopes = Array.isArray(current.scopes)
    ? (current.scopes as Array<{ type: string; resource_id: string }>)
    : [];
  const [selectedScopes, setSelectedScopes] = useState(
    scopes.map((item) => ({ type: item.type, id: item.resource_id })),
  );
  const label = `${initial ? "Save" : "Create"} ${kind === "maintenance" ? "maintenance window" : kind}`;
  return (
    <MutationForm
      label={label}
      onSaved={onSaved}
      onSubmit={async (data, key) => {
        let payload: Record<string, unknown>, path: string;
        const shared = {
          description: value(data, "description"),
          owner_id: value(data, "owner_id"),
          recurrence: recurrencePayload(data),
        };
        if (kind === "milestone") {
          payload = {
            ...shared,
            name: value(data, "name"),
            priority: value(data, "priority"),
            phase_id: value(data, "phase_id"),
            due_on: value(data, "due_on"),
            ...intervalPayload(data),
          };
          path = initial
            ? `project-milestones/${encodeURIComponent(initial.id)}`
            : `projects/${encodeURIComponent(projectID!)}/milestones`;
        } else if (kind === "maintenance") {
          if (!selectedScopes.length)
            throw new Error("Add at least one maintenance scope.");
          payload = {
            ...shared,
            title: value(data, "name"),
            ...intervalPayload(data),
            protected: data.get("protected") === "on",
            conflict_policy: value(data, "conflict_policy"),
            scopes: selectedScopes,
          };
          path = `maintenance-windows${initial ? `/${encodeURIComponent(initial.id)}` : ""}`;
        } else {
          payload = {
            ...shared,
            client_id: clientID,
            type: kind,
            title: value(data, "name"),
            vendor: value(data, "vendor"),
            external_reference: value(data, "external_reference"),
            currency: hasCost ? value(data, "currency").toUpperCase() : "",
            effective_on: value(data, "effective_on"),
            notice_on: value(data, "notice_on"),
            renewal_on: value(data, "renewal_on"),
            expiration_on: value(data, "expiration_on"),
            quantity_units: scaledDecimal(value(data, "quantity"), 10000),
            cost_minor: hasCost ? scaledDecimal(value(data, "cost"), 100) : 0,
            has_cost: hasCost,
            service_id: value(data, "service_id"),
            asset_id: value(data, "asset_id"),
            contract_id: value(data, "contract_id"),
          };
          path = `commercial-commitments${initial ? `/${encodeURIComponent(initial.id)}` : ""}`;
        }
        if (initial) payload.expected_version = initial.version;
        return calendarRequest(path, {
          method: initial ? "PATCH" : "POST",
          clientID: kind === "milestone" ? clientID : undefined,
          body: { ...payload, idempotency_key: key(payload) },
        });
      }}
    >
      <Field
        label={kind === "milestone" ? "Name" : "Title"}
        name="name"
        required
        maxLength={240}
        defaultValue={textValue(current.name || current.title)}
      />
      <Field
        label="Description"
        name="description"
        defaultValue={textValue(current.description)}
      />
      <Choice
        label="Owner"
        name="owner_id"
        required={commercial}
        options={technicians}
        defaultValue={textValue(current.owner_id)}
      />
      {kind === "milestone" ? (
        <>
          <Field
            label="Due date"
            name="due_on"
            type="date"
            required
            defaultValue={textValue(current.due_on)}
          />
          <Choice
            label="Phase"
            name="phase_id"
            options={phases.options}
            defaultValue={textValue(current.phase_id)}
          />
          {phases.error ? (
            <p role="alert">Phase choices unavailable: {phases.error}</p>
          ) : null}
          <Field
            label="Priority"
            name="priority"
            required
            defaultValue={textValue(current.priority) || "normal"}
          />
        </>
      ) : null}
      {!commercial ? (
        <IntervalFields initial={current} />
      ) : (
        <>
          <Field
            label="Vendor"
            name="vendor"
            required
            defaultValue={textValue(current.vendor)}
          />
          <Field
            label="External reference"
            name="external_reference"
            defaultValue={textValue(current.external_reference)}
          />
          {["effective_on", "notice_on", "renewal_on", "expiration_on"].map(
            (name) => (
              <Field
                key={name}
                label={name
                  .replace("_on", " date")
                  .replace(/^./, (s) => s.toUpperCase())}
                type="date"
                name={name}
                required={name === "effective_on" || name === "expiration_on"}
                defaultValue={textValue(current[name])}
              />
            ),
          )}
          <Field
            label="Quantity"
            name="quantity"
            required
            type="number"
            min={0}
            step="0.0001"
            defaultValue={
              typeof current.quantity_units === "number"
                ? current.quantity_units / 10000
                : 1
            }
          />
          <label>
            <input
              type="checkbox"
              checked={hasCost}
              onChange={(e) => setHasCost(e.target.checked)}
            />{" "}
            Include cost
          </label>
          {hasCost ? (
            <>
              <Field
                label="Cost (two decimal places)"
                name="cost"
                type="number"
                min={0}
                step="0.01"
                required
                defaultValue={
                  typeof current.cost_minor === "number"
                    ? current.cost_minor / 100
                    : ""
                }
              />
              <Field
                label="Currency code"
                name="currency"
                minLength={3}
                maxLength={3}
                required
                defaultValue={textValue(current.currency)}
              />
            </>
          ) : null}
          {["service", "asset", "contract"].map((kind) => (
            <Choice
              key={kind}
              label={`Related ${kind}`}
              name={`${kind}_id`}
              options={resources.options.filter((item) => item.kind === kind)}
              defaultValue={textValue(current[`${kind}_id`])}
            />
          ))}
        </>
      )}
      {resources.error ? (
        <Notice tone="warning" title="Resource choices unavailable">
          {resources.error}
        </Notice>
      ) : null}
      {kind === "maintenance" ? (
        <>
          <label>
            <input
              type="checkbox"
              name="protected"
              defaultChecked={current.protected === true}
            />{" "}
            Protected maintenance
          </label>
          <Choice
            label="Conflict policy"
            name="conflict_policy"
            empty={false}
            defaultValue={textValue(current.conflict_policy) || "warning"}
            options={[
              "informational",
              "warning",
              "overrideable_block",
              "hard_block",
            ]}
          />
          <fieldset>
            <legend>Affected resources</legend>
            <Choice
              label="Scope type"
              empty={false}
              value={scopeType}
              onChange={(e) => setScopeType(e.target.value)}
              options={["client", "service", "asset"]}
            />
            <Choice
              label="Add affected resource"
              value=""
              options={
                scopeType === "client"
                  ? clients
                  : resources.options.filter((item) => item.kind === scopeType)
              }
              onChange={(e) => {
                if (
                  e.target.value &&
                  !selectedScopes.some(
                    (item) =>
                      item.type === scopeType && item.id === e.target.value,
                  )
                )
                  setSelectedScopes([
                    ...selectedScopes,
                    { type: scopeType, id: e.target.value },
                  ]);
              }}
            />
            <ul>
              {selectedScopes.map((item) => (
                <li key={`${item.type}-${item.id}`}>
                  {[...clients, ...resources.options].find(
                    (option) => option.id === item.id,
                  )?.name || item.type}{" "}
                  <button
                    type="button"
                    onClick={() =>
                      setSelectedScopes(
                        selectedScopes.filter((found) => found !== item),
                      )
                    }
                  >
                    Remove scope
                  </button>
                </li>
              ))}
            </ul>
          </fieldset>
        </>
      ) : null}
      <RecurrenceEditor
        initial={current.recurrence as Recurrence | undefined}
      />
    </MutationForm>
  );
}
