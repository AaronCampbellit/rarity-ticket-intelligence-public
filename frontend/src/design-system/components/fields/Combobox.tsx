import { Check, ChevronDown, X } from "lucide-react";
import {
  Fragment,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
} from "react";

export type ComboboxOption = {
  value: string;
  label: string;
  description?: string;
  group?: string;
  disabled?: boolean;
};

export function Combobox({
  label,
  options,
  value,
  onChange,
  placeholder = "Search or choose…",
  disabled = false,
  query: controlledQuery,
  onQueryChange,
  onEscape,
  inputRef,
  autoFocus = false,
  closeOnSelect = true,
  filterOptions = true,
  alwaysOpen = false,
  updateQueryOnSelect = true,
}: {
  label: string;
  options: ComboboxOption[];
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  query?: string;
  onQueryChange?: (query: string) => void;
  onEscape?: () => void;
  inputRef?: RefObject<HTMLInputElement | null>;
  autoFocus?: boolean;
  closeOnSelect?: boolean;
  filterOptions?: boolean;
  alwaysOpen?: boolean;
  updateQueryOnSelect?: boolean;
}) {
  const listID = useId();
  const root = useRef<HTMLDivElement>(null);
  const selected = options.find((option) => option.value === value);
  const [internalQuery, setInternalQuery] = useState(selected?.label ?? "");
  const query = controlledQuery ?? internalQuery;
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const matches = useMemo(() => {
    const search = query.trim().toLowerCase();
    if (!filterOptions) return options;
    return options.filter(
      (option) =>
        !search ||
        option.label.toLowerCase().includes(search) ||
        option.description?.toLowerCase().includes(search),
    );
  }, [filterOptions, options, query]);
  const optionsIdentity = JSON.stringify(
    options.map((option) => [option.value, Boolean(option.disabled)]),
  );
  const expanded = alwaysOpen || open;
  const activeOption = active >= 0 ? matches[active] : undefined;

  useLayoutEffect(() => setActive(-1), [optionsIdentity]);

  function updateQuery(next: string) {
    setInternalQuery(next);
    onQueryChange?.(next);
  }

  function choose(option: ComboboxOption) {
    if (option.disabled) return;
    onChange(option.value);
    if (updateQueryOnSelect) updateQuery(option.label);
    if (closeOnSelect) setOpen(false);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      setOpen(false);
      updateQuery(selected?.label ?? "");
      onEscape?.();
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActive((current) =>
        matches.length === 0 ? -1 : Math.min(current + 1, matches.length - 1),
      );
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((current) =>
        matches.length === 0 ? -1 : Math.max(current - 1, 0),
      );
    } else if (event.key === "Enter" && expanded && activeOption) {
      event.preventDefault();
      choose(activeOption);
    }
  }

  function optionElement(option: ComboboxOption) {
    const index = matches.indexOf(option);
    return (
      <button
        type="button"
        role="option"
        tabIndex={-1}
        id={`${listID}-option-${index}`}
        aria-selected={option.value === value}
        disabled={option.disabled}
        data-active={index === active}
        key={option.value}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => choose(option)}
      >
        <span>
          <strong>{option.label}</strong>
          {option.description ? <small>{option.description}</small> : null}
        </span>
        {option.value === value ? <Check size={16} aria-hidden="true" /> : null}
      </button>
    );
  }

  const groups = Array.from(new Set(matches.map((option) => option.group)));

  return (
    <div
      ref={root}
      className="rti-combobox"
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <label>
        <span className="rti-field__label">{label}</span>
        <span className="rti-combobox__control">
          <input
            ref={inputRef}
            className="rti-input"
            role="combobox"
            aria-label={label}
            aria-expanded={expanded && matches.length > 0}
            aria-controls={expanded && matches.length > 0 ? listID : undefined}
            aria-activedescendant={
              expanded && activeOption
                ? `${listID}-option-${active}`
                : undefined
            }
            aria-autocomplete="list"
            disabled={disabled}
            autoFocus={autoFocus}
            placeholder={placeholder}
            value={query}
            onFocus={() => setOpen(true)}
            onKeyDown={onKeyDown}
            onChange={(event) => {
              updateQuery(event.target.value);
              setActive(-1);
              setOpen(true);
            }}
          />
          {value ? (
            <button
              type="button"
              aria-label={`Clear ${label}`}
              onClick={() => {
                onChange("");
                updateQuery("");
              }}
            >
              <X size={15} aria-hidden="true" />
            </button>
          ) : (
            <ChevronDown size={16} aria-hidden="true" />
          )}
        </span>
      </label>
      {expanded && matches.length > 0 ? (
        <div className="rti-option-list" id={listID} role="listbox">
          {groups.map((group) => {
            const children = matches
              .filter((option) => option.group === group)
              .map(optionElement);
            return group ? (
              <div key={group} role="group" aria-label={group}>
                <p>{group}</p>
                {children}
              </div>
            ) : (
              <Fragment key="ungrouped">{children}</Fragment>
            );
          })}
        </div>
      ) : expanded ? (
        <p>No matching options</p>
      ) : null}
    </div>
  );
}
