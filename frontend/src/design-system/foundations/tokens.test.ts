import { describe, expect, it } from "vitest";

import "./tokens.css";
import tokenSource from "./tokens.css?raw";

function relativeLuminance(color: string) {
  const channels = color
    .slice(1)
    .match(/../g)!
    .map((value) => Number.parseInt(value, 16) / 255)
    .map((value) =>
      value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4,
    );
  return 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!;
}

function contrast(first: string, second: string) {
  const [light, dark] = [
    relativeLuminance(first),
    relativeLuminance(second),
  ].sort((left, right) => right - left);
  return (light! + 0.05) / (dark! + 0.05);
}

const styleSources = import.meta.glob("../**/*.css", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

describe("semantic token contract", () => {
  it.each([
    "--rti-surface-canvas",
    "--rti-surface-panel",
    "--rti-text-primary",
    "--rti-text-muted",
    "--rti-border-default",
    "--rti-action-primary",
    "--rti-action-danger",
    "--rti-status-success",
    "--rti-status-warning",
    "--rti-status-danger",
    "--rti-focus-ring",
    "--rti-shadow-overlay",
  ])("defines %s", (token) => {
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(token),
    ).not.toBe("");
  });

  it("exposes compact and comfortable density", () => {
    document.documentElement.dataset.density = "compact";
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--rti-density-control-height",
      ),
    ).toContain("--rti-control-height-compact");

    document.documentElement.dataset.density = "comfortable";
    expect(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--rti-density-control-height",
      ),
    ).toContain("--rti-control-height-default");

    delete document.documentElement.dataset.density;
  });

  it("uses the Signal dark console palette as the product-wide visual contract", () => {
    const styles = getComputedStyle(document.documentElement);

    expect(styles.getPropertyValue("--rti-surface-canvas").trim()).toBe(
      "#141619",
    );
    expect(styles.getPropertyValue("--rti-surface-navigation").trim()).toBe(
      "#181b20",
    );
    expect(styles.getPropertyValue("--rti-surface-header").trim()).toBe(
      "#181b20",
    );
    expect(styles.getPropertyValue("--rti-surface-panel").trim()).toBe(
      "#1b1e23",
    );
    expect(styles.getPropertyValue("--rti-text-primary").trim()).toBe(
      "#e9edf2",
    );
    expect(
      contrast(
        styles.getPropertyValue("--rti-action-primary").trim(),
        styles.getPropertyValue("--rti-text-inverse").trim(),
      ),
    ).toBeGreaterThanOrEqual(4.5);

    expect(
      contrast(
        styles.getPropertyValue("--rti-text-danger").trim(),
        styles.getPropertyValue("--rti-surface-panel").trim(),
      ),
    ).toBeGreaterThanOrEqual(4.5);
    expect(styles.colorScheme).toBe("dark");
  });

  it("provides the Signal elevation values globally", () => {
    const styles = getComputedStyle(document.documentElement);
    expect(styles.getPropertyValue("--rti-shadow-raised").trim()).not.toBe("");
    expect(styles.getPropertyValue("--rti-shadow-floating").trim()).not.toBe(
      "",
    );
  });

  it("defines every semantic token consumed by the design system", () => {
    const consumed = new Set(
      Object.values(styleSources).flatMap((source) =>
        [...source.matchAll(/var\((--rti-[a-zA-Z0-9-]+)/g)].map(
          (match) => match[1],
        ),
      ),
    );
    const defined = new Set(
      [...tokenSource.matchAll(/(--rti-[a-zA-Z0-9-]+)\s*:/g)].map(
        (match) => match[1],
      ),
    );

    expect([...consumed].filter((token) => !defined.has(token))).toEqual([]);
  });
});
