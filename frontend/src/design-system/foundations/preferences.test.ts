import { describe, expect, it } from "vitest";

import {
  readPresentationPreferences,
  writePresentationPreferences,
} from "./preferences";

class MapStorage {
  private values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }
}

class ThrowingStorage extends MapStorage {
  setItem(): void {
    throw new Error("storage unavailable");
  }
}

describe("presentation preferences", () => {
  it("defaults to adaptive density", () => {
    expect(readPresentationPreferences(new MapStorage())).toEqual({
      density: "adaptive",
    });
  });

  it("round-trips only supported density values", () => {
    const storage = new MapStorage();

    writePresentationPreferences({ density: "compact" }, storage);

    expect(readPresentationPreferences(storage)).toEqual({
      density: "compact",
    });
  });

  it("ignores legacy direction values while retaining a valid density", () => {
    const storage = new MapStorage();
    storage.setItem(
      "rti:presentation-preferences",
      '{"direction":"atlas","density":"comfortable"}',
    );

    expect(readPresentationPreferences(storage)).toEqual({
      density: "comfortable",
    });
  });

  it("namespaces preferences by principal and tolerates unavailable storage", () => {
    const storage = new MapStorage();
    writePresentationPreferences(
      { density: "comfortable" },
      storage,
      "principal-1",
    );

    expect(readPresentationPreferences(storage, "principal-1").density).toBe(
      "comfortable",
    );
    expect(readPresentationPreferences(storage, "principal-2").density).toBe(
      "adaptive",
    );
    expect(() =>
      writePresentationPreferences(
        { density: "adaptive" },
        new ThrowingStorage(),
        "principal-1",
      ),
    ).not.toThrow();
  });
});
