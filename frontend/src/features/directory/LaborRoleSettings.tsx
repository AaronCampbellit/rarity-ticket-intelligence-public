import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Notice, StatePanel } from "../../design-system";

type LaborRole = {
  id: string;
  key: string;
  version: number;
  current_version: {
    id: string;
    name: string;
    internal_cost_minor: number;
    bill_rate_minor: number;
    currency: string;
    effective_from: string;
    effective_until?: string;
    enabled: boolean;
  };
};

export function LaborRoleSettings() {
  const [roles, setRoles] = useState<LaborRole[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch("/api/v1/admin/labor-roles", {
      credentials: "same-origin",
      signal,
    });
    if (!response.ok) throw new Error(`labor_roles_${response.status}`);
    const found = (await response.json()) as LaborRole[];
    setRoles(found);
    setSelectedID((current) => current || found[0]?.id || "");
    setState("ready");
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  const selected = roles.find((role) => role.id === selectedID);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch("/api/v1/admin/labor-roles", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
        },
        body: JSON.stringify({
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          internal_cost_minor: toMinor(data.get("internal_cost")),
          bill_rate_minor: toMinor(data.get("bill_rate")),
          currency: String(data.get("currency") ?? "").toUpperCase(),
          effective_from: toISO(data.get("effective_from")),
        }),
      });
      if (!response.ok) throw new Error(`labor_role_create_${response.status}`);
      const created = (await response.json()) as LaborRole;
      setRoles((current) => [...current, created]);
      setSelectedID(created.id);
      setState("ready");
      setMessage("Labor role created.");
      form.reset();
    } catch {
      setState("error");
    }
  }

  async function version(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/admin/labor-roles/${encodeURIComponent(selected.id)}/versions`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            expected_version: selected.version,
            name: String(data.get("name") ?? ""),
            internal_cost_minor: toMinor(data.get("internal_cost")),
            bill_rate_minor: toMinor(data.get("bill_rate")),
            currency: String(data.get("currency") ?? "").toUpperCase(),
            effective_from: toISO(data.get("effective_from")),
            enabled: data.get("enabled") === "on",
            reason: String(data.get("reason") ?? ""),
          }),
        },
      );
      if (!response.ok) {
        throw new Error(`labor_role_version_${response.status}`);
      }
      const updated = (await response.json()) as LaborRole;
      setRoles((current) =>
        current.map((role) => (role.id === updated.id ? updated : role)),
      );
      setState("ready");
      setMessage("Labor role version published.");
    } catch {
      setState("error");
    }
  }

  return (
    <section
      className="settings-card labor-role-settings"
      aria-labelledby="labor-role-heading"
    >
      <h2 id="labor-role-heading">Labor roles and rates</h2>
      <p>
        Append effective-dated cost and bill-rate versions without rewriting
        historical time.
      </p>
      {state === "loading" ? (
        <StatePanel state="loading" title="Loading labor roles" />
      ) : null}
      {state === "error" ? (
        <StatePanel
          state="error"
          title="Labor-role settings are unavailable"
          description="Refresh before retrying a concurrent role change."
        />
      ) : null}
      {message ? (
        <Notice tone="success" title="Labor roles updated">
          {message}
        </Notice>
      ) : null}
      <div className="labor-role-grid">
        <div>
          {!roles.length && state === "ready" ? (
            <p>No labor roles configured.</p>
          ) : null}
          {roles.map((role) => (
            <button
              type="button"
              key={role.id}
              className={selectedID === role.id ? "active" : ""}
              onClick={() => setSelectedID(role.id)}
            >
              <strong>{role.current_version.name}</strong>
              <span>
                {formatMoney(
                  role.current_version.internal_cost_minor,
                  role.current_version.currency,
                )}{" "}
                cost /{" "}
                {formatMoney(
                  role.current_version.bill_rate_minor,
                  role.current_version.currency,
                )}{" "}
                bill
              </span>
              <small>
                Version {role.version} ·{" "}
                {role.current_version.enabled ? "Enabled" : "Disabled"}
              </small>
            </button>
          ))}
        </div>
        {selected ? (
          <form
            key={`${selected.id}-${selected.version}`}
            onSubmit={(event) => void version(event)}
          >
            <h3>Publish next version</h3>
            <label>
              Labor role name
              <input
                name="name"
                defaultValue={selected.current_version.name}
                required
              />
            </label>
            <label>
              Internal hourly cost
              <input
                name="internal_cost"
                type="number"
                min="0"
                step="0.01"
                defaultValue={
                  selected.current_version.internal_cost_minor / 100
                }
                required
              />
            </label>
            <label>
              Hourly bill rate
              <input
                name="bill_rate"
                type="number"
                min="0"
                step="0.01"
                defaultValue={selected.current_version.bill_rate_minor / 100}
                required
              />
            </label>
            <label>
              Currency
              <input
                name="currency"
                minLength={3}
                maxLength={3}
                defaultValue={selected.current_version.currency}
                required
              />
            </label>
            <label>
              Effective from
              <input name="effective_from" type="datetime-local" required />
            </label>
            <label className="labor-role-check">
              <input
                name="enabled"
                type="checkbox"
                defaultChecked={selected.current_version.enabled}
              />
              Enabled
            </label>
            <label>
              Version reason
              <input name="reason" required />
            </label>
            <button type="submit" disabled={state === "saving"}>
              Publish role version
            </button>
          </form>
        ) : null}
      </div>
      <form
        className="labor-role-create"
        onSubmit={(event) => void create(event)}
      >
        <h3>Create labor role</h3>
        <label>
          New labor role key
          <input name="key" required />
        </label>
        <label>
          New labor role name
          <input name="name" required />
        </label>
        <label>
          New internal hourly cost
          <input
            name="internal_cost"
            type="number"
            min="0"
            step="0.01"
            required
          />
        </label>
        <label>
          New hourly bill rate
          <input name="bill_rate" type="number" min="0" step="0.01" required />
        </label>
        <label>
          New role currency
          <input
            name="currency"
            defaultValue="USD"
            minLength={3}
            maxLength={3}
            required
          />
        </label>
        <label>
          New role effective from
          <input name="effective_from" type="datetime-local" required />
        </label>
        <button type="submit" disabled={state === "saving"}>
          Create labor role
        </button>
      </form>
    </section>
  );
}

function toMinor(value: FormDataEntryValue | null): number {
  return Math.round(Number(value ?? 0) * 100);
}

function toISO(value: FormDataEntryValue | null): string {
  return new Date(String(value ?? "")).toISOString();
}

function formatMoney(minor: number, currency: string): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
  }).format(minor / 100);
}
