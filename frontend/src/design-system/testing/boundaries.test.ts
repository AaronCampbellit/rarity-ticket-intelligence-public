import { describe, expect, it } from "vitest";

const featureSources = import.meta.glob("../../features/**/*.{css,ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

function projectPath(path: string): string {
  return path.replace("../../", "src/");
}

describe("design-system feature boundaries", () => {
  it("requires features to use the public design-system entry point", () => {
    const violations = Object.entries(featureSources).filter(([, source]) =>
      /from\s+["'][^"']*design-system\/(components|patterns|templates|foundations)/.test(
        source,
      ),
    );
    expect(violations).toEqual([]);
  });

  it("does not allow product icons to be implemented as inline SVG", () => {
    const violations = Object.entries(featureSources).filter(
      ([path, source]) => path.endsWith(".tsx") && /<svg\b/.test(source),
    );
    expect(violations).toEqual([]);
  });

  it("requires feature CSS to consume semantic color tokens", () => {
    const violations = Object.entries(featureSources)
      .filter(
        ([path, source]) =>
          path.endsWith(".css") && /#[0-9a-fA-F]{3,8}\b/.test(source),
      )
      .map(([path]) => projectPath(path));

    expect(violations).toEqual([]);
  });

  it("does not let feature CSS restore a light-only canvas", () => {
    const violations = Object.entries(featureSources)
      .filter(
        ([path, source]) =>
          path.endsWith(".css") &&
          /(?:background|background-color)\s*:\s*(?:#fff(?:fff)?|white)\b/i.test(
            source,
          ),
      )
      .map(([path]) => projectPath(path));

    expect(violations).toEqual([]);
  });
});
