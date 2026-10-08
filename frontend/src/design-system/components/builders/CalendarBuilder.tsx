import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

const days = [
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
  "sunday",
] as const;

type Window = { start_minute: number; end_minute: number };
type Weekly = Record<string, Window[]>;

function minuteTime(value = 0) {
  const hours = Math.floor(value / 60)
    .toString()
    .padStart(2, "0");
  const minutes = (value % 60).toString().padStart(2, "0");
  return `${hours}:${minutes}`;
}

export function CalendarBuilder({
  definition,
}: {
  definition: {
    timezone: string;
    weekly: Weekly;
    holidays?: string[];
  };
}) {
  const [weekly, setWeekly] = useState<Weekly>(() =>
    Object.fromEntries(
      days.map((day) => [
        day,
        (definition.weekly[day] ?? []).map((window) => ({ ...window })),
      ]),
    ),
  );

  return (
    <fieldset className="rti-builder rti-calendar-builder">
      <legend>Coverage schedule</legend>
      <label>
        <span>Timezone</span>
        <input
          name="calendar_timezone"
          defaultValue={definition.timezone}
          placeholder="America/Chicago"
          required
        />
      </label>
      <div>
        {days.map((day) => {
          const windows = weekly[day] ?? [];
          const active = windows.length > 0;
          return (
            <div key={day} data-active={active}>
              <label className="rti-builder__check">
                <input
                  type="checkbox"
                  name="calendar_day"
                  value={day}
                  checked={active}
                  onChange={(event) =>
                    setWeekly((current) => ({
                      ...current,
                      [day]: event.target.checked
                        ? [{ start_minute: 480, end_minute: 1020 }]
                        : [],
                    }))
                  }
                />
                {day}
              </label>
              {windows.map((window, index) => (
                <div className="rti-calendar-builder__window" key={index}>
                  <label>
                    <span>Opens</span>
                    <input
                      type="time"
                      name={`calendar_start_${day}`}
                      value={minuteTime(window.start_minute)}
                      onChange={(event) =>
                        updateWindow(setWeekly, day, index, {
                          start_minute: timeMinute(event.target.value),
                        })
                      }
                    />
                  </label>
                  <label>
                    <span>Closes</span>
                    <input
                      type="time"
                      name={`calendar_end_${day}`}
                      value={minuteTime(window.end_minute)}
                      onChange={(event) =>
                        updateWindow(setWeekly, day, index, {
                          end_minute: timeMinute(event.target.value),
                        })
                      }
                    />
                  </label>
                  <button
                    type="button"
                    aria-label={`Remove ${day} window ${index + 1}`}
                    onClick={() =>
                      setWeekly((current) => ({
                        ...current,
                        [day]: current[day].filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      }))
                    }
                  >
                    <Trash2 size={15} aria-hidden="true" />
                  </button>
                </div>
              ))}
              {active ? (
                <button
                  className="rti-builder__add"
                  type="button"
                  onClick={() =>
                    setWeekly((current) => ({
                      ...current,
                      [day]: [
                        ...current[day],
                        { start_minute: 780, end_minute: 1020 },
                      ],
                    }))
                  }
                >
                  <Plus size={14} aria-hidden="true" /> Add window
                </button>
              ) : null}
            </div>
          );
        })}
      </div>
      <label>
        <span>Holiday dates</span>
        <textarea
          name="calendar_holidays"
          defaultValue={definition.holidays?.join("\n") ?? ""}
          placeholder={"2026-12-25\n2027-01-01"}
        />
        <small>One date per line</small>
      </label>
    </fieldset>
  );
}

function updateWindow(
  setWeekly: React.Dispatch<React.SetStateAction<Weekly>>,
  day: string,
  index: number,
  update: Partial<Window>,
) {
  setWeekly((current) => ({
    ...current,
    [day]: current[day].map((window, itemIndex) =>
      itemIndex === index ? { ...window, ...update } : window,
    ),
  }));
}

function timeMinute(value: FormDataEntryValue | string | null) {
  const [hours, minutes] = String(value ?? "00:00")
    .split(":")
    .map(Number);
  return hours * 60 + minutes;
}

export function calendarFromForm(data: FormData) {
  const selected = data.getAll("calendar_day").map(String);
  return {
    timezone: String(data.get("calendar_timezone") ?? ""),
    weekly: Object.fromEntries(
      selected.map((day) => {
        const starts = data.getAll(`calendar_start_${day}`);
        const ends = data.getAll(`calendar_end_${day}`);
        return [
          day,
          starts.map((start, index) => ({
            start_minute: timeMinute(start),
            end_minute: timeMinute(ends[index]),
          })),
        ];
      }),
    ),
    holidays: String(data.get("calendar_holidays") ?? "")
      .split(/\r?\n/)
      .map((value) => value.trim())
      .filter(Boolean),
  };
}
