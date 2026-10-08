import { CalendarAdministration } from "../calendar/CalendarAdministration";
import {
  type FormEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useState,
} from "react";

import { csrfHeaders, loadDirectory } from "../../api/browserSession";
import { MultiSelect, Notice, Page, StatePanel } from "../../design-system";
import { LaborRoleSettings } from "../directory/LaborRoleSettings";
import "../operations/operations.css";
import "./organization.css";

type Client = { id: string; display_id: string; name: string };
type Department = { id: string; key: string; name: string };
type Team = {
  id: string;
  department_id?: string;
  key: string;
  name: string;
  version: number;
  member_ids: string[];
};
type Technician = {
  id: string;
  display_name: string;
  email: string;
};
type Queue = {
  id: string;
  client_id?: string;
  department_id?: string;
  team_id?: string;
  key: string;
  name: string;
};
type Directory = {
  clients: Client[];
  departments: Department[];
  teams: Team[];
  queues: Queue[];
  technicians?: Technician[];
};

type MembershipConflict = {
  memberIDs: string[];
};

const emptyDirectory: Directory = {
  clients: [],
  departments: [],
  teams: [],
  queues: [],
  technicians: [],
};

export function DirectorySettingsPage({
  capabilities,
  principalID = "",
}: {
  capabilities: Set<string>;
  principalID?: string;
}) {
  const [directory, setDirectory] = useState<Directory>(emptyDirectory);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const canCreateClient = capabilities.has("client.create");
  const canManage = capabilities.has("organization.manage");

  const load = useCallback(async (signal?: AbortSignal) => {
    const loaded = (await loadDirectory(signal)) as unknown as Directory;
    const complete = {
      ...loaded,
      teams: loaded.teams ?? [],
      technicians: loaded.technicians ?? [],
    };
    setDirectory(complete);
    setState("ready");
    return complete;
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  async function create(
    event: FormEvent<HTMLFormElement>,
    endpoint: string,
    transform: (data: FormData) => Record<string, unknown>,
    label: string,
  ) {
    event.preventDefault();
    const form = event.currentTarget;
    const body = transform(new FormData(form));
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error("Directory mutation failed");
      form.reset();
      await load();
      setMessage(`${label} created.`);
    } catch {
      setState("error");
    }
  }

  async function createAvailability(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const technicianID = String(data.get("technician_id") ?? "").trim();
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/admin/technicians/${encodeURIComponent(
          technicianID,
        )}/availability`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            starts_at: new Date(
              String(data.get("starts_at") ?? ""),
            ).toISOString(),
            ends_at: new Date(String(data.get("ends_at") ?? "")).toISOString(),
            available_minutes: Number(data.get("available_minutes") ?? 0),
          }),
        },
      );
      if (!response.ok) throw new Error("Availability mutation failed");
      form.reset();
      await load();
      setMessage("Availability window created.");
    } catch {
      setState("error");
    }
  }

  async function createLaborCostRate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const technicianID = String(data.get("technician_id") ?? "").trim();
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/admin/technicians/${encodeURIComponent(
          technicianID,
        )}/labor-cost-rates`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            hourly_rate: {
              minor: Math.round(Number(data.get("hourly_rate") ?? 0) * 100),
              currency: String(data.get("currency") ?? "").toUpperCase(),
            },
            effective_at: new Date(
              String(data.get("effective_at") ?? ""),
            ).toISOString(),
          }),
        },
      );
      if (!response.ok) throw new Error("Labor cost rate mutation failed");
      form.reset();
      await load();
      setMessage("Labor cost rate created.");
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      className="operations-health"
      eyebrow="Organization administration"
      title="Clients and service directory"
      description="Maintain the MSP hierarchy used by routing, assignment, saved views, and notification policies."
    >
      {principalID &&
      (capabilities.has("calendar.read") || capabilities.has("*")) ? (
        <CalendarAdministration
          principalID={principalID}
          capabilities={capabilities}
          clients={directory.clients}
          initialTab="workforce"
          onChanged={() => {}}
        />
      ) : null}
      {state === "loading" ? (
        <StatePanel state="loading" title="Loading directory…" />
      ) : null}
      {state === "error" ? (
        <StatePanel
          state="error"
          title="Directory administration is unavailable"
          description="Verify MSP-wide scope and permissions."
        />
      ) : null}
      {message ? (
        <Notice tone="success" title="Directory updated">
          {message}
        </Notice>
      ) : null}
      <section aria-labelledby="directory-overview-heading">
        <h2 id="directory-overview-heading">Current directory</h2>
        <div className="health-grid">
          <DirectoryList
            title="Clients"
            empty="No clients are configured."
            items={directory.clients.map((item) => ({
              id: item.id,
              primary: item.name,
              secondary: item.display_id,
            }))}
          />
          <DirectoryList
            title="Departments"
            empty="No departments are configured."
            items={directory.departments.map((item) => ({
              id: item.id,
              primary: item.name,
              secondary: item.key,
            }))}
          />
          <DirectoryList
            title="Teams"
            empty="No teams are configured."
            items={directory.teams.map((item) => ({
              id: item.id,
              primary: item.name,
              secondary: `${item.key} · ${nameFor(
                directory.departments,
                item.department_id,
              )}`,
            }))}
          />
          <DirectoryList
            title="Queues"
            empty="No queues are configured."
            items={directory.queues.map((item) => ({
              id: item.id,
              primary: item.name,
              secondary: `${item.key} · ${
                item.client_id
                  ? nameFor(directory.clients, item.client_id)
                  : "MSP-wide"
              }`,
            }))}
          />
        </div>
      </section>
      {canCreateClient ? (
        <section
          className="settings-card"
          aria-labelledby="create-client-heading"
        >
          <h2 id="create-client-heading">Add client</h2>
          <form
            onSubmit={(event) =>
              void create(
                event,
                "/api/v1/admin/clients",
                (data) => ({
                  display_id: data.get("display_id"),
                  name: data.get("name"),
                }),
                "Client",
              )
            }
          >
            <label>
              Display ID
              <input name="display_id" required />
            </label>
            <label>
              Client name
              <input name="name" required />
            </label>
            <button type="submit">Add client</button>
          </form>
        </section>
      ) : null}
      {canManage ? (
        <>
          <TeamMembershipSettings
            teams={directory.teams}
            technicians={directory.technicians ?? []}
            reload={load}
          />
          <div className="settings-grid">
            <section
              className="settings-card"
              aria-labelledby="create-department-heading"
            >
              <h2 id="create-department-heading">Add department</h2>
              <DirectoryForm
                submitLabel="Add department"
                onSubmit={(event) =>
                  void create(
                    event,
                    "/api/v1/admin/departments",
                    basicDirectoryBody,
                    "Department",
                  )
                }
              />
            </section>
            <section
              className="settings-card"
              aria-labelledby="create-team-heading"
            >
              <h2 id="create-team-heading">Add team</h2>
              <DirectoryForm
                submitLabel="Add team"
                prefix={
                  <label>
                    Department
                    <select name="department_id" required>
                      <option value="">Select department</option>
                      {directory.departments.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.name}
                        </option>
                      ))}
                    </select>
                  </label>
                }
                onSubmit={(event) =>
                  void create(
                    event,
                    "/api/v1/admin/teams",
                    (data) => ({
                      ...basicDirectoryBody(data),
                      department_id: data.get("department_id"),
                    }),
                    "Team",
                  )
                }
              />
            </section>
            <section
              className="settings-card"
              aria-labelledby="create-queue-heading"
            >
              <h2 id="create-queue-heading">Add queue</h2>
              <DirectoryForm
                submitLabel="Add queue"
                prefix={
                  <>
                    <label>
                      Client scope
                      <select name="client_id">
                        <option value="">MSP-wide</option>
                        {directory.clients.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Department
                      <select name="department_id">
                        <option value="">No department</option>
                        {directory.departments.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Team
                      <select name="team_id">
                        <option value="">No team</option>
                        {directory.teams.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name}
                          </option>
                        ))}
                      </select>
                    </label>
                  </>
                }
                onSubmit={(event) =>
                  void create(
                    event,
                    "/api/v1/admin/queues",
                    (data) => ({
                      ...basicDirectoryBody(data),
                      client_id: data.get("client_id"),
                      department_id: data.get("department_id"),
                      team_id: data.get("team_id"),
                    }),
                    "Queue",
                  )
                }
              />
            </section>
            <section
              className="settings-card"
              aria-labelledby="create-availability-heading"
            >
              <h2 id="create-availability-heading">
                Add technician availability
              </h2>
              <p>
                Define an exact MSP-wide availability window used by Project
                capacity calculations.
              </p>
              <form onSubmit={(event) => void createAvailability(event)}>
                <label>
                  Availability technician ID
                  <input name="technician_id" required />
                </label>
                <label>
                  Available from
                  <input name="starts_at" type="datetime-local" required />
                </label>
                <label>
                  Available through
                  <input name="ends_at" type="datetime-local" required />
                </label>
                <label>
                  Available minutes
                  <input
                    name="available_minutes"
                    type="number"
                    min="0"
                    step="1"
                    required
                  />
                </label>
                <button type="submit">Add availability window</button>
              </form>
            </section>
            <section
              className="settings-card"
              aria-labelledby="create-labor-rate-heading"
            >
              <h2 id="create-labor-rate-heading">Add labor cost rate</h2>
              <p>
                Append an effective-dated internal hourly cost for Project
                actual-labor calculations.
              </p>
              <form onSubmit={(event) => void createLaborCostRate(event)}>
                <label>
                  Rate technician ID
                  <input name="technician_id" required />
                </label>
                <label>
                  Internal hourly cost
                  <input
                    name="hourly_rate"
                    type="number"
                    min="0"
                    step="0.01"
                    required
                  />
                </label>
                <label>
                  Rate currency
                  <input
                    name="currency"
                    defaultValue="USD"
                    minLength={3}
                    maxLength={3}
                    required
                  />
                </label>
                <label>
                  Rate effective at
                  <input name="effective_at" type="datetime-local" required />
                </label>
                <button type="submit">Add labor cost rate</button>
              </form>
            </section>
          </div>
          <LaborRoleSettings />
        </>
      ) : null}
    </Page>
  );
}

function TeamMembershipSettings({
  teams,
  technicians,
  reload,
}: {
  teams: Team[];
  technicians: Technician[];
  reload: () => Promise<Directory>;
}) {
  return (
    <section
      className="settings-card"
      aria-labelledby="team-membership-heading"
    >
      <h2 id="team-membership-heading">Manage team members</h2>
      <p>
        Team membership controls collaboration groups without changing any
        technician role assignments.
      </p>
      {!teams.length ? <p>No teams are available.</p> : null}
      <div className="settings-grid">
        {teams.map((team) => (
          <TeamMembershipEditor
            key={team.id}
            team={team}
            technicians={technicians}
            reload={reload}
          />
        ))}
      </div>
    </section>
  );
}

function TeamMembershipEditor({
  team,
  technicians,
  reload,
}: {
  team: Team;
  technicians: Technician[];
  reload: () => Promise<Directory>;
}) {
  const [selected, setSelected] = useState<string[]>(() => [
    ...(team.member_ids ?? []),
  ]);
  const [expectedVersion, setExpectedVersion] = useState(team.version);
  const [reason, setReason] = useState("");
  const [search, setSearch] = useState("");
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState<MembershipConflict | null>(null);
  const [knownTechnicianNames, setKnownTechnicianNames] = useState<
    Record<string, string>
  >(() =>
    Object.fromEntries(
      technicians.map((technician) => [technician.id, technician.display_name]),
    ),
  );
  const [feedback, setFeedback] = useState<
    "" | "saved" | "conflict" | "merged" | "error"
  >("");
  const normalizedSearch = search.trim().toLocaleLowerCase();
  const visibleTechnicians = normalizedSearch
    ? technicians.filter((technician) =>
        `${technician.display_name} ${technician.email}`
          .toLocaleLowerCase()
          .includes(normalizedSearch),
      )
    : technicians;
  const namesByID = new Map(
    technicians.map((technician) => [technician.id, technician.display_name]),
  );
  const labelForID = (technicianID: string) =>
    namesByID.get(technicianID) ??
    knownTechnicianNames[technicianID] ??
    technicianID;

  useEffect(() => {
    setKnownTechnicianNames((current) => {
      const next = { ...current };
      let changed = false;
      for (const technician of technicians) {
        if (next[technician.id] !== technician.display_name) {
          next[technician.id] = technician.display_name;
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [technicians]);

  const missingSelected = selected.filter(
    (technicianID) => !namesByID.has(technicianID),
  );
  const selectableTechnicians = [
    ...visibleTechnicians,
    ...missingSelected.map((technicianID) => ({
      id: technicianID,
      display_name: labelForID(technicianID),
      email: "No longer active",
    })),
  ];
  const addedOnServer = conflict
    ? conflict.memberIDs.filter(
        (technicianID) => !selected.includes(technicianID),
      )
    : [];
  const onlyInPending = conflict
    ? selected.filter(
        (technicianID) => !conflict.memberIDs.includes(technicianID),
      )
    : [];

  async function replace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (conflict) return;
    setSaving(true);
    setFeedback("");
    const technicianIDs = [...selected].sort();
    try {
      const response = await fetch(
        `/api/v1/admin/teams/${encodeURIComponent(team.id)}/members`,
        {
          method: "PUT",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            technician_ids: technicianIDs,
            expected_version: expectedVersion,
            reason,
          }),
        },
      );
      if (response.status === 409) {
        const current = await reload();
        const currentTeam = current.teams.find((item) => item.id === team.id);
        if (!currentTeam) throw new Error("Team is no longer available");
        setExpectedVersion(currentTeam.version);
        setConflict({
          memberIDs: [...(currentTeam.member_ids ?? [])].sort(),
        });
        setFeedback("conflict");
        return;
      }
      if (!response.ok) throw new Error("Team membership mutation failed");
      const updated = (await response.json()) as Team;
      setExpectedVersion(updated.version);
      setSelected([...(updated.member_ids ?? technicianIDs)]);
      const current = await reload();
      const currentTeam = current.teams.find((item) => item.id === team.id);
      if (currentTeam) {
        setExpectedVersion(currentTeam.version);
        setSelected([...(currentTeam.member_ids ?? technicianIDs)]);
      }
      setConflict(null);
      setFeedback("saved");
    } catch {
      setFeedback("error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <article>
      <h3>{team.name}</h3>
      <p>
        Pending selection:{" "}
        {selected.length ? selected.map(labelForID).join(", ") : "No members"}
      </p>
      {feedback === "conflict" && conflict ? (
        <div role="alert" aria-label={`${team.name} membership changed`}>
          The team changed while you were editing. The current membership was
          reloaded; your pending selection and reason were preserved.
          <h4>Current server members</h4>
          <p>
            {conflict.memberIDs.length
              ? conflict.memberIDs.map(labelForID).join(", ")
              : "No members"}
          </p>
          <p>
            Added on server:{" "}
            {addedOnServer.length
              ? addedOnServer.map(labelForID).join(", ")
              : "None"}
          </p>
          <p>
            Only in pending selection:{" "}
            {onlyInPending.length
              ? onlyInPending.map(labelForID).join(", ")
              : "None"}
          </p>
          <button
            type="button"
            onClick={() => {
              setSelected(
                [...new Set([...selected, ...conflict.memberIDs])].sort(),
              );
              setConflict(null);
              setFeedback("merged");
            }}
          >
            Merge current server members into pending selection
          </button>
        </div>
      ) : null}
      {feedback === "error" ? (
        <div role="alert">Team membership could not be updated.</div>
      ) : null}
      {feedback === "saved" ? (
        <p role="status">{team.name} membership updated.</p>
      ) : null}
      {feedback === "merged" ? (
        <p role="status">
          Current server members were merged into the pending selection. Review
          and save when ready.
        </p>
      ) : null}
      <form onSubmit={(event) => void replace(event)}>
        <label>
          Search {team.name} members
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.currentTarget.value)}
          />
        </label>
        <MultiSelect
          label={`${team.name} members`}
          options={selectableTechnicians.map((technician) => ({
            value: technician.id,
            label: technician.display_name,
            description: technician.email,
          }))}
          values={selected}
          onChange={setSelected}
        />
        <label>
          Reason for {team.name} membership change
          <input
            value={reason}
            onChange={(event) => setReason(event.currentTarget.value)}
            required
          />
        </label>
        <button type="submit" disabled={saving || conflict !== null}>
          {saving ? "Saving members…" : `Save ${team.name} members`}
        </button>
      </form>
    </article>
  );
}

function DirectoryForm({
  onSubmit,
  submitLabel,
  prefix,
}: {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  submitLabel: string;
  prefix?: ReactNode;
}) {
  return (
    <form onSubmit={onSubmit}>
      {prefix}
      <label>
        Stable key
        <input name="key" required />
      </label>
      <label>
        Name
        <input name="name" required />
      </label>
      <label>
        Reason
        <input name="reason" required />
      </label>
      <button type="submit">{submitLabel}</button>
    </form>
  );
}

function DirectoryList({
  title,
  empty,
  items,
}: {
  title: string;
  empty: string;
  items: Array<{ id: string; primary: string; secondary: string }>;
}) {
  return (
    <article>
      <h3>{title}</h3>
      {!items.length ? <p>{empty}</p> : null}
      <ul>
        {items.map((item) => (
          <li key={item.id}>
            <strong>{item.primary}</strong> <span>{item.secondary}</span>
          </li>
        ))}
      </ul>
    </article>
  );
}

function basicDirectoryBody(data: FormData) {
  return {
    key: data.get("key"),
    name: data.get("name"),
    reason: data.get("reason"),
  };
}

function nameFor(
  items: Array<{ id: string; name: string }>,
  id: string | undefined,
) {
  return items.find((item) => item.id === id)?.name ?? "Unassigned";
}
