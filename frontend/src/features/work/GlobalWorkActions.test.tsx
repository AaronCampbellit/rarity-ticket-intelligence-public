import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { GlobalWorkActions } from "./GlobalWorkActions";

describe("GlobalWorkActions", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("searches within the active client and renders authorized results", async () => {
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        Response.json([
          {
            ID: "work-1",
            ObjectType: "work_record",
            ClientID: "client-1",
            Title: "Email unavailable",
            Snippet: "Multiple users affected",
          },
        ]),
    );
    vi.stubGlobal("fetch", fetcher);

    render(
      <GlobalWorkActions
        authenticated
        clientID="client-1"
        onWorkCreated={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Search Rarity" }), {
      target: { value: "email" },
    });

    expect(await screen.findByText("Email unavailable")).toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/search?q=email&limit=25",
      expect.objectContaining({
        headers: expect.objectContaining({ "X-Rarity-Client-ID": "client-1" }),
      }),
    );
  });

  it("creates work for the active client with CSRF protection", async () => {
    document.cookie = "rarity_csrf=test-token";
    const created = vi.fn();
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        if (String(input).includes("/api/v1/tag-groups")) {
          return Response.json([
            {
              id: "group-1",
              label: "Technology",
              description: "",
              position: 1,
              state: "active",
              version: 1,
            },
          ]);
        }
        if (String(input).includes("/api/v1/tags")) {
          return Response.json([
            {
              id: "tag-vpn",
              label: "VPN",
              group_id: "group-1",
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]);
        }
        return Response.json(
          {
            ID: "work-1",
            DisplayID: "INC-100",
            Type: "incident",
            Title: "Email unavailable",
            Description: "",
            Status: "new",
            Priority: "high",
            QueueID: "",
            PrimaryOwnerID: "",
            UpdatedAt: "2026-07-30T12:00:00Z",
            Version: 1,
          },
          { status: 201 },
        );
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(
      <GlobalWorkActions
        authenticated
        clientID="client-1"
        onWorkCreated={created}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    const createButton = screen.getByRole("button", { name: "Create work" });
    expect(createButton).toBeDisabled();
    fireEvent.focus(
      await screen.findByRole("combobox", { name: /Classification tags/ }),
    );
    fireEvent.click(screen.getByRole("option", { name: /VPN/ }));
    fireEvent.change(screen.getByLabelText("Display ID"), {
      target: { value: "INC-100" },
    });
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Email unavailable" },
    });
    fireEvent.change(screen.getByLabelText("Priority"), {
      target: { value: "high" },
    });
    fireEvent.click(createButton);

    await waitFor(() => expect(created).toHaveBeenCalled());
    const [, init] = fetcher.mock.calls.find(([url]) =>
      String(url).includes("/api/v1/work-records"),
    )!;
    expect(init).toEqual(
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          "X-Rarity-Client-ID": "client-1",
          "X-Rarity-CSRF": "test-token",
        }),
      }),
    );
    expect(String(init?.body)).toContain('"display_id":"INC-100"');
    expect(String(init?.body)).toContain('"tag_ids":["tag-vpn"]');
  });

  it("retains ticket form values and tags for an archived selection", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/tag-groups")) {
        return Response.json([
          {
            id: "group-1",
            label: "Technology",
            description: "",
            position: 1,
            state: "active",
            version: 1,
          },
        ]);
      }
      if (url.includes("/api/v1/tags")) {
        return Response.json([
          {
            id: "tag-vpn",
            label: "VPN",
            group_id: "group-1",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      }
      return Response.json(
        { error: { code: "tag_archived" } },
        { status: 422 },
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(
      <GlobalWorkActions
        authenticated
        clientID="client-focus"
        onWorkCreated={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    const picker = await screen.findByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(picker);
    fireEvent.click(screen.getByRole("option", { name: /VPN/ }));
    fireEvent.change(screen.getByLabelText("Display ID"), {
      target: { value: "INC-101" },
    });
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "VPN unavailable" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create work" }));
    expect(
      await screen.findByText(
        "A selected tag is no longer active. Choose a current classification tag.",
      ),
    ).toBeInTheDocument();
    expect(document.activeElement).toBe(picker);
    expect(screen.getByLabelText("Display ID")).toHaveValue("INC-101");
    expect(screen.getByLabelText("Title")).toHaveValue("VPN unavailable");
    expect(
      screen.getByRole("group", { name: /Classification tags selections/ }),
    ).toHaveTextContent("VPN");
  });
});
