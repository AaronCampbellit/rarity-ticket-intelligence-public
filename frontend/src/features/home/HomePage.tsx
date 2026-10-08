import {
  ArrowRight,
  BookOpen,
  BriefcaseBusiness,
  CalendarClock,
  CircleAlert,
  Headphones,
  ReceiptText,
  Search,
  ServerCog,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import { useEffect, useState } from "react";

import { clientContextHeaders } from "../../api/clientContext";
import type { RouteID } from "../../app/routes";
import {
  StatusBadge,
  useMainContentID,
  useOptionalWorkspace,
} from "../../design-system";
import { listWorkRecords, type WorkRecord } from "../work/api";
import { MentionsWidget } from "../mentions/MentionsWidget";
import type { MentionDeepLink } from "../mentions/types";
import { ticketWorkspaceItem } from "../work/workspaceItem";
import "./home.css";
import { UpcomingSchedule } from "../calendar/UpcomingSchedule";

type HomePageProps = {
  capabilities: ReadonlySet<string>;
  availableRoutes: ReadonlySet<RouteID>;
  clientID?: string;
  principalID: string;
  onNavigate: (route: RouteID) => void;
  onOpenMention?: (link: MentionDeepLink) => void;
};

type HomeAction = {
  route: RouteID;
  title: string;
  description: string;
  icon: typeof Headphones;
};

type LoadState = "idle" | "loading" | "ready" | "error";
type HealthSnapshot = {
  state: "disabled" | "healthy" | "degraded" | "failed";
  generated_at: string;
  connections: Array<{ id: string; state: string }>;
};
type ApprovalEntry = { id: string; approval_state: string };
type AuditEntry = { id: string; occurred_at: string; action: string };

export function HomePage({
  capabilities,
  availableRoutes,
  clientID,
  principalID,
  onNavigate,
  onOpenMention = () => {},
}: HomePageProps) {
  const mainContentID = useMainContentID();
  const workspace = useOptionalWorkspace();
  const [work, setWork] = useState<WorkRecord[]>([]);
  const [workClientID, setWorkClientID] = useState<string>();
  const [workState, setWorkState] = useState<LoadState>("idle");
  const [health, setHealth] = useState<HealthSnapshot>();
  const [healthState, setHealthState] = useState<LoadState>("idle");
  const [approvals, setApprovals] = useState<ApprovalEntry[]>([]);
  const [approvalState, setApprovalState] = useState<LoadState>("idle");
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [auditState, setAuditState] = useState<LoadState>("idle");
  const availableActions: HomeAction[] = [
    {
      route: "work",
      title: "Triage and update work",
      description:
        "Open your authorized queue, switch views, and update records.",
      icon: Headphones,
    },
    {
      route: "knowledge",
      title: "Search knowledge",
      description:
        "Find internal guidance without leaving your current workflow.",
      icon: BookOpen,
    },
    {
      route: "ai-assist",
      title: "AI assistance",
      description: "Generate a review-only recommendation for a work record.",
      icon: Sparkles,
    },
    {
      route: "sales",
      title: "Manage the pipeline",
      description: "Move opportunities through an authorized sales workflow.",
      icon: BriefcaseBusiness,
    },
    {
      route: "project",
      title: "Coordinate delivery",
      description: "Review projects, tasks, resources, and change orders.",
      icon: CalendarClock,
    },
    {
      route: "billing",
      title: "Review time and billing",
      description: "Approve time entries and prepare authorized exports.",
      icon: ReceiptText,
    },
  ];
  const actions = availableActions.filter(({ route }) =>
    availableRoutes.has(route),
  );

  const hasOperations = availableRoutes.has("operations");
  const hasWork = availableRoutes.has("work");
  const hasAudit =
    availableRoutes.has("audit") && capabilities.has("audit.read");
  const hasBilling = availableRoutes.has("billing");
  const greeting = new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
  }).format(new Date());

  useEffect(() => {
    if (!clientID || !availableRoutes.has("work")) {
      setWork([]);
      setWorkClientID(undefined);
      setWorkState("idle");
      return;
    }
    const controller = new AbortController();
    setWork([]);
    setWorkClientID(undefined);
    setWorkState("loading");
    void listWorkRecords(clientID, controller.signal)
      .then((records) => {
        setWork(records);
        setWorkClientID(clientID);
        setWorkState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setWorkState("error");
      });
    return () => controller.abort();
  }, [availableRoutes, clientID]);

  useEffect(() => {
    if (!hasOperations) return;
    const controller = new AbortController();
    setHealthState("loading");
    void fetch("/api/v1/integrations/health", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error("health_unavailable");
        return (await response.json()) as HealthSnapshot;
      })
      .then((snapshot) => {
        setHealth(snapshot);
        setHealthState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setHealthState("error");
      });
    return () => controller.abort();
  }, [hasOperations]);

  useEffect(() => {
    if (!hasBilling || !clientID) return;
    const controller = new AbortController();
    setApprovals([]);
    setApprovalState("loading");
    void fetch("/api/v1/time-entries/approvals?state=pending&limit=100", {
      credentials: "same-origin",
      headers: clientContextHeaders(clientID),
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error("approvals_unavailable");
        return (await response.json()) as ApprovalEntry[];
      })
      .then((entries) => {
        setApprovals(entries);
        setApprovalState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setApprovalState("error");
      });
    return () => controller.abort();
  }, [clientID, hasBilling]);

  useEffect(() => {
    if (!hasAudit) return;
    const controller = new AbortController();
    setAuditState("loading");
    void fetch("/api/v1/admin/audit?limit=5", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error("audit_unavailable");
        return (await response.json()) as AuditEntry[];
      })
      .then((entries) => {
        setAudit(entries);
        setAuditState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setAuditState("error");
      });
    return () => controller.abort();
  }, [hasAudit]);

  const prioritizedWork =
    workClientID === clientID
      ? [...work].sort((left, right) => {
          const assignedLeft = left.primaryOwnerID === principalID ? 0 : 1;
          const assignedRight = right.primaryOwnerID === principalID ? 0 : 1;
          if (assignedLeft !== assignedRight)
            return assignedLeft - assignedRight;
          const rank = ["critical", "high", "medium", "low"];
          return rank.indexOf(left.priority) - rank.indexOf(right.priority);
        })
      : [];

  return (
    <main id={mainContentID} className="rti-home" data-mode="console">
      <header className="rti-home__welcome">
        <div>
          <p className="rti-eyebrow">{greeting}</p>
          <h1>Your operational home</h1>
          <p>Your work, schedule, and team activity.</p>
        </div>
        {availableRoutes.has("work") ? (
          <button type="button" onClick={() => onNavigate("work")}>
            <Search size={17} aria-hidden="true" />
            Open work
          </button>
        ) : null}
      </header>

      <div className="rti-home__grid">
        <div className="rti-home__primary">
          {hasWork ? (
            <section className="rti-home__work" aria-label="Signal live queue">
              <header>
                <div>
                  <span className="rti-section-icon">
                    <Headphones size={17} aria-hidden="true" />
                  </span>
                  <div>
                    <h2>Live work</h2>
                    <p>Assigned and available work for the active client</p>
                  </div>
                </div>
                <button type="button" onClick={() => onNavigate("work")}>
                  Open queue <ArrowRight size={14} aria-hidden="true" />
                </button>
              </header>
              <div className="rti-home__live-work" aria-live="polite">
                {workState === "idle" ? (
                  <p>Select an authorized client to load live work.</p>
                ) : null}
                {workState === "loading" ? <p>Loading live work…</p> : null}
                {workState === "error" ? (
                  <p>Live work is unavailable. Open the queue to retry.</p>
                ) : null}
                {workState === "ready" && !prioritizedWork.length ? (
                  <p>No open work is available in this client scope.</p>
                ) : null}
                {prioritizedWork.slice(0, 5).map((record) => (
                  <button
                    type="button"
                    key={record.id}
                    onClick={() => {
                      if (!clientID) return;
                      workspace?.openPreview(
                        ticketWorkspaceItem(record, clientID),
                      );
                    }}
                    onDoubleClick={() => {
                      if (!clientID) return;
                      workspace?.openRecord(
                        ticketWorkspaceItem(record, clientID),
                      );
                    }}
                  >
                    <span
                      className="rti-home__priority"
                      data-tone={record.priority.toLowerCase()}
                    >
                      {record.priority}
                    </span>
                    <span>
                      <strong>{record.title}</strong>
                      <small>
                        {record.displayID} ·{" "}
                        {record.primaryOwnerID === principalID
                          ? "Assigned to you"
                          : record.primaryOwnerID
                            ? "Assigned"
                            : "Unassigned"}{" "}
                        · {record.status}
                      </small>
                    </span>
                    <ArrowRight size={16} aria-hidden="true" />
                  </button>
                ))}
              </div>
            </section>
          ) : null}

          <section className="rti-home__schedule-panel">
            <header>
              <div>
                <span className="rti-section-icon">
                  <CalendarClock size={17} aria-hidden="true" />
                </span>
                <h2>Schedule</h2>
              </div>
            </header>
            <div className="rti-home__feed">
              {availableRoutes.has("calendar") ? (
                <UpcomingSchedule onNavigate={() => onNavigate("calendar")} />
              ) : (
                <p>Calendar access is not available for your role.</p>
              )}
            </div>
          </section>
        </div>
        <aside className="rti-home__side">
          {capabilities.has("mention.read") ? (
            <MentionsWidget onOpen={onOpenMention} />
          ) : null}
          {hasOperations ? (
            <section>
              <header>
                <div>
                  <span className="rti-section-icon">
                    <ServerCog size={17} aria-hidden="true" />
                  </span>
                  <h2>Platform health</h2>
                </div>
                {health ? (
                  <StatusBadge
                    tone={
                      health.state === "healthy"
                        ? "success"
                        : health.state === "degraded"
                          ? "warning"
                          : health.state === "failed"
                            ? "danger"
                            : "neutral"
                    }
                  >
                    {health.state}
                  </StatusBadge>
                ) : null}
              </header>
              <div className="rti-home__feed">
                {healthState === "loading" ? <p>Checking platform…</p> : null}
                {healthState === "error" ? (
                  <p>Health evidence is currently unavailable.</p>
                ) : null}
                {healthState === "ready" ? (
                  <p>
                    {health?.connections.length ?? 0} monitored connection
                    {(health?.connections.length ?? 0) === 1 ? "" : "s"}.
                  </p>
                ) : null}
                <button type="button" onClick={() => onNavigate("operations")}>
                  View live health <ArrowRight size={14} aria-hidden="true" />
                </button>
              </div>
            </section>
          ) : null}

          {hasBilling ? (
            <section>
              <header>
                <div>
                  <span className="rti-section-icon">
                    <ReceiptText size={17} aria-hidden="true" />
                  </span>
                  <h2>Pending approvals</h2>
                </div>
                {approvalState === "ready" ? (
                  <strong>{approvals.length}</strong>
                ) : null}
              </header>
              <div className="rti-home__feed">
                <p>
                  {approvalState === "loading"
                    ? "Loading approval queue…"
                    : approvalState === "error"
                      ? "Approval evidence is unavailable."
                      : approvalState === "idle"
                        ? "Select a client to load approvals."
                        : approvals.length
                          ? `${approvals.length} time ${approvals.length === 1 ? "entry needs" : "entries need"} review.`
                          : "No time entries are waiting for review."}
                </p>
                <button type="button" onClick={() => onNavigate("billing")}>
                  Review approvals <ArrowRight size={14} aria-hidden="true" />
                </button>
              </div>
            </section>
          ) : null}

          {hasAudit ? (
            <section>
              <header>
                <div>
                  <span className="rti-section-icon">
                    <ShieldCheck size={17} aria-hidden="true" />
                  </span>
                  <h2>Security activity</h2>
                </div>
              </header>
              <div className="rti-home__feed">
                <p>
                  {auditState === "loading"
                    ? "Loading audit evidence…"
                    : auditState === "error"
                      ? "Audit evidence is unavailable."
                      : audit.length
                        ? `${audit.length} recent ${audit.length === 1 ? "event" : "events"} available.`
                        : "No recent events in this scope."}
                </p>
                <button type="button" onClick={() => onNavigate("audit")}>
                  Review audit <ArrowRight size={14} aria-hidden="true" />
                </button>
              </div>
            </section>
          ) : null}
        </aside>
      </div>

      <section className="rti-home__launchers">
        <header>
          <CircleAlert size={16} aria-hidden="true" />
          <div>
            <h2>More workspaces</h2>
            <p>Role-aware tools available in your current session</p>
          </div>
        </header>
        <div className="rti-home__worklist">
          {actions.map(({ route, title, description, icon: Icon }) => (
            <button
              type="button"
              key={route}
              aria-label={`Open ${title}`}
              onClick={() => onNavigate(route)}
            >
              <span className="rti-home__priority" data-tone="neutral">
                <Icon size={16} aria-hidden="true" />
              </span>
              <span>
                <strong>{title}</strong>
                <small>{description}</small>
              </span>
              <ArrowRight
                className="rti-home__row-arrow"
                size={16}
                aria-hidden="true"
              />
            </button>
          ))}
        </div>
      </section>
    </main>
  );
}
