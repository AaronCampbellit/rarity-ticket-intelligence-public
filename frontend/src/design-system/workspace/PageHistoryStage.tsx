import { useRef, type ReactNode } from "react";

import type { PageFamily, RouteID } from "../../app/routes";
import { MainContentIDProvider } from "../foundations/mainContent";
import { DirectionPage } from "../templates/DirectionPage";

type HistoryEntry = {
  page: ReactNode;
  family: PageFamily;
  lastActivated: number;
};

export function PageHistoryStage({
  activeID,
  pageFamily,
  allowedRouteIDs,
  dirtyRouteIDs,
  children,
  limit = 8,
}: {
  activeID: RouteID;
  pageFamily: PageFamily;
  allowedRouteIDs: ReadonlySet<RouteID>;
  dirtyRouteIDs: ReadonlySet<string>;
  children: ReactNode;
  limit?: number;
}) {
  const pages = useRef(new Map<RouteID, HistoryEntry>());
  const activation = useRef(0);

  activation.current += 1;
  pages.current.set(activeID, {
    page: children,
    family: pageFamily,
    lastActivated: activation.current,
  });

  for (const routeID of pages.current.keys()) {
    if (routeID !== activeID && !allowedRouteIDs.has(routeID)) {
      pages.current.delete(routeID);
    }
  }

  const cleanCandidates = [...pages.current.entries()]
    .filter(
      ([routeID]) =>
        routeID !== activeID && !dirtyRouteIDs.has(`route:${routeID}`),
    )
    .sort(([, left], [, right]) => left.lastActivated - right.lastActivated);
  while (pages.current.size > Math.max(1, limit) && cleanCandidates.length) {
    const candidate = cleanCandidates.shift();
    if (candidate) pages.current.delete(candidate[0]);
  }

  return (
    <div className="rti-workspace-stage">
      {[...pages.current.entries()].map(([routeID, entry]) => (
        <MainContentIDProvider
          key={routeID}
          id={
            routeID === activeID
              ? "main-content"
              : `cached-main-content-${routeID}`
          }
        >
          <section
            className="rti-workspace-stage__page"
            data-history-route={routeID}
            hidden={routeID !== activeID}
          >
            <DirectionPage family={entry.family}>{entry.page}</DirectionPage>
          </section>
        </MainContentIDProvider>
      ))}
    </div>
  );
}
