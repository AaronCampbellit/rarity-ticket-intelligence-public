import { useEffect, useMemo, useState } from "react";

import {
  listTicketTimers,
  startTicketTimer,
  stopTicketTimer,
  type TicketTimeCapture,
} from "./api";

type TicketTimerProps = {
  clientID: string;
  workRecordID: string;
  onCapture: (capture: TicketTimeCapture) => void;
};

export function TicketTimer({
  clientID,
  workRecordID,
  onCapture,
}: TicketTimerProps) {
  const [captures, setCaptures] = useState<TicketTimeCapture[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    setError("");
    void listTicketTimers(clientID, workRecordID, controller.signal)
      .then((found) => setCaptures(found))
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === "AbortError")) {
          setError("Ticket timer could not be loaded.");
        }
      });
    return () => controller.abort();
  }, [clientID, workRecordID]);

  const running = useMemo(
    () => captures.find((capture) => capture.state === "running"),
    [captures],
  );
  const stopped = captures.filter((capture) => capture.state === "stopped");

  async function start() {
    setBusy(true);
    setError("");
    try {
      const idempotencyKey =
        globalThis.crypto?.randomUUID?.() ??
        `${workRecordID}-${Date.now().toString(36)}`;
      const found = await startTicketTimer(
        clientID,
        workRecordID,
        idempotencyKey,
      );
      setCaptures((current) => [found, ...current]);
    } catch {
      setError("Ticket timer could not be started.");
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    if (!running) return;
    setBusy(true);
    setError("");
    try {
      const found = await stopTicketTimer(clientID, running);
      setCaptures((current) =>
        current.map((capture) => (capture.id === found.id ? found : capture)),
      );
      onCapture(found);
    } catch {
      setError("Ticket timer could not be stopped.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="ticket-timer" aria-label="Ticket timer">
      <div className="ticket-timer__status">
        <strong>{running ? "Timer running" : "Ticket timer"}</strong>
        {running ? (
          <span>
            Started {new Date(running.startedAt).toLocaleTimeString()}
          </span>
        ) : (
          <span>Capture work directly against this ticket.</span>
        )}
      </div>
      {running ? (
        <button type="button" disabled={busy} onClick={() => void stop()}>
          Stop timer
        </button>
      ) : (
        <button type="button" disabled={busy} onClick={() => void start()}>
          Start timer
        </button>
      )}
      {stopped.map((capture) => (
        <button
          type="button"
          className="ticket-timer__capture"
          key={capture.id}
          onClick={() => onCapture(capture)}
        >
          Captured {formatDuration(capture.durationSeconds)}
        </button>
      ))}
      {error ? <p role="alert">{error}</p> : null}
    </div>
  );
}

function formatDuration(seconds: number): string {
  const minutes = Math.max(1, Math.round(seconds / 60));
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  if (hours === 0) return `${minutes}m`;
  if (remainder === 0) return `${hours}h`;
  return `${hours}h ${remainder}m`;
}
