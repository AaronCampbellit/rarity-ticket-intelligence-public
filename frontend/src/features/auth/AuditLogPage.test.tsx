import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { AuditLogPage } from "./AuditLogPage";

afterEach(() => vi.unstubAllGlobals());

it("renders append-only audit evidence from the authenticated scope", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json([
        {
          id: "audit-1",
          occurred_at: "2026-07-30T12:00:00Z",
          actor_type: "technician",
          actor_id: "admin-id",
          action: "role.capabilities_replaced",
          subject_type: "role",
          subject_id: "role-id",
          subject_version: 4,
          source: "api",
          reason: "Align service desk access",
          correlation_id: "correlation-id",
        },
      ]),
    ),
  );
  render(<AuditLogPage />);

  expect(
    await screen.findByText("role.capabilities_replaced"),
  ).toBeInTheDocument();
  expect(screen.getByText("Align service desk access")).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/admin/audit?limit=100",
    expect.objectContaining({ credentials: "same-origin" }),
  );
});
