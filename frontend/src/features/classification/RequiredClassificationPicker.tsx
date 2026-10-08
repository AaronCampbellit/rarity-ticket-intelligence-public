import { useEffect, type RefObject } from "react";

import { TagPicker } from "../../design-system";
import { useClientClassificationCatalog } from "./useClientCatalog";
import type { Tag } from "./types";

/** The fallback is a server-owned migration marker, never a user classification. */
export function meaningfulClassificationTags(tags: Tag[]) {
  return tags.filter(
    (tag) =>
      tag.state === "active" && !tag.systemFallback && !tag.systemManaged,
  );
}

export function RequiredClassificationPicker({
  clientID,
  selectedIDs,
  onChange,
  inputRef,
  error,
}: {
  clientID: string;
  selectedIDs: string[];
  onChange(ids: string[]): void;
  inputRef?: RefObject<HTMLInputElement | null>;
  error?: string;
}) {
  const { catalog, state } = useClientClassificationCatalog(clientID);
  const meaningfulTags = catalog && meaningfulClassificationTags(catalog.tags);
  useEffect(() => {
    if (!meaningfulTags) return;
    const valid = selectedIDs.filter((id) =>
      meaningfulTags.some((tag) => tag.id === id),
    );
    if (valid.length !== selectedIDs.length) onChange(valid);
  }, [meaningfulTags, onChange, selectedIDs]);
  if (!catalog) {
    return (
      <p role="status">
        {state === "error"
          ? "Classification tags are unavailable."
          : "Loading classification tags…"}
      </p>
    );
  }
  return (
    <TagPicker
      label="Classification tags"
      groups={catalog.groups}
      tags={meaningfulTags ?? []}
      selectedIds={selectedIDs}
      required
      error={error}
      inputRef={inputRef}
      onChange={onChange}
    />
  );
}
