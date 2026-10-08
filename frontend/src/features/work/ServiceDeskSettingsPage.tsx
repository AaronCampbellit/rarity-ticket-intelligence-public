import { type FormEvent, type ReactNode, useEffect, useState } from "react";

import {
  csrfHeaders,
  loadDirectory,
  type Directory,
  type DirectoryClient,
  type DirectoryEntry,
} from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import {
  Button,
  CalendarBuilder,
  ConditionBuilder,
  DestinationBuilder,
  Notice,
  Page,
  StatePanel,
  TagInput,
  WorkflowBuilder,
  calendarFromForm,
  conditionsFromForm,
  destinationsFromForm,
  workflowFromForm,
} from "../../design-system";
import { MentionNotificationPreferences } from "../mentions/MentionNotificationPreferences";
import "./work.css";

type Workflow = {
  id: string;
  key: string;
  name: string;
  version: number;
  priority: number;
  stable_order: number;
  fallback: boolean;
  enabled: boolean;
  conditions: Record<string, unknown>;
  definition: {
    states: Array<{
      key: string;
      requires_owner?: boolean;
      sla_behavior?: string;
    }>;
    transitions: Array<{ from: string; to: string }>;
  };
};

type BusinessCalendar = {
  id: string;
  client_id?: string;
  key: string;
  name: string;
  version: number;
  definition: {
    timezone: string;
    weekly: Record<string, Array<{ start_minute: number; end_minute: number }>>;
    holidays?: string[];
  };
};

type SLAPolicy = {
  id: string;
  client_id?: string;
  key: string;
  name: string;
  version: number;
  calendar: BusinessCalendar;
  conditions: Record<string, unknown>;
  response_target_seconds: number;
  resolution_target_seconds: number;
  warning_percent: number;
  pause_states?: string[];
  enabled: boolean;
  priority: number;
  stable_order: number;
  fallback: boolean;
};

type NotificationDestination = {
  channel: "in_app" | "email" | "teams" | "webhook";
  recipient_ref: string;
  content_classification: string;
};

type NotificationPolicy = {
  id: string;
  key: string;
  name: string;
  event_type: string;
  version: number;
  conditions: Record<string, unknown>;
  destinations: NotificationDestination[];
  quiet_period_seconds: number;
  critical_bypass: boolean;
  enabled: boolean;
  priority: number;
  stable_order: number;
};

type RoutingRule = {
  id?: string;
  position: number;
  client_id?: string;
  record_type?: string;
  priority?: string;
  queue_id: string;
};

type RoutingRuleSet = {
  id: string;
  version: number;
  rules: RoutingRule[];
};

const defaultDefinition = {
  states: [
    { key: "new", sla_behavior: "active" },
    { key: "in_progress", requires_owner: true, sla_behavior: "active" },
    { key: "resolved", sla_behavior: "resolved" },
    { key: "closed", sla_behavior: "resolved" },
  ],
  transitions: [
    { from: "new", to: "in_progress" },
    { from: "in_progress", to: "resolved" },
    { from: "resolved", to: "closed" },
    { from: "resolved", to: "in_progress" },
  ],
};

const defaultCalendarDefinition: BusinessCalendar["definition"] = {
  timezone: "America/Chicago",
  weekly: {
    monday: [{ start_minute: 480, end_minute: 1020 }],
    tuesday: [{ start_minute: 480, end_minute: 1020 }],
    wednesday: [{ start_minute: 480, end_minute: 1020 }],
    thursday: [{ start_minute: 480, end_minute: 1020 }],
    friday: [{ start_minute: 480, end_minute: 1020 }],
  },
  holidays: [],
};

export function ServiceDeskSettingsPage({
  clientID,
  capabilities,
}: {
  clientID: string;
  capabilities?: ReadonlySet<string>;
}) {
  const canManageWorkflows =
    !capabilities || capabilities.has("workflow.publish");
  const canManageSLA = !capabilities || capabilities.has("sla.manage");
  const canManageNotifications =
    !capabilities || capabilities.has("notification.manage");
  const canManageRouting = !capabilities || capabilities.has("routing.manage");
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [calendars, setCalendars] = useState<BusinessCalendar[]>([]);
  const [slaPolicies, setSLAPolicies] = useState<SLAPolicy[]>([]);
  const [notificationPolicies, setNotificationPolicies] = useState<
    NotificationPolicy[]
  >([]);
  const [routingRuleSet, setRoutingRuleSet] = useState<RoutingRuleSet>();
  const [routingDirectory, setRoutingDirectory] = useState<Directory>({
    clients: [],
    departments: [],
    teams: [],
    queues: [],
  });
  const [routingDirectoryState, setRoutingDirectoryState] = useState<
    "idle" | "loading" | "ready" | "error"
  >(canManageRouting ? "loading" : "idle");
  const [selectedID, setSelectedID] = useState("new");
  const [selectedCalendarID, setSelectedCalendarID] = useState("new");
  const [selectedSLAID, setSelectedSLAID] = useState("new");
  const [selectedNotificationID, setSelectedNotificationID] = useState("new");
  const [tab, setTab] = useState<
    "workflow" | "routing" | "sla" | "notification"
  >(() =>
    canManageWorkflows
      ? "workflow"
      : canManageRouting
        ? "routing"
        : canManageSLA
          ? "sla"
          : "notification",
  );
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const [loadAttempt, setLoadAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    const options = {
      credentials: "same-origin" as const,
      headers: clientID ? clientContextHeaders(clientID) : {},
      signal: controller.signal,
    };
    const load = async <T,>(
      path: string,
      allowed: boolean,
      requiresClient = false,
    ): Promise<T[]> => {
      if (!allowed || (requiresClient && !clientID)) return [];
      const response = await fetch(path, options);
      if (!response.ok)
        throw new Error("service_desk_configuration_unavailable");
      const payload: unknown = await response.json();
      return Array.isArray(payload) ? (payload as T[]) : [];
    };
    const loadRouting = async (): Promise<RoutingRuleSet | undefined> => {
      if (!canManageRouting) return undefined;
      const response = await fetch("/api/v1/routing-rules/versions", {
        method: "GET",
        credentials: "same-origin",
        signal: controller.signal,
      });
      if (response.status === 404) return undefined;
      if (!response.ok)
        throw new Error("service_desk_routing_configuration_unavailable");
      const payload: unknown = await response.json();
      return payload &&
        typeof payload === "object" &&
        "version" in payload &&
        "rules" in payload
        ? (payload as RoutingRuleSet)
        : undefined;
    };
    const loadRoutingDirectory = async (): Promise<Directory | undefined> => {
      if (!canManageRouting) return undefined;
      setRoutingDirectoryState("loading");
      try {
        const found = await loadDirectory(controller.signal);
        setRoutingDirectoryState("ready");
        return found;
      } catch {
        if (!controller.signal.aborted) setRoutingDirectoryState("error");
        return undefined;
      }
    };
    void Promise.all([
      load<Workflow>("/api/v1/workflows", canManageWorkflows, true),
      load<BusinessCalendar>("/api/v1/business-calendars", canManageSLA),
      load<SLAPolicy>("/api/v1/sla-policies", canManageSLA),
      load<NotificationPolicy>(
        "/api/v1/notification-policies",
        canManageNotifications,
        true,
      ),
      loadRouting(),
      loadRoutingDirectory(),
    ])
      .then(
        ([
          foundWorkflows,
          foundCalendars,
          foundPolicies,
          foundNotificationPolicies,
          foundRoutingRuleSet,
          foundRoutingDirectory,
        ]) => {
          setWorkflows(foundWorkflows);
          setCalendars(foundCalendars);
          setSLAPolicies(foundPolicies);
          setNotificationPolicies(foundNotificationPolicies);
          setRoutingRuleSet(foundRoutingRuleSet);
          if (foundRoutingDirectory) setRoutingDirectory(foundRoutingDirectory);
          setSelectedID(foundWorkflows[0]?.id ?? "new");
          setSelectedCalendarID(foundCalendars[0]?.id ?? "new");
          setSelectedSLAID(foundPolicies[0]?.id ?? "new");
          setSelectedNotificationID(foundNotificationPolicies[0]?.id ?? "new");
          setState("ready");
        },
      )
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [
    canManageNotifications,
    canManageRouting,
    canManageSLA,
    canManageWorkflows,
    clientID,
    loadAttempt,
  ]);

  const selected = workflows.find((workflow) => workflow.id === selectedID);
  const selectedCalendar = calendars.find(
    (calendar) => calendar.id === selectedCalendarID,
  );
  const selectedSLA = slaPolicies.find((policy) => policy.id === selectedSLAID);
  const selectedNotification = notificationPolicies.find(
    (policy) => policy.id === selectedNotificationID,
  );

  async function publish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const conditions = conditionsFromForm(data);
      const definition = workflowFromForm(data);
      const endpoint = selected
        ? `/api/v1/workflows/${encodeURIComponent(selected.id)}/versions`
        : "/api/v1/workflows";
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...clientContextHeaders(clientID),
        },
        body: JSON.stringify({
          expected_version: selected?.version ?? 0,
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          enabled: data.get("enabled") === "on",
          priority: Number(data.get("priority")),
          stable_order: Number(data.get("stable_order")),
          fallback: data.get("fallback") === "on",
          conditions,
          definition,
        }),
      });
      if (!response.ok) throw new Error(`workflow_publish_${response.status}`);
      const published = (await response.json()) as Workflow;
      setWorkflows((current) => {
        const exists = current.some((workflow) => workflow.id === published.id);
        return exists
          ? current.map((workflow) =>
              workflow.id === published.id ? published : workflow,
            )
          : [...current, published];
      });
      setSelectedID(published.id);
      setState("ready");
      setMessage(`Workflow version ${published.version} published.`);
    } catch {
      setState("error");
    }
  }

  async function publishCalendar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const definition = calendarFromForm(data);
      const clientScoped = data.get("calendar_scope") === "client";
      const endpoint = selectedCalendar
        ? `/api/v1/business-calendars/${encodeURIComponent(selectedCalendar.id)}/versions`
        : "/api/v1/business-calendars";
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...(clientScoped ? clientContextHeaders(clientID) : {}),
        },
        body: JSON.stringify({
          expected_version: selectedCalendar?.version ?? 0,
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          definition,
        }),
      });
      if (!response.ok) throw new Error(`calendar_publish_${response.status}`);
      const published = (await response.json()) as BusinessCalendar;
      setCalendars((current) => replaceByID(current, published));
      setSelectedCalendarID(published.id);
      setState("ready");
      setMessage(`Business calendar version ${published.version} published.`);
    } catch {
      setState("error");
    }
  }

  async function publishSLA(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const conditions = conditionsFromForm(data);
      const fallback = data.get("fallback") === "on";
      const clientScoped = selectedSLA
        ? Boolean(selectedSLA.client_id)
        : data.get("policy_scope") === "client";
      const endpoint = selectedSLA
        ? `/api/v1/sla-policies/${encodeURIComponent(selectedSLA.id)}/versions`
        : "/api/v1/sla-policies";
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...(clientScoped ? clientContextHeaders(clientID) : {}),
        },
        body: JSON.stringify({
          expected_version: selectedSLA?.version ?? 0,
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          calendar_id: String(data.get("calendar_id") ?? ""),
          conditions,
          response_target_seconds: Number(data.get("response_minutes")) * 60,
          resolution_target_seconds:
            Number(data.get("resolution_minutes")) * 60,
          warning_percent: Number(data.get("warning_percent")),
          pause_states: data
            .getAll("pause_states")
            .map(String)
            .map((value) => value.trim())
            .filter(Boolean),
          enabled: data.get("enabled") === "on",
          priority: Number(data.get("priority")),
          stable_order: Number(data.get("stable_order")),
          fallback,
        }),
      });
      if (!response.ok) throw new Error(`sla_publish_${response.status}`);
      const published = (await response.json()) as SLAPolicy;
      setSLAPolicies((current) => replaceByID(current, published));
      setSelectedSLAID(published.id);
      setState("ready");
      setMessage(`SLA policy version ${published.version} published.`);
    } catch {
      setState("error");
    }
  }

  async function publishRouting(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const clients = data.getAll("routing_client_id").map(String);
      const recordTypes = data.getAll("routing_record_type").map(String);
      const priorities = data.getAll("routing_priority").map(String);
      const queues = data.getAll("routing_queue_id").map(String);
      const rules = queues.map((queueID, index) => ({
        position: index + 1,
        ...(clients[index] ? { client_id: clients[index] } : {}),
        ...(recordTypes[index] ? { record_type: recordTypes[index] } : {}),
        ...(priorities[index]?.trim()
          ? { priority: priorities[index].trim() }
          : {}),
        queue_id: queueID,
      }));
      rules.push({
        position: rules.length + 1,
        queue_id: String(data.get("fallback_queue_id") ?? ""),
      });
      const response = await fetch("/api/v1/routing-rules/versions", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
        },
        body: JSON.stringify({
          expected_version: routingRuleSet?.version ?? 0,
          rules,
        }),
      });
      if (!response.ok) throw new Error(`routing_publish_${response.status}`);
      const published = (await response.json()) as RoutingRuleSet;
      setRoutingRuleSet(published);
      setState("ready");
      setMessage(`Routing rule set version ${published.version} published.`);
    } catch {
      setState("error");
    }
  }

  async function publishNotification(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      const conditions = conditionsFromForm(data);
      const destinations = destinationsFromForm(data);
      const eventType = String(data.get("event_type") ?? "");
      if (
        eventType === "mention.occurred" &&
        destinations.some(
          (destination) =>
            destination.channel !== "email" && destination.channel !== "teams",
        )
      ) {
        throw new Error("mention_destination_invalid");
      }
      const endpoint = selectedNotification
        ? `/api/v1/notification-policies/${encodeURIComponent(selectedNotification.id)}/versions`
        : "/api/v1/notification-policies";
      const response = await fetch(endpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...csrfHeaders(),
          ...clientContextHeaders(clientID),
        },
        body: JSON.stringify({
          expected_version: selectedNotification?.version ?? 0,
          key: String(data.get("key") ?? ""),
          name: String(data.get("name") ?? ""),
          event_type: eventType,
          conditions,
          destinations,
          quiet_period_seconds: Number(data.get("quiet_period_minutes")) * 60,
          critical_bypass: data.get("critical_bypass") === "on",
          enabled: data.get("enabled") === "on",
          priority: Number(data.get("priority")),
          stable_order: Number(data.get("stable_order")),
        }),
      });
      if (!response.ok) {
        throw new Error(`notification_publish_${response.status}`);
      }
      const published = (await response.json()) as NotificationPolicy;
      setNotificationPolicies((current) => replaceByID(current, published));
      setSelectedNotificationID(published.id);
      setState("ready");
      setMessage(`Notification policy version ${published.version} published.`);
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Service desk administration"
      title="Workflow, SLA, and notifications"
      description="Publish immutable MSP-wide and client-scoped configuration versions. Existing work keeps its recorded selection evidence."
    >
      <div className="service-desk-settings">
        <nav aria-label="Service desk configuration">
          {canManageWorkflows ? (
            <button
              type="button"
              aria-current={tab === "workflow" ? "page" : undefined}
              onClick={() => setTab("workflow")}
            >
              Workflows
            </button>
          ) : null}
          {canManageSLA ? (
            <button
              type="button"
              aria-current={tab === "sla" ? "page" : undefined}
              onClick={() => setTab("sla")}
            >
              Calendars and SLA
            </button>
          ) : null}
          {canManageRouting ? (
            <button
              type="button"
              aria-current={tab === "routing" ? "page" : undefined}
              onClick={() => setTab("routing")}
            >
              Routing
            </button>
          ) : null}
          {canManageNotifications ? (
            <button
              type="button"
              aria-current={tab === "notification" ? "page" : undefined}
              onClick={() => setTab("notification")}
            >
              Notifications
            </button>
          ) : null}
        </nav>
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading service desk configuration"
            description="Retrieving workflows, calendars, SLA policies, and notifications."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Configuration could not be loaded or published"
            description="Verify conditions, fallback rules, ordering, version, and permissions."
            action={
              <Button
                intent="primary"
                onClick={() => setLoadAttempt((current) => current + 1)}
              >
                Reload configuration
              </Button>
            }
            supportCode="SERVICE-DESK-CONFIG"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Configuration published">
            {message}
          </Notice>
        ) : null}
        {tab === "workflow" && canManageWorkflows && !clientID ? (
          <StatePanel
            state="empty"
            title="Select a client to manage workflows."
            description="Workflow versions are scoped to the active client."
          />
        ) : null}
        {tab === "workflow" && canManageWorkflows && clientID ? (
          <section id="workflow-editor" className="policy-editor">
            <aside>
              <button
                type="button"
                onClick={() => {
                  setSelectedID("new");
                  setState("ready");
                  setMessage("");
                }}
              >
                + New workflow
              </button>
              {workflows.map((workflow) => (
                <button
                  type="button"
                  key={workflow.id}
                  className={workflow.id === selectedID ? "active" : ""}
                  onClick={() => {
                    setSelectedID(workflow.id);
                    setState("ready");
                    setMessage("");
                  }}
                >
                  <span>{workflow.name}</span>
                  <small>
                    v{workflow.version} ·{" "}
                    {workflow.enabled ? "enabled" : "disabled"}
                  </small>
                </button>
              ))}
            </aside>
            <WorkflowEditor
              key={`${selected?.id ?? "new"}-${selected?.version ?? 0}`}
              workflow={selected}
              initialFallback={!workflows.length}
              saving={state === "saving"}
              onSubmit={publish}
            />
          </section>
        ) : null}
        {tab === "sla" && canManageSLA ? (
          <div className="sla-settings-stack">
            <PolicyWorkspace
              title="Business calendars"
              createLabel="+ New calendar"
              items={calendars}
              selectedID={selectedCalendarID}
              onSelect={setSelectedCalendarID}
              onCreate={() => setSelectedCalendarID("new")}
            >
              <CalendarEditor
                key={`${selectedCalendar?.id ?? "new"}-${selectedCalendar?.version ?? 0}`}
                calendar={selectedCalendar}
                clientID={clientID}
                saving={state === "saving"}
                onSubmit={publishCalendar}
              />
            </PolicyWorkspace>
            <PolicyWorkspace
              title="SLA policies"
              createLabel="+ New SLA policy"
              items={slaPolicies}
              selectedID={selectedSLAID}
              onSelect={setSelectedSLAID}
              onCreate={() => setSelectedSLAID("new")}
            >
              <SLAEditor
                key={`${selectedSLA?.id ?? "new"}-${selectedSLA?.version ?? 0}`}
                policy={selectedSLA}
                calendars={calendars}
                clientID={clientID}
                initialFallback={!slaPolicies.length}
                saving={state === "saving"}
                onSubmit={publishSLA}
              />
            </PolicyWorkspace>
          </div>
        ) : null}
        {tab === "routing" && canManageRouting ? (
          <RoutingEditor
            key={`${routingRuleSet?.id ?? "new"}-${routingRuleSet?.version ?? 0}`}
            ruleSet={routingRuleSet}
            clients={routingDirectory.clients}
            queues={routingDirectory.queues}
            directoryState={routingDirectoryState}
            saving={state === "saving"}
            onSubmit={publishRouting}
          />
        ) : null}
        {tab === "notification" && canManageNotifications && !clientID ? (
          <StatePanel
            state="empty"
            title="Select a client to manage notifications."
            description="Notification policies are scoped to the active client."
          />
        ) : null}
        {tab === "notification" && canManageNotifications && clientID ? (
          <>
            <MentionNotificationPreferences />
            <PolicyWorkspace
              title="Notification policies"
              createLabel="+ New notification policy"
              items={notificationPolicies}
              selectedID={selectedNotificationID}
              onSelect={setSelectedNotificationID}
              onCreate={() => setSelectedNotificationID("new")}
            >
              <NotificationEditor
                key={`${selectedNotification?.id ?? "new"}-${selectedNotification?.version ?? 0}`}
                policy={selectedNotification}
                saving={state === "saving"}
                onSubmit={publishNotification}
              />
            </PolicyWorkspace>
          </>
        ) : null}
      </div>
    </Page>
  );
}

function RoutingEditor({
  ruleSet,
  clients,
  queues,
  directoryState,
  saving,
  onSubmit,
}: {
  ruleSet?: RoutingRuleSet;
  clients: DirectoryClient[];
  queues: DirectoryEntry[];
  directoryState: "idle" | "loading" | "ready" | "error";
  saving: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  const ordered = [...(ruleSet?.rules ?? [])].sort(
    (left, right) => left.position - right.position,
  );
  const existingFallback = ordered.at(-1);
  const [conditionalRules, setConditionalRules] = useState<RoutingRule[]>(
    ordered.slice(0, -1),
  );
  const globalQueues = queues.filter((queue) => !queue.client_id);
  const updateRule = (index: number, change: Partial<RoutingRule>) => {
    setConditionalRules((current) =>
      current.map((rule, ruleIndex) =>
        ruleIndex === index ? { ...rule, ...change } : rule,
      ),
    );
  };
  const moveRule = (index: number, direction: -1 | 1) => {
    setConditionalRules((current) => {
      const destination = index + direction;
      if (destination < 0 || destination >= current.length) return current;
      const reordered = [...current];
      [reordered[index], reordered[destination]] = [
        reordered[destination],
        reordered[index],
      ];
      return reordered;
    });
  };

  return (
    <section
      aria-labelledby="routing-editor-heading"
      className="routing-editor"
    >
      <form onSubmit={(event) => void onSubmit(event)}>
        <header>
          <div>
            <p className="record-id">
              {ruleSet ? `Version ${ruleSet.version}` : "New configuration"}
            </p>
            <h2 id="routing-editor-heading">Work routing</h2>
            <p>
              Rules are evaluated from top to bottom. The final destination
              catches work that does not match an earlier rule.
            </p>
          </div>
        </header>
        <fieldset>
          <legend>Conditional routes</legend>
          <p>
            Add only the exceptions you need. Leave a condition at “Any” when it
            should not affect the match.
          </p>
          {conditionalRules.map((rule, index) => {
            const availableQueues = queues.filter(
              (queue) => !queue.client_id || queue.client_id === rule.client_id,
            );
            return (
              <div className="routing-rule-row" key={rule.id ?? index}>
                <strong>Rule {index + 1}</strong>
                <label>
                  <span>Client</span>
                  <select
                    aria-label={`Rule ${index + 1} client`}
                    name="routing_client_id"
                    value={rule.client_id ?? ""}
                    onChange={(event) =>
                      updateRule(index, {
                        client_id: event.target.value,
                        queue_id: "",
                      })
                    }
                  >
                    <option value="">Any client</option>
                    {clients.map((client) => (
                      <option key={client.id} value={client.id}>
                        {client.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>Work type</span>
                  <select
                    aria-label={`Rule ${index + 1} work type`}
                    name="routing_record_type"
                    value={rule.record_type ?? ""}
                    onChange={(event) =>
                      updateRule(index, { record_type: event.target.value })
                    }
                  >
                    <option value="">Any type</option>
                    <option value="incident">Incident</option>
                    <option value="request">Request</option>
                    <option value="change">Change</option>
                    <option value="problem">Problem</option>
                  </select>
                </label>
                <label>
                  <span>Priority</span>
                  <input
                    aria-label={`Rule ${index + 1} priority`}
                    name="routing_priority"
                    value={rule.priority ?? ""}
                    placeholder="Any priority"
                    onChange={(event) =>
                      updateRule(index, { priority: event.target.value })
                    }
                  />
                </label>
                <label>
                  <span>Send to</span>
                  <select
                    aria-label={`Rule ${index + 1} destination`}
                    name="routing_queue_id"
                    value={rule.queue_id}
                    onChange={(event) =>
                      updateRule(index, { queue_id: event.target.value })
                    }
                    required
                  >
                    <option value="" disabled>
                      Select a queue
                    </option>
                    {availableQueues.map((queue) => (
                      <option key={queue.id} value={queue.id}>
                        {queue.name}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="routing-rule-actions">
                  <button
                    type="button"
                    aria-label={`Move rule ${index + 1} up`}
                    disabled={index === 0}
                    onClick={() => moveRule(index, -1)}
                  >
                    Move up
                  </button>
                  <button
                    type="button"
                    aria-label={`Move rule ${index + 1} down`}
                    disabled={index === conditionalRules.length - 1}
                    onClick={() => moveRule(index, 1)}
                  >
                    Move down
                  </button>
                  <button
                    type="button"
                    aria-label={`Remove rule ${index + 1}`}
                    onClick={() =>
                      setConditionalRules((current) =>
                        current.filter((_, ruleIndex) => ruleIndex !== index),
                      )
                    }
                  >
                    Remove
                  </button>
                </div>
              </div>
            );
          })}
          <button
            type="button"
            onClick={() =>
              setConditionalRules((current) => [
                ...current,
                { position: current.length + 1, queue_id: "" },
              ])
            }
          >
            + Add conditional route
          </button>
        </fieldset>
        <label>
          <span>Fallback destination</span>
          <select
            aria-label="Fallback destination"
            name="fallback_queue_id"
            defaultValue={existingFallback?.queue_id ?? ""}
            required
          >
            <option value="" disabled>
              Select a global queue
            </option>
            {globalQueues.map((queue) => (
              <option key={queue.id} value={queue.id}>
                {queue.name}
              </option>
            ))}
          </select>
          <small>
            Required. This MSP-wide queue receives any work that did not match a
            conditional route.
          </small>
        </label>
        {directoryState === "error" ? (
          <Notice
            tone="danger"
            title="Routing destinations could not be loaded"
          >
            Verify directory permissions and connectivity, then reload this
            page.
          </Notice>
        ) : null}
        {directoryState === "ready" && !globalQueues.length ? (
          <Notice tone="warning" title="A global queue is required">
            Create an MSP-wide queue in Clients and directory before publishing
            routing.
          </Notice>
        ) : null}
        <button
          type="submit"
          disabled={
            saving || directoryState !== "ready" || !globalQueues.length
          }
        >
          {saving
            ? "Publishing…"
            : ruleSet
              ? "Publish routing replacement"
              : "Publish routing rules"}
        </button>
      </form>
    </section>
  );
}

function WorkflowEditor({
  workflow,
  initialFallback,
  saving,
  onSubmit,
}: {
  workflow?: Workflow;
  initialFallback: boolean;
  saving: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  return (
    <form onSubmit={(event) => void onSubmit(event)}>
      <header>
        <div>
          <p className="record-id">
            {workflow ? `Version ${workflow.version}` : "New configuration"}
          </p>
          <h2>{workflow?.name ?? "Create workflow"}</h2>
        </div>
      </header>
      <div className="policy-fields">
        <label>
          <span>Key</span>
          <input name="key" defaultValue={workflow?.key ?? ""} required />
        </label>
        <label>
          <span>Name</span>
          <input name="name" defaultValue={workflow?.name ?? ""} required />
        </label>
        <label>
          <span>Priority</span>
          <input
            name="priority"
            type="number"
            defaultValue={workflow?.priority ?? 0}
            required
          />
        </label>
        <label>
          <span>Stable order</span>
          <input
            name="stable_order"
            type="number"
            min="0"
            defaultValue={workflow?.stable_order ?? 0}
            required
          />
        </label>
        <label className="work-check">
          <input
            name="enabled"
            type="checkbox"
            defaultChecked={workflow?.enabled ?? true}
          />{" "}
          Enabled
        </label>
        <label className="work-check">
          <input
            name="fallback"
            type="checkbox"
            defaultChecked={workflow?.fallback ?? initialFallback}
          />{" "}
          Unconditional fallback
        </label>
      </div>
      <ConditionBuilder value={workflow?.conditions ?? {}} />
      <WorkflowBuilder definition={workflow?.definition ?? defaultDefinition} />
      <button type="submit" disabled={saving}>
        {saving
          ? "Publishing…"
          : workflow
            ? "Publish replacement version"
            : "Publish workflow"}
      </button>
    </form>
  );
}

function PolicyWorkspace<
  T extends { id: string; name: string; version: number },
>({
  title,
  createLabel,
  items,
  selectedID,
  onSelect,
  onCreate,
  children,
}: {
  title: string;
  createLabel: string;
  items: T[];
  selectedID: string;
  onSelect: (id: string) => void;
  onCreate: () => void;
  children: ReactNode;
}) {
  return (
    <section aria-label={title} className="policy-editor">
      <aside>
        <h2>{title}</h2>
        <button type="button" onClick={onCreate}>
          {createLabel}
        </button>
        {items.map((item) => (
          <button
            type="button"
            key={item.id}
            className={item.id === selectedID ? "active" : ""}
            onClick={() => onSelect(item.id)}
          >
            <span>{item.name}</span>
            <small>Version {item.version}</small>
          </button>
        ))}
      </aside>
      {children}
    </section>
  );
}

function CalendarEditor({
  calendar,
  clientID,
  saving,
  onSubmit,
}: {
  calendar?: BusinessCalendar;
  clientID: string;
  saving: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  return (
    <form onSubmit={(event) => void onSubmit(event)}>
      <header>
        <div>
          <p className="record-id">
            {calendar ? `Version ${calendar.version}` : "New configuration"}
          </p>
          <h2>{calendar?.name ?? "Create business calendar"}</h2>
        </div>
      </header>
      <div className="policy-fields">
        <label>
          <span>Availability</span>
          <select
            aria-label="Availability"
            name="calendar_scope"
            defaultValue={calendar?.client_id ? "client" : "global"}
            disabled={Boolean(calendar)}
          >
            <option value="global">All clients (MSP-wide)</option>
            <option value="client" disabled={!clientID}>
              Active client only
            </option>
          </select>
          {calendar ? (
            <input
              type="hidden"
              name="calendar_scope"
              value={calendar.client_id ? "client" : "global"}
            />
          ) : null}
          <small>
            Scope is fixed after the first version so existing SLA bindings
            remain stable.
          </small>
        </label>
        <label>
          <span>Key</span>
          <input name="key" defaultValue={calendar?.key ?? ""} required />
        </label>
        <label>
          <span>Name</span>
          <input name="name" defaultValue={calendar?.name ?? ""} required />
        </label>
      </div>
      <CalendarBuilder
        definition={calendar?.definition ?? defaultCalendarDefinition}
      />
      <button type="submit" disabled={saving}>
        {saving
          ? "Publishing…"
          : calendar
            ? "Publish calendar replacement"
            : "Publish calendar"}
      </button>
    </form>
  );
}

function SLAEditor({
  policy,
  calendars,
  clientID,
  initialFallback,
  saving,
  onSubmit,
}: {
  policy?: SLAPolicy;
  calendars: BusinessCalendar[];
  clientID: string;
  initialFallback: boolean;
  saving: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  const [fallback, setFallback] = useState(policy?.fallback ?? initialFallback);
  const [policyScope, setPolicyScope] = useState<"global" | "client">(
    policy
      ? policy.client_id
        ? "client"
        : "global"
      : initialFallback || !clientID
        ? "global"
        : "client",
  );
  const compatibleCalendars = calendars.filter(
    (calendar) =>
      !calendar.client_id ||
      (policyScope === "client" && calendar.client_id === clientID),
  );
  const [calendarID, setCalendarID] = useState(
    policy?.calendar.id ?? compatibleCalendars[0]?.id ?? "",
  );
  const selectScope = (scope: "global" | "client") => {
    setPolicyScope(scope);
    const compatible = calendars.filter(
      (calendar) =>
        !calendar.client_id ||
        (scope === "client" && calendar.client_id === clientID),
    );
    setCalendarID(compatible[0]?.id ?? "");
  };

  return (
    <form onSubmit={(event) => void onSubmit(event)}>
      <header>
        <div>
          <p className="record-id">
            {policy ? `Version ${policy.version}` : "New configuration"}
          </p>
          <h2>{policy?.name ?? "Create SLA policy"}</h2>
        </div>
      </header>
      <div className="policy-fields">
        <label>
          <span>Policy scope</span>
          <select
            aria-label="Policy scope"
            name="policy_scope"
            value={policyScope}
            disabled={Boolean(policy) || fallback}
            onChange={(event) =>
              selectScope(event.target.value as "global" | "client")
            }
          >
            <option value="global">All clients (MSP-wide)</option>
            <option value="client" disabled={!clientID}>
              Active client only
            </option>
          </select>
          {policy || fallback ? (
            <input type="hidden" name="policy_scope" value={policyScope} />
          ) : null}
          <small>
            Scope is fixed after the first version. A fallback policy must be
            MSP-wide.
          </small>
        </label>
        <label>
          <span>Key</span>
          <input name="key" defaultValue={policy?.key ?? ""} required />
        </label>
        <label>
          <span>Name</span>
          <input name="name" defaultValue={policy?.name ?? ""} required />
        </label>
        <label>
          <span>Business calendar</span>
          <select
            name="calendar_id"
            value={calendarID}
            onChange={(event) => setCalendarID(event.target.value)}
            required
          >
            <option value="" disabled>
              Select a calendar
            </option>
            {compatibleCalendars.map((calendar) => (
              <option key={calendar.id} value={calendar.id}>
                {calendar.name} v{calendar.version} ·{" "}
                {calendar.client_id ? "Active client" : "MSP-wide"}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Response target (minutes)</span>
          <input
            name="response_minutes"
            type="number"
            min="1"
            defaultValue={policy ? policy.response_target_seconds / 60 : 60}
            required
          />
        </label>
        <label>
          <span>Resolution target (minutes)</span>
          <input
            name="resolution_minutes"
            type="number"
            min="1"
            defaultValue={policy ? policy.resolution_target_seconds / 60 : 480}
            required
          />
        </label>
        <label>
          <span>Warning percent</span>
          <input
            name="warning_percent"
            type="number"
            min="1"
            max="99"
            defaultValue={policy?.warning_percent ?? 75}
            required
          />
        </label>
        <label>
          <span>Priority</span>
          <input
            name="priority"
            type="number"
            defaultValue={policy?.priority ?? 0}
            required
          />
        </label>
        <label>
          <span>Stable order</span>
          <input
            name="stable_order"
            type="number"
            min="0"
            defaultValue={policy?.stable_order ?? 0}
            required
          />
        </label>
        <label className="work-check">
          <input
            name="enabled"
            type="checkbox"
            defaultChecked={policy?.enabled ?? true}
          />{" "}
          Enabled
        </label>
        <label className="work-check">
          <input
            name="fallback"
            type="checkbox"
            checked={fallback}
            disabled={Boolean(policy?.client_id)}
            onChange={(event) => {
              const checked = event.target.checked;
              setFallback(checked);
              if (checked) selectScope("global");
            }}
          />{" "}
          Unconditional fallback
        </label>
      </div>
      <ConditionBuilder value={policy?.conditions ?? {}} />
      <TagInput
        label="Pause states"
        name="pause_states"
        defaultValues={policy?.pause_states ?? ["pending"]}
        placeholder="pending"
      />
      <button type="submit" disabled={saving || !compatibleCalendars.length}>
        {saving
          ? "Publishing…"
          : policy
            ? "Publish SLA replacement"
            : "Publish SLA policy"}
      </button>
    </form>
  );
}

function NotificationEditor({
  policy,
  saving,
  onSubmit,
}: {
  policy?: NotificationPolicy;
  saving: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  const [eventType, setEventType] = useState(policy?.event_type ?? "");
  const mentionPreset = eventType === "mention.occurred";
  const defaultDestinations: NotificationDestination[] = [
    {
      channel: "in_app",
      recipient_ref: "assigned_technician",
      content_classification: "internal",
    },
  ];
  return (
    <form onSubmit={(event) => void onSubmit(event)}>
      <header>
        <div>
          <p className="record-id">
            {policy ? `Version ${policy.version}` : "New configuration"}
          </p>
          <h2>{policy?.name ?? "Create notification policy"}</h2>
        </div>
      </header>
      {!policy ? (
        <button type="button" onClick={() => setEventType("mention.occurred")}>
          Use Mentions preset
        </button>
      ) : null}
      <div className="policy-fields">
        <label>
          <span>Key</span>
          <input name="key" defaultValue={policy?.key ?? ""} required />
        </label>
        <label>
          <span>Name</span>
          <input name="name" defaultValue={policy?.name ?? ""} required />
        </label>
        <label>
          <span>Event type</span>
          <input
            name="event_type"
            value={eventType}
            onChange={(event) => setEventType(event.target.value)}
            placeholder="sla.warning"
            readOnly={mentionPreset}
            required
          />
        </label>
        <label>
          <span>Quiet period (minutes)</span>
          <input
            name="quiet_period_minutes"
            type="number"
            min="0"
            defaultValue={policy ? policy.quiet_period_seconds / 60 : 10}
            required
          />
        </label>
        <label>
          <span>Priority</span>
          <input
            name="priority"
            type="number"
            defaultValue={policy?.priority ?? 0}
            required
          />
        </label>
        <label>
          <span>Stable order</span>
          <input
            name="stable_order"
            type="number"
            min="0"
            defaultValue={policy?.stable_order ?? 0}
            required
          />
        </label>
        <label className="work-check">
          <input
            name="enabled"
            type="checkbox"
            defaultChecked={policy?.enabled ?? true}
          />{" "}
          Enabled
        </label>
        <label className="work-check">
          <input
            name="critical_bypass"
            type="checkbox"
            defaultChecked={policy?.critical_bypass ?? true}
          />{" "}
          Critical events bypass quiet period
        </label>
      </div>
      <ConditionBuilder value={policy?.conditions ?? {}} />
      <DestinationBuilder
        key={mentionPreset ? "mention" : "general"}
        destinations={
          mentionPreset
            ? policy?.event_type === "mention.occurred"
              ? policy.destinations
              : [
                  {
                    channel: "email",
                    recipient_ref: "recipient",
                    content_classification: "internal",
                  },
                ]
            : (policy?.destinations ?? defaultDestinations)
        }
        allowedChannels={mentionPreset ? ["email", "teams"] : undefined}
      />
      <button type="submit" disabled={saving}>
        {saving
          ? "Publishing…"
          : policy
            ? "Publish notification replacement"
            : "Publish notification policy"}
      </button>
    </form>
  );
}

function replaceByID<T extends { id: string }>(
  current: T[],
  replacement: T,
): T[] {
  return current.some((item) => item.id === replacement.id)
    ? current.map((item) => (item.id === replacement.id ? replacement : item))
    : [...current, replacement];
}
