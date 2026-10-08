import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { findA11yViolations } from "../../testing/renderA11y";
import { useWorkspace, useWorkspaceDirtyState } from "../../workspace";
import { AppShell, type NavigationItem } from "./AppShell";
import appStyles from "../../../app.css?raw";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

const navigation: NavigationItem[] = [
  {
    id: "work",
    label: "Work",
    href: "#/work",
    group: "work",
  },
  {
    id: "sales",
    label: "Sales",
    href: "#/sales",
    group: "sales",
  },
  {
    id: "audit",
    label: "Audit",
    href: "#/audit",
    group: "admin-access",
  },
  {
    id: "setup",
    label: "Setup center",
    href: "#/setup",
    group: "admin-platform",
  },
  {
    id: "knowledge",
    label: "Knowledge",
    href: "#/knowledge",
    group: "organization",
  },
  {
    id: "operations",
    label: "Operations",
    href: "#/operations",
    group: "admin-operations",
  },
  {
    id: "teams-settings",
    label: "Teams settings",
    href: "#/teams-settings",
    group: "admin-integrations",
  },
];

function RecordWorkspaceHarness() {
  const { openPreview } = useWorkspace();
  return (
    <main id="main-content">
      <button
        type="button"
        onClick={() =>
          openPreview({
            id: "ticket:record-1",
            routeID: "work",
            recordID: "record-1",
            clientID: "client-1",
            label: "INC-1048",
            entityType: "ticket",
            openedAt: 1,
          })
        }
      >
        Preview INC-1048
      </button>
    </main>
  );
}

function DirtyWorkspaceHarness() {
  const [draft, setDraft] = useState("");
  useWorkspaceDirtyState(Boolean(draft.trim()), "route:sales");
  return (
    <main id="main-content">
      <label>
        Draft note
        <input
          aria-label="Draft note"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
        />
      </label>
    </main>
  );
}

function TwoRecordHarness() {
  const { openRecord, markDirty } = useWorkspace();
  const item = (id: string, label: string) => ({
    id: `ticket:${id}`,
    routeID: "work" as const,
    recordID: id,
    clientID: "client-1",
    label,
    entityType: "ticket" as const,
    openedAt: 1,
  });
  return (
    <main id="main-content">
      <button
        type="button"
        onClick={() => {
          openRecord(item("record-2", "INC-1049"));
          openRecord(item("record-1", "INC-1048"));
          markDirty("ticket:record-1", true);
        }}
      >
        Prepare dirty tabs
      </button>
    </main>
  );
}

describe("AppShell", () => {
  it("renders only the Signal shell geometry", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
        pageFamily="worklist"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    expect(view.container.querySelector(".rarity-sidebar")).toBeInTheDocument();
    expect(view.container.querySelector(".rti-signal-context")).toBeNull();
    expect(view.container.querySelector(".rti-atlas-rail")).toBeNull();
  });

  it("persists density without exposing a direction control", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
        canManagePresentation
        pageFamily="worklist"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    const app = view.container.querySelector(".rarity-app");
    expect(app).toHaveAttribute("data-density", "adaptive");
    expect(app).toHaveAttribute("data-page-family", "worklist");

    fireEvent.click(
      screen.getByRole("button", { name: "Presentation settings" }),
    );
    expect(screen.queryByLabelText("Design direction")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Workspace density"), {
      target: { value: "compact" },
    });

    expect(app).toHaveAttribute("data-density", "compact");
    expect(
      window.localStorage.getItem("rti:presentation-preferences:principal-1"),
    ).toBe('{"density":"compact"}');
  });

  it("collapses grouped navigation and restores the preference per principal", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Collapse navigation" }),
    );
    expect(view.container.querySelector(".rarity-sidebar")).toHaveAttribute(
      "data-collapsed",
      "true",
    );
    expect(window.localStorage.getItem("rti:navigation:principal-1")).toBe(
      '{"collapsed":true}',
    );
    expect(screen.getByRole("button", { name: "Admin" })).toBeVisible();
    expect(
      view.container.querySelector(".rarity-brand__identity"),
    ).toBeVisible();
  });

  it("groups navigation and identifies the active page", async () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        principalID="principal-1"
        topBar={<span>Active client: Northwind Legal</span>}
        buildLabel="Build abc123"
      >
        <main id="main-content">
          <h1>Sales</h1>
        </main>
      </AppShell>,
    );

    expect(screen.getByRole("link", { name: "Sales" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Work" })).toBeVisible();
    const admin = screen.getByRole("button", { name: "Admin" });
    expect(admin).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("heading", { name: "Organization" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Knowledge" })).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Audit" }),
    ).not.toBeInTheDocument();

    fireEvent.click(admin);
    expect(admin).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("heading", { name: "Access" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Platform" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Operations" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Integrations" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Audit" })).toBeVisible();
    expect(screen.getByText("Build abc123")).toBeVisible();
    expect(
      document
        .querySelector(".rarity-topbar")
        ?.querySelector(".rarity-topbar__title"),
    ).toHaveTextContent("Sales");
    expect(document.querySelector(".rarity-topbar__actions")).toHaveTextContent(
      "Active client: Northwind Legal",
    );
    expect(await findA11yViolations(view.container)).toEqual([]);
  });

  it("automatically expands Admin for the active nested route", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="audit"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Audit</h1>
        </main>
      </AppShell>,
    );

    expect(screen.getByRole("button", { name: "Admin" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("link", { name: "Audit" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("keeps the product brand available in the responsive top bar", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Sales</h1>
        </main>
      </AppShell>,
    );

    expect(
      document.querySelector(".rarity-topbar__mobile-brand"),
    ).toHaveTextContent("Rarity");
  });

  it("keeps a global workspace mounted while the active route changes", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        topBar={null}
        overlay={<aside role="dialog" aria-label="AI workspace" />}
        buildLabel="Build test"
      >
        <main id="main-content">Work</main>
      </AppShell>,
    );
    const workspace = screen.getByRole("dialog", { name: "AI workspace" });
    view.rerender(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        topBar={null}
        overlay={<aside role="dialog" aria-label="AI workspace" />}
        buildLabel="Build test"
      >
        <main id="main-content">Sales</main>
      </AppShell>,
    );
    expect(screen.getByRole("dialog", { name: "AI workspace" })).toBe(
      workspace,
    );
  });

  it("reserves narrow top-bar space for the product symbol", () => {
    expect(appStyles).toMatch(
      /@media \(max-width: 520px\)[\s\S]*?\.rti-product-brand--compact strong\s*\{\s*display:\s*none;/,
    );
  });

  it("opens and closes narrow navigation with focus restoration", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    const trigger = document.querySelector<HTMLButtonElement>(
      ".rti-navigation-trigger",
    )!;
    expect(trigger).toHaveAttribute("aria-label", "Open navigation");
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(trigger).toHaveFocus();
  });

  it("keeps route state alive without representing pages as record tabs", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <label>
            Opportunity note
            <input aria-label="Opportunity note" />
          </label>
        </main>
      </AppShell>,
    );

    fireEvent.change(screen.getByLabelText("Opportunity note"), {
      target: { value: "Call client tomorrow" },
    });
    view.rerender(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );
    view.rerender(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <label>
            Opportunity note
            <input aria-label="Opportunity note" />
          </label>
        </main>
      </AppShell>,
    );

    expect(screen.getByLabelText("Opportunity note")).toHaveValue(
      "Call client tomorrow",
    );
    expect(
      screen.queryByRole("navigation", { name: "Open records" }),
    ).not.toBeInTheDocument();
  });

  it("keeps promoted record tabs active and attaches their context to AI", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <RecordWorkspaceHarness />
      </AppShell>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview INC-1048" }));
    expect(
      screen.queryByRole("button", { name: /Pin INC-1048/ }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Open INC-1048 in workspace",
      }),
    );

    expect(screen.getByRole("button", { name: "INC-1048" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Open page assistant" }),
    );
    expect(
      screen.getByRole("button", { name: "Current record" }),
    ).toBeVisible();
  });

  it("restores a hash-managed record tab and deactivates it on browser Back", () => {
    const activeWorkspaceItem = {
      id: "ticket:record-1",
      routeID: "work" as const,
      recordID: "record-1",
      clientID: "client-1",
      label: "INC-1048",
      entityType: "ticket" as const,
      openedAt: 0,
    };
    const shell = (item: typeof activeWorkspaceItem | undefined) => (
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        activeWorkspaceItem={item}
        recordContextManaged
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>
    );
    const view = render(shell(activeWorkspaceItem));

    expect(screen.getByRole("button", { name: "INC-1048" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    view.rerender(shell(undefined));
    expect(
      screen.getByRole("button", { name: "INC-1048" }),
    ).not.toHaveAttribute("aria-current");
  });

  it("returns focus to the record that opened a preview", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <RecordWorkspaceHarness />
      </AppShell>,
    );

    const trigger = screen.getByRole("button", { name: "Preview INC-1048" });
    trigger.focus();
    fireEvent.click(trigger);
    const close = screen.getByRole("button", {
      name: "Close INC-1048 preview",
    });
    close.focus();
    fireEvent.click(close);
    expect(trigger).toHaveFocus();
  });

  it("keeps an unsaved record active instead of reassigning its draft", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <TwoRecordHarness />
      </AppShell>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Prepare dirty tabs" }));
    fireEvent.click(screen.getByRole("button", { name: "INC-1049" }));

    expect(
      screen.getByRole("button", { name: "INC-1048Unsaved changes" }),
    ).toHaveAttribute("aria-current", "page");
    expect(
      screen.getByText(
        "Save or discard changes in INC-1048 before switching records.",
      ),
    ).toBeVisible();
  });

  it("restores the dirty record when hash-managed navigation targets another record", async () => {
    const first = {
      id: "ticket:record-1",
      routeID: "work" as const,
      recordID: "record-1",
      clientID: "client-1",
      label: "INC-1048",
      entityType: "ticket" as const,
      openedAt: 1,
    };
    const second = {
      ...first,
      id: "ticket:record-2",
      recordID: "record-2",
      label: "INC-1049",
    };
    const onActivateWorkspaceItem = vi.fn();
    const shell = (activeWorkspaceItem?: typeof first) => (
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        activeWorkspaceItem={activeWorkspaceItem}
        recordContextManaged
        onActivateWorkspaceItem={onActivateWorkspaceItem}
        topBar={null}
        buildLabel="Build test"
      >
        <TwoRecordHarness />
      </AppShell>
    );
    const view = render(shell(first));
    fireEvent.click(screen.getByRole("button", { name: "Prepare dirty tabs" }));
    view.rerender(shell(undefined));
    await waitFor(() =>
      expect(
        screen.getByRole("button", {
          name: "INC-1048Unsaved changes",
        }),
      ).not.toHaveAttribute("aria-current"),
    );
    onActivateWorkspaceItem.mockClear();

    view.rerender(shell(second));

    await waitFor(() =>
      expect(onActivateWorkspaceItem).toHaveBeenCalledWith(
        expect.objectContaining(first),
        { replace: true },
      ),
    );
    view.rerender(shell(first));
    expect(
      screen.getByRole("button", { name: "INC-1048Unsaved changes" }),
    ).toHaveAttribute("aria-current", "page");
  });

  it("places the compact page assistant immediately after the primary action", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        primaryActions={<button type="button">Create</button>}
        topBar={<button type="button">Account</button>}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    const actions = within(
      view.container.querySelector(".rti-command-bar")!,
    ).getAllByRole("button");
    expect(actions.slice(0, 2).map((button) => button.textContent)).toEqual([
      "Create",
      "Ask Rarity",
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "Open page assistant" }),
    );
    expect(
      screen.getByRole("complementary", { name: "Rarity AI" }),
    ).toHaveAttribute("data-open", "true");
    expect(
      screen.getByRole("heading", { name: "Work", level: 1 }),
    ).toBeVisible();
  });

  it("opens record previews as a Signal inspector", () => {
    render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <RecordWorkspaceHarness />
      </AppShell>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview INC-1048" }));
    expect(screen.getByLabelText("INC-1048 preview")).toHaveAttribute(
      "data-mode",
      "inspector",
    );
  });

  it("keeps dirty state on the cached editor that owns it", () => {
    const view = render(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="sales"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <DirtyWorkspaceHarness />
      </AppShell>,
    );

    fireEvent.change(screen.getByLabelText("Draft note"), {
      target: { value: "Unsaved customer context" },
    });

    view.rerender(
      <AppShell
        brand={<strong>Rarity</strong>}
        navigation={navigation}
        activeID="work"
        principalID="principal-1"
        topBar={null}
        buildLabel="Build test"
      >
        <main id="main-content">
          <h1>Work</h1>
        </main>
      </AppShell>,
    );

    expect(screen.getByLabelText("Draft note")).toHaveValue(
      "Unsaved customer context",
    );
    expect(
      screen.queryByRole("navigation", { name: "Open records" }),
    ).not.toBeInTheDocument();
  });
});
