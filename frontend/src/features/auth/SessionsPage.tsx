import { useEffect, useState } from "react";

import { Page, StatePanel } from "../../design-system";
import {
  listSessions,
  revokeSession,
  type BrowserSession,
} from "../../api/browserSession";

function dateTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function SessionsPage() {
  const [sessions, setSessions] = useState<BrowserSession[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");

  useEffect(() => {
    let active = true;
    void listSessions()
      .then((found) => {
        if (active) {
          setSessions(found);
          setState("ready");
        }
      })
      .catch(() => {
        if (active) setState("error");
      });
    return () => {
      active = false;
    };
  }, []);

  async function revoke(id: string) {
    try {
      await revokeSession(id);
      setSessions((current) => current.filter((session) => session.id !== id));
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Account security"
      title="Active sessions"
      description="Review recent browser sessions and revoke access you do not recognize."
    >
      <div className="session-page">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading sessions"
            description="Retrieving recent authenticated browser sessions."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Active sessions could not be loaded"
            description="Refresh and verify account access."
            supportCode="ACTIVE-SESSIONS"
          />
        ) : null}
        <section aria-label="Session list">
          {sessions.map((session) => (
            <article className="session-card" key={session.id}>
              <div>
                <h2>
                  Device {session.device_fingerprint || "unavailable"}
                  {session.current ? " · Current" : ""}
                </h2>
                <p>
                  Last active {dateTime(session.last_seen_at)}
                  {session.network_prefix
                    ? ` · Network ${session.network_prefix}`
                    : ""}
                </p>
                <small>Absolute expiry {dateTime(session.expires_at)}</small>
              </div>
              <button type="button" onClick={() => void revoke(session.id)}>
                Revoke
              </button>
            </article>
          ))}
        </section>
      </div>
    </Page>
  );
}
