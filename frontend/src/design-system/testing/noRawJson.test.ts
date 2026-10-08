import { describe, expect, it } from "vitest";

const userFacingSources = import.meta.glob(
  ["/src/**/*.tsx", "!/src/**/*.test.tsx"],
  {
    eager: true,
    import: "default",
    query: "?raw",
  },
) as Record<string, string>;

const apiOnlyJsonLine =
  /response\.json|Response\.json|JSON\.(?:parse|stringify)|application\/json/i;

describe("user-facing structured data boundary", () => {
  it("keeps implementation JSON out of visible feature surfaces", () => {
    const visibleJsonReferences = Object.entries(userFacingSources).flatMap(
      ([file, source]) =>
        source
          .split("\n")
          .map((line, index) => ({ file, line, lineNumber: index + 1 }))
          .filter(
            ({ line }) => /\bjson\b/i.test(line) && !apiOnlyJsonLine.test(line),
          ),
    );

    expect(visibleJsonReferences).toEqual([]);
  });

  it("does not regress to raw configuration text areas", () => {
    const rawEditors = Object.entries(userFacingSources).filter(([, source]) =>
      /<textarea[^>]+name=["'](?:steps|conditions|definition|destinations|intake|object_storage|backups|capabilities|data_scopes|custom_fields|contact_ids|pause_states|event_types|allowed_sender_domains|required_fields|recovery_allowed_cidrs|allowedCIDRs)["']/i.test(
        source,
      ),
    );

    expect(rawEditors.map(([file]) => file)).toEqual([]);
  });

  it("does not render serialized payloads or raw JSON prompts", () => {
    const violations = Object.entries(userFacingSources).filter(
      ([, source]) =>
        /<pre[^>]*>\s*\{?\s*JSON\.stringify/i.test(source) ||
        /(?:raw JSON|JSON configuration|paste JSON)/i.test(source),
    );

    expect(violations.map(([file]) => file)).toEqual([]);
  });
});
