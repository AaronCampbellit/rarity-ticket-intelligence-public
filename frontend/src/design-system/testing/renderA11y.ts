import type { RenderResult } from "@testing-library/react";
import axe from "axe-core";

export async function findA11yViolations(container: RenderResult["container"]) {
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
  });

  return result.violations;
}
