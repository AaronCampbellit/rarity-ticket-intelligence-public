import { GripVertical, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type KeyValueRow = {
  id: string;
  key: string;
  value: string;
};

let nextKeyValueRow = 0;

function row(key = "", value = ""): KeyValueRow {
  nextKeyValueRow += 1;
  return { id: `key-value-${nextKeyValueRow}`, key, value };
}

export function KeyValueBuilder({
  label,
  name,
  initialValue = {},
}: {
  label: string;
  name: string;
  initialValue?: Record<string, string>;
}) {
  const [rows, setRows] = useState<KeyValueRow[]>(() => {
    const entries = Object.entries(initialValue);
    return entries.length
      ? entries.map(([key, value]) => row(key, value))
      : [row()];
  });

  return (
    <fieldset className="rti-repeatable-builder rti-key-value-builder">
      <legend>{label}</legend>
      <div className="rti-repeatable-builder__rows">
        {rows.map((item, index) => (
          <div className="rti-repeatable-builder__row" key={item.id}>
            <GripVertical size={15} aria-hidden="true" />
            <label>
              <span>Field {index + 1}</span>
              <input
                name={`${name}_key`}
                value={item.key}
                placeholder="Procurement reference"
                onChange={(event) =>
                  setRows((current) =>
                    current.map((candidate) =>
                      candidate.id === item.id
                        ? { ...candidate, key: event.target.value }
                        : candidate,
                    ),
                  )
                }
              />
            </label>
            <label>
              <span>Value</span>
              <input
                name={`${name}_value`}
                value={item.value}
                placeholder="PO pending"
                onChange={(event) =>
                  setRows((current) =>
                    current.map((candidate) =>
                      candidate.id === item.id
                        ? { ...candidate, value: event.target.value }
                        : candidate,
                    ),
                  )
                }
              />
            </label>
            <button
              type="button"
              aria-label={`Remove field ${index + 1}`}
              disabled={rows.length === 1}
              onClick={() =>
                setRows((current) =>
                  current.filter((candidate) => candidate.id !== item.id),
                )
              }
            >
              <Trash2 size={15} aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
      <button
        type="button"
        onClick={() => setRows((current) => [...current, row()])}
      >
        <Plus size={15} aria-hidden="true" />
        Add field
      </button>
    </fieldset>
  );
}

export function recordFromForm(form: FormData, name: string) {
  const keys = form.getAll(`${name}_key`).map(String);
  const values = form.getAll(`${name}_value`).map(String);
  return Object.fromEntries(
    keys.flatMap((key, index) => {
      const normalized = key.trim();
      return normalized ? [[normalized, values[index]?.trim() ?? ""]] : [];
    }),
  );
}
