import { Check, ChevronDown, X } from "lucide-react";
import { useId, useRef, useState } from "react";

import type { ComboboxOption } from "./Combobox";

export function MultiSelect({
  label,
  options,
  values,
  onChange,
}: {
  label: string;
  options: ComboboxOption[];
  values: string[];
  onChange: (values: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const listID = useId();
  const labelID = useId();
  const selected = options.filter((option) => values.includes(option.value));

  return (
    <div
      ref={root}
      className="rti-multi-select"
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <span id={labelID} className="rti-field__label">
        {label}
      </span>
      <div className="rti-multi-select__control">
        <div>
          {selected.map((option) => (
            <span className="rti-selection-chip" key={option.value}>
              {option.label}
              <button
                type="button"
                aria-label={`Remove ${option.label}`}
                onClick={() =>
                  onChange(values.filter((value) => value !== option.value))
                }
              >
                <X size={13} aria-hidden="true" />
              </button>
            </span>
          ))}
          {!selected.length ? <span>Choose one or more…</span> : null}
        </div>
        <button
          type="button"
          aria-label={`Choose ${label}`}
          aria-expanded={open}
          aria-controls={listID}
          onClick={() => setOpen((current) => !current)}
        >
          <ChevronDown size={16} aria-hidden="true" />
        </button>
      </div>
      {open ? (
        <div
          id={listID}
          className="rti-option-list"
          role="listbox"
          aria-labelledby={labelID}
          aria-multiselectable="true"
        >
          {options.map((option) => {
            const checked = values.includes(option.value);
            return (
              <button
                type="button"
                role="option"
                aria-selected={checked}
                disabled={option.disabled}
                key={option.value}
                onClick={() =>
                  onChange(
                    checked
                      ? values.filter((value) => value !== option.value)
                      : [...values, option.value],
                  )
                }
              >
                <span>
                  <strong>{option.label}</strong>
                  {option.description ? (
                    <small>{option.description}</small>
                  ) : null}
                </span>
                {checked ? <Check size={16} aria-hidden="true" /> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
