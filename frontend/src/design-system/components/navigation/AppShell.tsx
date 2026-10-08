import { Menu } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { PageFamily, RouteID } from "../../../app/routes";
import type { AIAssistAPI } from "../../../features/ai/types";
import {
  readPresentationPreferences,
  writePresentationPreferences,
} from "../../foundations/preferences";
import { PresentationProvider } from "../../foundations/presentation";
import {
  AIRail,
  blockingDirtyRecord,
  CommandPalette,
  PageHistoryStage,
  PreviewPane,
  WorkspaceProvider,
  WorkspaceTabs,
  useWorkspace,
  workspaceActions,
} from "../../workspace";
import type { WorkspaceItem } from "../../workspace";
import { CommandBar } from "./CommandBar";
import { type NavigationItem } from "./GroupedSidebar";
import { DirectionShell } from "./DirectionShell";
import { PresentationMenu } from "./PresentationMenu";
import { TopBar } from "./TopBar";
import "./navigation.css";

export type { NavigationItem } from "./GroupedSidebar";

type AppShellProps = {
  brand: ReactNode;
  navigation: NavigationItem[];
  activeID: string;
  topBar: ReactNode;
  primaryActions?: ReactNode;
  overlay?: ReactNode;
  buildLabel: string;
  principalID?: string;
  allowedClientIDs?: ReadonlySet<string>;
  canManagePresentation?: boolean;
  aiAssistAPI?: AIAssistAPI;
  pageFamily?: PageFamily;
  clientID?: string;
  recordID?: string;
  activeWorkspaceItem?: WorkspaceItem;
  recordContextManaged?: boolean;
  onActivateWorkspaceItem?: (
    item: WorkspaceItem,
    options?: { replace?: boolean },
  ) => void;
  children: ReactNode;
};

export function AppShell(props: AppShellProps) {
  const principalID = props.principalID ?? "anonymous";
  const allowedRouteKey = props.navigation.map(({ id }) => id).join("\u0000");
  const allowedRouteIDs = useMemo(
    () => new Set(allowedRouteKey.split("\u0000") as RouteID[]),
    [allowedRouteKey],
  );
  return (
    <WorkspaceProvider
      principalID={principalID}
      allowedRouteIDs={allowedRouteIDs}
      allowedClientIDs={props.allowedClientIDs}
      onOpenRecord={(item) => {
        if (props.onActivateWorkspaceItem) {
          props.onActivateWorkspaceItem(item);
          return;
        }
        props.navigation.find(({ id }) => id === item.routeID)?.onSelect?.();
      }}
    >
      <AppShellWorkspace {...props} principalID={principalID} />
    </WorkspaceProvider>
  );
}

function AppShellWorkspace({
  brand,
  navigation,
  activeID,
  topBar,
  primaryActions,
  buildLabel,
  principalID = "anonymous",
  canManagePresentation = false,
  aiAssistAPI,
  pageFamily = "record",
  clientID,
  recordID,
  activeWorkspaceItem,
  recordContextManaged = false,
  onActivateWorkspaceItem,
  children,
  overlay,
}: AppShellProps) {
  const [navigationOpen, setNavigationOpen] = useState(false);
  const [preferences, setPreferences] = useState(() =>
    readPresentationPreferences(window.localStorage, principalID),
  );
  const navigationTrigger = useRef<HTMLButtonElement>(null);
  const { state, dispatch } = useWorkspace();

  useEffect(() => {
    if (!recordContextManaged) return;
    if (activeWorkspaceItem) {
      const dirty = blockingDirtyRecord(state, activeWorkspaceItem.id);
      if (dirty) {
        onActivateWorkspaceItem?.(dirty, { replace: true });
        return;
      }
      if (
        state.activeID !== activeWorkspaceItem.id ||
        !state.tabs.some(({ id }) => id === activeWorkspaceItem.id)
      ) {
        dispatch(workspaceActions.openRecord(activeWorkspaceItem));
      }
      return;
    }
    if (state.activeID) dispatch(workspaceActions.deactivateTab());
  }, [
    activeWorkspaceItem,
    dispatch,
    recordContextManaged,
    state.activeID,
    state.tabs,
    onActivateWorkspaceItem,
  ]);

  useEffect(() => {
    if (!navigationOpen) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setNavigationOpen(false);
      navigationTrigger.current?.focus();
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [navigationOpen]);

  const activeLabel =
    navigation.find((item) => item.id === activeID)?.label ?? "Rarity";
  const workspaceContext =
    state.preview ?? state.tabs.find((item) => item.id === state.activeID);
  const pageHistoryRoutes = useMemo(
    () => new Set(navigation.map(({ id }) => id as RouteID)),
    [navigation],
  );
  const dirtyRouteIDs = useMemo(
    () =>
      new Set(
        Object.keys(state.dirtySources ?? {}).filter((id) =>
          id.startsWith("route:"),
        ),
      ),
    [state.dirtySources],
  );

  return (
    <PresentationProvider value={preferences}>
      <div
        className="rarity-app"
        data-density={preferences.density}
        data-page-family={pageFamily}
      >
        <a className="skip-link" href="#main-content">
          Skip to main content
        </a>
        <button
          ref={navigationTrigger}
          className="rti-navigation-trigger"
          type="button"
          aria-label="Open navigation"
          aria-expanded={navigationOpen}
          aria-controls="rti-primary-sidebar"
          onClick={() => setNavigationOpen((current) => !current)}
        >
          <Menu size={20} aria-hidden="true" />
        </button>
        {navigationOpen ? (
          <button
            className="rti-navigation-backdrop"
            type="button"
            aria-label="Close navigation"
            onClick={() => {
              setNavigationOpen(false);
              navigationTrigger.current?.focus();
            }}
          />
        ) : null}
        <DirectionShell
          brand={brand}
          navigation={navigation}
          activeID={activeID}
          buildLabel={buildLabel}
          principalID={principalID}
          open={navigationOpen}
          onNavigate={() => setNavigationOpen(false)}
        />
        <div className="rarity-workspace">
          <TopBar title={activeLabel}>
            <CommandBar>
              {primaryActions}
              <AIRail
                compact
                api={aiAssistAPI}
                context={{
                  routeID: activeID as RouteID,
                  routeLabel: activeLabel,
                  clientID: workspaceContext?.clientID ?? clientID,
                  recordID: workspaceContext?.recordID ?? recordID,
                  entityType: workspaceContext?.entityType,
                }}
              />
              <CommandPalette navigation={navigation} />
              {topBar}
              {canManagePresentation ? (
                <PresentationMenu
                  preferences={preferences}
                  onChange={(next) => {
                    setPreferences(next);
                    writePresentationPreferences(
                      next,
                      window.localStorage,
                      principalID,
                    );
                  }}
                />
              ) : null}
            </CommandBar>
          </TopBar>
          <WorkspaceTabs
            navigation={navigation}
            onActivate={(item) => {
              if (onActivateWorkspaceItem) {
                onActivateWorkspaceItem(item);
                return;
              }
              navigation.find(({ id }) => id === item.routeID)?.onSelect?.();
            }}
          />
          <PageHistoryStage
            activeID={activeID as RouteID}
            pageFamily={pageFamily}
            allowedRouteIDs={pageHistoryRoutes}
            dirtyRouteIDs={dirtyRouteIDs}
          >
            {children}
          </PageHistoryStage>
        </div>
        <PreviewPane />
        {state.closeRequest ? (
          <div className="rti-confirm-scrim">
            <section role="alertdialog" aria-labelledby="rti-unsaved-title">
              <h2 id="rti-unsaved-title">Discard unsaved changes?</h2>
              <p>This workspace has changes that have not been saved.</p>
              <div>
                <button
                  type="button"
                  onClick={() => dispatch(workspaceActions.cancelClose())}
                >
                  Keep open
                </button>
                <button
                  type="button"
                  onClick={() => {
                    const closingID = state.closeRequest?.id;
                    const remaining = state.tabs.filter(
                      ({ id }) => id !== closingID,
                    );
                    dispatch(workspaceActions.confirmClose());
                    const next = remaining.at(-1);
                    if (next) {
                      navigation
                        .find(({ id }) => id === next.routeID)
                        ?.onSelect?.();
                    } else {
                      navigation[0]?.onSelect?.();
                    }
                  }}
                >
                  Discard and close
                </button>
              </div>
            </section>
          </div>
        ) : null}
      </div>
      {overlay}
    </PresentationProvider>
  );
}
