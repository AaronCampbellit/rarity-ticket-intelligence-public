import { useEffect, useRef, useState } from "react";

import { TagChip } from "../../design-system";
import { createClassificationAPI } from "./api";
import type { TaggedObject, TagTarget } from "./types";
import "./classification.css";

/** Compact read-only effective tags for worklists and converted records. */
export function ObjectTagSummary({
  clientID,
  target,
}: {
  clientID: string;
  target: TagTarget;
}) {
  const targetIdentity = `${clientID}:${target.objectType}:${target.objectId}`;
  const [loaded, setLoaded] = useState<
    { targetIdentity: string; tagged: TaggedObject } | undefined
  >();
  const generation = useRef(0);

  useEffect(() => {
    const controller = new AbortController();
    const requestGeneration = ++generation.current;
    setLoaded(undefined);
    void createClassificationAPI(globalThis.fetch, clientID)
      .object(target, controller.signal)
      .then((item) => {
        if (
          !controller.signal.aborted &&
          requestGeneration === generation.current
        )
          setLoaded({ targetIdentity, tagged: item });
      })
      .catch(() => {
        if (
          !controller.signal.aborted &&
          requestGeneration === generation.current
        )
          setLoaded(undefined);
      });
    return () => {
      controller.abort();
      generation.current += 1;
    };
  }, [clientID, target.objectId, target.objectType, targetIdentity]);

  if (!loaded || loaded.targetIdentity !== targetIdentity) return null;
  const tagged = loaded.tagged;
  return (
    <span className="rti-object-tag-summary" aria-label="Classification tags">
      {tagged.effective
        .filter(
          (assignment) =>
            tagged.classificationState !== "unclassified" ||
            assignment.source !== "system_fallback",
        )
        .map((assignment) => (
          <TagChip
            key={`${assignment.inherited ? "inherited" : "direct"}-${assignment.tag.id}`}
            tag={assignment.tag}
            source={assignment.source}
            inherited={assignment.inherited}
          />
        ))}
      {tagged.classificationState === "unclassified" ? (
        <strong className="rti-classification-fallback">
          Unclassified — needs review
        </strong>
      ) : null}
    </span>
  );
}
