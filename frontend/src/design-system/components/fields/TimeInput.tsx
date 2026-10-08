import { Clock3 } from "lucide-react";
import { useId } from "react";

export function TimeInput({
  label,
  value,
  onChange,
  step = 300,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  step?: number;
}) {
  const id = useId();
  return (
    <label className="rti-date-field" htmlFor={id}>
      <span className="rti-field__label">{label}</span>
      <span className="rti-date-field__control">
        <Clock3 size={16} aria-hidden="true" />
        <input
          id={id}
          className="rti-input"
          type="time"
          value={value}
          step={step}
          onChange={(event) => onChange(event.target.value)}
        />
      </span>
    </label>
  );
}
