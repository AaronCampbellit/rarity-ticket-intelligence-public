import { useState } from "react";
import { Button, Dialog, Notice } from "../../design-system";
import { CommitmentForm, type CommitmentKind } from "./CommitmentForms";
import {
  PTOForm,
  PTOManagement,
  WorkforceScheduleSettings,
} from "./WorkforceForms";
import {
  CalendarPolicySettings,
  CustomDateSettings,
} from "./CalendarPolicySettings";
import { CalendarNotificationPreferences } from "./CalendarNotificationPreferences";
import {
  Choice,
  MutationForm,
  textValue,
  useSourceRows,
  value,
  type Option,
} from "./formSupport";
import { useCalendarDirectory, useOptions } from "./useCalendarDirectory";
import { calendarRequest } from "./requests";

export function CalendarAdministration({
  principalID,
  capabilities,
  clients,
  onChanged,
  defaultProjectID = "",
  defaultClientID = "",
  initialTab = "pto",
}: {
  principalID: string;
  capabilities: ReadonlySet<string>;
  clients: Option[];
  onChanged: () => void;
  defaultProjectID?: string;
  defaultClientID?: string;
  initialTab?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>
        Create and manage calendar records
      </Button>
      <Dialog
        open={open}
        title="Calendar records and settings"
        variant="drawer"
        onClose={() => setOpen(false)}
      >
        {open ? (
          <AdministrationContent
            principalID={principalID}
            capabilities={capabilities}
            clients={clients}
            onChanged={onChanged}
            defaultProjectID={defaultProjectID}
            defaultClientID={defaultClientID}
            initialTab={initialTab}
          />
        ) : null}
      </Dialog>
    </>
  );
}
function AdministrationContent({
  principalID,
  capabilities,
  clients,
  onChanged,
  defaultProjectID,
  defaultClientID,
  initialTab,
}: {
  principalID: string;
  capabilities: ReadonlySet<string>;
  clients: Option[];
  onChanged: () => void;
  defaultProjectID: string;
  defaultClientID: string;
  initialTab: string;
}) {
  const can = (capability: string) =>
      capabilities.has("*") || capabilities.has(capability),
    directory = useCalendarDirectory();
  const tabs = [
    { id: "pto", name: "PTO request" },
    { id: "pto_review", name: "Review PTO" },
    ...(can("project.edit") ? [{ id: "milestone", name: "Milestone" }] : []),
    ...(can("calendar.commitment.manage")
      ? [
          { id: "maintenance", name: "Maintenance window" },
          { id: "renewal", name: "Renewal" },
          { id: "license", name: "License" },
        ]
      : []),
    ...(can("calendar.workforce.manage")
      ? [{ id: "workforce", name: "Working schedules" }]
      : []),
    ...(can("calendar.policy.manage")
      ? [
          { id: "policy", name: "Conflict policy" },
          { id: "custom", name: "Custom date definitions" },
        ]
      : []),
    { id: "notifications", name: "Notification preferences" },
  ];
  const [tab, setTab] = useState(
      tabs.some((tab) => tab.id === initialTab) ? initialTab : tabs[0].id,
    ),
    [notice, setNotice] = useState("");
  const changed = () => {
    setNotice("Changes saved. Calendar projections will refresh.");
    onChanged();
  };
  return (
    <div className="calendar-administration">
      <Choice
        label="Record or setting"
        empty={false}
        options={tabs}
        value={tab}
        onChange={(e) => {
          setTab(e.target.value);
          setNotice("");
        }}
      />
      {directory.error ? (
        <Notice tone="warning" title="Directory choices unavailable">
          {directory.error}
        </Notice>
      ) : null}
      {notice ? <p role="status">{notice}</p> : null}
      {tab === "pto" ? (
        <PTOForm key={notice} principalID={principalID} onSaved={changed} />
      ) : tab === "pto_review" ? (
        <PTOManagement
          principalID={principalID}
          technicians={directory.technicians}
          canManage={can("calendar.workforce.manage")}
          onChanged={changed}
        />
      ) : tab === "workforce" ? (
        <WorkforceScheduleSettings
          technicians={directory.technicians}
          onChanged={changed}
        />
      ) : tab === "policy" ? (
        <CalendarPolicySettings
          technicians={directory.technicians}
          teams={directory.teams}
          onChanged={changed}
        />
      ) : tab === "custom" ? (
        <CustomDateSettings onChanged={changed} />
      ) : tab === "notifications" ? (
        <CalendarNotificationPreferences />
      ) : (
        <CommitmentManager
          key={tab}
          kind={tab as CommitmentKind}
          clients={clients}
          technicians={directory.technicians}
          onChanged={changed}
          defaultProjectID={defaultProjectID}
          defaultClientID={defaultClientID}
        />
      )}
    </div>
  );
}
function CommitmentManager({
  kind,
  clients,
  technicians,
  onChanged,
  defaultProjectID,
  defaultClientID,
}: {
  kind: CommitmentKind;
  clients: Option[];
  technicians: Option[];
  onChanged: () => void;
  defaultProjectID: string;
  defaultClientID: string;
}) {
  const [clientID, setClientID] = useState(defaultClientID),
    [projectID, setProjectID] = useState(defaultProjectID),
    [selected, setSelected] = useState(""),
    [creation, setCreation] = useState(0);
  const projects = useOptions(
    kind === "milestone" && clientID ? "projects?limit=100" : undefined,
    clientID,
  );
  const path =
    kind === "milestone"
      ? projectID
        ? `projects/${encodeURIComponent(projectID)}/milestones`
        : undefined
      : kind === "maintenance"
        ? "maintenance-windows"
        : clientID
          ? `commercial-commitments?client_ids=${encodeURIComponent(clientID)}`
          : undefined;
  const source = useSourceRows(
      path,
      kind === "milestone" ? clientID : undefined,
    ),
    rows = source.rows.filter((row) =>
      kind === "renewal" || kind === "license" ? row.type === kind : true,
    ),
    initial = rows.find((row) => row.id === selected);
  const saved = () => {
    setSelected("");
    setCreation((value) => value + 1);
    source.reload();
    onChanged();
  };
  const states =
    kind === "milestone"
      ? ["planned", "in_progress", "blocked", "completed", "cancelled"]
      : kind === "maintenance"
        ? ["planned", "active", "completed", "cancelled"]
        : ["active", "renewed", "expired", "cancelled"];
  return (
    <section>
      <Choice
        label="Record client"
        options={clients}
        value={clientID}
        onChange={(e) => {
          setClientID(e.target.value);
          setProjectID("");
          setSelected("");
        }}
      />
      {kind === "milestone" ? (
        <>
          <Choice
            label="Project"
            value={projectID}
            options={projects.options}
            onChange={(e) => {
              setProjectID(e.target.value);
              setSelected("");
            }}
          />
          {projects.options.length === 100 ? (
            <p>
              Showing the first 100 projects. Open another project workspace to
              manage its milestones.
            </p>
          ) : null}
          {projects.error ? <p role="alert">{projects.error}</p> : null}
        </>
      ) : null}
      {source.error ? (
        <Notice tone="danger" title="Records unavailable">
          {source.error}
        </Notice>
      ) : null}
      {path ? (
        <Button
          onClick={() => {
            setSelected("");
            source.reload();
          }}
        >
          Reload records
        </Button>
      ) : null}
      {source.ready ? (
        <>
          <Choice
            label="Existing record"
            empty="Create a new record"
            options={rows.map((row) => ({
              id: row.id,
              name: `${textValue(row.name || row.title)} · ${textValue(row.status)}`,
            }))}
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
          />
          <CommitmentForm
            key={`${selected}-${initial?.version ?? 0}-${clientID}-${creation}`}
            kind={kind}
            clientID={clientID}
            projectID={projectID}
            technicians={technicians}
            clients={clients}
            initial={initial}
            onSaved={saved}
          />
          {initial ? (
            <details>
              <summary>Change record status</summary>
              <MutationForm
                label="Confirm status change"
                onSaved={saved}
                onSubmit={(data, key) => {
                  const payload = {
                    expected_version: initial.version,
                    to_status: value(data, "to_status"),
                  };
                  const endpoint =
                    kind === "milestone"
                      ? "project-milestones"
                      : kind === "maintenance"
                        ? "maintenance-windows"
                        : "commercial-commitments";
                  return calendarRequest(
                    `${endpoint}/${encodeURIComponent(initial.id)}`,
                    {
                      method: "PATCH",
                      clientID: kind === "milestone" ? clientID : undefined,
                      body: { ...payload, idempotency_key: key(payload) },
                    },
                  );
                }}
              >
                <Choice
                  label="New status"
                  name="to_status"
                  required
                  options={states.filter((status) => status !== initial.status)}
                />
                <p>
                  The server checks allowed transitions for the current state.
                </p>
              </MutationForm>
            </details>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
