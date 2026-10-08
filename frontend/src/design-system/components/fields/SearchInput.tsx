import { Search, X } from "lucide-react";
import type { ChangeEvent, InputHTMLAttributes } from "react";

export function SearchInput({
  value,
  onChange,
  "aria-label": ariaLabel = "Search",
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
  value: string;
}) {
  return (
    <label className="rti-search-input">
      <Search size={16} aria-hidden="true" />
      <input
        {...props}
        className="rti-input"
        type="search"
        value={value}
        aria-label={ariaLabel}
        onChange={onChange}
      />
      {value ? (
        <button
          type="button"
          aria-label={`Clear ${ariaLabel}`}
          onClick={() => {
            const synthetic = {
              target: { value: "" },
              currentTarget: { value: "" },
            } as ChangeEvent<HTMLInputElement>;
            onChange?.(synthetic);
          }}
        >
          <X size={14} aria-hidden="true" />
        </button>
      ) : null}
    </label>
  );
}
