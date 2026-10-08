import { type FormEvent, useEffect, useRef, useState } from "react";

import {
  Button,
  Dialog,
  FormActions,
  Notice,
  Select,
  Textarea,
  TextInput,
} from "../../design-system";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import { isClassificationCreateError } from "../classification/api";
import { createWorkRecord, searchRarity, type SearchResult } from "./api";

export function GlobalWorkActions({
  authenticated,
  clientID,
  onWorkCreated,
}: {
  authenticated: boolean;
  clientID: string;
  onWorkCreated: () => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [searchState, setSearchState] = useState<
    "idle" | "loading" | "ready" | "error"
  >("idle");
  const [createOpen, setCreateOpen] = useState(false);
  const [createState, setCreateState] = useState<
    "idle" | "submitting" | "error"
  >("idle");
  const searchRequest = useRef(0);
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [classificationError, setClassificationError] = useState("");
  const classificationRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const trimmed = query.trim();
    if (!authenticated || !clientID || trimmed.length < 2) {
      setResults([]);
      setSearchState("idle");
      return;
    }
    const request = ++searchRequest.current;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setSearchState("loading");
      void searchRarity(clientID, trimmed, controller.signal)
        .then((found) => {
          if (request !== searchRequest.current) return;
          setResults(found);
          setSearchState("ready");
        })
        .catch(() => {
          if (!controller.signal.aborted && request === searchRequest.current) {
            setSearchState("error");
          }
        });
    }, 250);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [authenticated, clientID, query]);

  useEffect(() => {
    setTagIDs([]);
    setClassificationError("");
  }, [clientID]);

  async function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    if (!tagIDs.length) {
      setClassificationError(
        "Select at least one meaningful classification tag.",
      );
      classificationRef.current?.focus();
      return;
    }
    setClassificationError("");
    setCreateState("submitting");
    try {
      await createWorkRecord(clientID, {
        displayID: String(data.get("display_id") ?? ""),
        type: String(data.get("type") ?? ""),
        title: String(data.get("title") ?? ""),
        description: String(data.get("description") ?? ""),
        status: String(data.get("status") ?? ""),
        priority: String(data.get("priority") ?? ""),
        tagIDs,
      });
      form.reset();
      setTagIDs([]);
      setCreateState("idle");
      setCreateOpen(false);
      onWorkCreated();
    } catch (error) {
      if (isClassificationCreateError(error)) {
        setClassificationError(
          error instanceof Error && error.message === "tag_archived"
            ? "A selected tag is no longer active. Choose a current classification tag."
            : "Select at least one meaningful classification tag.",
        );
        classificationRef.current?.focus();
      }
      setCreateState("error");
    }
  }

  return (
    <>
      <div className="global-search">
        <label>
          <span className="sr-only">Search Rarity</span>
          <input
            type="search"
            role="combobox"
            value={query}
            disabled={!authenticated || !clientID}
            placeholder="Search work, clients, projects…"
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setQuery("");
                event.currentTarget.blur();
              }
            }}
            aria-controls="global-search-results"
            aria-autocomplete="list"
            aria-expanded={query.trim().length >= 2}
          />
        </label>
        {query.trim().length >= 2 ? (
          <div
            id="global-search-results"
            className="global-search-results"
            role="region"
            aria-live="polite"
          >
            {searchState === "loading" ? <p>Searching…</p> : null}
            {searchState === "error" ? <p>Search is unavailable.</p> : null}
            {searchState === "ready" && !results.length ? (
              <p>No authorized results found.</p>
            ) : null}
            {results.map((result) => (
              <a
                key={`${result.objectType}-${result.id}`}
                href={resultHref(result)}
                onClick={() => setQuery("")}
              >
                <span>{result.title}</span>
                <small>{result.objectType.replaceAll("_", " ")}</small>
                {result.snippet ? <p>{result.snippet}</p> : null}
              </a>
            ))}
          </div>
        ) : null}
      </div>
      <Button
        className="rti-create-button"
        intent="primary"
        disabled={!authenticated || !clientID}
        onClick={() => setCreateOpen(true)}
      >
        Create
      </Button>
      <Dialog
        open={createOpen}
        title="Create work"
        description="Create a record for the active Client."
        onClose={() => setCreateOpen(false)}
        dismissible={createState !== "submitting"}
        actions={
          <FormActions>
            <Button
              intent="tertiary"
              disabled={createState === "submitting"}
              onClick={() => setCreateOpen(false)}
            >
              Cancel
            </Button>
            <Button
              form="create-work-form"
              type="submit"
              intent="primary"
              loading={createState === "submitting"}
              loadingLabel="Creating work"
              disabled={createState === "submitting" || !tagIDs.length}
            >
              Create work
            </Button>
          </FormActions>
        }
      >
        <form
          id="create-work-form"
          className="work-create-dialog"
          onSubmit={(event) => void submitCreate(event)}
        >
          <label>
            <span>Display ID</span>
            <TextInput name="display_id" placeholder="INC-100" required />
          </label>
          <label>
            <span>Type</span>
            <Select name="type" defaultValue="incident">
              <option value="incident">Incident</option>
              <option value="request">Request</option>
              <option value="change">Change</option>
              <option value="problem">Problem</option>
            </Select>
          </label>
          <label className="work-create-wide">
            <span>Title</span>
            <TextInput name="title" required />
          </label>
          <label className="work-create-wide">
            <span>Description</span>
            <Textarea name="description" />
          </label>
          <RequiredClassificationPicker
            clientID={clientID}
            selectedIDs={tagIDs}
            error={classificationError}
            inputRef={classificationRef}
            onChange={(ids) => {
              setTagIDs(ids);
              setClassificationError("");
            }}
          />
          <label>
            <span>Initial status</span>
            <TextInput name="status" defaultValue="new" required />
          </label>
          <label>
            <span>Priority</span>
            <Select name="priority" defaultValue="normal">
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="urgent">Urgent</option>
            </Select>
          </label>
          {createState === "error" ? (
            <Notice tone="danger" title="Work could not be created" urgent>
              Verify the workflow values and your permissions, then try again.
            </Notice>
          ) : null}
        </form>
      </Dialog>
    </>
  );
}

function resultHref(result: SearchResult): string {
  if (result.objectType === "work_record") return "#/work";
  if (result.objectType === "opportunity") return "#/sales";
  if (result.objectType === "proposal") return "#/proposal";
  if (result.objectType === "project") return "#/project";
  return "#/work";
}
