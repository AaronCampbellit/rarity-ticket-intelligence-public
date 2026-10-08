import { Command, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import type { NavigationItem } from "../components/navigation/GroupedSidebar";

export function CommandPalette({
  navigation,
}: {
  navigation: NavigationItem[];
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  const dialog = useRef<HTMLElement>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen(true);
      }
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    if (open) window.setTimeout(() => input.current?.focus(), 0);
    else if (wasOpen.current) trigger.current?.focus();
    wasOpen.current = open;
  }, [open]);

  const matches = useMemo(() => {
    const value = query.trim().toLowerCase();
    return navigation
      .filter((item) => !value || item.label.toLowerCase().includes(value))
      .slice(0, 10);
  }, [navigation, query]);

  function moveSelection(direction: 1 | -1) {
    const controls = [
      ...(dialog.current?.querySelectorAll<HTMLButtonElement>(
        ".rti-command-palette__results button",
      ) ?? []),
    ];
    if (!controls.length) return;
    const current = controls.indexOf(
      document.activeElement as HTMLButtonElement,
    );
    controls[
      current < 0
        ? direction === 1
          ? 0
          : controls.length - 1
        : (current + direction + controls.length) % controls.length
    ].focus();
  }

  return (
    <>
      <button
        ref={trigger}
        className="rti-command-icon"
        type="button"
        aria-label="Open command palette"
        onClick={() => setOpen(true)}
      >
        <Command size={17} aria-hidden="true" />
        <kbd>⌘K</kbd>
      </button>
      {open ? (
        <div className="rti-command-palette__scrim" role="presentation">
          <section
            ref={dialog}
            className="rti-command-palette"
            role="dialog"
            aria-modal="true"
            aria-labelledby="rti-command-palette-title"
            onKeyDown={(event) => {
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                moveSelection(event.key === "ArrowDown" ? 1 : -1);
                return;
              }
              if (event.key !== "Tab") return;
              const controls = [
                ...(dialog.current?.querySelectorAll<HTMLElement>(
                  'button:not(:disabled), input:not(:disabled), [tabindex]:not([tabindex="-1"])',
                ) ?? []),
              ];
              if (!controls.length) return;
              const first = controls[0];
              const last = controls.at(-1)!;
              if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
              } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
              }
            }}
          >
            <header>
              <Search size={18} aria-hidden="true" />
              <h2 id="rti-command-palette-title">Go anywhere</h2>
              <button
                type="button"
                aria-label="Close command palette"
                onClick={() => setOpen(false)}
              >
                <X size={17} aria-hidden="true" />
              </button>
            </header>
            <input
              ref={input}
              type="search"
              value={query}
              aria-label="Search commands and pages"
              placeholder="Search pages and actions…"
              onChange={(event) => setQuery(event.target.value)}
            />
            <div className="rti-command-palette__results">
              <span>Navigate</span>
              {matches.map((item) => (
                <button
                  type="button"
                  key={item.id}
                  onClick={() => {
                    item.onSelect?.();
                    setOpen(false);
                    setQuery("");
                  }}
                >
                  <span>{item.label}</span>
                  <small>{item.group.replace("-", " ")}</small>
                </button>
              ))}
              {!matches.length ? <p>No matching workspace found.</p> : null}
            </div>
          </section>
        </div>
      ) : null}
    </>
  );
}
