import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import axe from "axe-core";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ClassificationSettingsPage } from "./ClassificationSettingsPage";
import type { ClassificationAdminAPI, ClassificationHealth } from "./types";

afterEach(cleanup);

const groups = [
  {
    id: "system",
    label: "System",
    description: "Built-in classification values",
    position: 0,
    state: "active" as const,
    systemManaged: true,
    version: 1,
  },
  {
    id: "technology",
    label: "Technology",
    description: "",
    position: 2,
    state: "active" as const,
    version: 3,
  },
  {
    id: "business",
    label: "Business",
    description: "",
    position: 1,
    state: "active" as const,
    version: 2,
  },
];
const tags = [
  {
    id: "vpn",
    label: "VPN",
    groupId: "technology",
    state: "active" as const,
    synonyms: ["Remote access"],
    version: 4,
  },
  {
    id: "m365",
    label: "M365",
    groupId: "technology",
    state: "active" as const,
    synonyms: [],
    version: 5,
  },
  {
    id: "unclassified",
    label: "Unclassified",
    groupId: "business",
    state: "active" as const,
    synonyms: [],
    version: 1,
    systemManaged: true,
  },
];

function api(
  overrides: Partial<ClassificationAdminAPI> = {},
): ClassificationAdminAPI {
  return {
    catalog: vi.fn().mockResolvedValue({ groups, tags }),
    createGroup: vi.fn(),
    updateGroup: vi.fn(),
    createTag: vi.fn(),
    updateTag: vi.fn(),
    impact: vi.fn().mockImplementation((tagId, operation, replacementTagId) =>
      Promise.resolve({
        operation,
        tagId,
        replacementTagId,
        affectedObjects: 7,
        affectedSavedViews: 2,
        affectedReports: 1,
        affectedAutomations: 3,
        fallbackByObjectType: { task: 2 },
      }),
    ),
    merge: vi.fn().mockResolvedValue({ ...tags[1], version: 6 }),
    archive: vi.fn(),
    health: vi.fn().mockResolvedValue({
      byObjectType: {
        task: { meaningful: 10, unclassified: 2, archiveFallback: 1 },
      },
    }),
    migrationHistory: vi.fn().mockResolvedValue([
      {
        id: "run-1",
        status: "completed",
        startedAt: "2026-08-05T00:00:00Z",
        rowsDiscovered: 12,
        rowsMigrated: 10,
        fallbackAssignments: 2,
        categorySourcePresent: true,
      },
    ]),
    ...overrides,
  };
}

describe("ClassificationSettingsPage", () => {
  it("loads and saves the governed AI model, threshold, metrics, and provider health", async () => {
    const client = api({
      aiPolicy: vi.fn().mockResolvedValue({
        automaticApplyEnabled: true,
        automaticApplyThreshold: 0.95,
        modelProfileId: "model-1",
        version: 2,
        modelOptions: [{ id: "model-1", label: "Classifier" }],
        retainedRate: 0.8,
        changeRate: 0.2,
        providerFailureHealth: "healthy",
      }),
      updateAIPolicy: vi.fn().mockResolvedValue({
        automaticApplyEnabled: true,
        automaticApplyThreshold: 0.975,
        modelProfileId: "model-1",
        version: 3,
        modelOptions: [{ id: "model-1", label: "Classifier" }],
        retainedRate: 0.8,
        changeRate: 0.2,
        providerFailureHealth: "healthy",
      }),
    });
    render(<ClassificationSettingsPage api={client} />);
    fireEvent.click(
      await screen.findByRole("tab", { name: "AI classification" }),
    );
    expect(await screen.findByText("Retained rate: 80%")).toBeVisible();
    expect(screen.getByText("Provider health: healthy")).toBeVisible();
    fireEvent.change(screen.getByLabelText("Automatic apply threshold"), {
      target: { value: ".975" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save AI policy" }));
    await waitFor(() =>
      expect(client.updateAIPolicy).toHaveBeenCalledWith(
        expect.objectContaining({
          automaticApplyThreshold: 0.975,
          modelProfileId: "model-1",
          expectedVersion: 2,
        }),
      ),
    );
  });

  it("has no detectable accessibility violations", async () => {
    const getContext = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue(null as never);
    render(<ClassificationSettingsPage api={api()} />);
    await screen.findByRole("heading", { name: "Classification settings" });
    const result = await axe.run(document.body);
    getContext.mockRestore();
    expect(result.violations).toEqual([]);
  });

  it("supports roving tab navigation and an escapable focus-restoring lifecycle dialog", async () => {
    render(<ClassificationSettingsPage api={api()} />);
    await screen.findByText("VPN");
    const catalogTab = screen.getByRole("tab", { name: "Catalog" });
    catalogTab.focus();
    fireEvent.keyDown(catalogTab, { key: "ArrowRight" });
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Groups" })).toHaveAttribute(
        "aria-selected",
        "true",
      ),
    );
    fireEvent.keyDown(screen.getByRole("tab", { name: "Groups" }), {
      key: "End",
    });
    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Migration history" }),
      ).toHaveAttribute("aria-selected", "true"),
    );
    fireEvent.click(screen.getByRole("tab", { name: "Catalog" }));
    const archive = screen.getByRole("button", { name: "Archive VPN" });
    archive.focus();
    fireEvent.click(archive);
    expect(screen.getByRole("dialog", { name: "Archive VPN" })).toBeVisible();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(archive).toHaveFocus());
  });

  it("orders groups, searches tags, and protects system tags", async () => {
    render(<ClassificationSettingsPage api={api()} />);
    await screen.findByRole("heading", { name: "Classification settings" });
    expect(
      screen
        .getByText("Business")
        .compareDocumentPosition(screen.getByText("Technology")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.change(screen.getByRole("searchbox", { name: "Search tags" }), {
      target: { value: "remote" },
    });
    expect(screen.getByText("VPN")).toBeVisible();
    expect(screen.queryByText("M365")).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("searchbox", { name: "Search tags" }), {
      target: { value: "" },
    });
    expect(
      screen.getByRole("button", { name: "System tag: Unclassified" }),
    ).toBeDisabled();
  });

  it("uses structured create, edit, regroup, and synonym forms", async () => {
    const client = api();
    render(<ClassificationSettingsPage api={client} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Add tag" }));
    fireEvent.change(screen.getByLabelText("Tag label"), {
      target: { value: "Cisco" },
    });
    fireEvent.change(screen.getByLabelText("Tag group"), {
      target: { value: "technology" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create tag" }));
    await waitFor(() =>
      expect(client.createTag).toHaveBeenCalledWith(
        expect.objectContaining({ label: "Cisco", groupId: "technology" }),
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit VPN" }));
    fireEvent.change(screen.getByLabelText("Synonyms"), {
      target: { value: "Remote access, tunnel" },
    });
    fireEvent.change(screen.getByLabelText("Tag group"), {
      target: { value: "business" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save tag" }));
    await waitFor(() =>
      expect(client.updateTag).toHaveBeenCalledWith(
        "vpn",
        expect.objectContaining({
          groupId: "business",
          synonyms: ["Remote access", "tunnel"],
          expectedVersion: 4,
        }),
      ),
    );
  });

  it("previews lifecycle impact, requires a reason and recovers from stale versions", async () => {
    const client = api({
      merge: vi
        .fn()
        .mockRejectedValue({ code: "version_conflict", status: 409 }),
    });
    render(<ClassificationSettingsPage api={client} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Merge VPN" }));
    fireEvent.change(screen.getByLabelText("Surviving tag"), {
      target: { value: "m365" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Preview merge impact" }),
    );
    expect(await screen.findByText("7 authorized objects")).toBeVisible();
    expect(
      screen.getByText("2 saved views · 1 report · 3 automations"),
    ).toBeVisible();
    expect(
      screen.getByText(/authorized objects will move to M365/),
    ).toBeVisible();
    expect(screen.getByText("Expected tag version: 4")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Confirm merge" }));
    expect(await screen.findByText("Enter a reason.")).toBeVisible();
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Duplicate terms" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm merge" }));
    expect(
      await screen.findByText(
        "Classification changed elsewhere. The catalog was refreshed; review the impact again.",
      ),
    ).toBeVisible();
  });

  it("shows archive fallback impact plus health and migration tabs", async () => {
    const client = api();
    render(<ClassificationSettingsPage api={client} clientID="client-a" />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Archive VPN" }));
    fireEvent.change(screen.getByLabelText("Replacement tag"), {
      target: { value: "m365" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Preview archive impact" }),
    );
    expect(
      await screen.findByText(/authorized objects will move to M365/),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "Classification health" }));
    expect(await screen.findByText("10 meaningful")).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "Migration history" }));
    expect(await screen.findByText("run-1")).toBeVisible();
  });

  it("loads health for the active Client and ignores an older Client response", async () => {
    let resolveFirst: ((value: ClassificationHealth) => void) | undefined;
    let resolveSecond: ((value: ClassificationHealth) => void) | undefined;
    const health = vi.fn<ClassificationAdminAPI["health"]>(
      (clientID) =>
        new Promise<ClassificationHealth>((resolve) => {
          if (clientID === "client-a") resolveFirst = resolve;
          else resolveSecond = resolve;
        }),
    );
    const client = api({ health });
    const page = (
      <ClassificationSettingsPage api={client} clientID="client-a" />
    );
    const view = render(page);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("tab", { name: "Classification health" }));
    await waitFor(() =>
      expect(health).toHaveBeenCalledWith("client-a", expect.any(AbortSignal)),
    );
    view.rerender(
      <ClassificationSettingsPage api={client} clientID="client-b" />,
    );
    await waitFor(() =>
      expect(health).toHaveBeenCalledWith("client-b", expect.any(AbortSignal)),
    );
    resolveSecond?.({
      byObjectType: {
        task: { meaningful: 20, unclassified: 0, archiveFallback: 0 },
      },
    });
    expect(await screen.findByText("20 meaningful")).toBeVisible();
    resolveFirst?.({
      byObjectType: {
        task: { meaningful: 1, unclassified: 9, archiveFallback: 9 },
      },
    });
    await waitFor(() =>
      expect(screen.getByText("20 meaningful")).toBeVisible(),
    );
    expect(screen.queryByText("1 meaningful")).toBeNull();
  });

  it("supports no-replacement archive, excludes system replacements, and manages groups", async () => {
    const client = api();
    render(<ClassificationSettingsPage api={client} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Archive VPN" }));
    expect(screen.queryByRole("option", { name: "Unclassified" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Preview archive impact" }),
    );
    expect(
      await screen.findByText(
        "Archive fallback: Tasks without another meaningful tag will receive Unclassified (2).",
      ),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("tab", { name: "Groups" }));
    await screen.findByText("Technology");
    fireEvent.click(screen.getByRole("button", { name: "Edit Technology" }));
    fireEvent.change(screen.getByLabelText("Edit group label"), {
      target: { value: "Technical services" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save group" }));
    await waitFor(() =>
      expect(client.updateGroup).toHaveBeenCalledWith(
        "technology",
        expect.objectContaining({ label: "Technical services", position: 2 }),
      ),
    );
  });

  it("keeps archived and system groups visible while preventing system changes", async () => {
    let businessState: "active" | "archived" = "active";
    const client = api({
      catalog: vi.fn().mockImplementation(() =>
        Promise.resolve({
          groups: groups.map((group) =>
            group.id === "business"
              ? { ...group, state: businessState }
              : group,
          ),
          tags,
        }),
      ),
      updateGroup: vi.fn().mockImplementation((_id, input) => {
        businessState = input.state;
        return Promise.resolve({ ...groups[1], state: input.state });
      }),
    });
    render(<ClassificationSettingsPage api={client} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("tab", { name: "Groups" }));
    expect(screen.getByText("System")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "System group: System" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Archive Business" }));
    await screen.findByRole("button", { name: "Reactivate Business" });
    fireEvent.click(
      screen.getByRole("button", { name: "Reactivate Business" }),
    );
    await waitFor(() =>
      expect(client.updateGroup).toHaveBeenLastCalledWith(
        "business",
        expect.objectContaining({ state: "active" }),
      ),
    );
  });

  it("rejects mismatched impact previews and restores focus after a successful lifecycle change", async () => {
    const mismatched = api({
      impact: vi.fn().mockResolvedValue({
        operation: "archive",
        tagId: "wrong-tag",
        replacementTagId: "m365",
        affectedObjects: 1,
        affectedSavedViews: 0,
        affectedReports: 0,
        affectedAutomations: 0,
        fallbackByObjectType: {},
      }),
    });
    render(<ClassificationSettingsPage api={mismatched} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Merge VPN" }));
    fireEvent.change(screen.getByLabelText("Surviving tag"), {
      target: { value: "m365" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Preview merge impact" }),
    );
    expect(
      await screen.findByText(
        /Impact preview did not match the selected lifecycle action/,
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Confirm merge" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    const successful = api();
    cleanup();
    render(<ClassificationSettingsPage api={successful} />);
    await screen.findByText("VPN");
    const merge = screen.getByRole("button", { name: "Merge VPN" });
    merge.focus();
    fireEvent.click(merge);
    fireEvent.change(screen.getByLabelText("Surviving tag"), {
      target: { value: "m365" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Preview merge impact" }),
    );
    await screen.findByText("7 authorized objects");
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Duplicate terms" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm merge" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(merge).toHaveFocus());
  });

  it("closes stale tag and group editors after refreshing the catalog", async () => {
    const client = api({
      updateTag: vi.fn().mockRejectedValue({ code: "version_conflict" }),
      updateGroup: vi.fn().mockRejectedValue({ code: "version_conflict" }),
    });
    render(<ClassificationSettingsPage api={client} />);
    await screen.findByText("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Edit VPN" }));
    fireEvent.click(screen.getByRole("button", { name: "Save tag" }));
    expect(
      await screen.findByText(
        "Classification changed elsewhere. The catalog was refreshed; review the impact again.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save tag" })).toBeNull();
    fireEvent.click(screen.getByRole("tab", { name: "Groups" }));
    await screen.findByText("Technology");
    fireEvent.click(screen.getByRole("button", { name: "Edit Technology" }));
    fireEvent.click(screen.getByRole("button", { name: "Save group" }));
    expect(
      await screen.findByText(
        "Classification group changed elsewhere. The catalog was refreshed.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save group" })).toBeNull();
  });
});
