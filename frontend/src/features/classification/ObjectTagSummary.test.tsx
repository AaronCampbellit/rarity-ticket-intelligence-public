import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ObjectTagSummary } from "./ObjectTagSummary";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
}

function object(label: string, id: string) {
  return Response.json({
    target: { object_type: "task", object_id: id },
    object_version: 1,
    direct: [],
    inherited: [],
    effective: [
      {
        id: `${id}-tag`,
        tag: {
          id: `${id}-tag`,
          label,
          group_id: "technology",
          state: "active",
          synonyms: [],
          version: 1,
        },
        source: "human",
        inherited: false,
      },
    ],
    classification_state: "classified",
  });
}

describe("ObjectTagSummary", () => {
  it("never paints loaded A tags after changing either target or active Client", async () => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise),
    );

    const view = render(
      <ObjectTagSummary
        clientID="client-a"
        target={{ objectType: "task", objectId: "a" }}
      />,
    );
    first.resolve(object("A tag", "a"));
    expect(await screen.findByText("A tag")).toBeVisible();

    view.rerender(
      <ObjectTagSummary
        clientID="client-a"
        target={{ objectType: "task", objectId: "b" }}
      />,
    );
    expect(screen.queryByText("A tag")).not.toBeInTheDocument();

    second.resolve(object("B tag", "b"));
    expect(await screen.findByText("B tag")).toBeVisible();
  });

  it("does not reuse a same-target summary after the Client changes", async () => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise),
    );
    const view = render(
      <ObjectTagSummary
        clientID="client-a"
        target={{ objectType: "task", objectId: "same" }}
      />,
    );
    first.resolve(object("Client A tag", "same"));
    expect(await screen.findByText("Client A tag")).toBeVisible();
    view.rerender(
      <ObjectTagSummary
        clientID="client-b"
        target={{ objectType: "task", objectId: "same" }}
      />,
    );
    expect(screen.queryByText("Client A tag")).not.toBeInTheDocument();
    second.resolve(object("Client B tag", "same"));
    expect(await screen.findByText("Client B tag")).toBeVisible();
  });
});
