import { GripVertical, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

import type { Tag, TagGroup } from "../../../features/classification/types";
import { TagPicker } from "../fields/TagPicker";

type ParameterRow = { id: string; key: string; value: string };
type StepRow = {
  id: string;
  action: string;
  connectionRef: string;
  parameters: ParameterRow[];
  condition?: { operator: string; value: string; children: unknown[] };
  preserved?: Record<string, unknown>;
};

const tagConditionOperators = [
  "has_any_tag",
  "has_all_tags",
  "has_no_tags",
  "has_tag_in_group",
] as const;

const actionKinds = [
  "add_comment",
  "update_field",
  "assign",
  "transition",
  "call_http",
  "add_tags",
  "remove_tags",
] as const;

function defaultParameters(kind: string): ParameterRow[] {
  const values: Record<string, Record<string, string>> = {
    add_comment: { body: "", visibility: "internal" },
    update_field: { field: "priority", value: "", reason: "" },
    assign: { owner_id: "" },
    transition: { to_status: "", reason: "" },
    call_http: { event: "work_record.updated" },
    add_tags: { tag_ids: "[]" },
    remove_tags: { tag_ids: "[]" },
  };
  return Object.entries(values[kind] ?? { value: "" }).map(
    ([key, value], index) => ({
      id: `parameter-${index}`,
      key,
      value,
    }),
  );
}

function newActionRow(id: string, kind = "add_comment"): StepRow {
  return {
    id,
    action: kind,
    connectionRef: "",
    parameters: defaultParameters(kind),
  };
}

function actionStep(row: StepRow) {
  const action: Record<string, unknown> = {
    kind: row.action,
    parameters: Object.fromEntries(
      row.parameters.map(({ key, value }) => [key, value]),
    ),
  };
  if (row.connectionRef) action.connection_ref = row.connectionRef;
  return { id: row.id, kind: "action", action };
}

function rowFromStep(step: Record<string, unknown>, index: number): StepRow {
  const action =
    step.action &&
    typeof step.action === "object" &&
    !Array.isArray(step.action)
      ? (step.action as Record<string, unknown>)
      : {};
  const parameters =
    action.parameters &&
    typeof action.parameters === "object" &&
    !Array.isArray(action.parameters)
      ? (action.parameters as Record<string, unknown>)
      : {};
  const kind = String(action.kind ?? "");
  const supported =
    step.kind === "action" &&
    Object.keys(step).every((key) => ["id", "kind", "action"].includes(key)) &&
    actionKinds.includes(kind as (typeof actionKinds)[number]) &&
    Object.keys(action).every((key) =>
      ["kind", "parameters", "connection_ref"].includes(key),
    ) &&
    Object.values(parameters).every((value) => typeof value === "string") &&
    (action.connection_ref === undefined ||
      typeof action.connection_ref === "string");
  const condition =
    step.condition &&
    typeof step.condition === "object" &&
    !Array.isArray(step.condition)
      ? (step.condition as Record<string, unknown>)
      : {};
  const conditionSupported =
    step.kind === "condition" &&
    tagConditionOperators.includes(
      String(condition.operator) as (typeof tagConditionOperators)[number],
    ) &&
    typeof condition.value === "string" &&
    Array.isArray(step.children);

  return {
    id: String(step.id ?? `step-${index + 1}`),
    action: supported ? kind : "",
    connectionRef: supported ? String(action.connection_ref ?? "") : "",
    parameters: supported
      ? Object.entries(parameters).map(([key, value], parameterIndex) => ({
          id: `parameter-${parameterIndex}`,
          key,
          value: String(value),
        }))
      : [],
    condition: conditionSupported
      ? {
          operator: String(condition.operator),
          value: String(condition.value),
          children: step.children as unknown[],
        }
      : undefined,
    preserved: supported || conditionSupported ? undefined : step,
  };
}

export function AutomationStepBuilder({
  steps,
  groups = [],
  tags = [],
}: {
  steps: Array<Record<string, unknown>>;
  groups?: TagGroup[];
  tags?: Tag[];
}) {
  const [rows, setRows] = useState(() => steps.map(rowFromStep));

  function updateRow(index: number, update: Partial<StepRow>) {
    setRows((current) =>
      current.map((row, rowIndex) =>
        rowIndex === index ? { ...row, ...update } : row,
      ),
    );
  }

  function updateConditionChild(
    index: number,
    row: StepRow,
    update: Partial<StepRow>,
  ) {
    if (!row.condition) return;
    const stored = row.condition.children[0];
    if (!stored || typeof stored !== "object" || Array.isArray(stored)) return;
    const child = rowFromStep(stored as Record<string, unknown>, 0);
    if (child.preserved || child.condition) return;
    updateRow(index, {
      condition: {
        ...row.condition,
        children: [actionStep({ ...child, ...update })],
      },
    });
  }

  return (
    <fieldset className="rti-builder rti-automation-builder">
      <legend>Automation steps</legend>
      <p>
        Steps run from top to bottom. Advanced branches stay intact until a
        guided editor supports their complete shape.
      </p>
      <div className="rti-builder__rows">
        {rows.map((row, index) => (
          <div className="rti-builder__row" key={`${row.id}-${index}`}>
            <GripVertical size={15} aria-hidden="true" />
            <input type="hidden" name="step_id" value={row.id} />
            <input
              type="hidden"
              name="step_mode"
              value={
                row.preserved
                  ? "preserved"
                  : row.condition
                    ? "guided_condition"
                    : "guided"
              }
            />
            <input
              type="hidden"
              name="step_preserved"
              value={row.preserved ? JSON.stringify(row.preserved) : ""}
            />
            {row.condition ? (
              <div className="rti-automation-builder__guided">
                <label>
                  <span>Tag condition</span>
                  <select
                    name={`step_condition_operator_${index}`}
                    value={row.condition.operator}
                    onChange={(event) =>
                      updateRow(index, {
                        condition: {
                          ...row.condition!,
                          operator: event.target.value,
                          value: "",
                        },
                      })
                    }
                  >
                    <option value="has_any_tag">Has any selected tag</option>
                    <option value="has_all_tags">Has all selected tags</option>
                    <option value="has_no_tags">
                      Has none of the selected tags
                    </option>
                    <option value="has_tag_in_group">Has a tag in group</option>
                  </select>
                </label>
                {row.condition.operator === "has_tag_in_group" ? (
                  <label>
                    <span>Tag group</span>
                    <select
                      name={`step_condition_value_${index}`}
                      value={row.condition.value}
                      required
                      onChange={(event) =>
                        updateRow(index, {
                          condition: {
                            ...row.condition!,
                            value: event.target.value,
                          },
                        })
                      }
                    >
                      <option value="">Select a group</option>
                      {groups
                        .filter((group) => group.state !== "archived")
                        .map((group) => (
                          <option key={group.id} value={group.id}>
                            {group.label}
                          </option>
                        ))}
                    </select>
                  </label>
                ) : (
                  <>
                    <TagPicker
                      label="Condition tags"
                      groups={groups}
                      tags={tags}
                      selectedIds={tagIDs([
                        {
                          id: "condition",
                          key: "tag_ids",
                          value: row.condition.value,
                        },
                      ])}
                      required
                      onChange={(ids) =>
                        updateRow(index, {
                          condition: {
                            ...row.condition!,
                            value: JSON.stringify(ids),
                          },
                        })
                      }
                    />
                    <input
                      type="hidden"
                      name={`step_condition_value_${index}`}
                      value={row.condition.value}
                    />
                  </>
                )}
                <input
                  type="hidden"
                  name={`step_children_${index}`}
                  value={JSON.stringify(row.condition.children)}
                />
                {(() => {
                  const stored = row.condition.children[0];
                  if (
                    row.condition.children.length !== 1 ||
                    !stored ||
                    typeof stored !== "object" ||
                    Array.isArray(stored)
                  ) {
                    return (
                      <small>
                        Existing nested steps are preserved exactly.
                      </small>
                    );
                  }
                  const child = rowFromStep(
                    stored as Record<string, unknown>,
                    0,
                  );
                  if (child.preserved || child.condition) {
                    return (
                      <small>
                        Existing nested steps are preserved exactly.
                      </small>
                    );
                  }
                  return (
                    <div className="rti-automation-builder__parameters">
                      <strong>Then</strong>
                      <label>
                        <span>Then action</span>
                        <select
                          value={child.action}
                          onChange={(event) =>
                            updateConditionChild(index, row, {
                              action: event.target.value,
                              connectionRef: "",
                              parameters: defaultParameters(event.target.value),
                            })
                          }
                        >
                          <option value="add_comment">Add comment</option>
                          <option value="update_field">Update field</option>
                          <option value="assign">Assign owner or team</option>
                          <option value="transition">Move to status</option>
                          <option value="call_http">
                            Call approved connection
                          </option>
                          <option value="add_tags">Add tags</option>
                          <option value="remove_tags">
                            Remove direct tags
                          </option>
                        </select>
                      </label>
                      {child.action === "add_tags" ||
                      child.action === "remove_tags" ? (
                        <TagPicker
                          label="Then action tags"
                          groups={groups}
                          tags={tags}
                          selectedIds={tagIDs(child.parameters)}
                          required
                          onChange={(ids) =>
                            updateConditionChild(index, row, {
                              parameters: [
                                {
                                  id: "parameter-0",
                                  key: "tag_ids",
                                  value: JSON.stringify(ids),
                                },
                              ],
                            })
                          }
                        />
                      ) : (
                        child.parameters.map((parameter, parameterIndex) => (
                          <label key={parameter.id}>
                            <span>{parameter.key.replaceAll("_", " ")}</span>
                            <input
                              value={parameter.value}
                              required
                              onChange={(event) =>
                                updateConditionChild(index, row, {
                                  parameters: child.parameters.map(
                                    (item, itemIndex) =>
                                      itemIndex === parameterIndex
                                        ? { ...item, value: event.target.value }
                                        : item,
                                  ),
                                })
                              }
                            />
                          </label>
                        ))
                      )}
                    </div>
                  );
                })()}
              </div>
            ) : row.preserved ? (
              <div className="rti-builder__preserved">
                <strong>
                  {String(row.preserved.kind ?? "Advanced step").replaceAll(
                    "_",
                    " ",
                  )}
                </strong>
                <span>Existing automation logic</span>
                <small>
                  This step is preserved exactly and is view-only in this guided
                  editor.
                </small>
              </div>
            ) : (
              <div className="rti-automation-builder__guided">
                <label>
                  <span>Action</span>
                  <select
                    name={`step_action_${index}`}
                    value={row.action}
                    onChange={(event) =>
                      updateRow(index, {
                        action: event.target.value,
                        connectionRef: "",
                        parameters: defaultParameters(event.target.value),
                      })
                    }
                  >
                    <option value="add_comment">Add comment</option>
                    <option value="update_field">Update field</option>
                    <option value="assign">Assign owner or team</option>
                    <option value="transition">Move to status</option>
                    <option value="call_http">Call approved connection</option>
                    <option value="add_tags">Add tags</option>
                    <option value="remove_tags">Remove direct tags</option>
                  </select>
                </label>
                {row.action === "call_http" ? (
                  <label>
                    <span>Approved connection</span>
                    <input
                      name={`step_connection_ref_${index}`}
                      value={row.connectionRef}
                      required
                      onChange={(event) =>
                        updateRow(index, {
                          connectionRef: event.target.value,
                        })
                      }
                    />
                  </label>
                ) : (
                  <input
                    type="hidden"
                    name={`step_connection_ref_${index}`}
                    value=""
                  />
                )}
                {row.action === "add_tags" || row.action === "remove_tags" ? (
                  <div className="rti-automation-builder__parameters">
                    <TagPicker
                      label={
                        row.action === "add_tags"
                          ? "Tags to add"
                          : "Direct tags to remove"
                      }
                      groups={groups}
                      tags={tags}
                      selectedIds={tagIDs(row.parameters)}
                      required
                      onChange={(ids) =>
                        updateRow(index, {
                          parameters: [
                            {
                              id: "parameter-0",
                              key: "tag_ids",
                              value: JSON.stringify(ids),
                            },
                          ],
                        })
                      }
                    />
                    <input
                      type="hidden"
                      name={`step_parameter_key_${index}`}
                      value="tag_ids"
                    />
                    <input
                      type="hidden"
                      name={`step_parameter_value_${index}`}
                      value={JSON.stringify(tagIDs(row.parameters))}
                    />
                    <small>
                      Inherited tags cannot be removed. Every object keeps its
                      required minimum classification.
                    </small>
                  </div>
                ) : (
                  <div className="rti-automation-builder__parameters">
                    <strong>Action details</strong>
                    {row.parameters.map((parameter, parameterIndex) => (
                      <div key={parameter.id}>
                        <label>
                          <span>Field</span>
                          <input
                            name={`step_parameter_key_${index}`}
                            value={parameter.key}
                            required
                            onChange={(event) =>
                              updateRow(index, {
                                parameters: row.parameters.map(
                                  (item, itemIndex) =>
                                    itemIndex === parameterIndex
                                      ? { ...item, key: event.target.value }
                                      : item,
                                ),
                              })
                            }
                          />
                        </label>
                        <label>
                          <span>Value</span>
                          <input
                            name={`step_parameter_value_${index}`}
                            value={parameter.value}
                            required
                            onChange={(event) =>
                              updateRow(index, {
                                parameters: row.parameters.map(
                                  (item, itemIndex) =>
                                    itemIndex === parameterIndex
                                      ? { ...item, value: event.target.value }
                                      : item,
                                ),
                              })
                            }
                          />
                        </label>
                        <button
                          type="button"
                          aria-label={`Remove detail ${parameterIndex + 1} from step ${index + 1}`}
                          onClick={() =>
                            updateRow(index, {
                              parameters: row.parameters.filter(
                                (_, itemIndex) => itemIndex !== parameterIndex,
                              ),
                            })
                          }
                        >
                          <Trash2 size={14} aria-hidden="true" />
                        </button>
                      </div>
                    ))}
                    <button
                      className="rti-builder__add"
                      type="button"
                      onClick={() =>
                        updateRow(index, {
                          parameters: [
                            ...row.parameters,
                            {
                              id: `parameter-${Date.now()}`,
                              key: "",
                              value: "",
                            },
                          ],
                        })
                      }
                    >
                      <Plus size={14} aria-hidden="true" /> Add detail
                    </button>
                  </div>
                )}
              </div>
            )}
            <button
              type="button"
              aria-label={`Remove automation step ${index + 1}`}
              disabled={Boolean(row.preserved)}
              onClick={() =>
                setRows((current) =>
                  current.filter((_, itemIndex) => itemIndex !== index),
                )
              }
            >
              <Trash2 size={15} aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
      <button
        className="rti-builder__add"
        type="button"
        onClick={() =>
          setRows((current) => [
            ...current,
            {
              id: `step-${Date.now()}`,
              action: "add_comment",
              connectionRef: "",
              parameters: defaultParameters("add_comment"),
            },
          ])
        }
      >
        <Plus size={15} aria-hidden="true" /> Add step
      </button>
      <button
        className="rti-builder__add"
        type="button"
        onClick={() => {
          const timestamp = Date.now();
          setRows((current) => [
            ...current,
            {
              id: `condition-${timestamp}`,
              action: "",
              connectionRef: "",
              parameters: [],
              condition: {
                operator: "has_any_tag",
                value: "[]",
                children: [
                  actionStep(newActionRow(`condition-action-${timestamp}`)),
                ],
              },
            },
          ]);
        }}
      >
        <Plus size={15} aria-hidden="true" /> Add condition
      </button>
    </fieldset>
  );
}

function tagIDs(parameters: ParameterRow[]) {
  try {
    const ids = JSON.parse(
      parameters.find((parameter) => parameter.key === "tag_ids")?.value ??
        "[]",
    ) as unknown;
    return Array.isArray(ids)
      ? ids.filter((id): id is string => typeof id === "string")
      : [];
  } catch {
    return [];
  }
}

export function automationStepsFromForm(data: FormData) {
  const ids = data.getAll("step_id").map(String);
  const modes = data.getAll("step_mode").map(String);
  const preserved = data.getAll("step_preserved").map(String);

  return ids.map((id, index) => {
    if (modes[index] === "preserved") {
      try {
        const step = JSON.parse(preserved[index]) as unknown;
        if (step && typeof step === "object" && !Array.isArray(step))
          return step as Record<string, unknown>;
      } catch {
        throw new Error("automation_step_preservation_failed");
      }
      throw new Error("automation_step_preservation_failed");
    }

    if (modes[index] === "guided_condition") {
      const operator = String(
        data.get(`step_condition_operator_${index}`) ?? "",
      );
      if (
        !tagConditionOperators.includes(
          operator as (typeof tagConditionOperators)[number],
        )
      )
        throw new Error("unsupported_automation_condition");
      const value = String(data.get(`step_condition_value_${index}`) ?? "");
      if (operator === "has_tag_in_group") {
        if (!value.trim())
          throw new Error("automation_condition_value_required");
      } else {
        try {
          const selected = JSON.parse(value) as unknown;
          if (
            !Array.isArray(selected) ||
            selected.length === 0 ||
            !selected.every((item) => typeof item === "string")
          ) {
            throw new Error("automation_condition_value_required");
          }
        } catch {
          throw new Error("automation_condition_value_required");
        }
      }
      const children = JSON.parse(
        String(data.get(`step_children_${index}`) ?? "[]"),
      ) as unknown;
      if (!Array.isArray(children))
        throw new Error("automation_condition_children_invalid");
      return {
        id,
        kind: "condition",
        condition: {
          field:
            operator === "has_tag_in_group" ? "group_ids" : "effective_tag_ids",
          operator,
          value,
        },
        children,
      };
    }

    const kind = String(data.get(`step_action_${index}`) ?? "");
    if (!actionKinds.includes(kind as (typeof actionKinds)[number]))
      throw new Error("unsupported_automation_action");
    const keys = data.getAll(`step_parameter_key_${index}`).map(String);
    const values = data.getAll(`step_parameter_value_${index}`).map(String);
    const connectionRef = String(
      data.get(`step_connection_ref_${index}`) ?? "",
    ).trim();
    const action: Record<string, unknown> = {
      kind,
      parameters: Object.fromEntries(
        keys
          .map((key, parameterIndex) => [key.trim(), values[parameterIndex]])
          .filter(([key]) => Boolean(key)),
      ),
    };
    if (connectionRef) action.connection_ref = connectionRef;
    return { id, kind: "action", action };
  });
}
