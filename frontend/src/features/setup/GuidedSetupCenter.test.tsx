import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { GuidedSetupCenter } from "./GuidedSetupCenter";

afterEach(cleanup);

const center = {
  msp_id: "00000000-0000-4000-8000-000000000001",
  public_url: "https://rarity.example",
  completed_at: "2026-08-03T22:20:13Z",
  configuration_version: 4,
  intake_status: {
    graph_configured: false,
    api_key_configured: false,
    forwarding_configured: false,
  },
  object_storage_status: {
    provider: "minio",
    endpoint: "https://minio:9000",
    bucket: "rarity-attachments",
    region: "us-east-1",
    access_key_configured: true,
    credential_configured: true,
  },
  backup_status: { evidence_key_configured: false },
  sections: [
    {
      key: "intake",
      label: "Mailbox and API intake",
      state: "action_required" as const,
      summary: "Choose an intake path.",
    },
    {
      key: "object_storage",
      label: "Object storage",
      state: "ready_to_verify" as const,
      summary: "Ready for a live test.",
    },
    {
      key: "backups",
      label: "Backup and PITR",
      state: "action_required" as const,
      summary: "Configure backups.",
    },
  ],
};

describe("GuidedSetupCenter", () => {
  it("uses plain-English workflows without JSON or reference syntax", () => {
    render(<GuidedSetupCenter center={center} />);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Start Mailbox and API intake setup",
      }),
    );
    expect(
      screen.getByRole("region", { name: "Mailbox and API intake setup" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Set up a Microsoft 365 mailbox" }),
    ).toHaveAttribute("href", "#/graph-settings");
    expect(document.body.textContent).not.toMatch(
      /JSON|env:\/\/|secret:\/\/|vault:\/\//,
    );
  });

  it("explains effective storage settings and host-managed backups", () => {
    render(<GuidedSetupCenter center={center} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Continue Object storage setup" }),
    );
    expect(screen.getByText("rarity-attachments")).toBeInTheDocument();
    expect(screen.getByText(/S3_SECRET_ACCESS_KEY/)).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Close Object storage setup" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Start Backup and PITR setup" }),
    );
    expect(screen.getByText(/separate failure domain/i)).toBeInTheDocument();
    expect(
      screen.getByText(/latest backup is less than 24 hours old/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText((_, element) => element?.tagName === "CODE"),
    ).toHaveTextContent(
      "--api-url https://rarity.example --expected-version 4",
    );
  });

  it("runs the protected storage verification with an audit reason", async () => {
    document.cookie = "rarity_csrf=csrf-value; path=/";
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        Response.json(center),
    );
    vi.stubGlobal("fetch", fetcher);
    render(<GuidedSetupCenter center={center} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Continue Object storage setup" }),
    );
    fireEvent.change(screen.getByLabelText("Reason for storage test"), {
      target: { value: "Confirm attachment bucket" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Test object storage" }),
    );
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    expect(fetcher.mock.calls[0]?.[0]).toBe(
      "/api/v1/setup/center/object-storage/verify",
    );
    expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({
      expected_version: 4,
      reason: "Confirm attachment bucket",
    });
  });
});
