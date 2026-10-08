import type { ReactNode } from "react";

type ButtonGroupProps = {
  label: string;
  children: ReactNode;
  className?: string;
};

export function ButtonGroup({
  label,
  children,
  className = "",
}: ButtonGroupProps) {
  return (
    <div
      className={`rti-button-group ${className}`.trim()}
      role="group"
      aria-label={label}
    >
      {children}
    </div>
  );
}
