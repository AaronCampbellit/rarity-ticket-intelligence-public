import { useState, type FormEvent } from "react";

import { Button } from "../components/actions/Button";
import { ButtonGroup } from "../components/actions/ButtonGroup";
import { Dialog } from "../components/containers/Dialog";
import { Field } from "../components/fields/Field";
import { Textarea } from "../components/fields/Textarea";
import { formatScope, type ScopeLabel } from "./ScopedAction";

export type ReasonEvidence = {
  reason: string;
  expectedVersion: number;
};

export function ReasonRequiredDialog({
  open,
  action,
  target,
  scope,
  expectedVersion,
  onConfirm,
  onClose,
  destructive = false,
}: {
  open: boolean;
  action: string;
  target: string;
  scope: ScopeLabel;
  expectedVersion: number;
  onConfirm: (evidence: ReasonEvidence) => void;
  onClose: () => void;
  destructive?: boolean;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalized = reason.trim();
    if (!normalized) {
      setError("Enter a reason.");
      return;
    }
    onConfirm({ reason: normalized, expectedVersion });
  }

  return (
    <Dialog
      open={open}
      title={action}
      description={`${target} · ${formatScope(scope)}`}
      onClose={onClose}
      dismissible={!destructive}
    >
      <form className="rti-reason-form" onSubmit={submit}>
        <Field id="rti-action-reason" label="Reason" error={error} required>
          <Textarea
            value={reason}
            onChange={(event) => {
              setReason(event.target.value);
              if (error) setError("");
            }}
          />
        </Field>
        <ButtonGroup label={`${action} confirmation`}>
          <Button type="submit" intent={destructive ? "danger" : "primary"}>
            {action}
          </Button>
          <Button onClick={onClose}>Cancel</Button>
        </ButtonGroup>
      </form>
    </Dialog>
  );
}
