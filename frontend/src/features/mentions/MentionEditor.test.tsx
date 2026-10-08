import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";

import { findA11yViolations } from "../../design-system/testing/renderA11y";
import { MentionEditor } from "./MentionEditor";
import mentionStyles from "./mentions.css?raw";
import type {
  MentionAPI,
  MentionCandidate,
  MentionContext,
  MentionDocument,
} from "./types";

afterEach(cleanup);

const context: MentionContext = {
  clientId: "client-1",
  parentType: "work_record",
  parentId: "work-1",
  sourceKind: "comment",
};
const emptyDocument: MentionDocument = {
  body: "",
  tokens: [],
  confirmedTeamSnapshots: {},
};
const person = {
  targetType: "staff" as const,
  id: "staff-2",
  label: "Mira",
  version: 1,
};

function apiReturning(values: MentionCandidate[] = [person]): MentionAPI {
  return {
    candidates: vi.fn().mockResolvedValue(values),
    save: vi.fn(),
  };
}

function ControlledEditor({
  initial = emptyDocument,
  api = apiReturning(),
  validationError,
}: {
  initial?: MentionDocument;
  api?: MentionAPI;
  validationError?: string;
}) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <MentionEditor
        value={value}
        context={context}
        api={api}
        onChange={setValue}
        validationError={validationError}
      />
      <output data-testid="document">{JSON.stringify(value)}</output>
    </>
  );
}

function currentDocument(): MentionDocument {
  return JSON.parse(
    screen.getByTestId("document").textContent ?? "{}",
  ) as MentionDocument;
}

function selectEnd(element: HTMLElement) {
  element.focus();
  const range = document.createRange();
  range.selectNodeContents(element);
  range.collapse(false);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

function selectTextOffset(element: HTMLElement, offset: number) {
  element.focus();
  const text = element.firstChild;
  if (!text) throw new Error("editor has no text node");
  const range = document.createRange();
  range.setStart(text, offset);
  range.collapse(true);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

function selectNodeOffset(node: Node, offset: number, editor: HTMLElement) {
  editor.focus();
  const range = document.createRange();
  range.setStart(node, offset);
  range.collapse(true);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

function mentionElement(token: MentionDocument["tokens"][number]) {
  const element = document.createElement("span");
  element.contentEditable = "false";
  element.dataset.mentionTokenId = token.id;
  element.dataset.mentionTargetType = token.targetType;
  element.dataset.mentionTargetId = token.targetId;
  element.textContent = token.label;
  return element;
}

describe("MentionEditor", () => {
  it("inserts a structured person token from keyboard selection with a stable UUID", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
      "00000000-0000-4000-8000-000000000001",
    );
    render(<ControlledEditor />);
    const editor = screen.getByRole("textbox", { name: "Internal message" });
    await user.click(editor);
    await user.type(editor, "Ask @mi");
    const picker = await screen.findByRole("combobox", {
      name: "Mention a person or team",
    });
    await screen.findByRole("option", { name: /Mira/ });
    fireEvent.keyDown(picker, { key: "ArrowDown" });
    fireEvent.keyDown(picker, { key: "Enter" });

    await waitFor(() => expect(currentDocument().tokens).toHaveLength(1));
    expect(currentDocument()).toEqual({
      body: "Ask @Mira",
      tokens: [
        {
          id: "00000000-0000-4000-8000-000000000001",
          targetType: "staff",
          targetId: "staff-2",
          label: "@Mira",
          start: 4,
          end: 9,
        },
      ],
      confirmedTeamSnapshots: {},
    });
    expect(editor).toHaveFocus();
    expect(screen.getByDisplayValue("Ask @Mira")).toHaveAttribute(
      "type",
      "hidden",
    );
  });

  it("uses UTF-16 spans around emoji and combining marks and keeps ordered spans after DOM moves", () => {
    const firstLabel = "@Mira";
    const secondLabel = "@NOC";
    const initial: MentionDocument = {
      body: `🚀 e\u0301 ${firstLabel} then ${secondLabel}`,
      tokens: [
        {
          id: "token-1",
          targetType: "staff",
          targetId: "staff-2",
          label: firstLabel,
          start: 6,
          end: 11,
        },
        {
          id: "token-2",
          targetType: "team",
          targetId: "team-1",
          label: secondLabel,
          start: 17,
          end: 21,
        },
      ],
      confirmedTeamSnapshots: {
        "team-1": { teamVersion: 2, eligibleMemberIds: ["staff-3"] },
      },
    };
    render(<ControlledEditor initial={initial} />);
    const editor = screen.getByRole("textbox");
    const chips = editor.querySelectorAll<HTMLElement>(
      "[data-mention-token-id]",
    );
    expect(chips).toHaveLength(2);
    editor.insertBefore(chips[1], editor.firstChild);
    fireEvent.input(editor);

    expect(currentDocument()).toEqual({
      body: "@NOC🚀 é @Mira then ",
      tokens: [
        {
          id: "token-2",
          targetType: "team",
          targetId: "team-1",
          label: "@NOC",
          start: 0,
          end: 4,
        },
        {
          id: "token-1",
          targetType: "staff",
          targetId: "staff-2",
          label: "@Mira",
          start: 10,
          end: 15,
        },
      ],
      confirmedTeamSnapshots: {
        "team-1": { teamVersion: 2, eligibleMemberIds: ["staff-3"] },
      },
    });
  });

  it("treats pasted @text and forged token markup as plain text", () => {
    render(<ControlledEditor />);
    const editor = screen.getByRole("textbox");
    selectEnd(editor);
    fireEvent.paste(editor, {
      clipboardData: {
        getData: (kind: string) =>
          kind === "text/plain"
            ? "@Jane"
            : '<span data-mention-token-id="stolen">@Admin</span>',
      },
    });
    expect(currentDocument()).toEqual({
      body: "@Jane",
      tokens: [],
      confirmedTeamSnapshots: {},
    });
  });

  it("deletes removed chips and never lets a retained token ID switch targets", () => {
    const initial: MentionDocument = {
      body: "Ask @Mira now",
      tokens: [
        {
          id: "token-1",
          targetType: "staff",
          targetId: "staff-2",
          label: "@Mira",
          start: 4,
          end: 9,
        },
      ],
      confirmedTeamSnapshots: {},
    };
    const view = render(<ControlledEditor initial={initial} />);
    const editor = screen.getByRole("textbox");
    const chip = editor.querySelector<HTMLElement>(
      "[data-mention-token-id='token-1']",
    )!;
    chip.dataset.mentionTargetType = "team";
    chip.dataset.mentionTargetId = "team-admins";
    fireEvent.input(editor);
    expect(currentDocument().tokens[0]).toMatchObject({
      targetType: "staff",
      targetId: "staff-2",
    });

    editor.querySelector("[data-mention-token-id='token-1']")?.remove();
    fireEvent.input(editor);
    expect(currentDocument()).toEqual({
      body: "Ask  now",
      tokens: [],
      confirmedTeamSnapshots: {},
    });
    expect(view.container.textContent).not.toContain("team-admins");
  });

  it("updates retained token labels, body, and later spans from fresh candidate data", async () => {
    const user = userEvent.setup();
    const initial: MentionDocument = {
      body: "@Mira ask @NOC",
      tokens: [
        {
          id: "token-1",
          targetType: "staff",
          targetId: "staff-2",
          label: "@Mira",
          start: 0,
          end: 5,
        },
        {
          id: "token-2",
          targetType: "team",
          targetId: "team-1",
          label: "@NOC",
          start: 10,
          end: 14,
        },
      ],
      confirmedTeamSnapshots: {},
    };
    render(
      <ControlledEditor
        initial={initial}
        api={apiReturning([{ ...person, label: "Mira Patel" }])}
      />,
    );
    const editor = screen.getByRole("textbox");
    selectEnd(editor);
    await user.type(editor, " @mi", { skipClick: true });
    await screen.findByRole("option", { name: /Mira Patel/ });
    await waitFor(() =>
      expect(currentDocument().tokens[0]).toMatchObject({
        label: "@Mira Patel",
        start: 0,
        end: 11,
      }),
    );
    expect(currentDocument().body).toBe("@Mira Patel ask @NOC @mi");
    expect(currentDocument().tokens[1]).toMatchObject({ start: 16, end: 20 });

    await user.click(screen.getByRole("option", { name: /Mira Patel/ }));
    expect(currentDocument().body).toBe("@Mira Patel ask @NOC @Mira Patel");
    expect(currentDocument().tokens).toEqual([
      expect.objectContaining({ id: "token-1", start: 0, end: 11 }),
      expect.objectContaining({ id: "token-2", start: 16, end: 20 }),
      expect.objectContaining({
        targetId: "staff-2",
        label: "@Mira Patel",
        start: 21,
        end: 32,
      }),
    ]);
  });

  it("keeps the caret in the middle of existing text and opens mentions at that caret", async () => {
    const user = userEvent.setup();
    render(
      <ControlledEditor
        initial={{
          body: "Hello world",
          tokens: [],
          confirmedTeamSnapshots: {},
        }}
      />,
    );
    const editor = screen.getByRole("textbox");
    selectTextOffset(editor, 6);
    await user.type(editor, "brave ", { skipClick: true });
    expect(currentDocument().body).toBe("Hello brave world");

    selectTextOffset(editor, 11);
    await user.type(editor, " @mi", { skipClick: true });
    expect(await screen.findByRole("option", { name: /Mira/ })).toBeVisible();
    screen.getByRole("combobox", { name: "Mention a person or team" }).focus();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(currentDocument().body).toBe("Hello brave @Mira world");
  });

  it("counts BR line breaks and UTF-16 text when opening and inserting a mention", async () => {
    const user = userEvent.setup();
    render(<ControlledEditor />);
    const editor = screen.getByRole("textbox");
    const prefix = document.createTextNode("😀");
    const query = document.createTextNode("@mi");
    editor.replaceChildren(prefix, document.createElement("br"), query);
    selectNodeOffset(query, query.length, editor);
    fireEvent.input(editor);

    expect(currentDocument().body).toBe("😀\n@mi");
    await user.click(await screen.findByRole("option", { name: /Mira/ }));
    expect(currentDocument()).toEqual({
      body: "😀\n@Mira",
      tokens: [
        expect.objectContaining({
          targetId: "staff-2",
          start: 3,
          end: 8,
        }),
      ],
      confirmedTeamSnapshots: {},
    });
  });

  it("keeps block line breaks when a mention query is before a retained token", async () => {
    const user = userEvent.setup();
    const retained = {
      id: "token-noc",
      targetType: "team" as const,
      targetId: "team-1",
      label: "@NOC",
      start: 4,
      end: 8,
    };
    render(
      <ControlledEditor
        initial={{
          body: "@mi\n@NOC",
          tokens: [retained],
          confirmedTeamSnapshots: {},
        }}
      />,
    );
    const editor = screen.getByRole("textbox");
    const query = document.createTextNode("@mi");
    const tokenBlock = document.createElement("div");
    tokenBlock.append(mentionElement(retained));
    editor.replaceChildren(query, tokenBlock);
    selectNodeOffset(query, query.length, editor);
    fireEvent.input(editor);

    await user.click(await screen.findByRole("option", { name: /Mira/ }));
    expect(currentDocument().body).toBe("@Mira\n@NOC");
    expect(currentDocument().tokens).toEqual([
      expect.objectContaining({ targetId: "staff-2", start: 0, end: 5 }),
      expect.objectContaining({ targetId: "team-1", start: 6, end: 10 }),
    ]);
  });

  it("keeps block line breaks when a mention query is after a retained token", async () => {
    const user = userEvent.setup();
    const retained = {
      id: "token-noc",
      targetType: "team" as const,
      targetId: "team-1",
      label: "@NOC",
      start: 0,
      end: 4,
    };
    render(
      <ControlledEditor
        initial={{
          body: "@NOC\n@mi",
          tokens: [retained],
          confirmedTeamSnapshots: {},
        }}
      />,
    );
    const editor = screen.getByRole("textbox");
    const tokenBlock = document.createElement("div");
    tokenBlock.append(mentionElement(retained));
    const queryBlock = document.createElement("div");
    const query = document.createTextNode("@mi");
    queryBlock.append(query);
    editor.replaceChildren(tokenBlock, queryBlock);
    selectNodeOffset(query, query.length, editor);
    fireEvent.input(editor);

    await user.click(await screen.findByRole("option", { name: /Mira/ }));
    expect(currentDocument().body).toBe("@NOC\n@Mira");
    expect(currentDocument().tokens).toEqual([
      expect.objectContaining({ targetId: "team-1", start: 0, end: 4 }),
      expect.objectContaining({ targetId: "staff-2", start: 5, end: 10 }),
    ]);
  });

  it("waits for IME composition to finish before reconciling the controlled document", () => {
    render(<ControlledEditor />);
    const editor = screen.getByRole("textbox");
    fireEvent.compositionStart(editor);
    editor.textContent = "調";
    fireEvent.input(editor);
    expect(currentDocument().body).toBe("");
    editor.textContent = "調査";
    fireEvent.input(editor);
    fireEvent.compositionEnd(editor);
    expect(currentDocument().body).toBe("調査");
  });

  it("dismisses the picker on outside interaction and restores editor focus", async () => {
    const user = userEvent.setup();
    render(
      <>
        <ControlledEditor />
        <button type="button">Outside action</button>
      </>,
    );
    const editor = screen.getByRole("textbox");
    await user.click(editor);
    await user.type(editor, "@mi");
    expect(
      await screen.findByRole("combobox", { name: "Mention a person or team" }),
    ).toBeVisible();
    const focus = vi.spyOn(editor, "focus");
    await user.click(screen.getByRole("button", { name: "Outside action" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("combobox", { name: "Mention a person or team" }),
      ).not.toBeInTheDocument(),
    );
    expect(editor).toHaveFocus();
    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("dismisses partial-team confirmation with Escape and restores editor focus exactly once", async () => {
    const user = userEvent.setup();
    const partial = {
      targetType: "team" as const,
      id: "team-partial",
      label: "Field Team",
      eligibleCount: 1,
      excludedCount: 1,
      eligibleMemberIds: ["staff-2"],
      version: 2,
    };
    render(<ControlledEditor api={apiReturning([partial])} />);
    const editor = screen.getByRole("textbox");
    await user.click(editor);
    await user.type(editor, "@fi");
    await user.click(await screen.findByRole("option", { name: /Field Team/ }));
    const cancel = screen.getByRole("button", { name: "Cancel" });
    cancel.focus();
    const focus = vi.spyOn(editor, "focus");
    await user.keyboard("{Escape}");

    await waitFor(() =>
      expect(screen.queryByLabelText("Mention suggestions")).toBeNull(),
    );
    expect(editor).toHaveFocus();
    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("shows a visible and accessible placeholder without serializing it", async () => {
    const placeholder = "Describe the internal update";
    render(
      <MentionEditor
        value={emptyDocument}
        context={context}
        api={apiReturning()}
        onChange={() => {}}
        placeholder={placeholder}
      />,
    );
    const editor = screen.getByRole("textbox");
    expect(editor).toHaveAttribute("aria-placeholder", placeholder);
    expect(editor).toHaveAttribute("data-placeholder", placeholder);
    expect(editor).toBeEmptyDOMElement();
    expect(screen.getByDisplayValue("")).toHaveAttribute("type", "hidden");
    expect(mentionStyles.replace(/\s+/gu, "")).toContain(
      ".rti-mention-editor__surface:empty::before{content:attr(data-placeholder)",
    );
    expect(await findA11yViolations(editor.parentElement!)).toEqual([]);
  });

  it("preserves the full document when server validation is shown and enforces the 100-token limit", async () => {
    const tokens = Array.from({ length: 100 }, (_, index) => ({
      id: `token-${index}`,
      targetType: "staff" as const,
      targetId: `staff-${index}`,
      label: `@P${index}`,
      start: 0,
      end: 0,
    }));
    let body = "";
    for (const token of tokens) {
      token.start = body.length;
      body += token.label;
      token.end = body.length;
      body += " ";
    }
    const initial = { body, tokens, confirmedTeamSnapshots: {} };
    const view = render(
      <ControlledEditor
        initial={initial}
        validationError="Mention token is stale. Review and submit again."
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Mention token is stale",
    );
    expect(currentDocument()).toEqual(initial);
    expect(screen.getByRole("textbox")).toHaveAttribute("aria-invalid", "true");
    expect(
      view.container.querySelectorAll("[data-mention-token-id]"),
    ).toHaveLength(100);
    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});
