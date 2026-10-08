import {
  useEffect,
  useId,
  useRef,
  type KeyboardEvent,
  type KeyboardEventHandler,
  type ReactNode,
  type RefObject,
} from "react";

export type DialogProps = {
  id?: string;
  open: boolean;
  title: string;
  description?: string;
  initialFocusRef?: RefObject<HTMLElement | null>;
  onClose: () => void;
  children: ReactNode;
  actions?: ReactNode;
  dismissible?: boolean;
  variant?: "dialog" | "drawer";
  onKeyDown?: KeyboardEventHandler<HTMLElement>;
};

const focusableSelector =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])';

export function Dialog({
  id,
  open,
  title,
  description,
  initialFocusRef,
  onClose,
  children,
  actions,
  dismissible = true,
  variant = "dialog",
  onKeyDown,
}: DialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const wasOpen = useRef(false);
  const titleID = useId();
  const descriptionID = useId();

  useEffect(() => {
    if (open && !wasOpen.current) {
      restoreFocusRef.current = document.activeElement as HTMLElement | null;
      const firstFocusable =
        initialFocusRef?.current ??
        dialogRef.current?.querySelector<HTMLElement>(focusableSelector);
      firstFocusable?.focus();
    }
    if (!open && wasOpen.current) restoreFocusRef.current?.focus();
    wasOpen.current = open;
  }, [initialFocusRef, open]);

  if (!open) return null;

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    onKeyDown?.(event);
    if (event.defaultPrevented) return;
    if (event.key === "Escape") {
      if (dismissible) onClose();
      return;
    }
    if (event.key !== "Tab" || !dialogRef.current) return;

    const focusable = [
      ...dialogRef.current.querySelectorAll<HTMLElement>(focusableSelector),
    ];
    if (!focusable.length) {
      event.preventDefault();
      dialogRef.current.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div className="rti-dialog-layer">
      <button
        className="rti-dialog-backdrop"
        type="button"
        tabIndex={-1}
        aria-label="Close dialog"
        data-testid="dialog-backdrop"
        onClick={() => {
          if (dismissible) onClose();
        }}
      />
      <div
        id={id}
        ref={dialogRef}
        className="rti-dialog"
        data-variant={variant}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        aria-describedby={description ? descriptionID : undefined}
        tabIndex={-1}
        onKeyDown={handleKeyDown}
      >
        <header className="rti-dialog__header">
          <h2 id={titleID}>{title}</h2>
          {description ? <p id={descriptionID}>{description}</p> : null}
        </header>
        <div className="rti-dialog__body">{children}</div>
        {actions ? (
          <footer className="rti-dialog__actions">{actions}</footer>
        ) : null}
      </div>
    </div>
  );
}
