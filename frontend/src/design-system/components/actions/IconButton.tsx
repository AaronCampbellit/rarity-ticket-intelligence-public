import type { LucideIcon } from "lucide-react";
import type { ButtonHTMLAttributes } from "react";

import { Button, type ButtonIntent, type ButtonSize } from "./Button";

type IconButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "aria-label" | "children"
> & {
  icon: LucideIcon;
  label: string;
  intent?: ButtonIntent;
  size?: ButtonSize;
};

export function IconButton({
  icon: Icon,
  label,
  title = label,
  ...props
}: IconButtonProps) {
  return (
    <Button
      {...props}
      className={`rti-icon-button ${props.className ?? ""}`.trim()}
      aria-label={label}
      title={title}
    >
      <Icon size={16} aria-hidden="true" />
    </Button>
  );
}
