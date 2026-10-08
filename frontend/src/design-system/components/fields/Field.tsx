import { cloneElement, type ReactElement, type ReactNode } from "react";

import "./fields.css";

type FieldControlProps = {
  id?: string;
  required?: boolean;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
};

export type FieldProps = {
  id: string;
  label: string;
  hint?: ReactNode;
  error?: ReactNode;
  required?: boolean;
  children: ReactElement<FieldControlProps>;
};

export function Field({
  id,
  label,
  hint,
  error,
  required = false,
  children,
}: FieldProps) {
  const hintID = hint ? `${id}-hint` : undefined;
  const errorID = error ? `${id}-error` : undefined;
  const describedBy = [children.props["aria-describedby"], hintID, errorID]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="rti-field">
      <label className="rti-field__label" htmlFor={id}>
        <span>{label}</span>
        {required ? (
          <span className="rti-field__required">Required</span>
        ) : null}
      </label>
      {cloneElement(children, {
        id,
        required,
        "aria-describedby": describedBy || undefined,
        "aria-invalid": error ? true : undefined,
      })}
      {hint ? (
        <div id={hintID} className="rti-field__hint">
          {hint}
        </div>
      ) : null}
      {error ? (
        <div id={errorID} className="rti-field__error">
          {error}
        </div>
      ) : null}
    </div>
  );
}
