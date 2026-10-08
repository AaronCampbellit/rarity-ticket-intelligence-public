import { cloneElement, useId, type ReactElement } from "react";

import "./patterns.css";

export type ScopeLabel = {
  organization: string;
  client?: string;
  record?: string;
};

type ActionControlProps = {
  disabled?: boolean;
  disabledReasonID?: string;
  "aria-describedby"?: string;
};

export function formatScope(scope: ScopeLabel) {
  return [scope.organization, scope.client, scope.record]
    .filter(Boolean)
    .join(" · ");
}

export function ScopedAction({
  scope,
  permitted,
  disabledReason,
  children,
}: {
  scope: ScopeLabel;
  permitted: boolean;
  disabledReason?: string;
  children: ReactElement<ActionControlProps>;
}) {
  const reasonID = useId();
  const control = cloneElement(children, {
    disabled: !permitted || children.props.disabled,
    disabledReasonID: !permitted ? reasonID : children.props.disabledReasonID,
    "aria-describedby": permitted
      ? children.props["aria-describedby"]
      : reasonID,
  });

  return (
    <div className="rti-scoped-action">
      <p className="rti-scoped-action__scope">{formatScope(scope)}</p>
      {control}
      {!permitted && disabledReason ? (
        <p id={reasonID} className="rti-scoped-action__reason">
          {disabledReason}
        </p>
      ) : null}
    </div>
  );
}
