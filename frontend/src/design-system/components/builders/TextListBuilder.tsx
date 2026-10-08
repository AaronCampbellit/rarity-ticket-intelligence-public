import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type TextListRow = { id: string; value: string };
let nextTextListRow = 0;

function row(value = ""): TextListRow {
  nextTextListRow += 1;
  return { id: `text-list-${nextTextListRow}`, value };
}

export function TextListBuilder({
  label,
  name,
  itemLabel = "Item",
  placeholder,
  initialValues = [],
}: {
  label: string;
  name: string;
  itemLabel?: string;
  placeholder?: string;
  initialValues?: string[] | null;
}) {
  const [rows, setRows] = useState<TextListRow[]>(() => {
    const values = initialValues ?? [];
    return values.length ? values.map(row) : [row()];
  });

  return (
    <fieldset className="rti-repeatable-builder rti-text-list-builder">
      <legend>{label}</legend>
      <div className="rti-repeatable-builder__rows">
        {rows.map((item, index) => (
          <div className="rti-repeatable-builder__row" key={item.id}>
            <label>
              <span>
                {itemLabel} {index + 1}
              </span>
              <input
                name={name}
                value={item.value}
                placeholder={placeholder}
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
              aria-label={`Remove ${itemLabel.toLowerCase()} ${index + 1}`}
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
        Add {itemLabel.toLowerCase()}
      </button>
    </fieldset>
  );
}
