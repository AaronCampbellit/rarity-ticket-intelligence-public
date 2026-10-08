import { CalendarDays, X } from "lucide-react";
import { useId } from "react";

export function DatePicker({
  label,
  value,
  onChange,
  min,
  max,
  error,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  min?: string;
  max?: string;
  error?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const errorID = `${id}-error`;
  return (
    <label className="rti-date-field" htmlFor={id}>
      <span className="rti-field__label">{label}</span>
      <span className="rti-date-field__control">
        <CalendarDays size={16} aria-hidden="true" />
        <input
          id={id}
          className="rti-input"
          type="date"
          value={value}
          min={min}
          max={max}
          disabled={disabled}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorID : undefined}
          onChange={(event) => onChange(event.target.value)}
        />
        {value && !disabled ? (
          <button
            type="button"
            aria-label={`Clear ${label}`}
            onClick={() => onChange("")}
          >
            <X size={14} aria-hidden="true" />
          </button>
        ) : null}
      </span>
      {error ? (
        <span className="rti-field__error" id={errorID}>
          {error}
        </span>
      ) : null}
    </label>
  );
}
