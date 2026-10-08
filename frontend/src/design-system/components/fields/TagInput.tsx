import { Plus, X } from "lucide-react";
import { useId, useState } from "react";

export function TagInput({
  label,
  name,
  values,
  defaultValues = [],
  onChange,
  placeholder = "Type a value and press Enter",
}: {
  label: string;
  name?: string;
  values?: string[];
  defaultValues?: string[];
  onChange?: (values: string[]) => void;
  placeholder?: string;
}) {
  const labelID = useId();
  const [internalValues, setInternalValues] = useState(defaultValues);
  const [draft, setDraft] = useState("");
  const current = values ?? internalValues;

  function update(next: string[]) {
    if (values === undefined) setInternalValues(next);
    onChange?.(next);
  }

  function commit() {
    const normalized = draft.trim();
    if (!normalized) return;
    if (!current.includes(normalized)) update([...current, normalized]);
    setDraft("");
  }

  return (
    <fieldset className="rti-tag-input">
      <legend id={labelID}>{label}</legend>
      <div className="rti-tag-input__control">
        {current.map((value) => (
          <span className="rti-selection-chip" key={value}>
            {value}
            <button
              type="button"
              aria-label={`Remove ${value}`}
              onClick={() =>
                update(current.filter((candidate) => candidate !== value))
              }
            >
              <X size={13} aria-hidden="true" />
            </button>
            {name ? <input type="hidden" name={name} value={value} /> : null}
          </span>
        ))}
        <input
          name={name}
          value={draft}
          aria-labelledby={labelID}
          placeholder={current.length ? "Add another…" : placeholder}
          onBlur={commit}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === ",") {
              event.preventDefault();
              commit();
            }
            if (event.key === "Backspace" && !draft && current.length) {
              update(current.slice(0, -1));
            }
          }}
        />
        <button
          type="button"
          aria-label={`Add ${label.toLowerCase()} value`}
          disabled={!draft.trim()}
          onMouseDown={(event) => event.preventDefault()}
          onClick={commit}
        >
          <Plus size={15} aria-hidden="true" />
        </button>
      </div>
      <small>Press Enter after each value.</small>
    </fieldset>
  );
}
