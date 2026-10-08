import { type FormEvent, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";

type Preference = {
  technician_id: string;
  event_type: "mention.occurred";
  email_enabled: boolean;
  teams_enabled: boolean;
  email_available: boolean;
  teams_available: boolean;
  time_zone: string;
  quiet_start?: string;
  quiet_end?: string;
  version: number;
};

type PreferenceDraft = Pick<
  Preference,
  "email_enabled" | "teams_enabled" | "time_zone" | "quiet_start" | "quiet_end"
>;

function draftFrom(preference: Preference): PreferenceDraft {
  return {
    email_enabled: preference.email_enabled,
    teams_enabled: preference.teams_enabled,
    time_zone: preference.time_zone,
    quiet_start: preference.quiet_start ?? "",
    quiet_end: preference.quiet_end ?? "",
  };
}

export function MentionNotificationPreferences() {
  const [preference, setPreference] = useState<Preference>();
  const [draft, setDraft] = useState<PreferenceDraft>();
  const [conflict, setConflict] = useState<{
    server: Preference;
    draft: PreferenceDraft;
  }>();
  const [status, setStatus] = useState<
    "loading" | "ready" | "saving" | "error"
  >("loading");
  const [message, setMessage] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/api/v1/notification-preferences/mentions", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error("preference_unavailable");
        const loaded = (await response.json()) as Preference;
        setPreference(loaded);
        setDraft(draftFrom(loaded));
        setStatus("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setStatus("error");
      });
    return () => controller.abort();
  }, []);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!preference || !draft || conflict) return;
    setStatus("saving");
    setMessage("");
    try {
      const response = await fetch(
        "/api/v1/notification-preferences/mentions",
        {
          method: "PATCH",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            email_enabled: draft.email_enabled,
            teams_enabled: draft.teams_enabled,
            time_zone: draft.time_zone,
            quiet_start: draft.quiet_start || null,
            quiet_end: draft.quiet_end || null,
            expected_version: preference.version,
          }),
        },
      );
      if (response.status === 409) {
        const currentResponse = await fetch(
          "/api/v1/notification-preferences/mentions",
          { credentials: "same-origin" },
        );
        if (!currentResponse.ok) throw new Error("preference_reload_failed");
        const current = (await currentResponse.json()) as Preference;
        setPreference(current);
        setConflict({ server: current, draft });
        setStatus("ready");
        return;
      }
      if (!response.ok) throw new Error("preference_save_failed");
      const saved = (await response.json()) as Preference;
      setPreference(saved);
      setDraft(draftFrom(saved));
      setStatus("ready");
      setMessage("Mention preferences saved.");
    } catch {
      setStatus("error");
      setMessage("Mention preferences could not be saved.");
    }
  }

  return (
    <details className="mention-preferences">
      <summary>Mention notification preferences</summary>
      {status === "loading" ? <p role="status">Loading preferences…</p> : null}
      {status === "error" && !preference ? (
        <p role="alert">Mention preferences are unavailable.</p>
      ) : null}
      {preference && draft ? (
        <form onSubmit={(event) => void save(event)}>
          <p>
            The organization selects available channels. You can turn them off
            and set local quiet hours.
          </p>
          <label>
            <input
              name="email_enabled"
              type="checkbox"
              checked={draft.email_enabled}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  email_enabled: event.currentTarget.checked,
                })
              }
              disabled={!preference.email_available}
            />{" "}
            Email
          </label>
          <label>
            <input
              name="teams_enabled"
              type="checkbox"
              checked={draft.teams_enabled}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  teams_enabled: event.currentTarget.checked,
                })
              }
              disabled={!preference.teams_available}
            />{" "}
            Microsoft Teams
          </label>
          <label>
            Time zone
            <input
              aria-label="Time zone"
              name="time_zone"
              value={draft.time_zone}
              onChange={(event) =>
                setDraft({ ...draft, time_zone: event.currentTarget.value })
              }
              required
            />
          </label>
          <label>
            Quiet hours start
            <input
              aria-label="Quiet hours start"
              name="quiet_start"
              type="time"
              value={draft.quiet_start}
              onChange={(event) =>
                setDraft({ ...draft, quiet_start: event.currentTarget.value })
              }
            />
          </label>
          <label>
            Quiet hours end
            <input
              aria-label="Quiet hours end"
              name="quiet_end"
              type="time"
              value={draft.quiet_end}
              onChange={(event) =>
                setDraft({ ...draft, quiet_end: event.currentTarget.value })
              }
            />
          </label>
          <button
            type="submit"
            disabled={status === "saving" || Boolean(conflict)}
          >
            Save mention preferences
          </button>
          {conflict ? (
            <div role="alert">
              <p>
                Mention settings changed on the server. Current:{" "}
                {conflict.server.time_zone}. Your draft:{" "}
                {conflict.draft.time_zone}.
              </p>
              <button
                type="button"
                onClick={() => {
                  setDraft(draftFrom(conflict.server));
                  setConflict(undefined);
                }}
              >
                Load server settings
              </button>
              <button
                type="button"
                onClick={() => {
                  setDraft(conflict.draft);
                  setConflict(undefined);
                }}
              >
                Reapply my draft
              </button>
            </div>
          ) : null}
          {message ? <p role="status">{message}</p> : null}
        </form>
      ) : null}
    </details>
  );
}
