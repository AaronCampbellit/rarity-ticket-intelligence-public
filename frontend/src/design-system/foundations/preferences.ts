export type DensityPreference = "adaptive" | "compact" | "comfortable";

export type PresentationPreferences = {
  density: DensityPreference;
};

type PreferenceStorage = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
};

function preferenceKey(principalID?: string) {
  return principalID
    ? `rti:presentation-preferences:${encodeURIComponent(principalID)}`
    : "rti:presentation-preferences";
}
const defaults: PresentationPreferences = {
  density: "adaptive",
};

export function readPresentationPreferences(
  storage: PreferenceStorage = window.localStorage,
  principalID?: string,
): PresentationPreferences {
  try {
    const parsed = JSON.parse(
      storage.getItem(preferenceKey(principalID)) ?? "{}",
    ) as { density?: unknown };
    if (
      parsed.density === "adaptive" ||
      parsed.density === "compact" ||
      parsed.density === "comfortable"
    ) {
      return { density: parsed.density };
    }
  } catch {
    // Preferences are optional; a corrupt or unavailable store uses defaults.
  }
  return defaults;
}

export function writePresentationPreferences(
  preferences: PresentationPreferences,
  storage: PreferenceStorage = window.localStorage,
  principalID?: string,
): void {
  try {
    storage.setItem(
      preferenceKey(principalID),
      JSON.stringify({ density: preferences.density }),
    );
  } catch {
    // Presentation preferences are optional and must never block the shell.
  }
}
