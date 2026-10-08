import axe from "axe-core";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { KnowledgePage } from "./KnowledgePage";
import { __resetClientClassificationCatalogForTests } from "../classification/useClientCatalog";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  __resetClientClassificationCatalogForTests();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
}

it("discovers, reads, revises, and publishes internal knowledge", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  let article = {
    id: "article-id",
    display_id: "KB-100",
    title: "Reset a token",
    state: "published" as "published" | "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  let body = "Published steps";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST" && url.endsWith("/versions")) {
        article = {
          ...article,
          title: "Reset an access token",
          state: "draft",
          current_version: 2,
        };
        body = "Revised steps";
        return Response.json({
          article,
          version: {
            article_id: article.id,
            version: 2,
            body,
            state: "draft",
          },
        });
      }
      if (init?.method === "POST" && url.endsWith("/publish")) {
        article = { ...article, state: "published" };
        return Response.json({
          article_id: article.id,
          version: 2,
          body,
          state: "published",
        });
      }
      if (url.includes("/knowledge/articles/article-id")) {
        return Response.json({
          article,
          version: {
            article_id: article.id,
            version: article.current_version,
            body,
            state: article.state,
          },
        });
      }
      return Response.json([article]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <KnowledgePage
      clientID="client-1"
      capabilities={
        new Set(["knowledge.read", "knowledge.edit", "knowledge.publish"])
      }
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Reset a token/ }));
  expect(
    (await screen.findAllByText("Published steps")).length,
  ).toBeGreaterThan(0);
  fireEvent.change(screen.getAllByLabelText("Title")[0], {
    target: { value: "Reset an access token" },
  });
  fireEvent.change(screen.getAllByLabelText("Body")[0], {
    target: { value: "Revised steps" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Save new draft version" }),
  );
  expect((await screen.findAllByText("Revised steps")).length).toBeGreaterThan(
    0,
  );
  const publishReason = screen.getByLabelText("Publication reason");
  fireEvent.click(
    screen.getByRole("button", { name: "Publish current version" }),
  );
  expect(
    fetcher.mock.calls.filter(([, init]) => init?.method === "POST"),
  ).toHaveLength(1);
  fireEvent.change(publishReason, {
    target: { value: "Reviewed for internal use" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Publish current version" }),
  );
  await waitFor(() =>
    expect(
      screen.getByText("Knowledge article published."),
    ).toBeInTheDocument(),
  );
  const posts = fetcher.mock.calls.filter(
    ([, init]) => init?.method === "POST",
  );
  expect(posts).toHaveLength(2);

  expect(posts[1]?.[1]?.body).toBe(
    JSON.stringify({
      expected_version: 2,
      reason: "Reviewed for internal use",
    }),
  );
  expect(posts[0]?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
    "X-Rarity-Client-ID": "client-1",
  });
  const knowledgeListRequest = fetcher.mock.calls.find(([input]) =>
    String(input).startsWith("/api/v1/knowledge/articles?"),
  );
  expect(knowledgeListRequest?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
  });
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("keeps authoring controls hidden for read-only technicians", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json([])),
  );
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read"])}
    />,
  );
  expect(
    await screen.findByText(
      "No internal knowledge articles match this client.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Create a draft" }),
  ).not.toBeInTheDocument();
});

it("direct-loads a selected article outside the first result page", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/knowledge/articles/off-page"))
      return Response.json({
        article: {
          id: "off-page",
          display_id: "KB-999",
          title: "Archived procedure",
          state: "published",
          current_version: 1,
          updated_at: "2026-08-01T00:00:00Z",
        },
        version: {
          article_id: "off-page",
          version: 1,
          body: "Exact body",
          state: "published",
        },
      });
    return Response.json([]);
  });
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read"])}
      selectedArticleID="off-page"
    />,
  );
  expect(await screen.findByText("Exact body")).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/knowledge/articles/off-page",
    expect.objectContaining({
      headers: expect.objectContaining({ "X-Rarity-Client-ID": "client-1" }),
    }),
  );
});

it("retains a draft and focuses its picker when the selected tag becomes stale", async () => {
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/tag-groups")
        return Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            version: 1,
          },
        ]);
      if (url === "/api/v1/tags")
        return Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      if (init?.method === "POST" && url === "/api/v1/knowledge/articles") {
        return Response.json(
          { error: { code: "tag_archived", message: "Network was archived" } },
          { status: 422 },
        );
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.edit"])}
    />,
  );
  const picker = await screen.findByRole("combobox", {
    name: /Classification tags/,
  });
  fireEvent.focus(picker);
  fireEvent.click(await screen.findByRole("option", { name: "Network" }));
  fireEvent.change(screen.getByLabelText("Article ID"), {
    target: { value: "KB-999" },
  });
  fireEvent.change(screen.getAllByLabelText("Title").at(-1)!, {
    target: { value: "Stale tag draft" },
  });
  fireEvent.change(screen.getAllByLabelText("Body").at(-1)!, {
    target: { value: "Keep this body" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create draft" }));
  await screen.findByText(
    "Classification changed. Confirm at least one current classification tag.",
  );
  expect(document.activeElement).toBe(picker);
  expect(screen.getByLabelText("Article ID")).toHaveValue("KB-999");
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toHaveTextContent("Network");
});

it("opens the selected article classification after publish requires tags", async () => {
  const article = {
    id: "article-id",
    display_id: "KB-100",
    title: "Reset a token",
    state: "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/tag-groups")
        return Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            version: 1,
          },
        ]);
      if (url === "/api/v1/tags")
        return Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      if (url.includes("/tags/history")) return Response.json([]);
      if (url.includes("/objects/knowledge_article/"))
        return Response.json({
          target: { object_type: "knowledge_article", object_id: article.id },
          object_version: 1,
          direct: [],
          inherited: [],
          effective: [],
          classification_state: "unclassified",
        });
      if (init?.method === "POST" && url.endsWith("/publish"))
        return Response.json(
          {
            error: {
              code: "classification_required",
              recovery_url: `/api/v1/objects/knowledge_article/${article.id}/tags`,
            },
          },
          { status: 422 },
        );
      if (url.includes(`/knowledge/articles/${article.id}`))
        return Response.json({
          article,
          version: {
            article_id: article.id,
            version: 1,
            body: "Draft steps",
            state: "draft",
          },
        });
      return Response.json([article]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Reset a token/ }));
  fireEvent.change(await screen.findByLabelText("Publication reason"), {
    target: { value: "Ready for internal use" },
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Publish current version" }),
  );
  const recovery = await screen.findByText(
    /Classification is required before publishing/,
  );
  expect(recovery.closest("[role=alert]")).toHaveAttribute(
    "data-tone",
    "warning",
  );
  expect(screen.queryByText("Knowledge updated")).not.toBeInTheDocument();
  const editor = await screen.findByRole("heading", { name: "Classification" });
  expect(editor.closest("section")).toHaveFocus();
  expect(
    screen.getByRole("button", { name: "Publish current version" }),
  ).toBeInTheDocument();
  view.rerender(
    <KnowledgePage
      clientID="client-2"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  await waitFor(() =>
    expect(
      screen.queryByText(/Classification is required before publishing/),
    ).not.toBeInTheDocument(),
  );
  expect(
    document.querySelector(".rti-object-tag-editor__disclosure"),
  ).not.toHaveAttribute("open");
});

it("ignores a late publish classification failure after selecting another article", async () => {
  const first = {
    id: "article-a",
    display_id: "KB-100",
    title: "Article A",
    state: "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  const second = {
    ...first,
    id: "article-b",
    display_id: "KB-101",
    title: "Article B",
  };
  let resolvePublish: (response: Response) => void = () => undefined;
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST" && url.endsWith("/publish"))
        return new Promise<Response>((resolve) => {
          resolvePublish = resolve;
        });
      if (url.includes("article-a"))
        return Response.json({
          article: first,
          version: {
            article_id: first.id,
            version: 1,
            body: "A",
            state: "draft",
          },
        });
      if (url.includes("article-b"))
        return Response.json({
          article: second,
          version: {
            article_id: second.id,
            version: 1,
            body: "B",
            state: "draft",
          },
        });
      return Response.json([first, second]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Article A/ }));
  fireEvent.change(await screen.findByLabelText("Publication reason"), {
    target: { value: "Ready for internal use" },
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Publish current version" }),
  );
  fireEvent.click(screen.getByRole("button", { name: /Article B/ }));
  await screen.findByRole("heading", { name: "Article B" });
  resolvePublish(
    Response.json(
      { error: { code: "classification_required" } },
      { status: 422 },
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByText(/Classification is required before publishing/),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("heading", { name: "Classification" }),
  ).not.toBeInTheDocument();
});

it("ignores a late publish success after selecting another article", async () => {
  const first = {
    id: "article-a",
    display_id: "KB-100",
    title: "Article A",
    state: "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  const second = {
    ...first,
    id: "article-b",
    display_id: "KB-101",
    title: "Article B",
  };
  const publish = deferred<Response>();
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (init?.method === "POST" && url.endsWith("/publish"))
      return publish.promise;
    if (url.includes("article-a"))
      return Promise.resolve(
        Response.json({
          article: first,
          version: {
            article_id: first.id,
            version: 1,
            body: "A",
            state: "draft",
          },
        }),
      );
    if (url.includes("article-b"))
      return Promise.resolve(
        Response.json({
          article: second,
          version: {
            article_id: second.id,
            version: 1,
            body: "B",
            state: "draft",
          },
        }),
      );
    return Promise.resolve(Response.json([first, second]));
  });
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Article A/ }));
  fireEvent.change(await screen.findByLabelText("Publication reason"), {
    target: { value: "Ready for internal use" },
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Publish current version" }),
  );
  fireEvent.click(screen.getByRole("button", { name: /Article B/ }));
  await screen.findByRole("heading", { name: "Article B" });
  await act(async () =>
    publish.resolve(
      Response.json({
        article_id: first.id,
        version: 1,
        body: "A",
        state: "published",
      }),
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByText("Knowledge article published."),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("heading", { name: "Article B" }),
  ).toBeInTheDocument();
});

it("ignores a selected article publish whose response body settles after another article opens", async () => {
  const first = {
    id: "article-a",
    display_id: "KB-100",
    title: "Article A",
    state: "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  const second = {
    ...first,
    id: "article-b",
    display_id: "KB-101",
    title: "Article B",
  };
  const body = deferred<{
    article_id: string;
    version: number;
    body: string;
    state: "published";
  }>();
  let publishCalls = 0;
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (init?.method === "POST" && url.endsWith("/publish")) {
      publishCalls += 1;
      return Promise.resolve({
        ok: true,
        json: () => body.promise,
      } as unknown as Response);
    }
    if (url.includes("article-a"))
      return Promise.resolve(
        Response.json({
          article: first,
          version: {
            article_id: first.id,
            version: 1,
            body: "A",
            state: "draft",
          },
        }),
      );
    if (url.includes("article-b"))
      return Promise.resolve(
        Response.json({
          article: second,
          version: {
            article_id: second.id,
            version: 1,
            body: "B",
            state: "draft",
          },
        }),
      );
    return Promise.resolve(Response.json([first, second]));
  });
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Article A/ }));
  fireEvent.change(await screen.findByLabelText("Publication reason"), {
    target: { value: "Ready for internal use" },
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Publish current version" }),
  );
  fireEvent.click(screen.getByRole("button", { name: /Article B/ }));
  await screen.findByRole("heading", { name: "Article B" });
  await act(async () =>
    body.resolve({
      article_id: first.id,
      version: 1,
      body: "A",
      state: "published",
    }),
  );
  await waitFor(() =>
    expect(
      screen.queryByText("Knowledge article published."),
    ).not.toBeInTheDocument(),
  );
  expect(publishCalls).toBe(1);
  expect(
    screen.getByRole("heading", { name: "Article B" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Classification" }),
  ).not.toBeInTheDocument();
  expect(
    document.querySelector(".rti-object-tag-editor__disclosure"),
  ).not.toHaveAttribute("open");
});

it("ignores a selected article publish refresh that settles after the Client changes", async () => {
  const first = {
    id: "article-a",
    display_id: "KB-100",
    title: "Article A",
    state: "draft",
    current_version: 1,
    updated_at: "2026-07-30T12:00:00Z",
  };
  const second = {
    ...first,
    id: "article-b",
    display_id: "KB-101",
    title: "Article B",
  };
  const refreshA = deferred<Response>();
  let publishCalls = 0;
  let clientALists = 0;
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (init?.method === "POST" && url.endsWith("/publish")) {
      publishCalls += 1;
      return Promise.resolve(
        Response.json({
          article_id: first.id,
          version: 1,
          body: "A",
          state: "published",
        }),
      );
    }
    if (url.startsWith("/api/v1/knowledge/articles?")) {
      const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
      if (client === "client-1" && ++clientALists === 2)
        return refreshA.promise;
      return Promise.resolve(Response.json([first, second]));
    }
    if (url.includes("article-a"))
      return Promise.resolve(
        Response.json({
          article: first,
          version: {
            article_id: first.id,
            version: 1,
            body: "A",
            state: "draft",
          },
        }),
      );
    if (url.includes("article-b"))
      return Promise.resolve(
        Response.json({
          article: second,
          version: {
            article_id: second.id,
            version: 1,
            body: "B",
            state: "draft",
          },
        }),
      );
    return Promise.resolve(Response.json([]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /Article A/ }));
  fireEvent.change(await screen.findByLabelText("Publication reason"), {
    target: { value: "Ready for internal use" },
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Publish current version" }),
  );
  await waitFor(() => expect(clientALists).toBe(2));
  view.rerender(
    <KnowledgePage
      clientID="client-2"
      capabilities={new Set(["knowledge.read", "knowledge.publish"])}
    />,
  );
  await act(async () => refreshA.resolve(Response.json([first, second])));
  await waitFor(() =>
    expect(
      screen.queryByText("Knowledge article published."),
    ).not.toBeInTheDocument(),
  );
  expect(publishCalls).toBe(1);
  expect(
    screen.queryByRole("heading", { name: "Classification" }),
  ).not.toBeInTheDocument();
  expect(
    document.querySelector(".rti-object-tag-editor__disclosure"),
  ).not.toHaveAttribute("open");
});

it("keeps a completed create mutation current after it selects the new article", async () => {
  const created = {
    id: "article-new",
    display_id: "KB-999",
    title: "New article",
    state: "draft" as const,
    current_version: 1,
    updated_at: "2026-08-05T12:00:00Z",
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/tag-groups")
        return Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            version: 1,
          },
        ]);
      if (url === "/api/v1/tags")
        return Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      if (init?.method === "POST" && url === "/api/v1/knowledge/articles")
        return Response.json({
          article: created,
          version: {
            article_id: created.id,
            version: 1,
            body: "Body",
            state: "draft",
          },
        });
      return Response.json([created]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.edit"])}
    />,
  );
  const picker = await screen.findByRole("combobox", {
    name: /Classification tags/,
  });
  fireEvent.focus(picker);
  fireEvent.click(await screen.findByRole("option", { name: "Network" }));
  fireEvent.change(screen.getByLabelText("Article ID"), {
    target: { value: created.display_id },
  });
  fireEvent.change(screen.getAllByLabelText("Title").at(-1)!, {
    target: { value: created.title },
  });
  fireEvent.change(screen.getAllByLabelText("Body").at(-1)!, {
    target: { value: "Body" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create draft" }));
  await screen.findByText("Knowledge draft created.");
  expect(
    screen.getByRole("heading", { name: created.title }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toBeEmptyDOMElement();
});

it("does not complete a create after its delayed body and A-scoped refresh outlive the Client", async () => {
  const created = {
    id: "article-new",
    display_id: "KB-999",
    title: "New article",
    state: "draft" as const,
    current_version: 1,
    updated_at: "2026-08-05T12:00:00Z",
  };
  const body = deferred<{
    article: typeof created;
    version: {
      article_id: string;
      version: number;
      body: string;
      state: "draft";
    };
  }>();
  const refreshA = deferred<Response>();
  let createCalls = 0;
  let clientALists = 0;
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/v1/tag-groups")
      return Promise.resolve(
        Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            version: 1,
          },
        ]),
      );
    if (url === "/api/v1/tags")
      return Promise.resolve(
        Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]),
      );
    if (init?.method === "POST" && url === "/api/v1/knowledge/articles") {
      createCalls += 1;
      return Promise.resolve({
        ok: true,
        json: () => body.promise,
      } as unknown as Response);
    }
    if (url.startsWith("/api/v1/knowledge/articles?")) {
      const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
      if (client === "client-1" && ++clientALists === 2)
        return refreshA.promise;
      return Promise.resolve(Response.json([]));
    }
    return Promise.resolve(Response.json([]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <KnowledgePage
      clientID="client-1"
      capabilities={new Set(["knowledge.read", "knowledge.edit"])}
    />,
  );
  const picker = await screen.findByRole("combobox", {
    name: /Classification tags/,
  });
  fireEvent.focus(picker);
  fireEvent.click(await screen.findByRole("option", { name: "Network" }));
  fireEvent.change(screen.getByLabelText("Article ID"), {
    target: { value: created.display_id },
  });
  fireEvent.change(screen.getAllByLabelText("Title").at(-1)!, {
    target: { value: created.title },
  });
  fireEvent.change(screen.getAllByLabelText("Body").at(-1)!, {
    target: { value: "Body" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create draft" }));
  await waitFor(() => expect(createCalls).toBe(1));
  await act(async () =>
    body.resolve({
      article: created,
      version: {
        article_id: created.id,
        version: 1,
        body: "Body",
        state: "draft",
      },
    }),
  );
  await waitFor(() => expect(clientALists).toBe(2));
  view.rerender(
    <KnowledgePage
      clientID="client-2"
      capabilities={new Set(["knowledge.read", "knowledge.edit"])}
    />,
  );
  await act(async () => refreshA.resolve(Response.json([])));
  await waitFor(() =>
    expect(
      screen.queryByText("Knowledge draft created."),
    ).not.toBeInTheDocument(),
  );
  expect(createCalls).toBe(1);
});
