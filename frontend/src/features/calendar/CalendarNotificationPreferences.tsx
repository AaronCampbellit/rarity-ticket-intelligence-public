import { useEffect, useState } from "react";
import { Button, Notice } from "../../design-system";
import { array, number, record, string } from "./api";
import { calendarRequest, calendarError } from "./requests";
import { Choice, MutationForm } from "./formSupport";
type Rule = {
  event_class: string;
  change_class: string;
  urgency: string;
  channel: string;
  enabled: boolean;
};
export function CalendarNotificationPreferences() {
  const [rules, setRules] = useState<Rule[]>([]),
    [version, setVersion] = useState<number>(),
    [error, setError] = useState(""),
    [revision, setRevision] = useState(0),
    [change, setChange] = useState("schedule"),
    [urgency, setUrgency] = useState("routine"),
    [channel, setChannel] = useState("in_app");
  useEffect(() => {
    const controller = new AbortController();
    setVersion(undefined);
    setError("");
    void calendarRequest("calendar/notification-preferences", {
      signal: controller.signal,
    })
      .then((value) => {
        const row = record(value),
          found = array(row.rules).map((value) => {
            const rule = record(value);
            if (typeof rule.enabled !== "boolean")
              throw new Error("Invalid notification preference");
            return {
              event_class: string(rule.event_class),
              change_class: string(rule.change_class),
              urgency: string(rule.urgency),
              channel: string(rule.channel),
              enabled: rule.enabled,
            };
          });
        if (!controller.signal.aborted) {
          setRules(found);
          setVersion(number(row.version));
        }
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(calendarError(error));
      });
    return () => controller.abort();
  }, [revision]);
  return (
    <section>
      <h3>Calendar notification preferences</h3>
      <p>
        Organization policy, delivery availability, and quiet periods still
        apply. Unlisted combinations use the server defaults.
      </p>
      <Button onClick={() => setRevision((value) => value + 1)}>
        Reload notification preferences
      </Button>
      {error ? (
        <Notice tone="danger" title="Preferences unavailable">
          {error}
        </Notice>
      ) : null}
      {version !== undefined ? (
        <MutationForm
          label="Save notification preferences"
          onSaved={() => setRevision((value) => value + 1)}
          onSubmit={() =>
            calendarRequest("calendar/notification-preferences", {
              method: "PUT",
              body: { rules, expected_version: version },
            })
          }
        >
          {rules.map((rule, index) => (
            <label
              key={[
                rule.event_class,
                rule.change_class,
                rule.urgency,
                rule.channel,
              ].join("-")}
            >
              <input
                type="checkbox"
                checked={rule.enabled}
                onChange={(e) =>
                  setRules(
                    rules.map((item, i) =>
                      i === index
                        ? { ...item, enabled: e.target.checked }
                        : item,
                    ),
                  )
                }
              />
              {rule.change_class.replaceAll("_", " ")} · {rule.urgency} ·{" "}
              {rule.channel.replaceAll("_", " ")}
            </label>
          ))}
          <Choice
            label="Change class"
            empty={false}
            value={change}
            onChange={(e) => setChange(e.target.value)}
            options={[
              "schedule",
              "pto",
              "conflict",
              "cancellation",
              "reminder",
            ]}
          />
          <Choice
            label="Urgency"
            empty={false}
            value={urgency}
            onChange={(e) => setUrgency(e.target.value)}
            options={["routine", "important", "urgent"]}
          />
          <Choice
            label="Channel"
            empty={false}
            value={channel}
            onChange={(e) => setChannel(e.target.value)}
            options={["in_app", "email"]}
          />
          <Button
            onClick={() => {
              if (
                !rules.some(
                  (rule) =>
                    rule.event_class === "calendar.schedule_changed" &&
                    rule.change_class === change &&
                    rule.urgency === urgency &&
                    rule.channel === channel,
                )
              )
                setRules([
                  ...rules,
                  {
                    event_class: "calendar.schedule_changed",
                    change_class: change,
                    urgency,
                    channel,
                    enabled: true,
                  },
                ]);
            }}
          >
            Add preference
          </Button>
        </MutationForm>
      ) : null}
    </section>
  );
}
