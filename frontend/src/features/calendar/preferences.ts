export function validTimezone(value: unknown): value is string {
  if (typeof value !== "string" || !value || value.length > 100) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: value }).format();
    return true;
  } catch {
    return false;
  }
}
export function readCalendarPreferences(
  storage: Pick<Storage, "getItem">,
  principalID: string,
) {
  try {
    const value = JSON.parse(
      storage.getItem(`rarity:calendar-preferences:${principalID}`) ?? "null",
    );
    return {
      timezone: validTimezone(value?.timezone) ? value.timezone : "UTC",
    };
  } catch {
    return { timezone: "UTC" };
  }
}
export function writeCalendarPreferences(
  storage: Pick<Storage, "setItem">,
  principalID: string,
  value: { timezone: string },
) {
  if (!principalID) return;
  try {
    storage.setItem(
      `rarity:calendar-preferences:${principalID}`,
      JSON.stringify({
        timezone: validTimezone(value.timezone) ? value.timezone : "UTC",
      }),
    );
  } catch {
    /* Storage is optional. */
  }
}
