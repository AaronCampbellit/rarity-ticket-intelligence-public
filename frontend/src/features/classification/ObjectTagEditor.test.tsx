import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type {
  ClassificationAPI,
  ClassificationCatalog,
  ClassificationSuggestion,
  TaggedObject,
} from "./types";
import { ObjectTagEditor } from "./ObjectTagEditor";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const tag = {
  id: "vpn",
  label: "VPN",
  groupId: "technology",
  state: "active" as const,
  synonyms: [],
  version: 1,
};
const inheritedTag = {
  id: "executive",
  label: "Executive",
  groupId: "audience",
  state: "active" as const,
  synonyms: [],
  version: 1,
};
const automatedTag = {
  id: "automation",
  label: "Automated",
  groupId: "technology",
  state: "active" as const,
  synonyms: [],
  version: 1,
};
const classificationCatalog = {
  groups: [
    { id: "technology", label: "Technology", description: "" },
    { id: "audience", label: "Audience", description: "" },
  ],
  tags: [tag, inheritedTag, automatedTag],
};
const taggedObject = {
  target: { objectType: "task" as const, objectId: "task-1" },
  objectVersion: 4,
  direct: [
    {
      id: "direct-1",
      tag,
      source: "human" as const,
      assignedAt: "2026-08-05T12:00:00Z",
      assignedBy: "Alex",
      inherited: false,
    },
  ],
  inherited: [
    {
      id: "inherited-1",
      tag: inheritedTag,
      source: "human" as const,
      assignedAt: "2026-08-04T12:00:00Z",
      assignedBy: "Maya",
      inherited: true,
      sourceObjectType: "project" as const,
      sourceObjectId: "project-1",
    },
  ],
  effective: [],
  classificationState: "classified" as const,
};
const secondTarget = { objectType: "task" as const, objectId: "task-2" };
const secondTaggedObject = {
  ...taggedObject,
  target: secondTarget,
  objectVersion: 8,
  direct: [
    {
      id: "direct-2",
      tag: automatedTag,
      source: "automation" as const,
      assignedAt: "2026-08-05T13:00:00Z",
      assignedBy: "Automation",
      inherited: false,
    },
  ],
  inherited: [],
};

function api(overrides: Partial<ClassificationAPI> = {}): ClassificationAPI {
  return {
    catalog: vi.fn().mockResolvedValue(classificationCatalog),
    object: vi.fn().mockResolvedValue(taggedObject),
    replaceDirect: vi
      .fn()
      .mockResolvedValue({ ...taggedObject, objectVersion: 5 }),
    history: vi.fn().mockResolvedValue([
      {
        id: "event-1",
        operation: "added",
        assignment: taggedObject.direct[0],
        targetVersion: 4,
        occurredAt: "2026-08-05T12:00:00Z",
        actorId: "Alex",
        inherited: false,
      },
    ]),
    ...overrides,
  };
}

it("retains unsaved direct selections when an inline API identity changes", async () => {
  const first = api();
  const view = render(
    <ObjectTagEditor
      api={first}
      clientID="client-1"
      target={taggedObject.target}
    />,
  );
  await screen.findByText("Direct tags");
  const picker = screen.getByRole("combobox", { name: /Classification tags/ });
  fireEvent.focus(picker);
  fireEvent.click(await screen.findByRole("option", { name: "Executive" }));
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toHaveTextContent("Executive");
  const replacement = api();
  view.rerender(
    <ObjectTagEditor
      api={replacement}
      clientID="client-1"
      target={taggedObject.target}
    />,
  );
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toHaveTextContent("Executive");
  expect(replacement.catalog).not.toHaveBeenCalled();
});

it("does not render the same object's prior Client classification before the new Client loads", async () => {
  const clientA = api();
  const clientB = api({
    object: vi.fn().mockResolvedValue({
      ...taggedObject,
      direct: secondTaggedObject.direct,
      inherited: [],
    }),
  });
  const view = render(
    <ObjectTagEditor
      api={clientA}
      clientID="client-a"
      target={taggedObject.target}
    />,
  );
  await screen.findByText("Direct tags");
  expect(screen.getByLabelText("VPN. Selected by a technician")).toBeVisible();

  view.rerender(
    <ObjectTagEditor
      api={clientB}
      clientID="client-b"
      target={taggedObject.target}
    />,
  );
  expect(screen.queryByText("Direct tags")).not.toBeInTheDocument();
  expect(
    await screen.findByLabelText("Automated. Applied by automation"),
  ).toBeVisible();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
}

function historyEntry(id: string, actorId: string) {
  return {
    id,
    operation: "added" as const,
    assignment: taggedObject.direct[0],
    targetVersion: 4,
    occurredAt: "2026-08-05T12:00:00Z",
    actorId,
    inherited: false,
  };
}

function aiSuggestion(
  status = "completed",
  disposition: "suggested" | "accepted" | "rejected" = "suggested",
): ClassificationSuggestion {
  return {
    id: "suggestion-1",
    status,
    version: 1,
    suggestions: [
      {
        id: "suggestion-item-1",
        tagId: "vpn",
        confidence: 0.99,
        rationale: "VPN access request",
        disposition,
      },
    ],
  };
}

describe("ObjectTagEditor", () => {
  it("does not paint a late AI request failure into a newly selected object", async () => {
    const pending = deferred<never>();
    const first = api({
      requestSuggestions: vi.fn().mockReturnValue(pending.promise),
    });
    const second = api({
      object: vi.fn().mockResolvedValue(secondTaggedObject),
      requestSuggestions: vi.fn(),
    });
    const view = render(
      <ObjectTagEditor
        api={first}
        target={taggedObject.target}
        clientID="client-1"
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Get AI suggestions" }),
    );
    view.rerender(
      <ObjectTagEditor
        api={second}
        target={secondTarget}
        clientID="client-2"
      />,
    );
    await screen.findByLabelText("Automated. Applied by automation");
    await act(async () => pending.reject(new Error("late")));
    expect(
      screen.queryByText("AI suggestions could not be requested."),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Get AI suggestions" }),
    ).toBeEnabled();
  });

  it("does not paint a late AI request success into a newly selected object", async () => {
    const pending = deferred<ClassificationSuggestion>();
    const first = api({
      requestSuggestions: vi.fn().mockReturnValue(pending.promise),
    });
    const second = api({
      object: vi.fn().mockResolvedValue(secondTaggedObject),
      requestSuggestions: vi.fn(),
    });
    const view = render(
      <ObjectTagEditor
        api={first}
        target={taggedObject.target}
        clientID="client-1"
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Get AI suggestions" }),
    );
    view.rerender(
      <ObjectTagEditor
        api={second}
        target={secondTarget}
        clientID="client-2"
      />,
    );
    await screen.findByLabelText("Automated. Applied by automation");
    await act(async () => pending.resolve(aiSuggestion()));
    expect(screen.queryByText(/VPN access request/)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Get AI suggestions" }),
    ).toBeEnabled();
  });

  it.each([
    ["success", false],
    ["error", true],
  ])(
    "ignores a late AI poll %s after a target and Client switch",
    async (_label, rejectPoll) => {
      vi.useFakeTimers();
      const poll = deferred<ClassificationSuggestion>();
      const first = api({
        requestSuggestions: vi.fn().mockResolvedValue(aiSuggestion("pending")),
        suggestion: vi.fn().mockReturnValue(poll.promise),
      });
      const second = api({
        object: vi.fn().mockResolvedValue(secondTaggedObject),
        requestSuggestions: vi.fn(),
      });
      const view = render(
        <ObjectTagEditor
          api={first}
          target={taggedObject.target}
          clientID="client-1"
        />,
      );
      await act(async () => vi.runAllTimersAsync());
      fireEvent.click(
        screen.getByRole("button", { name: "Get AI suggestions" }),
      );
      await act(async () => {});
      expect(screen.getByText("AI classification is pending.")).toBeVisible();
      await act(async () => vi.advanceTimersByTimeAsync(750));
      expect(first.suggestion).toHaveBeenCalledTimes(1);
      view.rerender(
        <ObjectTagEditor
          api={second}
          target={secondTarget}
          clientID="client-2"
        />,
      );
      await act(async () => vi.runAllTimersAsync());
      if (rejectPoll)
        await act(async () => poll.reject(new Error("late poll")));
      else await act(async () => poll.resolve(aiSuggestion()));
      expect(screen.queryByText(/VPN access request/)).not.toBeInTheDocument();
      expect(
        screen.queryByText("AI suggestion status could not be refreshed."),
      ).not.toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Get AI suggestions" }),
      ).toBeEnabled();
    },
  );

  it.each([
    ["accepted", "success", false],
    ["accepted", "error", true],
    ["dismissed", "success", false],
    ["dismissed", "error", true],
  ] as const)(
    "ignores a late %s decision %s after a target and Client switch",
    async (decision, _outcome, rejectDecision) => {
      const pending = deferred<ClassificationSuggestion>();
      const first = api({
        requestSuggestions: vi.fn().mockResolvedValue(aiSuggestion()),
        decideSuggestion: vi.fn().mockReturnValue(pending.promise),
      });
      const second = api({
        object: vi.fn().mockResolvedValue(secondTaggedObject),
        requestSuggestions: vi.fn(),
      });
      const view = render(
        <ObjectTagEditor
          api={first}
          target={taggedObject.target}
          clientID="client-1"
        />,
      );
      fireEvent.click(
        await screen.findByRole("button", { name: "Get AI suggestions" }),
      );
      const decisionButton = await screen.findByRole("button", {
        name: decision === "accepted" ? "Accept" : "Dismiss",
      });
      fireEvent.click(decisionButton);
      expect(screen.getByRole("button", { name: "Accept" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "Dismiss" })).toBeDisabled();
      view.rerender(
        <ObjectTagEditor
          api={second}
          target={secondTarget}
          clientID="client-2"
        />,
      );
      await screen.findByLabelText("Automated. Applied by automation");
      if (rejectDecision)
        await act(async () => pending.reject(new Error("late decision")));
      else
        await act(async () =>
          pending.resolve(
            aiSuggestion(
              "accepted",
              decision === "accepted" ? "accepted" : "rejected",
            ),
          ),
        );
      expect(
        screen.queryByText("AI suggestion could not be decided."),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByText(/AI tag accepted|AI suggestion dismissed/),
      ).not.toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Get AI suggestions" }),
      ).toBeEnabled();
    },
  );

  it("does not paint accepted-object or history refreshes after a target switch", async () => {
    const refreshedObject = deferred<TaggedObject>();
    const refreshedHistory = deferred<ReturnType<typeof historyEntry>[]>();
    const first = api({
      requestSuggestions: vi.fn().mockResolvedValue(aiSuggestion()),
      decideSuggestion: vi
        .fn()
        .mockResolvedValue(aiSuggestion("accepted", "accepted")),
      object: vi
        .fn()
        .mockResolvedValueOnce(taggedObject)
        .mockReturnValueOnce(refreshedObject.promise),
      history: vi
        .fn()
        .mockResolvedValueOnce([])
        .mockReturnValueOnce(refreshedHistory.promise),
    });
    const second = api({
      object: vi.fn().mockResolvedValue(secondTaggedObject),
      requestSuggestions: vi.fn(),
    });
    const view = render(
      <ObjectTagEditor
        api={first}
        target={taggedObject.target}
        clientID="client-1"
      />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Get AI suggestions" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Accept" }));
    await waitFor(() => expect(first.object).toHaveBeenCalledTimes(2));
    view.rerender(
      <ObjectTagEditor
        api={second}
        target={secondTarget}
        clientID="client-2"
      />,
    );
    await screen.findByLabelText("Automated. Applied by automation");
    await act(async () => {
      refreshedObject.resolve({ ...taggedObject, direct: [] });
      refreshedHistory.resolve([historyEntry("stale-ai", "Stale AI")]);
    });
    expect(
      screen.getByLabelText("Automated. Applied by automation"),
    ).toBeVisible();
    expect(screen.queryByText(/Stale AI added VPN/)).not.toBeInTheDocument();
    expect(
      screen.queryByText("AI tag accepted and applied."),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Get AI suggestions" }),
    ).toBeEnabled();
  });

  it("shows direct and inherited tags, saves a complete direct set, and explains history", async () => {
    const client = api();
    render(
      <ObjectTagEditor
        api={client}
        target={taggedObject.target}
        clientID="client-1"
      />,
    );
    await screen.findByText("Direct tags");
    expect(screen.getByText("Inherited tags")).toBeVisible();
    const sourceProject = screen.getByRole("link", {
      name: "View source project",
    });
    expect(sourceProject).toHaveAttribute(
      "href",
      "#/project?recordType=project&recordID=project-1&clientID=client-1&label=Project",
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove VPN" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() =>
      expect(client.replaceDirect).toHaveBeenCalledWith(
        taggedObject.target,
        expect.objectContaining({ tagIDs: [], expectedVersion: 4 }),
        expect.anything(),
      ),
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Classification saved.",
    );
    expect(screen.getByText(/Alex added VPN/)).toBeVisible();
  });

  it("reloads and compares current tags after a version conflict", async () => {
    const current = { ...taggedObject, objectVersion: 6, direct: [] };
    const client = api({
      replaceDirect: vi
        .fn()
        .mockRejectedValue({ code: "version_conflict", status: 409 }),
      object: vi
        .fn()
        .mockResolvedValueOnce(taggedObject)
        .mockResolvedValueOnce(current),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    expect(
      await screen.findByText("Classification changed elsewhere"),
    ).toBeVisible();
    expect(screen.getByText("Your submitted tags: VPN")).toBeVisible();
    expect(screen.getByText("Current tags: None")).toBeVisible();
  });

  it("moves focus to classification-required recovery", async () => {
    const client = api({
      replaceDirect: vi
        .fn()
        .mockRejectedValue({ code: "classification_required", status: 422 }),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "Select at least one meaningful classification tag.",
    );
    expect(alert).toHaveFocus();
  });

  it("preserves direct assignment provenance on every direct chip", async () => {
    const client = api({
      object: vi.fn().mockResolvedValue({
        ...taggedObject,
        direct: [
          taggedObject.direct[0],
          {
            id: "direct-automation",
            tag: automatedTag,
            source: "automation",
            inherited: false,
          },
        ],
      }),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    expect(
      screen.getByLabelText("VPN. Selected by a technician"),
    ).toBeVisible();
    expect(
      screen.getByLabelText("Automated. Applied by automation"),
    ).toBeVisible();
  });

  it("keeps successful save completion when the optional history refresh fails", async () => {
    const client = api({
      history: vi
        .fn()
        .mockResolvedValueOnce([])
        .mockRejectedValueOnce(new Error("history unavailable")),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Classification saved.",
    );
    expect(screen.getByText("History could not be refreshed.")).toBeVisible();
    expect(
      screen.queryByText("Classification could not be saved. Try again."),
    ).not.toBeInTheDocument();
  });

  it("keeps classification editing available when initial history loading fails", async () => {
    const client = api({
      history: vi.fn().mockRejectedValue(new Error("offline")),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    expect(screen.getByText("History could not be refreshed.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Remove VPN" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() => expect(client.replaceDirect).toHaveBeenCalled());
  });

  it("ignores a late initial history resolution after a newer post-save refresh", async () => {
    const initialHistory = deferred<ReturnType<typeof historyEntry>[]>();
    const refreshedHistory = deferred<ReturnType<typeof historyEntry>[]>();
    const client = api({
      history: vi
        .fn()
        .mockImplementationOnce(() => initialHistory.promise)
        .mockImplementationOnce(() => refreshedHistory.promise),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() => expect(client.history).toHaveBeenCalledTimes(2));
    await act(async () =>
      refreshedHistory.resolve([historyEntry("fresh", "Fresh")]),
    );
    expect(await screen.findByText(/Fresh added VPN/)).toBeVisible();
    await act(async () =>
      initialHistory.resolve([historyEntry("stale", "Stale")]),
    );
    await waitFor(() =>
      expect(screen.getByText(/Fresh added VPN/)).toBeVisible(),
    );
    expect(screen.queryByText(/Stale added VPN/)).not.toBeInTheDocument();
  });

  it("ignores a late initial history rejection after a newer post-save refresh", async () => {
    const initialHistory = deferred<ReturnType<typeof historyEntry>[]>();
    const refreshedHistory = deferred<ReturnType<typeof historyEntry>[]>();
    const client = api({
      history: vi
        .fn()
        .mockImplementationOnce(() => initialHistory.promise)
        .mockImplementationOnce(() => refreshedHistory.promise),
    });
    render(<ObjectTagEditor api={client} target={taggedObject.target} />);
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() => expect(client.history).toHaveBeenCalledTimes(2));
    await act(async () =>
      refreshedHistory.resolve([historyEntry("fresh", "Fresh")]),
    );
    expect(await screen.findByText(/Fresh added VPN/)).toBeVisible();
    await act(async () =>
      initialHistory.reject(new Error("late initial failure")),
    );
    await waitFor(() =>
      expect(screen.getByText(/Fresh added VPN/)).toBeVisible(),
    );
    expect(
      screen.queryByText("History could not be refreshed."),
    ).not.toBeInTheDocument();
  });

  it("keeps the new target unchanged when a prior target save resolves late", async () => {
    const delayedSave = deferred<typeof taggedObject>();
    const client = api({
      object: vi
        .fn()
        .mockImplementation((target) =>
          Promise.resolve(
            target.objectId === secondTarget.objectId
              ? secondTaggedObject
              : taggedObject,
          ),
        ),
      replaceDirect: vi.fn().mockImplementation(() => delayedSave.promise),
      history: vi
        .fn()
        .mockImplementation((target) =>
          Promise.resolve([
            historyEntry(
              target.objectId === secondTarget.objectId ? "second" : "first",
              target.objectId === secondTarget.objectId ? "Blair" : "Alex",
            ),
          ]),
        ),
    });
    const view = render(
      <ObjectTagEditor api={client} target={taggedObject.target} />,
    );
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() => expect(client.replaceDirect).toHaveBeenCalledTimes(1));

    view.rerender(<ObjectTagEditor api={client} target={secondTarget} />);
    expect(
      await screen.findByLabelText("Automated. Applied by automation"),
    ).toBeVisible();
    expect(await screen.findByText(/Blair added VPN/)).toBeVisible();

    await act(async () =>
      delayedSave.resolve({ ...taggedObject, objectVersion: 5 }),
    );
    await waitFor(() =>
      expect(
        screen.getByLabelText("Automated. Applied by automation"),
      ).toBeVisible(),
    );
    expect(screen.getByText(/Blair added VPN/)).toBeVisible();
    expect(screen.queryByText("Classification saved.")).not.toBeInTheDocument();
  });

  it("keeps the new target unchanged when a prior target save rejects late", async () => {
    const delayedSave = deferred<typeof taggedObject>();
    const client = api({
      object: vi
        .fn()
        .mockImplementation((target) =>
          Promise.resolve(
            target.objectId === secondTarget.objectId
              ? secondTaggedObject
              : taggedObject,
          ),
        ),
      replaceDirect: vi.fn().mockImplementation(() => delayedSave.promise),
      history: vi
        .fn()
        .mockImplementation((target) =>
          Promise.resolve([
            historyEntry(
              target.objectId === secondTarget.objectId ? "second" : "first",
              target.objectId === secondTarget.objectId ? "Blair" : "Alex",
            ),
          ]),
        ),
    });
    const view = render(
      <ObjectTagEditor api={client} target={taggedObject.target} />,
    );
    await screen.findByText("Direct tags");
    fireEvent.click(
      screen.getByRole("button", { name: "Save classification" }),
    );
    await waitFor(() => expect(client.replaceDirect).toHaveBeenCalledTimes(1));

    view.rerender(<ObjectTagEditor api={client} target={secondTarget} />);
    expect(
      await screen.findByLabelText("Automated. Applied by automation"),
    ).toBeVisible();
    expect(await screen.findByText(/Blair added VPN/)).toBeVisible();

    await act(async () =>
      delayedSave.reject({ code: "classification_required", status: 422 }),
    );
    await waitFor(() =>
      expect(
        screen.getByLabelText("Automated. Applied by automation"),
      ).toBeVisible(),
    );
    expect(screen.getByText(/Blair added VPN/)).toBeVisible();
    expect(
      screen.queryByText("Select at least one meaningful classification tag."),
    ).not.toBeInTheDocument();
  });

  it("does not render or save A's classification when B's load fails", async () => {
    const client = api({
      object: vi
        .fn()
        .mockResolvedValueOnce(taggedObject)
        .mockRejectedValueOnce(new Error("B unavailable")),
    });
    const view = render(
      <ObjectTagEditor api={client} target={taggedObject.target} />,
    );
    await screen.findByText("Direct tags");

    view.rerender(<ObjectTagEditor api={client} target={secondTarget} />);
    expect(
      await screen.findByText("Classification could not be loaded."),
    ).toBeVisible();
    expect(screen.queryByText("Direct tags")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Save classification" }),
    ).not.toBeInTheDocument();
    expect(client.replaceDirect).not.toHaveBeenCalled();
  });

  it("ignores late A catalog and object resolutions after B has loaded", async () => {
    const catalogA = deferred<ClassificationCatalog>();
    const objectA = deferred<TaggedObject>();
    const catalogB = deferred<ClassificationCatalog>();
    const objectB = deferred<TaggedObject>();
    const client = api({
      catalog: vi
        .fn()
        .mockImplementationOnce(() => catalogA.promise)
        .mockImplementationOnce(() => catalogB.promise),
      object: vi
        .fn()
        .mockImplementationOnce(() => objectA.promise)
        .mockImplementationOnce(() => objectB.promise),
    });
    const view = render(
      <ObjectTagEditor api={client} target={taggedObject.target} />,
    );
    await waitFor(() => expect(client.object).toHaveBeenCalledTimes(1));

    view.rerender(<ObjectTagEditor api={client} target={secondTarget} />);
    await waitFor(() => expect(client.object).toHaveBeenCalledTimes(2));
    await act(async () => {
      catalogB.resolve(classificationCatalog);
      objectB.resolve(secondTaggedObject);
    });
    expect(
      await screen.findByLabelText("Automated. Applied by automation"),
    ).toBeVisible();

    await act(async () => {
      catalogA.resolve(classificationCatalog);
      objectA.resolve(taggedObject);
    });
    await waitFor(() =>
      expect(
        screen.getByLabelText("Automated. Applied by automation"),
      ).toBeVisible(),
    );
    expect(
      screen.queryByText("Classification could not be loaded."),
    ).not.toBeInTheDocument();
  });

  it("ignores a late A catalog failure after B has loaded", async () => {
    const catalogA = deferred<ClassificationCatalog>();
    const objectA = deferred<TaggedObject>();
    const catalogB = deferred<ClassificationCatalog>();
    const objectB = deferred<TaggedObject>();
    const client = api({
      catalog: vi
        .fn()
        .mockImplementationOnce(() => catalogA.promise)
        .mockImplementationOnce(() => catalogB.promise),
      object: vi
        .fn()
        .mockImplementationOnce(() => objectA.promise)
        .mockImplementationOnce(() => objectB.promise),
    });
    const view = render(
      <ObjectTagEditor api={client} target={taggedObject.target} />,
    );
    await waitFor(() => expect(client.object).toHaveBeenCalledTimes(1));

    view.rerender(<ObjectTagEditor api={client} target={secondTarget} />);
    await waitFor(() => expect(client.object).toHaveBeenCalledTimes(2));
    await act(async () => {
      catalogB.resolve(classificationCatalog);
      objectB.resolve(secondTaggedObject);
    });
    expect(
      await screen.findByLabelText("Automated. Applied by automation"),
    ).toBeVisible();

    await act(async () => catalogA.reject(new Error("late A failure")));
    await waitFor(() =>
      expect(
        screen.getByLabelText("Automated. Applied by automation"),
      ).toBeVisible(),
    );
    expect(
      screen.queryByText("Classification could not be loaded."),
    ).not.toBeInTheDocument();
    objectA.reject(new Error("late A object failure"));
  });
});
