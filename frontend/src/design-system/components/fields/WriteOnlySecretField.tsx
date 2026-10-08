import { useState } from "react";

import { Button } from "../actions/Button";
import { Field } from "./Field";
import { TextInput } from "./TextInput";

export type WriteOnlySecretFieldProps = {
  id: string;
  label: string;
  configured: boolean;
  configuredLabel?: string;
  replacementLabel?: string;
  name: string;
  required?: boolean;
  error?: string;
};

export function WriteOnlySecretField({
  id,
  label,
  configured,
  configuredLabel = "Credential configured",
  replacementLabel,
  name,
  required = false,
  error,
}: WriteOnlySecretFieldProps) {
  const [revealed, setRevealed] = useState(false);
  const inputLabel = configured
    ? (replacementLabel ?? `Replace ${label}`)
    : label;

  return (
    <div className="rti-secret-field">
      {configured ? (
        <p className="rti-secret-field__status">{configuredLabel}</p>
      ) : null}
      <div className="rti-secret-field__control">
        <Field
          id={id}
          label={inputLabel}
          error={error}
          required={required && !configured}
          hint={
            configured
              ? "Leave blank to keep the configured credential."
              : "The credential is accepted once and is never shown again."
          }
        >
          <TextInput
            name={name}
            type={revealed ? "text" : "password"}
            autoComplete="new-password"
          />
        </Field>
        <Button
          intent="tertiary"
          size="compact"
          aria-label={`${revealed ? "Hide" : "Show"} ${label}`}
          onClick={() => setRevealed((current) => !current)}
        >
          {revealed ? "Hide" : "Show"}
        </Button>
      </div>
    </div>
  );
}
