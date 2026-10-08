import { useState } from "react";

import type { ComboboxOption } from "../fields/Combobox";
import { MultiSelect } from "../fields/MultiSelect";

export function ScopeBuilder({
  label,
  name,
  options,
  initialValues = [],
}: {
  label: string;
  name: string;
  options: ComboboxOption[];
  initialValues?: string[];
}) {
  const [values, setValues] = useState(initialValues);
  return (
    <div className="rti-scope-builder">
      <MultiSelect
        label={label}
        options={options}
        values={values}
        onChange={setValues}
      />
      {values.map((value) => (
        <input key={value} type="hidden" name={name} value={value} />
      ))}
    </div>
  );
}
