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

import { BillingPage } from "./BillingPage";

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

it("reviews pending time with a reason and scoped CSRF mutation", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const entry = {
    id: "entry-1",
    work_record_id: "work-100",
    technician_id: "tech-1",
    duration_seconds: 5400,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T13:30:00Z",
    note: "Remediation",
  };
  let approved = false;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        approved = true;
        return Response.json({
          ...entry,
          approval_state: "approved",
          version: 3,
        });
      }
      return Response.json(approved ? [] : [entry]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve", "time_entry.export"])}
    />,
  );
  expect(await screen.findByText("work-100")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Reviewed against contract" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  await waitFor(() =>
    expect(screen.getByText("Time entry approved.")).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
    "X-Rarity-Client-ID": "client-1",
  });
  expect(fetcher.mock.calls[0]?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"reason":"Reviewed against contract"',
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

it("hides mutation controls when the technician only has queue visibility", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json([])),
  );
  render(<BillingPage clientID="client-1" capabilities={new Set()} />);
  expect(
    await screen.findByText("No time entries are awaiting approval."),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Download audited CSV" }),
  ).not.toBeInTheDocument();
});

it("routes approval and export classification failures to the blocking time entry", async () => {
  const entry = {
    id: "entry-1",
    work_record_id: "work-100",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "Remediation",
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
      if (url.includes("/objects/time_entry/"))
        return Response.json({
          target: { object_type: "time_entry", object_id: entry.id },
          object_version: 1,
          direct: [],
          inherited: [],
          effective: [],
          classification_state: "unclassified",
        });
      if (init?.method === "POST" && url.includes("/approval"))
        return Response.json(
          {
            error: {
              code: "classification_required",
              recovery_url: `/api/v1/objects/time_entry/${entry.id}/tags`,
            },
          },
          { status: 422 },
        );
      if (init?.method === "POST" && url === "/api/v1/billing-exports")
        return Response.json(
          {
            error: {
              code: "classification_required",
              recovery_url: `/api/v1/objects/time_entry/${entry.id}/tags`,
            },
          },
          { status: 422 },
        );
      return Response.json([entry]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve", "time_entry.export"])}
    />,
  );
  await screen.findByText("work-100");
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Reviewed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  await screen.findByText(/Classification is required before approval/);
  const editor = await screen.findByRole("heading", { name: "Classification" });
  expect(editor.closest("section")).toHaveFocus();
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.approve", "time_entry.export"])}
    />,
  );
  await waitFor(() =>
    expect(
      screen.queryByText(/Classification is required before approval/),
    ).not.toBeInTheDocument(),
  );
  expect(
    document.querySelector(".rti-object-tag-editor__disclosure"),
  ).not.toHaveAttribute("open");
  fireEvent.change(screen.getByLabelText("Start date"), {
    target: { value: "2026-07-01" },
  });
  fireEvent.change(screen.getByLabelText("End date"), {
    target: { value: "2026-08-01" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Download audited CSV" }));
  await screen.findByText(/Classification is required before export/);
  expect(
    (await screen.findByRole("heading", { name: "Classification" })).closest(
      "section",
    ),
  ).toHaveFocus();
});

it("ignores a late approval classification failure after the Client changes", async () => {
  const entry = {
    id: "entry-1",
    work_record_id: "work-100",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "Remediation",
  };
  let resolveApproval: (response: Response) => void = () => undefined;
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST" && url.includes("/approval"))
        return new Promise<Response>((resolve) => {
          resolveApproval = resolve;
        });
      return Response.json([entry]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await screen.findByText("work-100");
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Reviewed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  resolveApproval(
    Response.json(
      {
        error: {
          code: "classification_required",
          recovery_url: "/api/v1/objects/time_entry/entry-1/tags",
        },
      },
      { status: 422 },
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByText(/Classification is required before approval/),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("heading", { name: "Classification" }),
  ).not.toBeInTheDocument();
});

it("does not paint the previous Client's entries while the next Client loads", async () => {
  const entry = (id: string) => ({
    id,
    work_record_id: id,
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: id,
  });
  const clientB = deferred<Response>();
  const fetcher = vi.fn((_: RequestInfo | URL, init?: RequestInit) => {
    const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
    return client === "client-2"
      ? clientB.promise
      : Promise.resolve(Response.json([entry("work-a")]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await screen.findAllByText("work-a");

  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  expect(screen.queryAllByText("work-a")).toHaveLength(0);
  await act(async () => clientB.resolve(Response.json([entry("work-b")])));
  expect((await screen.findAllByText("work-b")).length).toBeGreaterThan(0);
});

it("ignores a late approval success after the Client changes", async () => {
  const entry = {
    id: "entry-a",
    work_record_id: "work-a",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "A",
  };
  const approval = deferred<Response>();
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "POST" && String(input).includes("/approval"))
      return approval.promise;
    const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
    return Promise.resolve(Response.json(client === "client-2" ? [] : [entry]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await screen.findAllByText("work-a");
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Reviewed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await act(async () =>
    approval.resolve(Response.json({ ...entry, approval_state: "approved" })),
  );
  await waitFor(() =>
    expect(screen.queryByText("Time entry approved.")).not.toBeInTheDocument(),
  );
  expect(screen.queryAllByText("work-a")).toHaveLength(0);
});

it("ignores a late export failure after the Client changes", async () => {
  const entry = {
    id: "entry-a",
    work_record_id: "work-a",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "A",
  };
  const exportRequest = deferred<Response>();
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "POST" && String(input) === "/api/v1/billing-exports")
      return exportRequest.promise;
    const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
    return Promise.resolve(Response.json(client === "client-2" ? [] : [entry]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.export"])}
    />,
  );
  await screen.findAllByText("work-a");
  fireEvent.change(screen.getByLabelText("Start date"), {
    target: { value: "2026-07-01" },
  });
  fireEvent.change(screen.getByLabelText("End date"), {
    target: { value: "2026-08-01" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Download audited CSV" }));
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.export"])}
    />,
  );
  await act(async () =>
    exportRequest.resolve(
      Response.json(
        { error: { code: "classification_required" } },
        { status: 422 },
      ),
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByText(/Classification is required before export/),
    ).not.toBeInTheDocument(),
  );
  expect(screen.queryAllByText("work-a")).toHaveLength(0);
});

it("ignores approval completion when its A-scoped refresh resolves after a Client change", async () => {
  const entry = {
    id: "entry-a",
    work_record_id: "work-a",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "A",
  };
  const refreshA = deferred<Response>();
  let clientALoads = 0;
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "POST" && String(input).includes("/approval"))
      return Promise.resolve(
        Response.json({ ...entry, approval_state: "approved" }),
      );
    const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
    if (client === "client-1" && ++clientALoads === 2) return refreshA.promise;
    return Promise.resolve(Response.json(client === "client-1" ? [entry] : []));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await screen.findAllByText("work-a");
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Reviewed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  await waitFor(() => expect(clientALoads).toBe(2));
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.approve"])}
    />,
  );
  await act(async () => refreshA.resolve(Response.json([])));
  await waitFor(() =>
    expect(screen.queryByText("Time entry approved.")).not.toBeInTheDocument(),
  );
  expect(screen.queryAllByText("work-a")).toHaveLength(0);
});

it("does not download or announce a late export blob after the Client changes", async () => {
  const entry = {
    id: "entry-a",
    work_record_id: "work-a",
    technician_id: "tech-1",
    duration_seconds: 600,
    billable: true,
    approval_state: "pending",
    version: 2,
    started_at: "2026-07-30T12:00:00Z",
    ended_at: "2026-07-30T12:10:00Z",
    note: "A",
  };
  const blob = deferred<Blob>();
  const createObjectURL = vi.fn(() => "blob:export");
  vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL: vi.fn() });
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (
      init?.method === "POST" &&
      String(input) === "/api/v1/billing-exports"
    ) {
      return Promise.resolve({
        ok: true,
        blob: () => blob.promise,
        headers: new Headers({ "X-Rarity-Export-ID": "export-a" }),
      } as unknown as Response);
    }
    const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
    return Promise.resolve(Response.json(client === "client-2" ? [] : [entry]));
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <BillingPage
      clientID="client-1"
      capabilities={new Set(["time_entry.export"])}
    />,
  );
  await screen.findAllByText("work-a");
  fireEvent.change(screen.getByLabelText("Start date"), {
    target: { value: "2026-07-01" },
  });
  fireEvent.change(screen.getByLabelText("End date"), {
    target: { value: "2026-08-01" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Download audited CSV" }));
  view.rerender(
    <BillingPage
      clientID="client-2"
      capabilities={new Set(["time_entry.export"])}
    />,
  );
  await act(async () => blob.resolve(new Blob(["entry-a"])));
  await waitFor(() =>
    expect(screen.queryByText(/Billing CSV exported/)).not.toBeInTheDocument(),
  );
  expect(createObjectURL).not.toHaveBeenCalled();
  expect(screen.queryAllByText("work-a")).toHaveLength(0);
});
