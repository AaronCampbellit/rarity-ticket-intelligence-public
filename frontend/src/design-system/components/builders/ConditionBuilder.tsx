import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type Row = {
  id: string;
  path: string;
  value: string;
  type: string;
  preserved?: boolean;
};

function flatten(
  value: Record<string, unknown>,
  prefix = "",
): Array<Omit<Row, "id">> {
  return Object.entries(value).flatMap(([key, item]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    if (item && typeof item === "object" && !Array.isArray(item)) {
      return flatten(item as Record<string, unknown>, path);
    }
    if (
      Array.isArray(item) &&
      item.some((entry) => entry && typeof entry === "object")
    ) {
      return [
        {
          path,
          value: `${item.length} structured ${item.length === 1 ? "entry" : "entries"}`,
          type: "preserved",
          preserved: true,
        },
      ];
    }
    return [
      {
        path,
        value: Array.isArray(item)
          ? item.map(String).join(", ")
          : String(item ?? ""),
        type: Array.isArray(item) ? "list" : typeof item,
      },
    ];
  });
}

export function ConditionBuilder({
  value,
  title = "Apply when",
}: {
  value: Record<string, unknown>;
  title?: string;
}) {
  const [rows, setRows] = useState<Row[]>(() =>
    flatten(value).map((row, index) => ({ ...row, id: `condition-${index}` })),
  );
  const originalEditablePaths = flatten(value)
    .filter(({ preserved }) => !preserved)
    .map(({ path }) => path);
  return (
    <fieldset className="rti-builder">
      <legend>{title}</legend>
      <p>All conditions must match. Leave this empty to apply universally.</p>
      <input
        type="hidden"
        name="condition_preserved"
        value={JSON.stringify(value)}
      />
      {originalEditablePaths.map((path) => (
        <input
          key={path}
          type="hidden"
          name="condition_original_path"
          value={path}
        />
      ))}
      <div className="rti-builder__rows">
        {rows.map((row, index) => (
          <div className="rti-builder__row" key={row.id}>
            <span>{index + 1}</span>
            {row.preserved ? (
              <>
                <div className="rti-builder__preserved">
                  <strong>{row.path.replaceAll(".", " · ")}</strong>
                  <span>{row.value}</span>
                  <small>
                    Advanced grouping is preserved exactly and is view-only in
                    this guided editor.
                  </small>
                </div>
              </>
            ) : (
              <>
                <label>
                  <span>Field</span>
                  <input
                    name="condition_path"
                    value={row.path}
                    placeholder="priority"
                    onChange={(event) =>
                      setRows((current) =>
                        current.map((item) =>
                          item.id === row.id
                            ? { ...item, path: event.target.value }
                            : item,
                        ),
                      )
                    }
                  />
                </label>
                <label>
                  <span>Value type</span>
                  <select
                    name="condition_type"
                    value={row.type}
                    onChange={(event) =>
                      setRows((current) =>
                        current.map((item) =>
                          item.id === row.id
                            ? { ...item, type: event.target.value }
                            : item,
                        ),
                      )
                    }
                  >
                    <option value="string">Text</option>
                    <option value="number">Number</option>
                    <option value="boolean">Yes / no</option>
                    <option value="list">List</option>
                  </select>
                </label>
                <label>
                  <span>Matches</span>
                  {row.type === "boolean" ? (
                    <select
                      name="condition_value"
                      value={row.value}
                      onChange={(event) =>
                        setRows((current) =>
                          current.map((item) =>
                            item.id === row.id
                              ? { ...item, value: event.target.value }
                              : item,
                          ),
                        )
                      }
                    >
                      <option value="true">Yes</option>
                      <option value="false">No</option>
                    </select>
                  ) : (
                    <input
                      name="condition_value"
                      value={row.value}
                      placeholder={
                        row.type === "list" ? "critical, high" : "critical"
                      }
                      onChange={(event) =>
                        setRows((current) =>
                          current.map((item) =>
                            item.id === row.id
                              ? { ...item, value: event.target.value }
                              : item,
                          ),
                        )
                      }
                    />
                  )}
                </label>
              </>
            )}
            <button
              type="button"
              aria-label={`Remove condition ${index + 1}`}
              disabled={row.preserved}
              onClick={() =>
                setRows((current) =>
                  current.filter((item) => item.id !== row.id),
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
              id: `condition-${Date.now()}`,
              path: "",
              type: "string",
              value: "",
            },
          ])
        }
      >
        <Plus size={15} aria-hidden="true" /> Add condition
      </button>
    </fieldset>
  );
}

export function conditionsFromForm(data: FormData): Record<string, unknown> {
  const paths = data.getAll("condition_path").map(String);
  const types = data.getAll("condition_type").map(String);
  const values = data.getAll("condition_value").map(String);
  let result: Record<string, unknown> = {};
  try {
    const preserved = JSON.parse(
      String(data.get("condition_preserved") ?? "{}"),
    ) as unknown;
    if (preserved && typeof preserved === "object" && !Array.isArray(preserved))
      result = preserved as Record<string, unknown>;
  } catch {
    result = {};
  }
  data.getAll("condition_original_path").forEach((path) => {
    deletePath(result, String(path));
  });
  paths.forEach((path, index) => {
    if (!path.trim()) return;
    const type = types[index];
    const value =
      type === "number"
        ? Number(values[index])
        : type === "boolean"
          ? values[index] === "true"
          : type === "list"
            ? values[index]
                .split(",")
                .map((item) => item.trim())
                .filter(Boolean)
            : values[index];
    const keys = path.split(".").filter(Boolean);
    let target = result;
    keys.forEach((key, keyIndex) => {
      if (keyIndex === keys.length - 1) target[key] = value;
      else {
        target[key] =
          target[key] && typeof target[key] === "object" ? target[key] : {};
        target = target[key] as Record<string, unknown>;
      }
    });
  });
  return result;
}

function deletePath(result: Record<string, unknown>, path: string) {
  const keys = path.split(".").filter(Boolean);
  const parents: Array<{ value: Record<string, unknown>; key: string }> = [];
  let target = result;
  for (const [index, key] of keys.entries()) {
    if (index === keys.length - 1) {
      delete target[key];
      break;
    }
    const next = target[key];
    if (!next || typeof next !== "object" || Array.isArray(next)) return;
    parents.push({ value: target, key });
    target = next as Record<string, unknown>;
  }
  for (const { value, key } of parents.reverse()) {
    const candidate = value[key];
    if (
      candidate &&
      typeof candidate === "object" &&
      !Array.isArray(candidate) &&
      !Object.keys(candidate).length
    ) {
      delete value[key];
    }
  }
}
