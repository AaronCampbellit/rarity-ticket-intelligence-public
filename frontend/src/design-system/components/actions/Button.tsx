import { LoaderCircle, type LucideIcon } from "lucide-react";
import type { ButtonHTMLAttributes } from "react";

import "./actions.css";

export type ButtonIntent =
  "primary" | "secondary" | "tertiary" | "danger" | "link";
export type ButtonSize = "compact" | "default";

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  intent?: ButtonIntent;
  size?: ButtonSize;
  loading?: boolean;
  loadingLabel?: string;
  leadingIcon?: LucideIcon;
  disabledReasonID?: string;
};

export function Button({
  children,
  className = "",
  disabled = false,
  disabledReasonID,
  intent = "secondary",
  leadingIcon: LeadingIcon,
  loading = false,
  loadingLabel = "Working",
  size = "default",
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      type={type}
      className={`rti-button ${className}`.trim()}
      data-intent={intent}
      data-size={size}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      aria-label={loading ? loadingLabel : props["aria-label"]}
      aria-describedby={disabledReasonID ?? props["aria-describedby"]}
    >
      {loading ? (
        <LoaderCircle
          className="rti-button__spinner"
          size={16}
          aria-hidden="true"
        />
      ) : LeadingIcon ? (
        <LeadingIcon size={16} aria-hidden="true" />
      ) : null}
      <span>{children}</span>
      {loading ? <span className="sr-only">{loadingLabel}</span> : null}
    </button>
  );
}
