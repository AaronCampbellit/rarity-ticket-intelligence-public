import { DatePicker } from "./DatePicker";

export function DateRangePicker({
  label,
  start,
  end,
  onChange,
}: {
  label: string;
  start: string;
  end: string;
  onChange: (range: { start: string; end: string }) => void;
}) {
  const error =
    start && end && end < start
      ? "End date must be on or after the start date."
      : "";
  return (
    <fieldset className="rti-date-range">
      <legend>{label}</legend>
      <div>
        <DatePicker
          label="Start date"
          value={start}
          max={end || undefined}
          onChange={(value) => onChange({ start: value, end })}
        />
        <span aria-hidden="true">to</span>
        <DatePicker
          label="End date"
          value={end}
          min={start || undefined}
          error={error || undefined}
          onChange={(value) => onChange({ start, end: value })}
        />
      </div>
      {error ? (
        <p className="rti-field__error" role="alert">
          {error}
        </p>
      ) : null}
    </fieldset>
  );
}
