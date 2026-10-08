import { useState, type FormHTMLAttributes } from "react";

import { useWorkspaceDirtyState } from "./useWorkspace";

export function DirtyForm({
  ownerID,
  onChange,
  onClick,
  onReset,
  ...props
}: FormHTMLAttributes<HTMLFormElement> & { ownerID: string }) {
  const [dirtyOwnerID, setDirtyOwnerID] = useState<string>();
  useWorkspaceDirtyState(dirtyOwnerID === ownerID, ownerID);

  return (
    <form
      {...props}
      onChange={(event) => {
        setDirtyOwnerID(ownerID);
        onChange?.(event);
      }}
      onClick={(event) => {
        if (event.target instanceof Element && event.target.closest("button")) {
          setDirtyOwnerID(ownerID);
        }
        onClick?.(event);
      }}
      onReset={(event) => {
        setDirtyOwnerID(undefined);
        onReset?.(event);
      }}
    />
  );
}
