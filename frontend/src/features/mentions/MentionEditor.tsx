import {
  useCallback,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type FormEvent,
} from "react";

import { Popover } from "../../design-system";
import { createMentionAPI } from "./api";
import { MentionPicker } from "./MentionPicker";
import type {
  MentionAPI,
  MentionCandidate,
  MentionContext,
  MentionDocument,
  MentionToken,
  TeamConfirmation,
} from "./types";
import "./mentions.css";

const maximumTokens = 100;
const defaultMentionAPI = createMentionAPI();

type MentionQuery = {
  start: number;
  end: number;
  query: string;
};

export type MentionEditorProps = {
  value: MentionDocument;
  context: MentionContext;
  onChange(value: MentionDocument): void;
  api?: MentionAPI;
  label?: string;
  name?: string;
  placeholder?: string;
  validationError?: string;
  disabled?: boolean;
};

function copyConfirmations(values: MentionDocument["confirmedTeamSnapshots"]) {
  return Object.fromEntries(
    Object.entries(values).map(([teamID, confirmation]) => [
      teamID,
      {
        teamVersion: confirmation.teamVersion,
        eligibleMemberIds: [...confirmation.eligibleMemberIds],
      },
    ]),
  );
}

function retainUsedConfirmations(
  confirmations: MentionDocument["confirmedTeamSnapshots"],
  tokens: MentionToken[],
) {
  const teamIDs = new Set(
    tokens
      .filter((token) => token.targetType === "team")
      .map((token) => token.targetId),
  );
  return Object.fromEntries(
    Object.entries(confirmations)
      .filter(([teamID]) => teamIDs.has(teamID))
      .map(([teamID, confirmation]) => [
        teamID,
        {
          teamVersion: confirmation.teamVersion,
          eligibleMemberIds: [...confirmation.eligibleMemberIds],
        },
      ]),
  );
}

function parseEditorDOM(
  root: HTMLElement,
  current: MentionDocument,
): MentionDocument {
  const known = new Map(current.tokens.map((token) => [token.id, token]));
  const retained = new Set<string>();
  const tokens: MentionToken[] = [];
  let body = "";

  function appendText(text: string) {
    body += text.replace(/\r\n?/gu, "\n");
  }

  function visit(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) {
      appendText(node.nodeValue ?? "");
      return;
    }
    if (!(node instanceof HTMLElement)) return;
    if (node.tagName === "BR") {
      appendText("\n");
      return;
    }
    const tokenID = node.dataset.mentionTokenId;
    if (tokenID) {
      const token = known.get(tokenID);
      if (!token || retained.has(tokenID)) {
        appendText(node.textContent ?? "");
        return;
      }
      retained.add(tokenID);
      const start = body.length;
      appendText(token.label);
      tokens.push({ ...token, start, end: body.length });
      return;
    }
    const block = node.tagName === "DIV" || node.tagName === "P";
    if (block && body.length > 0 && !body.endsWith("\n")) appendText("\n");
    node.childNodes.forEach(visit);
  }

  root.childNodes.forEach(visit);
  return {
    body,
    tokens,
    confirmedTeamSnapshots: retainUsedConfirmations(
      current.confirmedTeamSnapshots,
      tokens,
    ),
  };
}

function mentionQueryAtOffset(
  document: MentionDocument,
  offset: number,
): MentionQuery | undefined {
  if (offset < 0 || offset > document.body.length) return undefined;
  const match = /(?:^|[\s([{])@([^@\s]{0,64})$/u.exec(
    document.body.slice(0, offset),
  );
  if (!match) return undefined;
  const start = offset - match[1].length - 1;
  if (
    document.tokens.some((token) => start >= token.start && start < token.end)
  ) {
    return undefined;
  }
  return { start, end: offset, query: match[1] };
}

function tokenKey(targetType: string, targetID: string) {
  return `${targetType}\u0000${targetID}`;
}

export function synchronizeMentionLabels(
  document: MentionDocument,
  candidates: MentionCandidate[],
): MentionDocument {
  const labels = new Map(
    candidates.map((candidate) => [
      tokenKey(candidate.targetType, candidate.id),
      `@${candidate.label}`,
    ]),
  );
  let body = "";
  let cursor = 0;
  let changed = false;
  const tokens = [...document.tokens]
    .sort((left, right) => left.start - right.start)
    .map((token) => {
      body += document.body.slice(cursor, token.start);
      const label =
        labels.get(tokenKey(token.targetType, token.targetId)) ?? token.label;
      const start = body.length;
      body += label;
      cursor = token.end;
      if (label !== token.label) changed = true;
      return { ...token, label, start, end: body.length };
    });
  body += document.body.slice(cursor);
  if (!changed) return document;
  return {
    body,
    tokens,
    confirmedTeamSnapshots: copyConfirmations(document.confirmedTeamSnapshots),
  };
}

function caretAfterOffset(root: HTMLElement, offset: number) {
  const selection = window.getSelection();
  if (!selection) return;
  const range = document.createRange();
  let traversed = 0;

  function locate(node: Node): boolean {
    if (node.nodeType === Node.TEXT_NODE) {
      const length = node.nodeValue?.length ?? 0;
      if (offset <= traversed + length) {
        range.setStart(node, Math.max(0, offset - traversed));
        return true;
      }
      traversed += length;
      return false;
    }
    if (!(node instanceof HTMLElement)) return false;
    if (node.dataset.mentionTokenId) {
      const length = node.textContent?.length ?? 0;
      if (offset <= traversed + length) {
        range.setStartAfter(node);
        return true;
      }
      traversed += length;
      return false;
    }
    for (const child of node.childNodes) {
      if (locate(child)) return true;
    }
    return false;
  }

  if (!locate(root)) range.selectNodeContents(root);
  range.collapse(false);
  selection.removeAllRanges();
  selection.addRange(range);
}

function currentSelectionOffset(root: HTMLElement): number | undefined {
  const selection = window.getSelection();
  if (!selection?.rangeCount) return undefined;
  const selected = selection.getRangeAt(0);
  if (!root.contains(selected.endContainer)) return undefined;
  const before = document.createRange();
  before.selectNodeContents(root);
  before.setEnd(selected.endContainer, selected.endOffset);
  const wrapper = document.createElement("div");
  wrapper.append(before.cloneContents());
  let serialized = "";

  function appendText(text: string) {
    serialized += text.replace(/\r\n?/gu, "\n");
  }

  function visit(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) {
      appendText(node.nodeValue ?? "");
      return;
    }
    if (!(node instanceof HTMLElement)) return;
    if (node.tagName === "BR") {
      appendText("\n");
      return;
    }
    if (node.dataset.mentionTokenId) {
      appendText(node.textContent ?? "");
      return;
    }
    const block = node.tagName === "DIV" || node.tagName === "P";
    if (block && serialized.length > 0 && !serialized.endsWith("\n")) {
      appendText("\n");
    }
    node.childNodes.forEach(visit);
  }

  wrapper.childNodes.forEach(visit);
  return serialized.length;
}

function renderEditorDOM(root: HTMLElement, value: MentionDocument) {
  const fragment = document.createDocumentFragment();
  let cursor = 0;
  for (const token of [...value.tokens].sort(
    (left, right) => left.start - right.start,
  )) {
    fragment.append(
      document.createTextNode(value.body.slice(cursor, token.start)),
    );
    const chip = document.createElement("span");
    chip.className = "rti-mention-editor__token";
    chip.contentEditable = "false";
    chip.dataset.mentionTokenId = token.id;
    chip.dataset.mentionTargetType = token.targetType;
    chip.dataset.mentionTargetId = token.targetId;
    chip.textContent = token.label;
    fragment.append(chip);
    cursor = token.end;
  }
  const trailingText = value.body.slice(cursor);
  if (trailingText) fragment.append(document.createTextNode(trailingText));
  root.replaceChildren(fragment);
}

export function MentionEditor({
  value,
  context,
  onChange,
  api = defaultMentionAPI,
  label = "Internal message",
  name = "body",
  placeholder = "Write an internal message. Type @ to mention someone.",
  validationError,
  disabled = false,
}: MentionEditorProps) {
  const editorID = useId();
  const errorID = useId();
  const editorRef = useRef<HTMLDivElement>(null);
  const pendingCaret = useRef<number | undefined>(undefined);
  const composing = useRef(false);
  const valueRef = useRef(value);
  valueRef.current = value;
  const [mentionQuery, setMentionQuery] = useState<MentionQuery>();
  const [localError, setLocalError] = useState("");

  useLayoutEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    renderEditorDOM(editor, value);
    if (pendingCaret.current !== undefined) {
      editor.focus();
      caretAfterOffset(editor, pendingCaret.current);
      pendingCaret.current = undefined;
    }
  }, [value]);

  const updateFromDOM = useCallback(() => {
    const root = editorRef.current;
    if (!root) return;
    const caret = currentSelectionOffset(root);
    const next = parseEditorDOM(root, valueRef.current);
    valueRef.current = next;
    pendingCaret.current = caret ?? next.body.length;
    onChange(next);
    setMentionQuery(mentionQueryAtOffset(next, caret ?? next.body.length));
    setLocalError("");
  }, [onChange]);

  const handleCandidates = useCallback(
    (candidates: MentionCandidate[]) => {
      const current = valueRef.current;
      const next = synchronizeMentionLabels(current, candidates);
      if (next === current) return;
      if (mentionQuery) {
        const nextByID = new Map(next.tokens.map((token) => [token.id, token]));
        let shift = 0;
        for (const token of current.tokens) {
          if (token.end > mentionQuery.start) continue;
          const updated = nextByID.get(token.id);
          if (updated) shift += updated.label.length - token.label.length;
        }
        setMentionQuery(mentionQueryAtOffset(next, mentionQuery.end + shift));
      }
      valueRef.current = next;
      onChange(next);
    },
    [mentionQuery, onChange],
  );

  function insertCandidate(
    candidate: MentionCandidate,
    confirmation: TeamConfirmation | undefined,
  ) {
    const query = mentionQuery;
    if (!query) return;
    const current = valueRef.current;
    if (current.tokens.length >= maximumTokens) {
      setLocalError("A message can contain at most 100 mentions.");
      return;
    }
    const labelValue = `@${candidate.label}`;
    const delta = labelValue.length - (query.end - query.start);
    const inserted: MentionToken = {
      id: crypto.randomUUID(),
      targetType: candidate.targetType,
      targetId: candidate.id,
      label: labelValue,
      start: query.start,
      end: query.start + labelValue.length,
    };
    const shifted = current.tokens.map((token) =>
      token.start >= query.end
        ? {
            ...token,
            start: token.start + delta,
            end: token.end + delta,
          }
        : token,
    );
    const tokens = [...shifted, inserted].sort(
      (left, right) => left.start - right.start,
    );
    const confirmations = copyConfirmations(current.confirmedTeamSnapshots);
    if (candidate.targetType === "team") {
      if (confirmation) confirmations[candidate.id] = confirmation;
      else delete confirmations[candidate.id];
    }
    const next: MentionDocument = {
      body:
        current.body.slice(0, query.start) +
        labelValue +
        current.body.slice(query.end),
      tokens,
      confirmedTeamSnapshots: confirmations,
    };
    valueRef.current = next;
    pendingCaret.current = inserted.end;
    onChange(next);
    setMentionQuery(undefined);
    setLocalError("");
  }

  function pastePlainText(event: ClipboardEvent<HTMLDivElement>) {
    event.preventDefault();
    const text = event.clipboardData.getData("text/plain");
    const selection = window.getSelection();
    if (!selection?.rangeCount || !editorRef.current) return;
    const range = selection.getRangeAt(0);
    if (!editorRef.current.contains(range.commonAncestorContainer)) return;
    range.deleteContents();
    const node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
    updateFromDOM();
  }

  const error = validationError || localError;
  return (
    <div className="rti-mention-editor">
      <label className="rti-field__label" htmlFor={editorID}>
        {label}
      </label>
      <Popover
        open={Boolean(mentionQuery)}
        onOpenChange={(open) => {
          if (!open) setMentionQuery(undefined);
        }}
        toggleOnTrigger={false}
        attachTriggerAria={false}
        align="start"
        label="Mention suggestions"
        trigger={
          <div
            id={editorID}
            ref={editorRef}
            className="rti-mention-editor__surface"
            role="textbox"
            aria-label={label}
            aria-placeholder={placeholder}
            aria-multiline="true"
            aria-invalid={error ? "true" : undefined}
            aria-describedby={error ? errorID : undefined}
            contentEditable={!disabled}
            data-placeholder={value.body ? undefined : placeholder}
            suppressContentEditableWarning
            onInput={() => {
              if (!composing.current) updateFromDOM();
            }}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
              updateFromDOM();
            }}
            onPaste={pastePlainText}
          />
        }
      >
        {mentionQuery ? (
          <div className="rti-mention-editor__picker">
            <MentionPicker
              context={context}
              api={api}
              initialQuery={mentionQuery.query}
              onCandidates={handleCandidates}
              onSelect={insertCandidate}
              onDismiss={() => {
                setMentionQuery(undefined);
                editorRef.current?.focus();
              }}
            />
          </div>
        ) : null}
      </Popover>
      <input type="hidden" name={name} value={value.body} readOnly />
      {error ? (
        <p id={errorID} className="rti-field__error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
