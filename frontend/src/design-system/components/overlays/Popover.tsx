import {
  cloneElement,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
  type Ref,
} from "react";

function assignRef(
  ref: Ref<HTMLElement> | undefined,
  value: HTMLElement | null,
) {
  if (typeof ref === "function") ref(value);
  else if (ref) ref.current = value;
}

export function Popover({
  trigger,
  children,
  label,
  align = "end",
  open: controlledOpen,
  onOpenChange,
  toggleOnTrigger = true,
  attachTriggerAria = true,
}: {
  trigger: ReactElement<{
    onClick?: () => void;
    "aria-expanded"?: boolean;
    "aria-controls"?: string;
    ref?: Ref<HTMLElement>;
  }>;
  children: ReactNode | ((close: () => void) => ReactNode);
  label: string;
  align?: "start" | "end";
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  toggleOnTrigger?: boolean;
  attachTriggerAria?: boolean;
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const root = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const id = useId();

  function changeOpen(next: boolean) {
    if (controlledOpen === undefined) setInternalOpen(next);
    onOpenChange?.(next);
  }

  useEffect(() => {
    if (!open) return;
    const dismiss = (event: MouseEvent) => {
      if (root.current?.contains(event.target as Node)) return;
      changeOpen(false);
      queueMicrotask(() => triggerRef.current?.focus());
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      changeOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", escape);
    };
  }, [open, controlledOpen, onOpenChange]);

  const originalRef = trigger.props.ref;

  return (
    <div ref={root} className="rti-popover">
      {cloneElement(trigger, {
        "aria-expanded": attachTriggerAria ? open : undefined,
        "aria-controls": attachTriggerAria && open ? id : undefined,
        onClick: () => {
          trigger.props.onClick?.();
          if (toggleOnTrigger) changeOpen(!open);
        },
        ref: (element: HTMLElement | null) => {
          triggerRef.current = element;
          assignRef(originalRef, element);
        },
      })}
      {open ? (
        <section
          id={id}
          className="rti-popover__surface"
          data-align={align}
          aria-label={label}
        >
          {typeof children === "function"
            ? (children as (close: () => void) => ReactNode)(() =>
                changeOpen(false),
              )
            : children}
        </section>
      ) : null}
    </div>
  );
}
