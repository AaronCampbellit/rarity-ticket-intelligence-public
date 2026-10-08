import { LogOut, Sparkles } from "lucide-react";
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  loadDirectory,
  logout,
  principalHasNavigation,
  refreshSession,
  type DirectoryClient,
  type DirectoryEntry,
  type PrincipalNavigation,
} from "./api/browserSession";

import { aiAssistAPI as defaultAIAssistAPI } from "./features/ai/api";
import type {
  AIAssistAPI,
  AISettingsAPI,
  AIWorkspaceAPI,
} from "./features/ai/types";
import { AIWorkspace } from "./features/ai/AIWorkspace";

import { GlobalWorkActions } from "./features/work/GlobalWorkActions";

import { SetupPage } from "./features/setup/SetupPage";
import { PublicAccessPage } from "./features/auth/PublicAccessPage";
import {
  clearReturnHash,
  loadApplicationAccess,
  rememberReturnHash,
  peekReturnHash,
  safeReturnHash,
  type ApplicationAccess,
} from "./features/auth/applicationAccess";
import { sessionExpiredEvent } from "./api/sessionExpiry";

import type { MentionDeepLink } from "./features/mentions/types";
import type { TeamsSettingsAPI } from "./features/teams/types";

import type { ClassificationAdminAPI } from "./features/classification/types";
import { NotificationCenter } from "./features/notifications/NotificationCenter";

import {
  AppShell,
  RarityBrand,
  clearWorkspaceStorage,
  useMainContentID,
  type WorkspaceItem,
} from "./design-system";
import {
  navigation,
  navigationGroup,
  pageFromHash,
  mentionNavigationFromHash,
  type Page,
} from "./navigation";
import { routeForID } from "./app/routes";
import "./app.css";
import { FeatureBoundary } from "./app/FeatureBoundary";

const CalendarPage = lazy(() =>
  import("./features/calendar/CalendarPage").then((module) => ({
    default: module.CalendarPage,
  })),
);

const LiveConversionPage = lazy(() =>
  import("./features/projects/LiveConversionPage").then((module) => ({
    default: module.LiveConversionPage,
  })),
);
const ProjectWorklist = lazy(() =>
  import("./features/projects/ProjectWorklist").then((module) => ({
    default: module.ProjectWorklist,
  })),
);
const AISettingsPage = lazy(() =>
  import("./features/ai/AISettingsPage").then((module) => ({
    default: module.AISettingsPage,
  })),
);
const AIAssistPanel = lazy(() =>
  import("./features/ai/AIAssistPanel").then((module) => ({
    default: module.AIAssistPanel,
  })),
);
const TeamsSettingsPage = lazy(() =>
  import("./features/teams/TeamsSettingsPage").then((module) => ({
    default: module.TeamsSettingsPage,
  })),
);
const SessionsPage = lazy(() =>
  import("./features/auth/SessionsPage").then((module) => ({
    default: module.SessionsPage,
  })),
);
const RecoveryAccessPage = lazy(() =>
  import("./features/auth/RecoveryAccessPage").then((module) => ({
    default: module.RecoveryAccessPage,
  })),
);
const RoleManagementPage = lazy(() =>
  import("./features/auth/RoleManagementPage").then((module) => ({
    default: module.RoleManagementPage,
  })),
);
const AuditLogPage = lazy(() =>
  import("./features/auth/AuditLogPage").then((module) => ({
    default: module.AuditLogPage,
  })),
);
const ServiceKeysPage = lazy(() =>
  import("./features/auth/ServiceKeysPage").then((module) => ({
    default: module.ServiceKeysPage,
  })),
);
const TechnicianWorklist = lazy(() =>
  import("./features/work/TechnicianWorklist").then((module) => ({
    default: module.TechnicianWorklist,
  })),
);
const TimesheetPage = lazy(() =>
  import("./features/timesheets/TimesheetPage").then((module) => ({
    default: module.TimesheetPage,
  })),
);
const ServiceDeskSettingsPage = lazy(() =>
  import("./features/work/ServiceDeskSettingsPage").then((module) => ({
    default: module.ServiceDeskSettingsPage,
  })),
);
const OperationsHealthPage = lazy(() =>
  import("./features/operations/OperationsHealthPage").then((module) => ({
    default: module.OperationsHealthPage,
  })),
);
const WebhooksPage = lazy(() =>
  import("./features/operations/WebhooksPage").then((module) => ({
    default: module.WebhooksPage,
  })),
);
const DattoReconciliationPage = lazy(() =>
  import("./features/operations/DattoReconciliationPage").then((module) => ({
    default: module.DattoReconciliationPage,
  })),
);
const DattoSettingsPage = lazy(() =>
  import("./features/operations/DattoSettingsPage").then((module) => ({
    default: module.DattoSettingsPage,
  })),
);
const ForwardingSettingsPage = lazy(() =>
  import("./features/operations/ForwardingSettingsPage").then((module) => ({
    default: module.ForwardingSettingsPage,
  })),
);
const GraphSettingsPage = lazy(() =>
  import("./features/operations/GraphSettingsPage").then((module) => ({
    default: module.GraphSettingsPage,
  })),
);
const AutomationPage = lazy(() =>
  import("./features/automation/AutomationPage").then((module) => ({
    default: module.AutomationPage,
  })),
);
const KnowledgePage = lazy(() =>
  import("./features/knowledge/KnowledgePage").then((module) => ({
    default: module.KnowledgePage,
  })),
);
const BillingPage = lazy(() =>
  import("./features/billing/BillingPage").then((module) => ({
    default: module.BillingPage,
  })),
);
const DirectorySettingsPage = lazy(() =>
  import("./features/organization/DirectorySettingsPage").then((module) => ({
    default: module.DirectorySettingsPage,
  })),
);
const ClientResourcesPage = lazy(() =>
  import("./features/organization/ClientResourcesPage").then((module) => ({
    default: module.ClientResourcesPage,
  })),
);
const HomePage = lazy(() =>
  import("./features/home/HomePage").then((module) => ({
    default: module.HomePage,
  })),
);
const PipelineSettingsPage = lazy(() =>
  import("./features/sales/PipelineSettingsPage").then((module) => ({
    default: module.PipelineSettingsPage,
  })),
);
const ClassificationSettingsPage = lazy(() =>
  import("./features/classification/ClassificationSettingsPage").then(
    (module) => ({ default: module.ClassificationSettingsPage }),
  ),
);
const ClassificationInsightsPage = lazy(() =>
  import("./features/classification/ClassificationInsightsPage").then(
    (module) => ({ default: module.ClassificationInsightsPage }),
  ),
);
const ProspectsPage = lazy(() =>
  import("./features/sales/ProspectsPage").then((module) => ({
    default: module.ProspectsPage,
  })),
);
const OpportunityWorklist = lazy(() =>
  import("./features/sales/SalesWorklists").then((module) => ({
    default: module.OpportunityWorklist,
  })),
);
const ProposalWorklist = lazy(() =>
  import("./features/sales/SalesWorklists").then((module) => ({
    default: module.ProposalWorklist,
  })),
);
const DesignSystemCatalog = lazy(() =>
  import("./design-system/catalog/DesignSystemCatalog").then((module) => ({
    default: module.DesignSystemCatalog,
  })),
);

export type BuildInfo = {
  revision: string;
};

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function workRecordIDFromHash(hash: string): string | undefined {
  const query = hash.split("?", 2)[1];
  const workRecordID = query
    ? new URLSearchParams(query).get("workRecordID")?.trim()
    : undefined;
  return workRecordID && uuidPattern.test(workRecordID)
    ? workRecordID
    : undefined;
}

function boundedParameter(
  parameters: URLSearchParams,
  name: string,
  maximum: number,
) {
  const value = parameters.get(name)?.trim();
  return value && value.length <= maximum ? value : undefined;
}

export function workspaceItemFromHash(hash: string): WorkspaceItem | undefined {
  const query = hash.split("?", 2)[1];
  if (!query) return undefined;
  const parameters = new URLSearchParams(query);
  const entityType = parameters.get("recordType");
  if (
    entityType !== "ticket" &&
    entityType !== "task" &&
    entityType !== "project"
  ) {
    return undefined;
  }
  const recordID = boundedParameter(parameters, "recordID", 128);
  const clientID = boundedParameter(parameters, "clientID", 128);
  const label = boundedParameter(parameters, "label", 80);
  const parentRecordID = boundedParameter(parameters, "parentRecordID", 128);
  if (!recordID || !clientID || !label) return undefined;
  return {
    id: `${entityType}:${recordID}`,
    routeID: entityType === "ticket" ? "work" : "project",
    recordID,
    ...(parentRecordID ? { parentRecordID } : {}),
    clientID,
    label,
    entityType,
    openedAt: 0,
  };
}

export function workspaceItemFromMentionLink(
  link: MentionDeepLink,
  allowedClientIDs: ReadonlySet<string>,
): WorkspaceItem | undefined {
  const navigation = mentionNavigationFromHash(link.href);
  const routeID = link.parentType === "project" ? "project" : "work";
  const entityType =
    link.parentType === "work_record" ? "ticket" : link.parentType;
  if (
    !navigation ||
    navigation.routeID !== routeID ||
    navigation.parentID !== link.parentId ||
    !link.clientId ||
    link.clientId.length > 128 ||
    !allowedClientIDs.has(link.clientId) ||
    link.sourceAvailable !== Boolean(navigation.sourceID) ||
    (link.sourceAvailable &&
      (!link.sourceId ||
        !link.tokenId ||
        navigation.sourceID !== link.sourceId)) ||
    (!link.sourceAvailable &&
      (navigation.sourceID !== undefined ||
        link.sourceId !== undefined ||
        link.tokenId !== undefined))
  ) {
    return undefined;
  }
  return {
    id: `${entityType}:${link.parentId}`,
    routeID,
    recordID: link.parentId,
    clientID: link.clientId,
    label: `Mentioned ${link.parentType === "work_record" ? "work" : link.parentType}`,
    entityType,
    openedAt: 0,
  };
}

export function App({
  build,
  aiSettingsAPI,
  aiAssistAPI,
  aiWorkspaceAPI,
  teamsSettingsAPI,
  classificationAdminAPI,
  catalogEnabled = import.meta.env.DEV ||
    (import.meta.env.MODE !== "production" &&
      import.meta.env.VITE_ENABLE_DESIGN_SYSTEM_CATALOG === "true"),
}: {
  build: BuildInfo;
  aiSettingsAPI?: AISettingsAPI;
  aiAssistAPI?: AIAssistAPI;
  aiWorkspaceAPI?: AIWorkspaceAPI;
  teamsSettingsAPI?: TeamsSettingsAPI;
  classificationAdminAPI?: ClassificationAdminAPI;
  catalogEnabled?: boolean;
}) {
  const [access, setAccess] = useState<
    ApplicationAccess | { kind: "loading" } | { kind: "error" }
  >({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [hash, setHash] = useState(window.location.hash);

  const replaceHash = useCallback((next: string) => {
    window.history.replaceState(
      null,
      "",
      `${window.location.pathname}${window.location.search}${next}`,
    );
    setHash(next);
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  }, []);

  useEffect(() => {
    const update = () => setHash(window.location.hash);
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setAccess({ kind: "loading" });
    void loadApplicationAccess(controller.signal)
      .then((resolved) => {
        if (!controller.signal.aborted) setAccess(resolved);
      })
      .catch(() => {
        if (!controller.signal.aborted) setAccess({ kind: "error" });
      });
    return () => controller.abort();
  }, [attempt]);

  useEffect(() => {
    // Read the browser as the source of truth here. React Strict Mode may replay
    // this effect before the state update queued by replaceHash is rendered.
    const activeHash = window.location.hash || hash;
    if (access.kind === "setup") {
      if (!activeHash.startsWith("#/setup")) replaceHash("#/setup");
      return;
    }
    if (access.kind === "unauthenticated") {
      if (activeHash === "#/break-glass" || activeHash.startsWith("#/login"))
        return;
      rememberReturnHash(activeHash, catalogEnabled);
      replaceHash("#/login");
      return;
    }
    if (access.kind !== "authenticated") return;

    const allowed = new Set(access.principal.navigation);
    const currentPage = pageFromHash(activeHash, catalogEnabled);
    if (
      (safeReturnHash(activeHash, catalogEnabled) &&
        (currentPage === "home" || allowed.has(currentPage))) ||
      (currentPage === "design-system" && catalogEnabled) ||
      (currentPage === "setup" && allowed.has("setup"))
    ) {
      clearReturnHash();
      return;
    }

    const remembered = peekReturnHash(catalogEnabled);
    if (remembered) {
      const rememberedPage = pageFromHash(remembered, catalogEnabled);
      if (
        allowed.has(rememberedPage) ||
        (rememberedPage === "design-system" && catalogEnabled)
      ) {
        replaceHash(remembered);
        return;
      }
      clearReturnHash();
    }

    replaceHash("#/home");
  }, [access, catalogEnabled, hash, replaceHash]);

  useEffect(() => {
    if (access.kind !== "authenticated") return;
    const expire = () => {
      rememberReturnHash(window.location.hash, catalogEnabled);
      setAccess({
        kind: "unauthenticated",
        entraAvailable: access.entraAvailable,
      });
      replaceHash("#/login");
    };
    window.addEventListener(sessionExpiredEvent, expire);
    return () => window.removeEventListener(sessionExpiredEvent, expire);
  }, [access, catalogEnabled, replaceHash]);

  if (access.kind === "loading") {
    return <PublicAccessPage state="loading" />;
  }
  if (access.kind === "error") {
    return (
      <PublicAccessPage
        state="error"
        onRetry={() => setAttempt((current) => current + 1)}
      />
    );
  }
  if (access.kind === "setup") {
    return (
      <main id="main-content" className="public-access public-access--setup">
        <div className="public-access__brand public-access__brand--full">
          <RarityBrand variant="full" />
        </div>
        <div className="public-access__brand public-access__brand--compact">
          <RarityBrand variant="compact" />
        </div>
        <section className="public-access__card">
          <SetupPage installationComplete={false} />
        </section>
      </main>
    );
  }
  if (access.kind === "unauthenticated") {
    const loginErrorValue = new URLSearchParams(
      hash.split("?", 2)[1] ?? "",
    ).get("error");
    const loginError =
      loginErrorValue === "entra" || loginErrorValue === "unavailable"
        ? loginErrorValue
        : undefined;
    return (
      <PublicAccessPage
        state={hash === "#/break-glass" ? "recovery" : "login"}
        entraAvailable={access.entraAvailable}
        loginError={loginError}
        onLocalAuthenticated={() => setAttempt((current) => current + 1)}
      />
    );
  }

  return (
    <AuthenticatedWorkspace
      build={build}
      principal={access.principal}
      entraAvailable={access.entraAvailable}
      aiSettingsAPI={aiSettingsAPI}
      aiAssistAPI={aiAssistAPI}
      aiWorkspaceAPI={aiWorkspaceAPI}
      teamsSettingsAPI={teamsSettingsAPI}
      classificationAdminAPI={classificationAdminAPI}
      catalogEnabled={catalogEnabled}
      onSignedOut={() => {
        clearReturnHash();
        setAccess({
          kind: "unauthenticated",
          entraAvailable: access.entraAvailable,
        });
        replaceHash("#/login");
      }}
    />
  );
}

function AuthenticatedWorkspace({
  build,
  principal,
  entraAvailable,
  aiSettingsAPI,
  aiAssistAPI,
  aiWorkspaceAPI,
  teamsSettingsAPI,
  classificationAdminAPI,
  catalogEnabled,
  onSignedOut,
}: {
  build: BuildInfo;
  principal: PrincipalNavigation;
  entraAvailable: boolean;
  aiSettingsAPI?: AISettingsAPI;
  aiAssistAPI?: AIAssistAPI;
  aiWorkspaceAPI?: AIWorkspaceAPI;
  teamsSettingsAPI?: TeamsSettingsAPI;
  classificationAdminAPI?: ClassificationAdminAPI;
  catalogEnabled: boolean;
  onSignedOut: () => void;
}) {
  const [page, setPage] = useState<Page>(() =>
    pageFromHash(window.location.hash, catalogEnabled),
  );
  const [navigationHash, setNavigationHash] = useState(
    () => window.location.hash,
  );
  const [assistWorkRecordID, setAssistWorkRecordID] = useState<
    string | undefined
  >(() => workRecordIDFromHash(window.location.hash));
  const [activeWorkspaceItem, setActiveWorkspaceItem] = useState<
    WorkspaceItem | undefined
  >(() => workspaceItemFromHash(window.location.hash));
  const [authenticatedMentionLink, setAuthenticatedMentionLink] = useState<
    MentionDeepLink | undefined
  >();
  const [notice, setNotice] = useState("");
  const [clients, setClients] = useState<DirectoryClient[]>([]);
  const [teams, setTeams] = useState<DirectoryEntry[]>([]);
  const [directoryLoaded, setDirectoryLoaded] = useState(false);
  const [selectedClientID, setSelectedClientID] = useState("");
  const [allowedPages] = useState(
    () =>
      new Set<Page>([
        "home",
        ...(principal.navigation.filter((route) =>
          principalHasNavigation(principal, route),
        ) as Page[]),
      ]),
  );
  const [capabilities] = useState(() => new Set(principal.capabilities));
  const [workRefresh, setWorkRefresh] = useState(0);
  const [resourceRefresh, setResourceRefresh] = useState(0);
  const [aiWorkspaceOpen, setAIWorkspaceOpen] = useState(false);
  const directoryRefreshSequence = useRef(0);
  const isPhone = usePhoneViewport();
  const phoneRouteBlocked = isPhone && !routeForID(page).mobile;
  const allowedClientIDs = useMemo(
    () =>
      directoryLoaded ? new Set(clients.map((client) => client.id)) : undefined,
    [clients, directoryLoaded],
  );

  useEffect(() => {
    if (!window.location.hash) window.location.hash = "#/home";
    const update = () => {
      setNavigationHash(window.location.hash);
      setPage(pageFromHash(window.location.hash, catalogEnabled));
      setAssistWorkRecordID(workRecordIDFromHash(window.location.hash));
      setActiveWorkspaceItem(workspaceItemFromHash(window.location.hash));
      setAuthenticatedMentionLink(undefined);
    };
    update();
    window.addEventListener("hashchange", update);
    window.addEventListener("popstate", update);
    return () => {
      window.removeEventListener("hashchange", update);
      window.removeEventListener("popstate", update);
    };
  }, [catalogEnabled]);

  useEffect(() => {
    const refresh = () => {
      void refreshSession().catch(() => {
        // A concurrent tab may already have replaced the shared cookie.
      });
    };
    refresh();
    const interval = window.setInterval(refresh, 5 * 60 * 1000);
    return () => window.clearInterval(interval);
  }, []);

  const refreshAuthorizedClients = useCallback(async (signal?: AbortSignal) => {
    const requestSequence = ++directoryRefreshSequence.current;
    let directory: Awaited<ReturnType<typeof loadDirectory>>;
    try {
      directory = await loadDirectory(signal);
    } catch (error) {
      if (
        signal?.aborted ||
        requestSequence !== directoryRefreshSequence.current
      ) {
        return;
      }
      throw error;
    }
    if (
      signal?.aborted ||
      requestSequence !== directoryRefreshSequence.current
    ) {
      return;
    }
    setClients(directory.clients);
    setTeams(directory.teams);
    setSelectedClientID((current) =>
      directory.clients.some((client) => client.id === current)
        ? current
        : (directory.clients[0]?.id ?? ""),
    );
    setDirectoryLoaded(true);
  }, []);

  useEffect(() => {
    if (
      activeWorkspaceItem?.clientID &&
      allowedClientIDs?.has(activeWorkspaceItem.clientID)
    ) {
      setSelectedClientID(activeWorkspaceItem.clientID);
    }
  }, [activeWorkspaceItem, allowedClientIDs]);

  useEffect(() => {
    const controller = new AbortController();
    void refreshAuthorizedClients(controller.signal).catch(() => {
      if (!controller.signal.aborted) {
        setClients([]);
        setTeams([]);
        setSelectedClientID("");
        setDirectoryLoaded(true);
      }
    });
    return () => controller.abort();
  }, [refreshAuthorizedClients]);

  useEffect(() => {
    if (page === "design-system" || allowedPages.has(page)) {
      return;
    }
    const fallback = navigation.find(({ page: candidate }) =>
      allowedPages.has(candidate),
    )?.page;
    if (fallback) navigate(fallback);
  }, [allowedPages, page]);

  function navigate(
    next: Page,
    workRecordID?: string,
    workspaceItem?: WorkspaceItem,
    replace = false,
  ) {
    setAuthenticatedMentionLink(undefined);
    const query = new URLSearchParams();
    if (workRecordID) query.set("workRecordID", workRecordID);
    if (workspaceItem) {
      query.set("recordType", workspaceItem.entityType);
      query.set("recordID", workspaceItem.recordID);
      query.set("clientID", workspaceItem.clientID);
      query.set("label", workspaceItem.label);
      if (workspaceItem.parentRecordID) {
        query.set("parentRecordID", workspaceItem.parentRecordID);
      }
    }
    const parameters = query.size ? `?${query.toString()}` : "";
    window.history[replace ? "replaceState" : "pushState"](
      null,
      "",
      `${window.location.pathname}${window.location.search}#/${next}${parameters}`,
    );
    setPage(next);
    setAssistWorkRecordID(workRecordID);
    setActiveWorkspaceItem(workspaceItem);
  }

  function openMention(link: MentionDeepLink) {
    const item = allowedClientIDs
      ? workspaceItemFromMentionLink(link, allowedClientIDs)
      : undefined;
    if (!item) {
      setNotice("This mention is no longer available.");
      return;
    }
    const routeID = item.routeID as Page;
    setNotice("");
    setSelectedClientID(item.clientID);
    window.history.pushState(
      null,
      "",
      `${window.location.pathname}${window.location.search}${link.href}`,
    );
    setNavigationHash(link.href);
    setPage(routeID);
    setAssistWorkRecordID(
      item.entityType === "ticket" ? item.recordID : undefined,
    );
    setActiveWorkspaceItem(item);
    setAuthenticatedMentionLink(link);
  }

  return (
    <AppShell
      brand={
        <RarityBrand
          variant="full"
          href="#/home"
          onNavigate={() => navigate("home")}
        />
      }
      navigation={(catalogEnabled
        ? [
            ...navigation,
            { page: "design-system" as const, label: "Design system" },
          ]
        : navigation
      )
        .filter(
          (item) =>
            (!isPhone || routeForID(item.page).mobile) &&
            (item.page === "design-system" ||
              item.page === "home" ||
              allowedPages.has(item.page)),
        )
        .map((item) => ({
          id: item.page,
          label: item.label,
          href: `#/${item.page}`,
          group: navigationGroup(item.page),
          onSelect: () => navigate(item.page),
        }))}
      activeID={page}
      buildLabel={`Build ${build.revision}`}
      overlay={
        <AIWorkspace
          open={aiWorkspaceOpen}
          clients={clients}
          pageClientID={selectedClientID}
          capabilities={capabilities}
          api={aiWorkspaceAPI}
          onClientCreated={refreshAuthorizedClients}
          onClientResourcesChanged={async () => {
            setResourceRefresh((current) => current + 1);
          }}
          onClose={() => setAIWorkspaceOpen(false)}
        />
      }
      principalID={principal.id}
      allowedClientIDs={allowedClientIDs}
      canManagePresentation={capabilities.has("role.manage")}
      aiAssistAPI={aiAssistAPI ?? defaultAIAssistAPI}
      pageFamily={routeForID(page).family}
      clientID={selectedClientID || undefined}
      recordID={assistWorkRecordID}
      activeWorkspaceItem={
        activeWorkspaceItem &&
        allowedClientIDs?.has(activeWorkspaceItem.clientID)
          ? activeWorkspaceItem
          : undefined
      }
      recordContextManaged
      onActivateWorkspaceItem={(item: WorkspaceItem, options) => {
        if (
          item.clientID &&
          allowedClientIDs?.has(item.clientID) &&
          item.clientID !== selectedClientID
        ) {
          setSelectedClientID(item.clientID);
        }
        navigate(item.routeID as Page, item.recordID, item, options?.replace);
      }}
      primaryActions={
        <>
          <GlobalWorkActions
            authenticated
            clientID={selectedClientID}
            onWorkCreated={() => {
              setNotice("Work created for the active Client.");
              setWorkRefresh((current) => current + 1);
              navigate("work");
            }}
          />
          {capabilities.has("ai.assist") ? (
            <button
              type="button"
              className="rti-open-ai-workspace"
              aria-label="Open AI workspace"
              aria-expanded={aiWorkspaceOpen}
              onClick={() => setAIWorkspaceOpen(true)}
            >
              <Sparkles size={16} aria-hidden="true" />
              <span>Ask Rarity</span>
            </button>
          ) : null}
        </>
      }
      topBar={
        <div className="rti-account-controls">
          <NotificationCenter key={principal.id} />
          {clients.length ? (
            <label>
              <span className="sr-only">Active client</span>
              <select
                aria-label="Active client"
                value={selectedClientID}
                onChange={(event) => setSelectedClientID(event.target.value)}
              >
                {clients.map((client) => (
                  <option key={client.id} value={client.id}>
                    {client.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <button
            type="button"
            className="rti-shell-icon"
            aria-label="Sign out"
            title="Sign out"
            onClick={() => {
              void logout()
                .catch(() => {
                  // Local presentation must close even if revocation fails.
                })
                .finally(() => {
                  clearWorkspaceStorage(principal.id);
                  setClients([]);
                  setTeams([]);
                  setSelectedClientID("");
                  onSignedOut();
                });
            }}
          >
            <LogOut size={17} aria-hidden="true" />
            <span className="sr-only">Sign out</span>
          </button>
        </div>
      }
    >
      {phoneRouteBlocked ? (
        <MobileRouteHandoff
          title={routeForID(page).label}
          onReturn={() => navigate("home")}
        />
      ) : (
        <FeatureBoundary key={page}>
          <Suspense fallback={<WorkspaceLoading />}>
            {notice ? (
              <div className="rarity-notice" role="status">
                {notice}
              </div>
            ) : null}
            {page === "home" ? (
              <WorkspaceHome
                capabilities={capabilities}
                availableRoutes={allowedPages}
                clientID={selectedClientID || undefined}
                principalID={principal.id}
                onNavigate={navigate}
                onOpenMention={openMention}
              />
            ) : null}
            {page === "work" ? (
              <TechnicianWorklist
                key={`${selectedClientID}-${workRefresh}`}
                clientID={selectedClientID}
                capabilities={capabilities}
                selectedWorkRecordID={assistWorkRecordID}
                selectedTaskID={
                  activeWorkspaceItem?.routeID === "work" &&
                  activeWorkspaceItem.entityType === "task"
                    ? activeWorkspaceItem.recordID
                    : undefined
                }
                preferencePrincipalID={principal.id}
                authenticatedMentionLink={authenticatedMentionLink}
              />
            ) : null}
            {page === "calendar" ? (
              <CalendarPage
                key={principal.id}
                principalID={principal.id}
                capabilities={capabilities}
                clients={clients}
              />
            ) : null}
            {page === "timesheets" ? (
              <TimesheetPage
                clientID={selectedClientID}
                capabilities={capabilities}
                selectedTimeEntryID={
                  new URLSearchParams(
                    navigationHash.split("?", 2)[1] ?? "",
                  ).get("timeEntryID") ?? undefined
                }
              />
            ) : null}
            {page === "sales" && selectedClientID ? (
              <OpportunityWorklist
                clientID={selectedClientID}
                capabilities={capabilities}
                principalID={principal.id}
              />
            ) : page === "sales" ? (
              <ClientRequiredState title="Opportunities" />
            ) : null}
            {page === "proposal" && selectedClientID ? (
              <ProposalWorklist
                clientID={selectedClientID}
                capabilities={capabilities}
              />
            ) : page === "proposal" ? (
              <ClientRequiredState title="Proposals" />
            ) : null}
            {page === "conversion" && selectedClientID ? (
              <LiveConversionPage
                clientID={selectedClientID}
                onConverted={() => {
                  setNotice("Project created from the accepted Proposal.");
                  navigate("project");
                }}
              />
            ) : page === "conversion" ? (
              <ClientRequiredState title="Create Project" />
            ) : null}
            {page === "project" && selectedClientID ? (
              <ProjectWorklist
                clientID={selectedClientID}
                capabilities={capabilities}
                principalID={principal.id}
                selectedRecord={
                  activeWorkspaceItem?.routeID === "project"
                    ? activeWorkspaceItem
                    : undefined
                }
                authenticatedMentionLink={authenticatedMentionLink}
              />
            ) : page === "project" ? (
              <ClientRequiredState title="Projects" />
            ) : null}
            {page === "ai-assist" ? (
              <AIAssistPanel
                workRecordID={assistWorkRecordID ?? ""}
                supportedFeatures={[
                  "summary",
                  "reply_draft",
                  "similar_suggestions",
                ]}
                api={aiAssistAPI ?? defaultAIAssistAPI}
                generationEnabled={!!assistWorkRecordID}
              />
            ) : null}
            {page === "ai-settings" ? (
              <AISettingsPage
                api={aiSettingsAPI}
                authenticated
                entraAvailable={entraAvailable}
              />
            ) : null}
            {page === "teams-settings" ? (
              <TeamsSettingsPage
                api={teamsSettingsAPI}
                clientID={selectedClientID}
              />
            ) : null}
            {page === "sessions" ? <SessionsPage /> : null}
            {page === "recovery-access" ? (
              <RecoveryAccessPage authenticated />
            ) : null}
            {page === "role-settings" ? <RoleManagementPage /> : null}
            {page === "audit" ? <AuditLogPage /> : null}
            {page === "service-desk-settings" ? (
              <ServiceDeskSettingsPage
                clientID={selectedClientID}
                capabilities={capabilities}
              />
            ) : null}
            {page === "classification-settings" &&
            allowedPages.has("classification-settings") ? (
              <ClassificationSettingsPage
                api={classificationAdminAPI}
                clientID={selectedClientID}
              />
            ) : null}
            {page === "classification-insights" &&
            allowedPages.has("classification-insights") ? (
              <ClassificationInsightsPage
                api={
                  classificationAdminAPI?.report
                    ? {
                        report: classificationAdminAPI.report,
                        reportEvidence: classificationAdminAPI.reportEvidence,
                        reportTechnicians:
                          classificationAdminAPI.reportTechnicians,
                        catalog: classificationAdminAPI.catalog,
                      }
                    : undefined
                }
                clientID={selectedClientID}
                technicianOptions={[
                  { id: principal.id, label: "Signed-in technician" },
                ]}
                teamOptions={teams.map((team) => ({
                  id: team.id,
                  label: team.name,
                }))}
              />
            ) : null}
            {page === "setup" ? <SetupPage installationComplete /> : null}
            {page === "operations" ? <OperationsHealthPage /> : null}
            {page === "automation" ? (
              <AutomationPage
                clientID={selectedClientID}
                capabilities={capabilities}
              />
            ) : null}
            {page === "service-keys" ? (
              <ServiceKeysPage clientID={selectedClientID} />
            ) : null}
            {page === "webhooks" ? (
              <WebhooksPage
                clientID={selectedClientID}
                capabilities={capabilities}
              />
            ) : null}
            {page === "datto-reconciliation" ? (
              <DattoReconciliationPage clientID={selectedClientID} />
            ) : null}
            {page === "datto-settings" ? <DattoSettingsPage /> : null}
            {page === "forwarding-settings" ? <ForwardingSettingsPage /> : null}
            {page === "graph-settings" ? <GraphSettingsPage /> : null}
            {page === "knowledge" ? (
              <KnowledgePage
                clientID={selectedClientID}
                capabilities={capabilities}
                selectedArticleID={
                  new URLSearchParams(
                    navigationHash.split("?", 2)[1] ?? "",
                  ).get("articleID") ?? undefined
                }
              />
            ) : null}
            {page === "billing" ? (
              <BillingPage
                clientID={selectedClientID}
                capabilities={capabilities}
              />
            ) : null}
            {page === "directory-settings" ? (
              <DirectorySettingsPage
                capabilities={capabilities}
                principalID={principal.id}
              />
            ) : null}
            {page === "client-resources" ? (
              <ClientResourcesPage
                clientID={selectedClientID}
                capabilities={capabilities}
                selectedAssetID={
                  new URLSearchParams(
                    navigationHash.split("?", 2)[1] ?? "",
                  ).get("assetID") ?? undefined
                }
                refreshToken={resourceRefresh}
              />
            ) : null}
            {page === "pipeline-settings" ? (
              <PipelineSettingsPage capabilities={capabilities} />
            ) : null}
            {page === "prospects" ? (
              <ProspectsPage capabilities={capabilities} />
            ) : null}
            {page === "design-system" && catalogEnabled ? (
              <DesignSystemCatalog />
            ) : null}
          </Suspense>
        </FeatureBoundary>
      )}
    </AppShell>
  );
}

function WorkspaceHome({
  capabilities,
  availableRoutes,
  clientID,
  principalID,
  onNavigate,
  onOpenMention,
}: {
  capabilities: ReadonlySet<string>;
  availableRoutes: ReadonlySet<Page>;
  clientID?: string;
  principalID: string;
  onNavigate: (route: Page) => void;
  onOpenMention: (link: MentionDeepLink) => void;
}) {
  return (
    <HomePage
      capabilities={capabilities}
      availableRoutes={availableRoutes}
      clientID={clientID}
      principalID={principalID}
      onNavigate={onNavigate}
      onOpenMention={onOpenMention}
    />
  );
}

function ClientRequiredState({ title }: { title: string }) {
  const mainContentID = useMainContentID();
  return (
    <main id={mainContentID} className="rti-mobile-handoff">
      <p className="rti-eyebrow">Client scope required</p>
      <h1>{title}</h1>
      <p>
        Choose an authorized client to load live records. Rarity does not show
        sample operational data in a production workspace.
      </p>
    </main>
  );
}

function MobileRouteHandoff({
  title,
  onReturn,
}: {
  title: string;
  onReturn: () => void;
}) {
  const mainContentID = useMainContentID();
  return (
    <main id={mainContentID} className="rti-mobile-handoff">
      <p className="rti-eyebrow">Desktop workspace</p>
      <h1>{title}</h1>
      <p>
        This workspace needs a larger screen. On a phone, Rarity keeps triage,
        ticket updates, notes, time, approvals, search, and AI assistance in
        reach.
      </p>
      <button type="button" onClick={onReturn}>
        Return to mobile Home
      </button>
    </main>
  );
}

function usePhoneViewport() {
  const query = "(max-width: 47.99rem)";
  const [isPhone, setIsPhone] = useState(() =>
    typeof window.matchMedia === "function"
      ? window.matchMedia(query).matches
      : false,
  );

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const media = window.matchMedia(query);
    const update = () => setIsPhone(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  return isPhone;
}

function WorkspaceLoading() {
  const mainContentID = useMainContentID();
  return (
    <main id={mainContentID} tabIndex={-1} className="rarity-notice">
      <p role="status">Loading workspace…</p>
    </main>
  );
}
