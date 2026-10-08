import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { SetupPage } from "./SetupPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  document.cookie = "rarity_csrf=; Max-Age=0; path=/";
  window.history.replaceState(null, "", "/#/setup");
});

it("completes setup without Entra and keeps the token outside the body", async () => {
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, _init?: RequestInit) =>
      Response.json({ completed: true }, { status: 201 }),
  );
  vi.stubGlobal("fetch", fetcher);
  window.history.replaceState(
    null,
    "",
    "/#/setup?bootstrap_token=bootstrap-secret",
  );
  render(<SetupPage />);

  const values: Record<string, string> = {
    "Installation MSP ID": "00000000-0000-4000-8000-000000000001",
    "Organization name": "Rarity MSP",
    "Organization display ID": "RARITY",
    "Administrator email": "admin@example.com",
    "Administrator display name": "Rarity Admin",
    "Local administrator username": "local-admin",
    "Local administrator password": "correct horse battery staple",
  };
  for (const [label, value] of Object.entries(values)) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
  }
  expect(window.location.hash).toBe("#/setup");
  expect(screen.getByText("Deployment authority accepted")).toBeInTheDocument();
  expect(document.body.textContent).not.toContain("bootstrap-secret");
  expect(document.body.textContent).not.toMatch(/JSON|env:\/\//);
  fireEvent.click(
    screen.getByRole("button", { name: "Complete secure setup" }),
  );

  expect(await screen.findByText("Setup complete")).toBeInTheDocument();
  const [, request] = fetcher.mock.calls[0];
  expect(request?.headers).toMatchObject({
    Authorization: "Bootstrap bootstrap-secret",
  });
  expect(String(request?.body)).not.toContain("bootstrap-secret");
  const body = JSON.parse(String(request?.body)) as Record<string, unknown>;
  expect(body).not.toHaveProperty("entra_tenant_id");
  expect(body).not.toHaveProperty("entra_client_secret");
});

it("requires the setup link from deployment output when authority is missing", () => {
  window.history.replaceState(null, "", "/#/setup");
  render(<SetupPage />);

  expect(
    screen.getByText("Open the setup link from deployment output"),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Complete secure setup" }),
  ).toBeDisabled();
  expect(
    screen.queryByLabelText("Single-use bootstrap token"),
  ).not.toBeInTheDocument();
});

it("reveals Entra fields only when configure now is selected", () => {
  render(<SetupPage />);
  expect(
    screen.queryByLabelText("Entra application client secret"),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByLabelText("Configure Microsoft Entra now"));
  expect(screen.getByLabelText("Entra tenant ID")).toBeRequired();
  expect(
    screen.getByLabelText("Entra application client secret"),
  ).toBeRequired();
});

it("has no detectable WCAG A or AA violations", async () => {
  const { container } = render(<SetupPage />);
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  await waitFor(() => expect(result.violations).toEqual([]));
});

it("loads truthful authenticated setup-center status", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({
      completed_at: "2026-07-30T15:00:00Z",
      configuration_version: 3,
      intake_status: {},
      object_storage_status: {},
      backup_status: { evidence_key_configured: false },
      sections: [
        {
          key: "backups",
          label: "Backup and PITR",
          state: "action_required",
          summary: "Configure pgBackRest and its verification authority.",
          remediation_href: "#/setup",
        },
      ],
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  render(<SetupPage installationComplete />);

  expect(await screen.findByText("Backup and PITR")).toBeInTheDocument();
  expect(
    screen.getByText("Configure pgBackRest and its verification authority."),
  ).toBeInTheDocument();
  expect(screen.getByText("Action required")).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/setup/center",
    expect.objectContaining({ credentials: "same-origin" }),
  );
});

it("guides backup setup without exposing the legacy reference editor", async () => {
  const missing = {
    completed_at: "2026-07-30T15:00:00Z",
    configuration_version: 3,
    intake: {},
    object_storage: {},
    backups: {},
    intake_status: {},
    object_storage_status: {},
    backup_status: { evidence_key_configured: false },
    sections: [
      {
        key: "backups",
        label: "Backup and PITR",
        state: "action_required",
        summary: "Configure pgBackRest and its verification authority.",
      },
    ],
  };
  const fetcher = vi.fn(async () => Response.json(missing));
  vi.stubGlobal("fetch", fetcher);
  render(<SetupPage installationComplete />);

  const configure = await screen.findByRole("button", {
    name: "Start Backup and PITR setup",
  });
  fireEvent.click(configure);
  expect(
    screen.getByRole("region", { name: "Backup and PITR setup" }),
  ).toBeInTheDocument();
  expect(document.body.textContent).not.toMatch(/JSON|env:\/\//);
  expect(fetcher).toHaveBeenCalledTimes(1);
});
