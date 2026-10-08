function quotedAttributes(startTag) {
  return [
    ...startTag.matchAll(
      /\s+([A-Za-z_:][A-Za-z0-9:._-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')/gu,
    ),
  ].map((match) => ({
    name: match[1],
    value: match[2] ?? match[3],
  }));
}

export function releaseAcceptanceDocumentationErrors(siteIndex) {
  const uncommented = siteIndex.replace(/<!--[\s\S]*?(?:-->|$)/gu, "");
  const signedReleaseArtifactGates = [...uncommented.matchAll(/<li\b[^>]*>/giu)]
    .map((match) => quotedAttributes(match[0]))
    .filter((attributes) =>
      attributes.some(
        ({ name, value }) =>
          name === "data-gate-id" && value === "signed-release-artifacts",
      ),
    );

  if (signedReleaseArtifactGates.length !== 1) {
    return [
      `expected exactly one signed release artifact gate, found ${signedReleaseArtifactGates.length}`,
    ];
  }

  const gateStates = signedReleaseArtifactGates[0]
    .filter(({ name }) => name === "data-gate-state")
    .map(({ value }) => value);

  return [
    gateStates.length !== 1 || gateStates[0] !== "complete"
      ? "signed release artifact gate is not recorded as complete"
      : "",
  ].filter(Boolean);
}
