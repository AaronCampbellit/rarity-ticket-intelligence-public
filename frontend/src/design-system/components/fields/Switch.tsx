import type { ChangeEventHandler } from "react";

type SwitchProps = {
  label: string;
  checked: boolean;
  onChange: ChangeEventHandler<HTMLInputElement>;
  name?: string;
  disabled?: boolean;
  disabledReasonID?: string;
};

export function Switch({
  label,
  checked,
  onChange,
  name,
  disabled = false,
  disabledReasonID,
}: SwitchProps) {
  return (
    <label className="rti-switch">
      <input
        type="checkbox"
        name={name}
        checked={checked}
        onChange={onChange}
        disabled={disabled}
        aria-describedby={disabledReasonID}
        aria-label={`${label} ${checked ? "On" : "Off"}`}
      />
      <span>{label}</span>
      <span className="rti-switch__state">{checked ? "On" : "Off"}</span>
    </label>
  );
}
