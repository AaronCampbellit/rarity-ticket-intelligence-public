import { describe, expect, it } from "vitest";

import {
  isRecognizedProtectedHash,
  mentionNavigationFromHash,
  navigationGroup,
  pageFromHash,
} from "./navigation";

describe("application hashes", () => {
  it("recognizes login as a public page", () => {
    expect(pageFromHash("#/login", false)).toBe("login");
  });

  it.each([
    ["#/sales?workRecordID=abc", true],
    ["#/timesheets", true],
    ["#/design-system", false],
    ["#/login", false],
    ["#/break-glass", false],
    ["#/setup", false],
    ["#/unknown", false],
    ["https://evil.example", false],
  ] as const)("classifies %s as protected=%s", (hash, expected) => {
    expect(isRecognizedProtectedHash(hash, false)).toBe(expected);
  });

  it("parses only bounded exact mention workspace hashes", () => {
    expect(
      mentionNavigationFromHash(
        "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
      ),
    ).toEqual({
      routeID: "work",
      parentID: "work-1",
      mentionOccurrenceID: "occurrence-1",
      sourceID: "source-1",
    });
    expect(
      mentionNavigationFromHash(
        "#/project?parentID=project-1&mentionOccurrenceID=occurrence-2",
      ),
    ).toEqual({
      routeID: "project",
      parentID: "project-1",
      mentionOccurrenceID: "occurrence-2",
    });
    expect(mentionNavigationFromHash("#/work?parentID=work-1")).toBeUndefined();
    expect(
      mentionNavigationFromHash(
        "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&clientID=forged",
      ),
    ).toBeUndefined();
    expect(
      mentionNavigationFromHash(
        `#/work?parentID=${"x".repeat(129)}&mentionOccurrenceID=occurrence-1`,
      ),
    ).toBeUndefined();
  });
});

describe("navigationGroup", () => {
  it("places timesheets with technician work", () => {
    expect(navigationGroup("timesheets")).toBe("work");
  });

  it.each([
    ["sessions", "admin-access"],
    ["recovery-access", "admin-access"],
    ["role-settings", "admin-access"],
    ["audit", "admin-access"],
    ["setup", "admin-platform"],
    ["service-keys", "admin-platform"],
    ["billing", "admin-platform"],
    ["service-desk-settings", "admin-platform"],
    ["pipeline-settings", "admin-platform"],
    ["teams-settings", "admin-integrations"],
    ["datto-settings", "admin-integrations"],
    ["graph-settings", "admin-integrations"],
    ["webhooks", "admin-integrations"],
    ["automation", "admin-integrations"],
  ] as const)("places %s in %s", (page, group) => {
    expect(navigationGroup(page)).toBe(group);
  });

  it.each([
    ["ai-assist", "admin-operations"],
    ["ai-settings", "admin-operations"],
    ["operations", "admin-operations"],
    ["datto-reconciliation", "admin-operations"],
    ["forwarding-settings", "admin-operations"],
  ] as const)("places %s in %s", (page, group) => {
    expect(navigationGroup(page)).toBe(group);
  });

  it.each([
    ["knowledge", "organization"],
    ["directory-settings", "organization"],
    ["client-resources", "organization"],
  ] as const)("promotes %s to %s", (page, group) => {
    expect(navigationGroup(page)).toBe(group);
  });
});
