import { Check, Search } from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type RefObject,
  type KeyboardEvent,
} from "react";

import { TagChip } from "../data/TagChip";
import type {
  Tag,
  TagAssignment,
  TagGroup,
  TagSuggestion,
} from "../../../features/classification/types";
import "./fields.css";

export type TagPickerProps = {
  label: string;
  groups: TagGroup[];
  tags: Tag[];
  selectedIds: string[];
  selectedAssignments?: Pick<TagAssignment, "tag" | "source">[];
  inheritedIds?: string[];
  recentIds?: string[];
  frequentIds?: string[];
  suggestions?: TagSuggestion[];
  required?: boolean;
  disabled?: boolean;
  error?: string;
  inputRef?: RefObject<HTMLInputElement | null>;
  onChange(ids: string[]): void;
  onSuggestionDecision?(id: string, decision: "accept" | "dismiss"): void;
};

export function TagPicker({
  label,
  groups,
  tags,
  selectedIds,
  selectedAssignments = [],
  inheritedIds = [],
  recentIds = [],
  frequentIds = [],
  suggestions = [],
  required = false,
  disabled = false,
  error,
  inputRef,
  onChange,
  onSuggestionDecision,
}: TagPickerProps) {
  const inputID = useId();
  const listID = useId();
  const errorID = useId();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [announcement, setAnnouncement] = useState({ id: 0, message: "" });
  const [decidedSuggestionIDs, setDecidedSuggestionIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const root = useRef<HTMLDivElement>(null);
  const direct = selectedIds.flatMap((id) => {
    const tag = tags.find((candidate) => candidate.id === id);
    if (!tag) return [];
    return [
      {
        tag,
        source:
          selectedAssignments.find((assignment) => assignment.tag.id === id)
            ?.source ?? "human",
      },
    ];
  });
  const inherited = tags.filter((tag) => inheritedIds.includes(tag.id));
  const available = useMemo(() => {
    const term = query.trim().toLocaleLowerCase();
    return tags.filter(
      (tag) =>
        tag.state === "active" &&
        (!term ||
          tag.label.toLocaleLowerCase().includes(term) ||
          tag.synonyms.some((synonym) =>
            synonym.toLocaleLowerCase().includes(term),
          )),
    );
  }, [query, tags]);
  const recent = recentIds
    .map((id) => available.find((tag) => tag.id === id))
    .filter((tag): tag is Tag => Boolean(tag));
  const frequent = frequentIds
    .map((id) => available.find((tag) => tag.id === id))
    .filter((tag): tag is Tag => Boolean(tag))
    .filter((tag) => !recent.some((recentTag) => recentTag.id === tag.id));
  const featuredIDs = new Set([...recent, ...frequent].map((tag) => tag.id));
  const grouped = groups
    .filter((group) => group.state !== "archived")
    .map((group) => ({
      group,
      tags: available.filter(
        (tag) => tag.groupId === group.id && !featuredIDs.has(tag.id),
      ),
    }))
    .filter(({ tags: groupTags }) => groupTags.length > 0);
  const ordered = [
    ...recent,
    ...frequent,
    ...grouped.flatMap(({ tags }) => tags),
  ];
  const optionIndexes = new Map(ordered.map((tag, index) => [tag.id, index]));
  const suggested = suggestions.reduce<
    { suggestion: TagSuggestion; tag: Tag }[]
  >((items, suggestion) => {
    const suggestedTag = tags.find((tag) => tag.id === suggestion.tagId);
    if (
      suggestedTag?.state === "active" &&
      !decidedSuggestionIDs.has(suggestion.id)
    )
      items.push({ suggestion, tag: suggestedTag });
    return items;
  }, []);

  useEffect(() => {
    setActive(-1);
    if (open) {
      setAnnouncement((previous) => ({
        id: previous.id + 1,
        message: `${ordered.length} matching tags.`,
      }));
    }
  }, [open, query, ordered.length]);

  useEffect(() => {
    if (active < 0) return;
    const activeOption = document.getElementById(`${listID}-option-${active}`);
    if (typeof activeOption?.scrollIntoView === "function") {
      activeOption.scrollIntoView({ block: "nearest" });
    }
  }, [active, listID]);

  function announce(message: string) {
    setAnnouncement((previous) => ({ id: previous.id + 1, message }));
  }

  function select(tag: Tag) {
    const selected = selectedIds.includes(tag.id);
    const next = selected
      ? selectedIds.filter((id) => id !== tag.id)
      : [...selectedIds, tag.id];
    onChange(next);
    announce(`${tag.label} ${selected ? "removed" : "selected"}.`);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") setOpen(false);
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActive((current) => Math.min(current + 1, ordered.length - 1));
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      setActive((current) =>
        current < 0 ? ordered.length - 1 : Math.max(current - 1, 0),
      );
    }
    if (event.key === "Enter" && open && ordered[active]) {
      event.preventDefault();
      select(ordered[active]);
    }
    if (event.key === "Backspace" && !query && selectedIds.length) {
      const last = selectedIds[selectedIds.length - 1];
      const tag = tags.find((candidate) => candidate.id === last);
      onChange(selectedIds.slice(0, -1));
      announce(`${tag?.label ?? "Tag"} removed.`);
    }
  }

  function option(tag: Tag) {
    const selected = selectedIds.includes(tag.id);
    const index = optionIndexes.get(tag.id) ?? -1;
    return (
      <button
        key={tag.id}
        id={`${listID}-option-${index}`}
        type="button"
        tabIndex={-1}
        role="option"
        aria-selected={selected}
        data-active={active === index}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => select(tag)}
      >
        <span>
          <strong>{tag.label}</strong>
          {tag.synonyms.length ? (
            <small>{tag.synonyms.join(", ")}</small>
          ) : null}
        </span>
        {selected ? <Check size={16} aria-hidden="true" /> : null}
      </button>
    );
  }

  return (
    <div
      className="rti-tag-picker"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <label className="rti-field__label" htmlFor={inputID}>
        <span>{label}</span>
        {required ? (
          <span className="rti-field__required">Required</span>
        ) : null}
      </label>
      <div
        className="rti-tag-picker__chips"
        role="group"
        aria-label={`${label} selections`}
      >
        {direct.map(({ tag, source }) => (
          <TagChip
            key={tag.id}
            tag={tag}
            source={source}
            onRemove={disabled ? undefined : () => select(tag)}
          />
        ))}
        {inherited.map((tag) => (
          <TagChip key={`inherited-${tag.id}`} tag={tag} inherited />
        ))}
      </div>
      <div className="rti-tag-picker__control">
        <Search size={16} aria-hidden="true" />
        <input
          id={inputID}
          ref={inputRef}
          className="rti-input"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={listID}
          aria-activedescendant={
            open && active >= 0 ? `${listID}-option-${active}` : undefined
          }
          aria-describedby={error ? errorID : undefined}
          aria-invalid={error ? "true" : undefined}
          aria-required={required || undefined}
          placeholder="Search tags by label or synonym"
          value={query}
          disabled={disabled}
          onFocus={() => setOpen(true)}
          onKeyDown={onKeyDown}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
        />
      </div>
      <p className="sr-only" aria-live="polite">
        <span key={announcement.id}>{announcement.message}</span>
      </p>
      {error ? (
        <p id={errorID} className="rti-field__error" role="alert">
          {error}
        </p>
      ) : null}
      {suggested.length ? (
        <section
          className="rti-tag-picker__suggestions"
          aria-label="AI tag suggestions"
        >
          <strong>Suggested</strong>
          {suggested.map(({ suggestion, tag }) => (
            <span key={suggestion.id}>
              <TagChip tag={tag} source="ai_suggestion_pending" />
              <button
                type="button"
                disabled={disabled}
                aria-label={`Accept suggestion ${tag.label}`}
                onClick={() => {
                  onSuggestionDecision?.(suggestion.id, "accept");
                  setDecidedSuggestionIDs((ids) =>
                    new Set(ids).add(suggestion.id),
                  );
                  announce(`${tag.label} suggestion accepted.`);
                }}
              >
                Accept
              </button>
              <button
                type="button"
                disabled={disabled}
                aria-label={`Dismiss suggestion ${tag.label}`}
                onClick={() => {
                  onSuggestionDecision?.(suggestion.id, "dismiss");
                  setDecidedSuggestionIDs((ids) =>
                    new Set(ids).add(suggestion.id),
                  );
                  announce(`${tag.label} suggestion dismissed.`);
                }}
              >
                Dismiss
              </button>
            </span>
          ))}
        </section>
      ) : null}
      {open ? (
        <div
          id={listID}
          role="listbox"
          aria-label={`${label} results`}
          aria-multiselectable="true"
          className="rti-tag-picker__list"
        >
          {recent.length ? (
            <div role="group" aria-label="Recent">
              <p>Recent</p>
              {recent.map(option)}
            </div>
          ) : null}
          {frequent.length ? (
            <div role="group" aria-label="Frequent">
              <p>Frequent</p>
              {frequent.map(option)}
            </div>
          ) : null}
          {grouped.map(({ group, tags: groupTags }) => (
            <div key={group.id} role="group" aria-label={group.label}>
              <p>{group.label}</p>
              {groupTags.map(option)}
            </div>
          ))}
          {!ordered.length ? (
            <p className="rti-tag-picker__empty">No matching tags.</p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
