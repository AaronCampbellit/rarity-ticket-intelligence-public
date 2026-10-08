import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  serializeWorkspace,
  useWorkspace,
  WorkspaceProvider,
  workspaceStorageKey,
} from "../../design-system";
import { ChangeOrderEditor } from "./ChangeOrderEditor";
import type { ChangeOrderWorkspace } from "./types";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const changeOrder: ChangeOrderWorkspace = {
  id: "CO-12",
  displayID: "CO-12",
  state: "issued",
  version: 3,
  currentVersion: {
    id: "cov-2",
    version: 2,
    description: "Add endpoint hardening",
    currency: "USD",
    revenueDeltaMinor: 240000,
    costDeltaMinor: 80000,
    laborDeltaMinutes: 720,
  },
  decisions: [],
};

describe("ChangeOrderEditor", () => {
  it("requires an override reason without asking for a special permission", () => {
    render(
      <ChangeOrderEditor changeOrder={changeOrder} onOverride={vi.fn()} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Override approval" }));

    expect(screen.getByText("Enter a reason for the override.")).toBeVisible();
    expect(screen.getByLabelText("Override reason")).toHaveFocus();
    expect(screen.queryByText(/special permission/i)).not.toBeInTheDocument();
  });

  it("submits exact version evidence with the reason", async () => {
    const onOverride = vi.fn().mockResolvedValue(true);
    render(
      <ChangeOrderEditor changeOrder={changeOrder} onOverride={onOverride} />,
    );
    fireEvent.change(screen.getByLabelText("Override reason"), {
      target: { value: "Customer approval recorded in steering call" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Override approval" }));

    expect(onOverride).toHaveBeenCalledWith({
      versionID: "cov-2",
      expectedVersion: 3,
      reason: "Customer approval recorded in steering call",
    });
    await waitFor(() =>
      expect(screen.getByLabelText("Override reason")).toHaveValue(""),
    );
  });

  it("keeps the override reason when persistence fails", async () => {
    const onOverride = vi.fn().mockResolvedValue(false);
    render(
      <ChangeOrderEditor changeOrder={changeOrder} onOverride={onOverride} />,
    );
    fireEvent.change(screen.getByLabelText("Override reason"), {
      target: { value: "Keep this evidence" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Override approval" }));

    await waitFor(() => expect(onOverride).toHaveBeenCalled());
    expect(screen.getByLabelText("Override reason")).toHaveValue(
      "Keep this evidence",
    );
  });

  it("marks the owning project record tab dirty while an override draft exists", async () => {
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

    render(
      <WorkspaceProvider
        principalID="principal-1"
        allowedRouteIDs={new Set(["project"])}
        allowedClientIDs={new Set(["client-1"])}
      >
        <ChangeOrderEditor
          changeOrder={changeOrder}
          onOverride={vi.fn().mockResolvedValue(true)}
          workspaceOwnerID={project.id}
        />
        <DirtyProbe />
      </WorkspaceProvider>,
    );
    fireEvent.change(screen.getByLabelText("Override reason"), {
      target: { value: "Pending approval evidence" },
    });

    await waitFor(() =>
      expect(screen.getByTestId("dirty-state")).toHaveTextContent("dirty"),
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
