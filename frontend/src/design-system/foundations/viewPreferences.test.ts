import { beforeEach, describe, expect, it } from "vitest";

import { readViewPreference, writeViewPreference } from "./viewPreferences";

describe("route view preferences", () => {
  beforeEach(() => localStorage.clear());

  it("stores a supported view per principal and route", () => {
    writeViewPreference(localStorage, "principal-1", "project", "kanban");

    expect(readViewPreference(localStorage, "principal-1", "project")).toBe(
      "kanban",
    );
    expect(readViewPreference(localStorage, "principal-2", "project")).toBe(
      "list",
    );
    expect(readViewPreference(localStorage, "principal-1", "work")).toBe(
      "list",
    );
  });

  it("falls back to list for corrupt or unsupported values", () => {
    localStorage.setItem("rti:view:principal-1:work", '"timeline"');
    expect(readViewPreference(localStorage, "principal-1", "work")).toBe(
      "list",
    );
    localStorage.setItem("rti:view:principal-1:work", "{");
    expect(readViewPreference(localStorage, "principal-1", "work")).toBe(
      "list",
    );
  });
});
