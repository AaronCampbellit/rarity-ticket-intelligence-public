import { Check } from "lucide-react";
import {
  useRef,
  type KeyboardEvent,
  type ReactElement,
  type ReactNode,
} from "react";

import { Popover } from "./Popover";

export type MenuItem = {
  id: string;
  label: string;
  description?: string;
  icon?: ReactNode;
  selected?: boolean;
  disabled?: boolean;
  danger?: boolean;
  onSelect: () => void;
};

export function Menu({
  label,
  trigger,
  items,
}: {
  label: string;
  trigger: ReactElement<{
    onClick?: () => void;
    "aria-expanded"?: boolean;
    "aria-controls"?: string;
  }>;
  items: MenuItem[];
}) {
  const menu = useRef<HTMLDivElement>(null);
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const controls = [
      ...(menu.current?.querySelectorAll<HTMLButtonElement>(
        "button:not(:disabled)",
      ) ?? []),
    ];
    if (!controls.length) return;
    const current = controls.indexOf(
      document.activeElement as HTMLButtonElement,
    );
    const target =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? controls.length - 1
          : event.key === "ArrowDown"
            ? (current + 1) % controls.length
            : (current - 1 + controls.length) % controls.length;
    controls[target].focus();
  }
  return (
    <Popover label={label} trigger={trigger}>
      {(close: () => void) => (
        <div
          ref={menu}
          className="rti-menu"
          role="menu"
          aria-label={label}
          onKeyDown={onKeyDown}
        >
          {items.map((item) => (
            <button
              type="button"
              role="menuitem"
              key={item.id}
              disabled={item.disabled}
              data-danger={item.danger}
              onClick={() => {
                item.onSelect();
                close();
              }}
            >
              {item.icon}
              <span>
                <strong>{item.label}</strong>
                {item.description ? <small>{item.description}</small> : null}
              </span>
              {item.selected ? <Check size={16} aria-hidden="true" /> : null}
            </button>
          ))}
        </div>
      )}
    </Popover>
  );
}
