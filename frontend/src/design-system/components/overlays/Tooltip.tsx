import { useId, type ReactNode } from "react";

export function Tooltip({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <span className="rti-tooltip">
      <span tabIndex={0} aria-describedby={id}>
        {children}
      </span>
      <span id={id} role="tooltip">
        {label}
      </span>
    </span>
  );
}
