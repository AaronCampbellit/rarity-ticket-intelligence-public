import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { DattoReconciliationPage } from "./DattoReconciliationPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("renders evidence and records a reasoned scoped decision", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const candidate = {
    id: "candidate-1",
    snapshot_id: "snapshot-1",
    client_id: "client-1",
    asset_id: "asset-1",
    evidence: {
      hostname: "ACME-DC01",
      serial_number: "1234",
      confidence: 0.87,
    },
    state: "pending",
  };
  let decided = false;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        decided = true;
        return Response.json({
          candidate_id: "candidate-1",
          decision: "choose_datto",
        });
      }
      return Response.json({ items: decided ? [] : [candidate] });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(<DattoReconciliationPage clientID="client-1" />);

  expect(
    await screen.findByRole("heading", { name: "Asset asset-1" }),
  ).toBeInTheDocument();
  expect(screen.getByText("ACME-DC01")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Decision"), {
    target: { value: "choose_datto" },
  });
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Datto serial verified" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Record decision" }));
  expect(
    await screen.findByText("Reconciliation decision recorded."),
  ).toBeInTheDocument();

  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain('"decision":"choose_datto"');
  expect(String(mutation?.[1]?.body)).toContain(
    '"reason":"Datto serial verified"',
  );
  await waitFor(() =>
    expect(screen.getByText("No candidates need review")).toBeInTheDocument(),
  );
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("shows a truthful empty state", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ items: [] })),
  );
  render(<DattoReconciliationPage clientID="client-1" />);
  expect(
    await screen.findByText("No candidates need review"),
  ).toBeInTheDocument();
});
