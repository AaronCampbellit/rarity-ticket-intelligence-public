import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { RouteID } from "../../app/routes";
import { PresentationProvider } from "../foundations/presentation";
import { PageHistoryStage } from "./PageHistoryStage";

afterEach(cleanup);

const routes: RouteID[] = [
  "home",
  "work",
  "sales",
  "proposal",
  "project",
  "knowledge",
  "billing",
  "audit",
  "operations",
];

function HistoryHarness({
  route,
  dirtyRouteIDs = new Set<string>(),
  limit = 8,
}: {
  route: RouteID;
  dirtyRouteIDs?: ReadonlySet<string>;
  limit?: number;
}) {
  return (
    <PresentationProvider value={{ density: "adaptive" }}>
      <PageHistoryStage
        activeID={route}
        pageFamily="worklist"
        allowedRouteIDs={new Set(routes)}
        dirtyRouteIDs={dirtyRouteIDs}
        limit={limit}
      >
        <main>
          <h1>{route}</h1>
          <label>
            {route} note
            <input aria-label={`${route} note`} />
          </label>
        </main>
      </PageHistoryStage>
    </PresentationProvider>
  );
}

describe("PageHistoryStage", () => {
  it("retains route state without exposing route tabs", () => {
    const view = render(<HistoryHarness route="sales" />);
    fireEvent.change(screen.getByLabelText("sales note"), {
      target: { value: "Call client tomorrow" },
    });

    view.rerender(<HistoryHarness route="work" />);
    view.rerender(<HistoryHarness route="sales" />);

    expect(screen.getByLabelText("sales note")).toHaveValue(
      "Call client tomorrow",
    );
    expect(
      screen.queryByRole("navigation", { name: "Open records" }),
    ).not.toBeInTheDocument();
  });

  it("evicts the least-recent clean route after eight surfaces", () => {
    const view = render(<HistoryHarness route="home" />);
    for (const route of routes.slice(1)) {
      view.rerender(<HistoryHarness route={route} />);
    }

    expect(
      view.container.querySelector('[data-history-route="home"]'),
    ).not.toBeInTheDocument();
    expect(
      view.container.querySelector('[data-history-route="operations"]'),
    ).toBeInTheDocument();
  });

  it("does not evict a route with registered dirty state", () => {
    const dirty = new Set(["route:home"]);
    const view = render(
      <HistoryHarness route="home" dirtyRouteIDs={dirty} limit={2} />,
    );
    for (const route of ["work", "sales"] as const) {
      view.rerender(
        <HistoryHarness route={route} dirtyRouteIDs={dirty} limit={2} />,
      );
    }

    expect(
      view.container.querySelector('[data-history-route="home"]'),
    ).toBeInTheDocument();
    expect(
      view.container.querySelector('[data-history-route="work"]'),
    ).not.toBeInTheDocument();
  });
});
