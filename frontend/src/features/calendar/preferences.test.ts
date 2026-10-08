import { expect, it } from "vitest";
import {
  readCalendarPreferences,
  writeCalendarPreferences,
} from "./preferences";
import { calendarWindow, zonedTimestamp } from "./dates";
it("keeps validated timezone preferences separate by principal", () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
  };
  writeCalendarPreferences(storage, "one", { timezone: "America/Chicago" });
  expect(readCalendarPreferences(storage, "one").timezone).toBe(
    "America/Chicago",
  );
  expect(readCalendarPreferences(storage, "two").timezone).toBe("UTC");
  writeCalendarPreferences(storage, "one", { timezone: "invalid" });
  expect(readCalendarPreferences(storage, "one").timezone).toBe("UTC");
});
it("uses the viewer's local midnight across daylight-saving transitions", () => {
  const window = calendarWindow("2026-03-08", "day", "America/Chicago");
  expect((Date.parse(window.end) - Date.parse(window.start)) / 3600000).toBe(
    23,
  );
  expect(() =>
    zonedTimestamp("2026-03-08T02:30:00", "America/Chicago"),
  ).toThrow(/does not exist/);
});

it("requires disambiguation for a repeated daylight-saving time", () => {
  expect(() =>
    zonedTimestamp("2026-11-01T01:30:00", "America/Chicago"),
  ).toThrow(/occurs twice/);
  expect(zonedTimestamp("2026-11-01T07:30:00", "UTC")).toBe(
    "2026-11-01T07:30:00.000Z",
  );
});
