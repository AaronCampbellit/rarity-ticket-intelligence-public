import { describe, expect, it } from "vitest";

import {
  initialWorkspaceState,
  restoreWorkspace,
  serializeWorkspace,
  workspaceActions,
  workspaceReducer,
  workspaceStorageKey,
} from "./useWorkspace";
import type { WorkspaceItem } from "./types";

const ticket: WorkspaceItem = {
  id: "ticket:INC-1048",
  routeID: "work",
  recordID: "INC-1048",
  clientID: "client-1",
  label: "INC-1048",
  entityType: "ticket",
  openedAt: 1,
};

const task: WorkspaceItem = {
  id: "task:task-1",
  routeID: "project",
  recordID: "task-1",
  parentRecordID: "project-1",
  clientID: "client-1",
  label: "Schedule kickoff",
  entityType: "task",
  openedAt: 2,
};

describe("workspaceReducer", () => {
  it("opens a preview as a record tab without duplication", () => {
    const previewed = workspaceReducer(
      initialWorkspaceState,
      workspaceActions.openPreview(ticket),
    );
    const opened = workspaceReducer(
      previewed,
      workspaceActions.openRecord(ticket),
    );
    const reopened = workspaceReducer(
      opened,
      workspaceActions.openRecord(ticket),
    );

    expect(reopened.tabs).toEqual([ticket]);
    expect(reopened.activeID).toBe(ticket.id);
    expect(reopened.preview).toBeUndefined();
  });

  it("refuses to close dirty tabs without confirmation", () => {
    const opened = workspaceReducer(
      initialWorkspaceState,
      workspaceActions.openRecord(ticket),
    );
    const dirty = workspaceReducer(
      opened,
      workspaceActions.markDirty(ticket.id, true),
    );

    expect(
      workspaceReducer(dirty, workspaceActions.closeTab(ticket.id))
        .closeRequest,
    ).toEqual({ id: ticket.id });
  });

  it("refuses to switch records while the active record is dirty", () => {
    const opened = workspaceReducer(
      workspaceReducer(
        initialWorkspaceState,
        workspaceActions.openRecord(task),
      ),
      workspaceActions.openRecord(ticket),
    );
    const dirty = workspaceReducer(
      opened,
      workspaceActions.markDirty(ticket.id, true),
    );

    expect(
      workspaceReducer(dirty, workspaceActions.activateTab(task.id)).activeID,
    ).toBe(ticket.id);
    expect(
      workspaceReducer(dirty, workspaceActions.openRecord(task)).activeID,
    ).toBe(ticket.id);

    const inactive = workspaceReducer(dirty, workspaceActions.deactivateTab());
    expect(
      workspaceReducer(inactive, workspaceActions.openRecord(task)).activeID,
    ).toBeUndefined();
    expect(
      workspaceReducer(inactive, workspaceActions.openRecord(ticket)).activeID,
    ).toBe(ticket.id);
  });

  it("keeps a tab dirty until every mounted draft source is clean", () => {
    const opened = workspaceReducer(
      initialWorkspaceState,
      workspaceActions.openRecord(ticket),
    );
    const twoDrafts = workspaceReducer(
      workspaceReducer(
        opened,
        workspaceActions.markDirtySource(ticket.id, "change-1", true),
      ),
      workspaceActions.markDirtySource(ticket.id, "change-2", true),
    );
    const oneDraft = workspaceReducer(
      twoDrafts,
      workspaceActions.markDirtySource(ticket.id, "change-1", false),
    );

    expect(oneDraft.tabs[0].dirty).toBe(true);
    expect(
      workspaceReducer(
        oneDraft,
        workspaceActions.markDirtySource(ticket.id, "change-2", false),
      ).tabs[0].dirty,
    ).toBe(false);
  });

  it("persists record navigation only inside an authorized client scope", () => {
    const serialized = JSON.parse(
      serializeWorkspace(
        {
          ...initialWorkspaceState,
          tabs: [ticket, task],
          activeID: task.id,
        },
        new Set(["client-1"]),
      ),
    );
    expect(serialized.tabs).toEqual([
      {
        id: ticket.id,
        routeID: "work",
        recordID: "INC-1048",
        clientID: "client-1",
        label: "INC-1048",
        entityType: "ticket",
        openedAt: 1,
      },
      {
        id: "task:task-1",
        routeID: "project",
        recordID: "task-1",
        parentRecordID: "project-1",
        clientID: "client-1",
        label: "Schedule kickoff",
        entityType: "task",
        openedAt: 2,
      },
    ]);
    expect(serialized.version).toBe(4);
    expect(serialized.activeID).toBe(task.id);

    expect(
      JSON.parse(
        serializeWorkspace(
          { ...initialWorkspaceState, tabs: [ticket, task] },
          new Set(["different-client"]),
        ),
      ).tabs,
    ).toEqual([]);
  });

  it("rejects the old route-tab schema instead of guessing record types", () => {
    sessionStorage.setItem(
      workspaceStorageKey("principal-1"),
      JSON.stringify({
        version: 3,
        tabs: [
          {
            id: "route:work",
            routeID: "work",
            label: "Work",
            kind: "route",
            openedAt: 1,
          },
        ],
      }),
    );

    expect(
      restoreWorkspace(
        "principal-1",
        new Set(["home", "work"]),
        new Set(["client-1"]),
      ),
    ).toEqual(initialWorkspaceState);
  });

  it("restores record context only when both its route and client remain authorized", () => {
    sessionStorage.setItem(
      workspaceStorageKey("principal-1"),
      serializeWorkspace(
        {
          tabs: [ticket],
          activeID: ticket.id,
        },
        new Set(["client-1"]),
      ),
    );

    expect(
      restoreWorkspace("principal-1", new Set(["work"]), new Set(["client-1"])),
    ).toEqual({
      tabs: [ticket],
      activeID: ticket.id,
    });
    expect(
      restoreWorkspace("principal-1", new Set(["work"]), new Set(["client-2"])),
    ).toEqual(initialWorkspaceState);
  });

  it("purges in-memory tabs, previews, and dirty state when authorization changes", () => {
    const current = {
      tabs: [{ ...ticket, dirty: true }],
      activeID: ticket.id,
      preview: ticket,
      dirtySources: { [ticket.id]: ["draft-1"] },
    };

    expect(
      workspaceReducer(
        current,
        workspaceActions.hydrate(initialWorkspaceState),
      ),
    ).toEqual(initialWorkspaceState);
  });

  it("reorders open work with the keyboard action", () => {
    const opened = workspaceReducer(
      workspaceReducer(
        initialWorkspaceState,
        workspaceActions.openRecord(task),
      ),
      workspaceActions.openRecord(ticket),
    );

    expect(
      workspaceReducer(
        opened,
        workspaceActions.moveTab(ticket.id, -1),
      ).tabs.map(({ id }) => id),
    ).toEqual([ticket.id, task.id]);
  });
});
