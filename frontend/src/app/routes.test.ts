import { describe, expect, it } from "vitest";

import { routeForHash, routeManifest } from "./routes";

describe("routeManifest", () => {
  it("keeps every current deep link and adds Home", () => {
    expect(routeForHash("#/service-desk-settings", false).id).toBe(
      "service-desk-settings",
    );
    expect(routeForHash("#/sales?workRecordID=record-1", false).id).toBe(
      "sales",
    );
    expect(routeForHash("#/home", false).id).toBe("home");
    expect(
      routeForHash(
        "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1",
        false,
      ).id,
    ).toBe("work");
    expect(
      routeForHash(
        "#/project?parentID=project-1&mentionOccurrenceID=occurrence-2",
        false,
      ).id,
    ).toBe("project");
    expect(routeForHash("#/sales/opportunities/opportunity-1", false).id).toBe(
      "sales",
    );
    expect(new Set(routeManifest.map((route) => route.id)).size).toBe(
      routeManifest.length,
    );
  });

  it("declares the presentation metadata every product route needs", () => {
    for (const route of routeManifest) {
      expect(route.roles.length).toBeGreaterThan(0);
      expect(["compact", "comfortable"]).toContain(route.density);
      expect(route.family).toBeTruthy();
      expect(typeof route.mobile).toBe("boolean");
    }
  });

  it("keeps the catalog behind its explicit availability boundary", () => {
    expect(routeForHash("#/design-system", false).id).toBe("home");
    expect(routeForHash("#/design-system", true).id).toBe("design-system");
  });
});
