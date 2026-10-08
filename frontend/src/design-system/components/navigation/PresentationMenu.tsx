import { SlidersHorizontal, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type {
  DensityPreference,
  PresentationPreferences,
} from "../../foundations/preferences";

export function PresentationMenu({
  preferences,
  onChange,
}: {
  preferences: PresentationPreferences;
  onChange: (preferences: PresentationPreferences) => void;
}) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [open]);

  return (
    <div className="rti-presentation-menu">
      <button
        ref={triggerRef}
        type="button"
        className="rti-command-icon"
        aria-label="Presentation settings"
        aria-expanded={open}
        aria-controls="rti-presentation-menu"
        onClick={() => setOpen((current) => !current)}
      >
        <SlidersHorizontal size={17} aria-hidden="true" />
      </button>
      {open ? (
        <section
          id="rti-presentation-menu"
          className="rti-presentation-menu__panel"
          aria-label="Presentation settings"
        >
          <header>
            <div>
              <strong>Workspace appearance</strong>
              <span>Applies to your account on this device.</span>
            </div>
            <button
              type="button"
              className="rti-command-icon"
              aria-label="Close presentation settings"
              onClick={() => {
                setOpen(false);
                triggerRef.current?.focus();
              }}
            >
              <X size={16} aria-hidden="true" />
            </button>
          </header>
          <label>
            <span>Workspace density</span>
            <select
              value={preferences.density}
              onChange={(event) =>
                onChange({
                  ...preferences,
                  density: event.target.value as DensityPreference,
                })
              }
            >
              <option value="adaptive">Adaptive by workspace</option>
              <option value="compact">Compact</option>
              <option value="comfortable">Comfortable</option>
            </select>
          </label>
        </section>
      ) : null}
    </div>
  );
}
