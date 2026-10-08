import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { TextListBuilder } from "../index";
import { DirtyForm } from "./DirtyForm";
import {
  serializeWorkspace,
  useWorkspace,
  WorkspaceProvider,
  workspaceStorageKey,
} from "./useWorkspace";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

describe("DirtyForm", () => {
  it("tracks builder button changes and never reassigns dirtiness to a new owner", async () => {
    const project = {
      id: "project:project-1",
      routeID: "project" as const,
      recordID: "project-1",
      clientID: "client-1",
      label: "PRJ-1042",
      entityType: "project" as const,
      openedAt: 1,
    };
    sessionStorage.setItem(
      workspaceStorageKey("principal-1"),
      serializeWorkspace(
        { tabs: [project], activeID: project.id },
        new Set(["client-1"]),
      ),
    );

    const shell = (ownerID: string) => (
      <WorkspaceProvider
        principalID="principal-1"
        allowedRouteIDs={new Set(["project"])}
        allowedClientIDs={new Set(["client-1"])}
      >
        <DirtyForm ownerID={ownerID}>
          <TextListBuilder label="Deliverables" name="deliverables" />
        </DirtyForm>
        <DirtyProbe />
      </WorkspaceProvider>
    );
    const view = render(shell(project.id));
    fireEvent.click(screen.getByRole("button", { name: "Add item" }));

    await waitFor(() =>
      expect(screen.getByTestId("dirty-state")).toHaveTextContent("dirty"),
    );

    view.rerender(shell("project:project-2"));
    await waitFor(() =>
      expect(screen.getByTestId("dirty-state")).toHaveTextContent("clean"),
    );
  });
});

function DirtyProbe() {
  const { state } = useWorkspace();
  return (
    <output data-testid="dirty-state">
      {state.tabs[0]?.dirty ? "dirty" : "clean"}
    </output>
  );
}
