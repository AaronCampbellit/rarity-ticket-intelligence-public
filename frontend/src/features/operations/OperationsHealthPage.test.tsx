import axe from "axe-core";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { OperationsHealthPage } from "./OperationsHealthPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("renders authoritative integration freshness and delivery failures", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        state: "degraded",
        generated_at: "2026-07-30T15:00:00Z",
        connections: [
          {
            id: "graph-connection-1",
            kind: "graph",
            state: "degraded",
            reason: "freshness_lag",
            last_error_code: "delta_delayed",
            freshness_lag_nanoseconds: 900_000_000_000,
            queue_delay_nanoseconds: 0,
            consecutive_failures: 1,
            pending_failures: 2,
          },
        ],
      }),
    ),
  );
  const { container } = render(<OperationsHealthPage />);

  expect(await screen.findByText("graph-connection-1")).toBeInTheDocument();
  expect(screen.getByText("freshness lag")).toBeInTheDocument();
  expect(screen.getByText("15m")).toBeInTheDocument();
  expect(screen.getByText("delta_delayed")).toBeInTheDocument();
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("shows an explicit empty state when no connections exist", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        state: "healthy",
        generated_at: "2026-07-30T15:00:00Z",
        connections: [],
      }),
    ),
  );
  render(<OperationsHealthPage />);
  expect(
    await screen.findByText("No integration connections"),
  ).toBeInTheDocument();
});
