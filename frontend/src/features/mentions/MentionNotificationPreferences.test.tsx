import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import axe from "axe-core";
import { afterEach, expect, it, vi } from "vitest";

import { MentionNotificationPreferences } from "./MentionNotificationPreferences";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("shows organization availability and saves the current technician preference", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PATCH")
        return Response.json({
          technician_id: "tech",
          event_type: "mention.occurred",
          email_enabled: false,
          teams_enabled: false,
          email_available: true,
          teams_available: false,
          time_zone: "America/New_York",
          quiet_start: "22:00",
          quiet_end: "06:00",
          version: 4,
        });
      return Response.json({
        technician_id: "tech",
        event_type: "mention.occurred",
        email_enabled: true,
        teams_enabled: false,
        email_available: true,
        teams_available: false,
        time_zone: "UTC",
        quiet_start: "",
        quiet_end: "",
        version: 3,
      });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(<MentionNotificationPreferences />);
  fireEvent.click(await screen.findByText("Mention notification preferences"));
  expect(
    screen.getByRole("checkbox", { name: "Microsoft Teams" }),
  ).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: "Email" }));
  fireEvent.change(screen.getByLabelText("Time zone"), {
    target: { value: "America/New_York" },
  });
  fireEvent.change(screen.getByLabelText("Quiet hours start"), {
    target: { value: "22:00" },
  });
  fireEvent.change(screen.getByLabelText("Quiet hours end"), {
    target: { value: "06:00" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Save mention preferences" }),
  );
  await waitFor(() =>
    expect(screen.getByText("Mention preferences saved.")).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "PATCH",
  );
  expect(mutation?.[0]).toBe("/api/v1/notification-preferences/mentions");
  expect(JSON.parse(String(mutation?.[1]?.body))).toMatchObject({
    email_enabled: false,
    teams_enabled: false,
    time_zone: "America/New_York",
    quiet_start: "22:00",
    quiet_end: "06:00",
    expected_version: 3,
  });
  expect(
    (
      await axe.run(container, {
        rules: { "color-contrast": { enabled: false } },
      })
    ).violations,
  ).toEqual([]);
});

it("preserves the draft and requires an explicit choice after a version conflict", async () => {
  const initial = {
    technician_id: "tech",
    event_type: "mention.occurred",
    email_enabled: true,
    teams_enabled: false,
    email_available: true,
    teams_available: true,
    time_zone: "UTC",
    quiet_start: "",
    quiet_end: "",
    version: 3,
  };
  const current = {
    ...initial,
    email_enabled: false,
    teams_enabled: true,
    time_zone: "America/Chicago",
    version: 4,
  };
  let getCalls = 0;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PATCH") {
        const patchCalls = fetcher.mock.calls.filter(
          ([, candidate]) => candidate?.method === "PATCH",
        ).length;
        return patchCalls === 1
          ? Response.json({ code: "version_conflict" }, { status: 409 })
          : Response.json({
              ...initial,
              email_enabled: true,
              time_zone: "America/New_York",
              version: 5,
            });
      }
      getCalls += 1;
      return Response.json(getCalls === 1 ? initial : current);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<MentionNotificationPreferences />);
  fireEvent.click(await screen.findByText("Mention notification preferences"));
  fireEvent.change(screen.getByLabelText("Time zone"), {
    target: { value: "America/New_York" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Save mention preferences" }),
  );

  const conflict = await screen.findByRole("alert");
  expect(conflict).toHaveTextContent("settings changed on the server");
  expect(conflict).toHaveTextContent("Current: America/Chicago");
  expect(conflict).toHaveTextContent("Your draft: America/New_York");
  expect(screen.getByLabelText("Time zone")).toHaveValue("America/New_York");
  expect(
    screen.getByRole("button", { name: "Save mention preferences" }),
  ).toBeDisabled();
  expect(
    fetcher.mock.calls.filter(([, init]) => init?.method === "PATCH"),
  ).toHaveLength(1);

  fireEvent.click(screen.getByRole("button", { name: "Reapply my draft" }));
  expect(
    screen.getByRole("button", { name: "Save mention preferences" }),
  ).toBeEnabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Save mention preferences" }),
  );
  await waitFor(() =>
    expect(
      fetcher.mock.calls.filter(([, init]) => init?.method === "PATCH"),
    ).toHaveLength(2),
  );
  const lastMutation = fetcher.mock.calls
    .filter(([, init]) => init?.method === "PATCH")
    .at(-1);
  expect(JSON.parse(String(lastMutation?.[1]?.body))).toMatchObject({
    time_zone: "America/New_York",
    expected_version: 4,
  });
});

it("can discard a conflicted draft and load the current server settings", async () => {
  const initial = {
    technician_id: "tech",
    event_type: "mention.occurred",
    email_enabled: true,
    teams_enabled: false,
    email_available: true,
    teams_available: true,
    time_zone: "UTC",
    quiet_start: "",
    quiet_end: "",
    version: 3,
  };
  const current = { ...initial, time_zone: "America/Denver", version: 4 };
  let gets = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PATCH") return Response.json({}, { status: 409 });
      gets += 1;
      return Response.json(gets === 1 ? initial : current);
    }),
  );
  render(<MentionNotificationPreferences />);
  fireEvent.click(await screen.findByText("Mention notification preferences"));
  fireEvent.change(screen.getByLabelText("Time zone"), {
    target: { value: "America/Los_Angeles" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Save mention preferences" }),
  );
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Load server settings" }));
  expect(screen.getByLabelText("Time zone")).toHaveValue("America/Denver");
});
