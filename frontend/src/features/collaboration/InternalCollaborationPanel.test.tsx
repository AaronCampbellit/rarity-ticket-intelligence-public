import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { findA11yViolations } from "../../design-system/testing/renderA11y";
import { createMentionAPI, MentionAPIError } from "../mentions/api";
import type {
  CollaborationAPI,
  InternalContentSource,
  MentionDeepLink,
  MentionDocument,
} from "../mentions/types";
import { InternalCollaborationPanel } from "./InternalCollaborationPanel";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", window.location.pathname);
});

const emptyDocument: MentionDocument = {
  body: "",
  tokens: [],
  confirmedTeamSnapshots: {},
};

const mutationPanelProps = {
  capabilities: new Set(["mention.create"]),
  canEditParent: true,
};

function source(
  id: string,
  sourceKind: InternalContentSource["sourceKind"],
  body: string,
  overrides: Partial<InternalContentSource> = {},
): InternalContentSource {
  return {
    id,
    parentType: "work_record",
    parentId: "work-1",
    sourceKind,
    document: { body, tokens: [], confirmedTeamSnapshots: {} },
    authorId: "staff-1",
    lifecycleState: "active",
    version: 1,
    createdAt: "2026-08-08T12:00:00Z",
    updatedAt: "2026-08-08T12:00:00Z",
    readOnly: false,
    legacy: false,
    ...overrides,
  };
}

function wireSource(value: InternalContentSource) {
  return {
    id: value.id,
    parent_type: value.parentType,
    parent_id: value.parentId,
    source_kind: value.sourceKind,
    body: value.document.body,
    tokens: value.document.tokens.map((token) => ({
      id: token.id,
      target_type: token.targetType,
      target_id: token.targetId,
      label: token.label,
      start: token.start,
      end: token.end,
    })),
    author_id: value.authorId,
    lifecycle_state: value.lifecycleState,
    version: value.version,
    created_at: value.createdAt,
    updated_at: value.updatedAt,
    redacted_at: value.redactedAt,
    read_only: value.readOnly,
    legacy: value.legacy,
  };
}

function apiWith(rows: InternalContentSource[] = []): CollaborationAPI {
  return {
    list: vi.fn().mockResolvedValue(rows),
    candidates: vi.fn().mockResolvedValue([]),
    save: vi.fn().mockImplementation(async (context, document, options) =>
      source(
        options.sourceId || `${context.sourceKind}-new`,
        context.sourceKind,
        document.body,
        {
          parentType: context.parentType,
          parentId: context.parentId,
          document,
          version: options.expectedVersion + 1,
        },
      ),
    ),
    redact: vi.fn().mockImplementation(async (context, sourceId, version) =>
      source(sourceId, context.sourceKind, "", {
        parentType: context.parentType,
        parentId: context.parentId,
        document: emptyDocument,
        lifecycleState: "redacted",
        version: version + 1,
        redactedAt: "2026-08-08T15:00:00Z",
      }),
    ),
  };
}

function replaceEditor(name: string, body: string) {
  const editor = screen.getByRole("textbox", { name });
  editor.textContent = body;
  fireEvent.input(editor);
}

function authenticatedLink(
  overrides: Partial<MentionDeepLink & { tokenId: string }> = {},
) {
  return {
    href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&sourceID=comment-1",
    clientId: "client-1",
    parentType: "work_record" as const,
    parentId: "work-1",
    sourceId: "comment-1",
    tokenId: "token-1",
    sourceAvailable: true,
    itemVersion: 4,
    ...overrides,
  } satisfies MentionDeepLink & { tokenId: string };
}

describe("InternalCollaborationPanel", () => {
  it("focuses and outlines the exact authenticated token with reduced-motion scrolling", async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    vi.stubGlobal("matchMedia", vi.fn().mockReturnValue({ matches: true }));
    const link = authenticatedLink();
    window.history.replaceState(null, "", link.href);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        authenticatedMentionLink={link}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={apiWith([
          source("comment-1", "comment", "Exact @Mira comment", {
            document: {
              body: "Exact @Mira comment",
              tokens: [
                {
                  id: "token-1",
                  targetType: "staff",
                  targetId: "staff-1",
                  label: "@Mira",
                  start: 6,
                  end: 11,
                },
              ],
              confirmedTeamSnapshots: {},
            },
          }),
        ])}
      />,
    );

    const token = await waitFor(() => {
      const found = document.querySelector<HTMLElement>(
        '[data-mention-token-id="token-1"]',
      );
      expect(found).not.toBeNull();
      return found!;
    });
    await waitFor(() => expect(token).toHaveFocus());
    expect(token).toHaveClass("mention-token-focus");
    expect(token.closest("article")).not.toHaveClass("mention-source-focus");
    expect(scrollIntoView).toHaveBeenCalledWith({
      behavior: "auto",
      block: "center",
    });
  });

  it("keeps history visible but removes every composer and mutation action without create plus parent edit", async () => {
    const api = apiWith([
      source("comment-1", "comment", "Readable internal history"),
    ]);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        capabilities={new Set(["mention.read", "work_record.read"])}
        canEditParent={false}
        api={api}
      />,
    );

    expect(await screen.findByText("Readable internal history")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /internal/i }),
    ).not.toBeInTheDocument();
    expect(api.list).toHaveBeenCalledTimes(1);
    expect(api.save).not.toHaveBeenCalled();
    expect(api.redact).not.toHaveBeenCalled();
  });

  it("fails closed when a host omits mutation permission inputs", async () => {
    const api = apiWith([
      source("comment-1", "comment", "History remains readable"),
    ]);
    render(
      <InternalCollaborationPanel
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );

    expect(await screen.findByText("History remains readable")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(api.save).not.toHaveBeenCalled();
    expect(api.redact).not.toHaveBeenCalled();
  });

  it("allows create-only collaboration when parent edit is effective", async () => {
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        capabilities={new Set(["mention.create", "work_record.edit"])}
        canEditParent
        api={apiWith([])}
      />,
    );

    expect(
      await screen.findByRole("textbox", { name: "New internal comment" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Add internal comment" }),
    ).toBeEnabled();
  });

  it("focuses a non-disclosing notice when the source or token is unavailable", async () => {
    const link = authenticatedLink({
      href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1",
      sourceId: undefined,
      tokenId: undefined,
      sourceAvailable: false,
    } as Partial<MentionDeepLink & { tokenId: string }>);
    window.history.replaceState(null, "", link.href);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        authenticatedMentionLink={link}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={apiWith([])}
      />,
    );

    const notice = await screen.findByRole("status", {
      name: "Mention source unavailable",
    });
    await waitFor(() => expect(notice).toHaveFocus());
    expect(notice).toHaveTextContent(
      "source was removed, redacted, or no longer contains the mention",
    );
    expect(notice).not.toHaveTextContent("comment-1");
  });

  it("treats an exact source that became redacted as unavailable", async () => {
    const link = authenticatedLink();
    window.history.replaceState(null, "", link.href);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        authenticatedMentionLink={link}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={apiWith([
          source("comment-1", "comment", "", {
            lifecycleState: "redacted",
            redactedAt: "2026-08-08T14:00:00Z",
          }),
        ])}
      />,
    );

    const notice = await screen.findByRole("status", {
      name: "Mention source unavailable",
    });
    await waitFor(() => expect(notice).toHaveFocus());
  });

  it("shows the safe notice when an active exact source no longer has the authenticated token", async () => {
    const link = {
      href: "#/work?parentID=work-1&mentionOccurrenceID=occurrence-1&sourceID=comment-1",
      clientId: "client-1",
      parentType: "work_record",
      parentId: "work-1",
      sourceId: "comment-1",
      tokenId: "token-removed",
      sourceAvailable: true,
      itemVersion: 4,
    } satisfies MentionDeepLink & { tokenId: string };
    window.history.replaceState(null, "", link.href);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        authenticatedMentionLink={link}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={apiWith([
          source("comment-1", "comment", "Edited without the mention", {
            document: {
              body: "Edited without the mention",
              tokens: [],
              confirmedTeamSnapshots: {},
            },
          }),
        ])}
      />,
    );

    const notice = await screen.findByRole("status", {
      name: "Mention source unavailable",
    });
    await waitFor(() => expect(notice).toHaveFocus());
    expect(
      screen.getByText("Edited without the mention").closest("article"),
    ).not.toHaveClass("mention-source-focus");
  });

  it("renders non-null internal regions, legacy plain text, edited and redacted states without a visibility selector", async () => {
    const api = apiWith([
      source("details-1", "details", "Private rollout details", { version: 2 }),
      source("comment-1", "comment", "Edited investigation", {
        version: 3,
        updatedAt: "2026-08-08T14:00:00Z",
      }),
      source("legacy-1", "comment", "Historical plain @Mira", {
        readOnly: true,
        legacy: true,
      }),
      source("note-1", "note", "", {
        document: emptyDocument,
        lifecycleState: "redacted",
        version: 2,
        redactedAt: "2026-08-08T14:00:00Z",
      }),
    ]);
    const { container } = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );

    expect(
      await screen.findByRole("region", { name: "Internal details" }),
    ).toBeVisible();
    expect(
      screen.getByRole("region", { name: "Internal comments" }),
    ).toBeVisible();
    expect(
      screen.getByRole("region", { name: "Internal notes" }),
    ).toBeVisible();
    expect(screen.getByText("Edited · Version 3")).toBeVisible();
    const legacy = screen
      .getByText("Historical plain @Mira")
      .closest("article");
    expect(legacy).toHaveTextContent("Legacy internal comment · Read only");
    expect(
      within(legacy as HTMLElement).queryByRole("textbox"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Redacted internal note")).toBeVisible();
    expect(screen.queryByLabelText(/visibility/i)).not.toBeInTheDocument();
    expect(screen.getAllByText("Internal only").length).toBeGreaterThan(0);
    expect(await findA11yViolations(container)).toEqual([]);
  });

  it("saves details, creates comments and notes, then edits and redacts allowed native sources", async () => {
    const api = apiWith([source("comment-1", "comment", "Original comment")]);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    await screen.findByRole("region", { name: "Internal details" });

    replaceEditor("Internal details", "Private details");
    fireEvent.click(
      screen.getByRole("button", { name: "Save internal details" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(1));
    replaceEditor("New internal comment", "New comment");
    fireEvent.click(
      screen.getByRole("button", { name: "Add internal comment" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(2));
    replaceEditor("New internal note", "New note");
    fireEvent.click(screen.getByRole("button", { name: "Add internal note" }));
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(3));
    expect(
      vi.mocked(api.save).mock.calls.map(([context]) => context.sourceKind),
    ).toEqual(["details", "comment", "note"]);

    const originalComment = screen
      .getByText("Original comment")
      .closest("article");
    expect(originalComment).not.toBeNull();
    fireEvent.click(
      within(originalComment!).getByRole("button", {
        name: "Edit internal comment",
      }),
    );
    replaceEditor("Edit internal comment", "Edited comment");
    fireEvent.click(
      screen.getByRole("button", { name: "Save edited comment" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(4));
    expect(vi.mocked(api.save).mock.calls[3]?.[2]).toMatchObject({
      sourceId: "comment-1",
      expectedVersion: 1,
    });

    const editedComment = screen.getByText("Edited comment").closest("article");
    expect(editedComment).not.toBeNull();
    fireEvent.click(
      within(editedComment!).getByRole("button", {
        name: "Edit internal comment",
      }),
    );
    fireEvent.click(
      within(editedComment!).getByRole("button", {
        name: "Redact internal comment",
      }),
    );
    await waitFor(() =>
      expect(api.redact).toHaveBeenCalledWith(
        expect.objectContaining({
          parentType: "work_record",
          parentId: "work-1",
          sourceKind: "comment",
        }),
        "comment-1",
        expect.any(Number),
        expect.any(String),
        expect.any(AbortSignal),
      ),
    );
    expect(await screen.findByText("Redacted internal comment")).toBeVisible();
    expect(
      screen.queryByRole("textbox", { name: "Edit internal comment" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("");
  });

  it("redacts native details terminally, clears only its structured draft after success, and preserves every draft on failure", async () => {
    const details = source("details-1", "details", "Private @NOC details", {
      document: {
        body: "Private @NOC details",
        tokens: [
          {
            id: "token-1",
            targetType: "team",
            targetId: "team-1",
            label: "@NOC",
            start: 8,
            end: 12,
          },
        ],
        confirmedTeamSnapshots: {
          "team-1": { teamVersion: 1, eligibleMemberIds: ["staff-2"] },
        },
      },
    });
    const api = apiWith([details]);
    vi.mocked(api.redact).mockRejectedValueOnce(new Error("unavailable"));
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: "Internal details" }),
      ).toHaveTextContent("Private @NOC details"),
    );
    replaceEditor("New internal comment", "Unrelated comment draft");
    fireEvent.click(
      screen.getByRole("button", { name: "Redact internal details" }),
    );
    expect(
      await screen.findByText("Internal content could not be redacted."),
    ).toBeVisible();
    expect(
      screen.getByRole("textbox", { name: "Internal details" }),
    ).toHaveTextContent("Private @NOC details");
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("Unrelated comment draft");

    vi.mocked(api.redact).mockImplementationOnce(
      async (context, sourceId, version) =>
        source(sourceId, context.sourceKind, "", {
          document: emptyDocument,
          lifecycleState: "redacted",
          version: version + 1,
        }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Redact internal details" }),
    );
    expect(await screen.findByText("Redacted internal details")).toBeVisible();
    expect(
      screen.getByRole("textbox", { name: "Internal details" }),
    ).toHaveTextContent("");
    expect(
      screen.getByRole("textbox", { name: "Internal details" }),
    ).toHaveAttribute("contenteditable", "false");
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("Unrelated comment draft");
  });

  it("restores an edit with its exact source operation and never converts a missing edit target into create", async () => {
    const original = source("comment-1", "comment", "Original comment");
    const api = apiWith();
    vi.mocked(api.list).mockImplementation(async (context) =>
      context.parentId === "work-1" ? [original] : [],
    );
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    const card = (await screen.findByText("Original comment")).closest(
      "article",
    )!;
    fireEvent.click(
      within(card).getByRole("button", { name: "Edit internal comment" }),
    );
    replaceEditor("Edit internal comment", "Preserved edit");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-2"
        authorId="staff-1"
        api={api}
      />,
    );
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: "Edit internal comment" }),
      ).toHaveTextContent("Preserved edit"),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save edited comment" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalled());
    expect(vi.mocked(api.save).mock.calls.at(-1)?.[2]).toMatchObject({
      sourceId: "comment-1",
      expectedVersion: 1,
    });

    vi.mocked(api.list).mockResolvedValue([]);
    const restored = source("note-1", "note", "Original note");
    vi.mocked(api.list).mockResolvedValueOnce([restored]);
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-3"
        authorId="staff-1"
        api={api}
      />,
    );
    const noteCard = (await screen.findByText("Original note")).closest(
      "article",
    )!;
    fireEvent.click(
      within(noteCard).getByRole("button", { name: "Edit internal note" }),
    );
    replaceEditor("Edit internal note", "Orphaned note edit");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-4"
        authorId="staff-1"
        api={api}
      />,
    );
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-3"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "original internal note is unavailable",
    );
    expect(
      screen.getByRole("button", { name: "Save edited note" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("textbox", { name: "Edit internal note" }),
    ).toHaveTextContent("Orphaned note edit");
    fireEvent.click(
      screen.getByRole("button", { name: "Discard unavailable edit" }),
    );
    expect(
      screen.getByRole("textbox", { name: "New internal note" }),
    ).toHaveTextContent("");
  });

  it("blocks a restored edit while its exact source validation is pending", async () => {
    let resolveRestored!: (rows: InternalContentSource[]) => void;
    const original = source("comment-1", "comment", "Original comment");
    const api = apiWith();
    vi.mocked(api.list)
      .mockResolvedValueOnce([original])
      .mockResolvedValueOnce([])
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveRestored = resolve;
          }),
      );
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    const card = (await screen.findByText("Original comment")).closest(
      "article",
    )!;
    fireEvent.click(
      within(card).getByRole("button", { name: "Edit internal comment" }),
    );
    replaceEditor("Edit internal comment", "Pending restored edit");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-2"
        authorId="staff-1"
        api={api}
      />,
    );
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );

    expect(
      await screen.findByRole("status", {
        name: "Validating restored internal comment",
      }),
    ).toBeVisible();
    const save = screen.getByRole("button", {
      name: "Save edited comment",
    });
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(api.save).not.toHaveBeenCalled();

    resolveRestored([original]);
    await waitFor(() => expect(save).toBeEnabled());
  });

  it("keeps a restored edit non-mutable when its source list fails", async () => {
    const original = source("note-1", "note", "Original note");
    const api = apiWith();
    vi.mocked(api.list)
      .mockResolvedValueOnce([original])
      .mockResolvedValueOnce([])
      .mockRejectedValueOnce(new Error("list unavailable"));
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    const card = (await screen.findByText("Original note")).closest("article")!;
    fireEvent.click(
      within(card).getByRole("button", { name: "Edit internal note" }),
    );
    replaceEditor("Edit internal note", "Failed-list edit");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-2"
        authorId="staff-1"
        api={api}
      />,
    );
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );

    expect(
      await screen.findByRole("alert", {
        name: "Unavailable restored internal note",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("textbox", { name: "Edit internal note" }),
    ).toHaveTextContent("Failed-list edit");
    const save = screen.getByRole("button", { name: "Save edited note" });
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(api.save).not.toHaveBeenCalled();
  });

  it.each([
    ["missing", []],
    [
      "redacted",
      [
        source("comment-1", "comment", "", {
          document: emptyDocument,
          lifecycleState: "redacted",
          version: 2,
        }),
      ],
    ],
  ] as const)(
    "never mutates a restored edit whose exact source is %s",
    async (_state, restoredRows) => {
      const original = source("comment-1", "comment", "Original comment");
      const api = apiWith();
      vi.mocked(api.list)
        .mockResolvedValueOnce([original])
        .mockResolvedValueOnce([])
        .mockResolvedValueOnce([...restoredRows]);
      const view = render(
        <InternalCollaborationPanel
          {...mutationPanelProps}
          clientId="client-1"
          parentType="work_record"
          parentId="work-1"
          authorId="staff-1"
          api={api}
        />,
      );
      const card = (await screen.findByText("Original comment")).closest(
        "article",
      )!;
      fireEvent.click(
        within(card).getByRole("button", { name: "Edit internal comment" }),
      );
      replaceEditor("Edit internal comment", "Unavailable edit draft");
      view.rerender(
        <InternalCollaborationPanel
          {...mutationPanelProps}
          clientId="client-1"
          parentType="task"
          parentId="task-2"
          authorId="staff-1"
          api={api}
        />,
      );
      await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
      view.rerender(
        <InternalCollaborationPanel
          {...mutationPanelProps}
          clientId="client-1"
          parentType="work_record"
          parentId="work-1"
          authorId="staff-1"
          api={api}
        />,
      );

      expect(
        await screen.findByRole("alert", {
          name: "Unavailable restored internal comment",
        }),
      ).toBeVisible();
      expect(
        screen.getByRole("textbox", { name: "Edit internal comment" }),
      ).toHaveTextContent("Unavailable edit draft");
      const save = screen.getByRole("button", {
        name: "Save edited comment",
      });
      expect(save).toBeDisabled();
      fireEvent.click(save);
      expect(api.save).not.toHaveBeenCalled();
    },
  );

  it("PATCHes the stable source URL through restored-edit 409 reapply", async () => {
    const original = source("comment-1", "comment", "Original comment");
    const current = source("comment-1", "comment", "Current server comment", {
      version: 2,
    });
    const saved = source("comment-1", "comment", "Validated restored edit", {
      version: 3,
    });
    let workListCalls = 0;
    let patchCalls = 0;
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "PATCH") {
          patchCalls += 1;
          if (patchCalls === 1) {
            return Response.json(
              { error: { code: "version_conflict", message: "Conflict" } },
              { status: 409 },
            );
          }
          return Response.json(wireSource(saved));
        }
        if (url === "/api/v1/tasks/task-2/internal-content") {
          return Response.json([]);
        }
        workListCalls += 1;
        return Response.json([
          wireSource(workListCalls >= 3 ? current : original),
        ]);
      },
    );
    const api = createMentionAPI(fetcher as typeof fetch);
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    const card = (await screen.findByText("Original comment")).closest(
      "article",
    )!;
    fireEvent.click(
      within(card).getByRole("button", { name: "Edit internal comment" }),
    );
    replaceEditor("Edit internal comment", "Validated restored edit");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-2"
        authorId="staff-1"
        api={api}
      />,
    );
    await screen.findByText("No internal comments yet.");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    const save = await screen.findByRole("button", {
      name: "Save edited comment",
    });
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current server text: Current server comment",
    );
    fireEvent.click(screen.getByRole("button", { name: "Reapply my draft" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Save edited comment" }),
    );
    await screen.findByText("Validated restored edit");
    const mutationCalls = fetcher.mock.calls.filter(
      ([, init]) => init?.method !== undefined,
    );
    expect(mutationCalls).toHaveLength(2);
    for (const [url, init] of mutationCalls) {
      expect(url).toBe("/api/v1/internal-content/comment-1");
      expect(init?.method).toBe("PATCH");
    }
    expect(
      mutationCalls.map(
        ([, init]) => JSON.parse(String(init?.body)).expected_version,
      ),
    ).toEqual([1, 2]);
  });

  it("preserves create behavior for a new draft when the source list fails", async () => {
    const api = apiWith();
    vi.mocked(api.list).mockRejectedValueOnce(new Error("list unavailable"));
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(
      await screen.findByText("Internal content is unavailable."),
    ).toBeVisible();
    replaceEditor("New internal comment", "Independent new draft");
    fireEvent.click(
      screen.getByRole("button", { name: "Add internal comment" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.save).mock.calls[0]?.[2]).toMatchObject({
      expectedVersion: 0,
    });
    expect(vi.mocked(api.save).mock.calls[0]?.[2].sourceId).toBeUndefined();
  });

  it.each(["comment", "note"] as const)(
    "loading a redacted server %s exits edit mode and prevents PATCH",
    async (kind) => {
      const initial = source(`${kind}-1`, kind, `Original ${kind}`);
      const redacted = source(`${kind}-1`, kind, "", {
        document: emptyDocument,
        lifecycleState: "redacted",
        version: 2,
      });
      const api = apiWith([initial]);
      vi.mocked(api.list)
        .mockResolvedValueOnce([initial])
        .mockResolvedValueOnce([redacted]);
      vi.mocked(api.save).mockRejectedValueOnce(
        new MentionAPIError("version_conflict", 409),
      );
      render(
        <InternalCollaborationPanel
          {...mutationPanelProps}
          clientId="client-1"
          parentType="work_record"
          parentId="work-1"
          authorId="staff-1"
          api={api}
        />,
      );
      const label = `internal ${kind}`;
      const card = (await screen.findByText(`Original ${kind}`)).closest(
        "article",
      )!;
      fireEvent.click(
        within(card).getByRole("button", { name: `Edit ${label}` }),
      );
      replaceEditor(`Edit ${label}`, `My ${kind} draft`);
      fireEvent.click(
        screen.getByRole("button", { name: `Save edited ${kind}` }),
      );
      fireEvent.click(
        await screen.findByRole("button", { name: "Load server version" }),
      );
      expect(
        screen.queryByRole("textbox", { name: `Edit ${label}` }),
      ).not.toBeInTheDocument();
      expect(
        screen.getByRole("textbox", { name: `New ${label}` }),
      ).toHaveTextContent("");
      fireEvent.click(
        screen.getByRole("button", { name: `Add internal ${kind}` }),
      );
      expect(api.save).toHaveBeenCalledTimes(1);
    },
  );

  it("synchronously hides sources and actions when the Client or parent context changes", async () => {
    let resolveNext!: (rows: InternalContentSource[]) => void;
    const api = apiWith([source("comment-1", "comment", "Client one secret")]);
    vi.mocked(api.list)
      .mockResolvedValueOnce([
        source("comment-1", "comment", "Client one secret"),
      ])
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveNext = resolve;
          }),
      );
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(await screen.findByText("Client one secret")).toBeVisible();
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-2"
        parentType="project"
        parentId="project-2"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(screen.queryByText("Client one secret")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Edit internal comment" }),
    ).not.toBeInTheDocument();
    resolveNext([]);
  });

  it("aborts switched loads and keeps separate drafts for every parent and source kind", async () => {
    const pending = new Map<string, (rows: InternalContentSource[]) => void>();
    const signals = new Map<string, AbortSignal | undefined>();
    const api = apiWith();
    api.list = vi.fn((context, signal) => {
      signals.set(context.parentId, signal);
      return new Promise<InternalContentSource[]>((resolve) =>
        pending.set(context.parentId, resolve),
      );
    });
    const view = render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    replaceEditor("New internal comment", "Ticket one draft");
    replaceEditor("New internal note", "Ticket one note");
    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-2"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(signals.get("work-1")?.aborted).toBe(true);
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("");
    replaceEditor("New internal comment", "Task two draft");
    pending.get("work-1")?.([
      source("late", "comment", "Late ticket response"),
    ]);
    pending.get("task-2")?.([]);
    await screen.findByText("No internal comments yet.");
    expect(screen.queryByText("Late ticket response")).not.toBeInTheDocument();

    view.rerender(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("Ticket one draft");
    expect(
      screen.getByRole("textbox", { name: "New internal note" }),
    ).toHaveTextContent("Ticket one note");
  });

  it("preserves a structured draft, refetches exact team snapshots, and requires explicit reconfirmation without retrying", async () => {
    const user = userEvent.setup();
    const api = apiWith();
    api.save = vi
      .fn()
      .mockRejectedValue(
        new MentionAPIError("mention_team_confirmation_stale", 409),
      );
    api.candidates = vi
      .fn()
      .mockResolvedValueOnce([
        {
          targetType: "team",
          id: "team-1",
          label: "NOC",
          eligibleCount: 2,
          excludedCount: 1,
          eligibleMemberIds: ["staff-2", "staff-3"],
          version: 2,
        },
      ])
      .mockResolvedValue([
        {
          targetType: "team",
          id: "team-1",
          label: "NOC",
          eligibleCount: 1,
          excludedCount: 2,
          eligibleMemberIds: ["staff-2"],
          version: 3,
        },
      ]);
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="task"
        parentId="task-1"
        authorId="staff-1"
        api={api}
      />,
    );
    await screen.findByText("No internal comments yet.");
    const editor = screen.getByRole("textbox", {
      name: "New internal comment",
    });
    await user.click(editor);
    await user.type(editor, "Ask @NO");
    await user.click(await screen.findByRole("option", { name: /NOC/ }));
    await user.click(
      screen.getByRole("button", { name: "Confirm NOC mention" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Add internal comment" }),
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("NOC");
    expect(alert).toHaveTextContent("Eligible 2 → 1");
    expect(alert).toHaveTextContent("Excluded 2");
    expect(
      screen.getByRole("textbox", { name: "New internal comment" }),
    ).toHaveTextContent("Ask @NOC");
    expect(api.candidates).toHaveBeenCalledWith(
      expect.objectContaining({
        parentType: "task",
        parentId: "task-1",
        sourceKind: "comment",
      }),
      "",
      expect.any(AbortSignal),
      { targetType: "team", targetId: "team-1" },
    );
    expect(api.save).toHaveBeenCalledTimes(1);
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm updated recipients" }),
    );
    expect(api.save).toHaveBeenCalledTimes(1);
    fireEvent.click(
      screen.getByRole("button", { name: "Add internal comment" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(2));
    expect(
      vi.mocked(api.save).mock.calls[1]?.[1].confirmedTeamSnapshots,
    ).toEqual({
      "team-1": { teamVersion: 3, eligibleMemberIds: ["staff-2"] },
    });
  });

  it("reloads a 409 server version, preserves the draft, and blocks save until an explicit resolution", async () => {
    const initial = source("details-1", "details", "Server v1");
    const current = source("details-1", "details", "Server v2", { version: 2 });
    const api = apiWith([initial]);
    vi.mocked(api.list)
      .mockResolvedValueOnce([initial])
      .mockResolvedValueOnce([current]);
    vi.mocked(api.save)
      .mockRejectedValueOnce(new MentionAPIError("version_conflict", 409))
      .mockResolvedValueOnce(
        source("details-1", "details", "My draft", { version: 3 }),
      );
    render(
      <InternalCollaborationPanel
        {...mutationPanelProps}
        clientId="client-1"
        parentType="work_record"
        parentId="work-1"
        authorId="staff-1"
        api={api}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: "Internal details" }),
      ).toHaveTextContent("Server v1"),
    );
    replaceEditor("Internal details", "My draft");
    fireEvent.click(
      screen.getByRole("button", { name: "Save internal details" }),
    );

    const conflict = await screen.findByRole("alert");
    expect(conflict).toHaveTextContent("Current server text: Server v2");
    expect(conflict).toHaveTextContent("Your draft: My draft");
    expect(
      screen.getByRole("textbox", { name: "Internal details" }),
    ).toHaveTextContent("My draft");
    expect(
      screen.getByRole("button", { name: "Save internal details" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Reapply my draft" }));
    expect(
      screen.getByRole("button", { name: "Save internal details" }),
    ).toBeEnabled();
    expect(api.save).toHaveBeenCalledTimes(1);
    fireEvent.click(
      screen.getByRole("button", { name: "Save internal details" }),
    );
    await waitFor(() => expect(api.save).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.save).mock.calls[1]?.[2].expectedVersion).toBe(2);
  });
});
