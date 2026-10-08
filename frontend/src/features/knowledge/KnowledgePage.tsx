import { CustomDateEditor } from "../calendar/CustomDateEditor";
import {
  type FormEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import {
  Notice,
  Page,
  SearchInput,
  StatePanel,
  StatusBadge,
} from "../../design-system";
import "../operations/operations.css";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import {
  createClassificationAPI,
  classificationResponseError,
  isClassificationCreateError,
} from "../classification/api";
import "./knowledge.css";

type Article = {
  id: string;
  display_id: string;
  title: string;
  state: "draft" | "published" | "archived";
  current_version: number;
  updated_at: string;
};

type ArticleDetail = {
  article: Article;
  version: {
    article_id: string;
    version: number;
    body: string;
    state: "draft" | "published";
    published_at?: string;
  };
};

export function KnowledgePage({
  clientID,
  capabilities,
  selectedArticleID,
}: {
  clientID: string;
  capabilities: Set<string>;
  selectedArticleID?: string;
}) {
  const [articles, setArticles] = useState<Article[]>([]);
  const [selected, setSelected] = useState<ArticleDetail | null>(null);
  const [query, setQuery] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const [tagIDs, setTagIDs] = useState<string[]>([]);
  const [classificationError, setClassificationError] = useState("");
  const [selectedEditorRecoveryRequest, setSelectedEditorRecoveryRequest] =
    useState(0);
  const [publishClassificationRecovery, setPublishClassificationRecovery] =
    useState("");
  const classificationRef = useRef<HTMLInputElement>(null);
  const mutationTargetRef = useRef("");
  mutationTargetRef.current = `${clientID}:${selected?.article.id ?? ""}`;
  const clientScopeRef = useRef(clientID);
  clientScopeRef.current = clientID;
  const canEdit = capabilities.has("knowledge.edit");
  const canPublish = capabilities.has("knowledge.publish");

  useEffect(() => {
    setTagIDs([]);
    setClassificationError("");
    setSelectedEditorRecoveryRequest(0);
    setPublishClassificationRecovery("");
  }, [clientID]);

  useEffect(() => {
    setSelectedEditorRecoveryRequest(0);
    setPublishClassificationRecovery("");
  }, [selected?.article.id]);

  const load = useCallback(
    async (search = "", signal?: AbortSignal) => {
      const parameters = new URLSearchParams({ limit: "100" });
      if (search.trim()) parameters.set("q", search.trim());
      const response = await fetch(
        `/api/v1/knowledge/articles?${parameters.toString()}`,
        {
          credentials: "same-origin",
          headers: clientContextHeaders(clientID),
          signal,
        },
      );
      if (!response.ok) throw new Error("Knowledge unavailable");
      const found = (await response.json()) as Article[];
      if (clientID !== clientScopeRef.current) return;
      setArticles(found);
      setState("ready");
    },
    [clientID],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load("", controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [load]);

  async function open(
    article: Pick<Article, "id">,
    expectedTarget = mutationTargetRef.current,
  ) {
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/knowledge/articles/${encodeURIComponent(article.id)}`,
        {
          credentials: "same-origin",
          headers: clientContextHeaders(clientID),
        },
      );
      if (!response.ok) throw new Error("Article unavailable");
      const detail = (await response.json()) as ArticleDetail;
      if (expectedTarget !== mutationTargetRef.current) return false;
      setSelected(detail);
      return true;
    } catch {
      setState("error");
    }
  }

  useEffect(() => {
    if (selectedArticleID && selected?.article.id !== selectedArticleID)
      void open({ id: selectedArticleID });
  }, [selectedArticleID, selected?.article.id]);

  async function mutate(
    url: string,
    body: Record<string, unknown>,
    success: string,
    recoveryTarget: "create" | "selected" = "create",
  ) {
    const mutationTarget =
      recoveryTarget === "create"
        ? clientScopeRef.current
        : mutationTargetRef.current;
    const mutationIsCurrent = () =>
      recoveryTarget === "create"
        ? mutationTarget === clientScopeRef.current
        : mutationTarget === mutationTargetRef.current;
    setState("saving");
    setMessage("");
    setPublishClassificationRecovery("");
    try {
      const response = await fetch(url, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...clientContextHeaders(clientID),
          ...csrfHeaders(),
        },
        body: JSON.stringify(body),
      });
      if (!mutationIsCurrent()) return false;
      if (!response.ok)
        await classificationResponseError(
          response,
          "knowledge_mutation_failed",
        );
      const result = (await response.json()) as
        ArticleDetail | ArticleDetail["version"];
      if (!mutationIsCurrent()) return false;
      if ("article" in result) {
        setSelected(result);
      } else if (selected) {
        if (!(await open(selected.article, mutationTarget))) return false;
      }
      await load(query);
      if (!mutationIsCurrent()) return false;
      setMessage(success);
      return true;
    } catch (cause) {
      if (!mutationIsCurrent()) return false;
      if (isClassificationCreateError(cause)) {
        if (recoveryTarget === "selected") {
          setPublishClassificationRecovery(
            "Classification is required before publishing. Update this article's classification and try again.",
          );
          setSelectedEditorRecoveryRequest((request) => request + 1);
        } else {
          setClassificationError(
            "Classification changed. Confirm at least one current classification tag.",
          );
          classificationRef.current?.focus();
        }
        setState("ready");
      } else {
        setState("error");
      }
      return false;
    }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!tagIDs.length) {
      setClassificationError(
        "Select at least one meaningful classification tag.",
      );
      classificationRef.current?.focus();
      return;
    }
    const form = new FormData(event.currentTarget);
    const created = await mutate(
      "/api/v1/knowledge/articles",
      {
        display_id: form.get("display_id"),
        title: form.get("title"),
        body: form.get("body"),
        tag_ids: tagIDs,
      },
      "Knowledge draft created.",
    );
    if (created) setTagIDs([]);
  }

  return (
    <Page
      eyebrow="Internal guidance"
      title="Knowledge"
      description="Find current internal guidance and maintain versioned drafts for the active Client."
    >
      <div className="operations-health knowledge-page">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading knowledge"
            description="Retrieving current guidance and drafts."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Knowledge is unavailable"
            description="Verify the active Client and your permissions."
            supportCode="KNOWLEDGE-UNAVAILABLE"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Knowledge updated">
            {message}
          </Notice>
        ) : null}
        {publishClassificationRecovery ? (
          <Notice tone="warning" title="Classification needs attention" urgent>
            {publishClassificationRecovery}
          </Notice>
        ) : null}
        {state !== "loading" ? (
          <div className="knowledge-layout">
            <section aria-labelledby="knowledge-library-heading">
              <h2 id="knowledge-library-heading">Article library</h2>
              <form
                className="knowledge-search"
                onSubmit={(event) => {
                  event.preventDefault();
                  setState("loading");
                  void load(query).catch(() => setState("error"));
                }}
                role="search"
              >
                <SearchInput
                  aria-label="Search by ID or title"
                  placeholder="Search articles…"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  maxLength={200}
                />
                <button type="submit">Search</button>
              </form>
              {!articles.length ? (
                <p>No internal knowledge articles match this client.</p>
              ) : (
                <ul className="knowledge-list">
                  {articles.map((article) => (
                    <li key={article.id}>
                      <button type="button" onClick={() => void open(article)}>
                        <span>{article.display_id}</span>
                        <strong>{article.title}</strong>
                        <span className="knowledge-list__metadata">
                          <StatusBadge
                            tone={
                              article.state === "published"
                                ? "success"
                                : article.state === "draft"
                                  ? "warning"
                                  : "neutral"
                            }
                          >
                            {article.state}
                          </StatusBadge>
                          <small>Version {article.current_version}</small>
                          <ObjectTagSummary
                            clientID={clientID}
                            target={{
                              objectType: "knowledge_article",
                              objectId: article.id,
                            }}
                          />
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <section aria-labelledby="knowledge-article-heading">
              <h2 id="knowledge-article-heading">
                {selected ? selected.article.title : "Article"}
              </h2>
              {selected ? (
                <>
                  <p className="record-id">
                    {selected.article.display_id} · {selected.article.state} ·
                    version {selected.article.current_version}
                  </p>
                  <article className="knowledge-body">
                    {selected.version.body
                      .split(/\n{2,}/)
                      .map((paragraph, index) => (
                        <p key={`${selected.article.id}-${index}`}>
                          {paragraph}
                        </p>
                      ))}
                  </article>
                  <DeferredObjectTagEditor
                    api={createClassificationAPI(globalThis.fetch, clientID)}
                    clientID={clientID}
                    target={{
                      objectType: "knowledge_article",
                      objectId: selected.article.id,
                    }}
                    recoveryRequest={selectedEditorRecoveryRequest}
                  />
                  <CustomDateEditor
                    objectType="knowledge_article"
                    objectID={selected.article.id}
                    clientID={clientID}
                  />
                  {canEdit ? (
                    <form
                      className="settings-card"
                      onSubmit={(event) => {
                        event.preventDefault();
                        const form = new FormData(event.currentTarget);
                        void mutate(
                          `/api/v1/knowledge/articles/${encodeURIComponent(
                            selected.article.id,
                          )}/versions`,
                          {
                            expected_version: selected.article.current_version,
                            title: form.get("title"),
                            body: form.get("body"),
                          },
                          "Knowledge draft revised.",
                        );
                      }}
                    >
                      <h3>Revise article</h3>
                      <label>
                        Title
                        <input
                          name="title"
                          defaultValue={selected.article.title}
                          required
                        />
                      </label>
                      <label>
                        Body
                        <textarea
                          name="body"
                          rows={12}
                          defaultValue={selected.version.body}
                          required
                        />
                      </label>
                      <button type="submit">Save new draft version</button>
                    </form>
                  ) : null}
                  {canPublish && selected.article.state === "draft" ? (
                    <form
                      className="settings-card"
                      onSubmit={(event) => {
                        event.preventDefault();
                        const form = new FormData(event.currentTarget);
                        void mutate(
                          `/api/v1/knowledge/articles/${encodeURIComponent(
                            selected.article.id,
                          )}/publish`,
                          {
                            expected_version: selected.article.current_version,
                            reason: form.get("reason"),
                          },
                          "Knowledge article published.",
                          "selected",
                        );
                      }}
                    >
                      <h3>Publish internal article</h3>
                      <label>
                        Publication reason
                        <input name="reason" required />
                      </label>
                      <button type="submit">Publish current version</button>
                    </form>
                  ) : null}
                </>
              ) : (
                <p>Select an article to read its current version.</p>
              )}
            </section>
          </div>
        ) : null}
        {canEdit ? (
          <section
            className="settings-card"
            aria-labelledby="new-article-heading"
          >
            <h2 id="new-article-heading">Create a draft</h2>
            <form onSubmit={create}>
              <label>
                Article ID
                <input name="display_id" placeholder="KB-100" required />
              </label>
              <label>
                Title
                <input name="title" required />
              </label>
              <label>
                Body
                <textarea name="body" rows={12} required />
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
              <button type="submit" disabled={!tagIDs.length}>
                Create draft
              </button>
            </form>
          </section>
        ) : null}
      </div>
    </Page>
  );
}
