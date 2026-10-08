import { describe, expect, it } from "vitest";

import { routeManifest } from "../../app/routes";
import appSource from "../../App.tsx?raw";
import { assertRouteCoverage } from "./routeCoverage";

function renderedAuthenticatedRoutes() {
  return new Set(
    [...appSource.matchAll(/page\s*===\s*["']([^"']+)["']/g)].map(
      (match) => match[1],
    ),
  );
}

describe("authenticated route coverage", () => {
  it("derives migration coverage from the actual authenticated renderer", () => {
    expect(assertRouteCoverage(renderedAuthenticatedRoutes())).toEqual([]);
  });

  it("keeps phone scope limited to essential field work and responsive administration", () => {
    expect(
      routeManifest
        .filter(({ protected: guarded, mobile }) => guarded && mobile)
        .map(({ id }) => id),
    ).toEqual([
      "home",
      "work",
      "timesheets",
      "knowledge",
      "ai-assist",
      "classification-settings",
      "billing",
    ]);
  });
});
