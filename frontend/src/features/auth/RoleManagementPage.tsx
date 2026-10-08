import { type FormEvent, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import {
  Notice,
  Page,
  ScopeBuilder,
  StatePanel,
  TextListBuilder,
} from "../../design-system";

type Role = {
  id: string;
  key: string;
  name: string;
  system_role: boolean;
  capabilities: string[];
  version: number;
};

export function RoleManagementPage() {
  const [roles, setRoles] = useState<Role[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/api/v1/admin/roles", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(`roles_${response.status}`);
        return (await response.json()) as Role[];
      })
      .then((found) => {
        setRoles(found);
        setSelectedID(found[0]?.id ?? "");
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, []);

  const selected = roles.find((role) => role.id === selectedID);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) return;
    const data = new FormData(event.currentTarget);
    const capabilities = data
      .getAll("capabilities")
      .map(String)
      .map((value) => value.trim())
      .filter(Boolean);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/admin/roles/${encodeURIComponent(selected.id)}/capabilities`,
        {
          method: "PUT",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            expected_version: selected.version,
            capabilities,
            reason: String(data.get("reason") ?? ""),
          }),
        },
      );
      if (!response.ok) throw new Error(`role_update_${response.status}`);
      const updated = (await response.json()) as Role;
      setRoles((current) =>
        current.map((role) => (role.id === updated.id ? updated : role)),
      );
      setState("ready");
      setMessage("Role capabilities updated and audited.");
    } catch {
      setState("error");
    }
  }

  async function createRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const capabilities = data
      .getAll("capabilities")
      .map(String)
      .map((value) => value.trim())
      .filter(Boolean);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch("/api/v1/admin/roles", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
        },
        body: JSON.stringify({
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          capabilities,
          reason: String(data.get("reason") ?? ""),
        }),
      });
      if (!response.ok) throw new Error(`role_create_${response.status}`);
      const created = (await response.json()) as Role;
      setRoles((current) => [...current, created]);
      setSelectedID(created.id);
      setState("ready");
      setMessage("Role created and audited.");
      form.reset();
    } catch {
      setState("error");
    }
  }

  async function assignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setState("saving");
    setMessage("");
    try {
      const response = await fetch("/api/v1/admin/role-assignments", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
        },
        body: JSON.stringify({
          principal_id: String(data.get("principal_id") ?? ""),
          role_key: String(data.get("role_key") ?? ""),
          client_id: String(data.get("client_id") ?? ""),
          reason: String(data.get("reason") ?? ""),
        }),
      });
      if (!response.ok) throw new Error(`role_assign_${response.status}`);
      setState("ready");
      setMessage("Role assignment created and audited.");
      form.reset();
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Administration"
      title="Global roles"
      description="Compose MSP-wide roles from explicit capabilities. Changes require a reason and take effect on the next principal refresh."
    >
      <div className="role-management">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading roles"
            description="Retrieving global role definitions and capabilities."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Role management is unavailable"
            description="The action may have been rejected. Refresh before retrying a concurrent change."
            supportCode="ROLE-MANAGEMENT"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Role updated">
            {message}
          </Notice>
        ) : null}
        {roles.length ? (
          <div className="role-management-grid">
            <nav aria-label="Roles">
              {roles.map((role) => (
                <button
                  type="button"
                  key={role.id}
                  className={role.id === selectedID ? "active" : ""}
                  onClick={() => {
                    setSelectedID(role.id);
                    setState("ready");
                    setMessage("");
                  }}
                >
                  <span>{role.name}</span>
                  <small>
                    {role.system_role ? "System role" : "Custom role"} ·{" "}
                    {role.capabilities.length} capabilities
                  </small>
                </button>
              ))}
            </nav>
            {selected ? (
              <form key={`${selected.id}-${selected.version}`} onSubmit={save}>
                <header>
                  <div>
                    <p className="record-id">{selected.key}</p>
                    <h2>{selected.name}</h2>
                  </div>
                  <small>Version {selected.version}</small>
                </header>
                <ScopeBuilder
                  label="Capabilities"
                  name="capabilities"
                  initialValues={selected.capabilities}
                  options={capabilityOptions(roles)}
                />
                <label>
                  <span>Reason for change</span>
                  <input name="reason" required />
                </label>
                <button type="submit" disabled={state === "saving"}>
                  {state === "saving" ? "Saving…" : "Save capabilities"}
                </button>
              </form>
            ) : null}
          </div>
        ) : state === "ready" ? (
          <StatePanel
            state="empty"
            title="No roles configured"
            description="No global roles are available."
          />
        ) : null}
        <div className="role-management-actions">
          <section aria-labelledby="create-role-heading">
            <h2 id="create-role-heading">Create custom role</h2>
            <form onSubmit={(event) => void createRole(event)}>
              <label>
                <span>New role key</span>
                <input name="key" required />
              </label>
              <label>
                <span>New role name</span>
                <input name="name" required />
              </label>
              <TextListBuilder
                label="New role capabilities"
                name="capabilities"
                itemLabel="Capability"
                placeholder="timesheet.review"
              />
              <label>
                <span>New role reason</span>
                <input name="reason" required />
              </label>
              <button type="submit" disabled={state === "saving"}>
                Create role
              </button>
            </form>
          </section>
          <section aria-labelledby="assign-role-heading">
            <h2 id="assign-role-heading">Assign role</h2>
            <form onSubmit={(event) => void assignRole(event)}>
              <label>
                <span>Assignment technician ID</span>
                <input name="principal_id" required />
              </label>
              <label>
                <span>Assignment role</span>
                <select name="role_key" required defaultValue="">
                  <option value="" disabled>
                    Select role
                  </option>
                  {roles.map((role) => (
                    <option key={role.id} value={role.key}>
                      {role.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>Assignment client ID</span>
                <input
                  name="client_id"
                  placeholder="Leave blank for MSP-wide"
                />
              </label>
              <label>
                <span>Assignment reason</span>
                <input name="reason" required />
              </label>
              <button type="submit" disabled={state === "saving"}>
                Assign role
              </button>
            </form>
          </section>
        </div>
      </div>
    </Page>
  );
}

function capabilityOptions(roles: Role[]) {
  return Array.from(
    new Set([
      "work_record.read",
      "work_record.create",
      "work_record.update",
      "work_record.comment",
      "asset.read",
      "integration.read",
      "knowledge.read",
      "knowledge.manage",
      "reporting.read",
      "admin.roles.manage",
      ...roles.flatMap((role) => role.capabilities),
    ]),
  ).map((capability) => ({
    value: capability,
    label: capability
      .split(/[._]/)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(" "),
    description: capability,
  }));
}
