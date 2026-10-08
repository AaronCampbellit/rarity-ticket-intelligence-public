import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import axe from "axe-core";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ClassificationInsightsPage } from "./ClassificationInsightsPage";
import type {
  ClassificationReport,
  ClassificationReportFilter,
  ClassificationReportKind,
} from "./types";

afterEach(cleanup);

describe("ClassificationInsightsPage", () => {
  it("loads every report lens with preserved filters and record evidence", async () => {
    const report = vi.fn(
      async (
        kind: ClassificationReportKind,
        _filter: ClassificationReportFilter,
      ): Promise<ClassificationReport> => ({
        kind,
        projectionAsOf:
          kind === "usage" ? "2026-08-08T10:00:00Z" : "2026-08-08T12:00:00Z",
        rows:
          kind === "recurring-issues"
            ? [
                {
                  tagId: "vpn",
                  objectType: "work_record" as const,
                  count: 3,
                  objectIds: ["ticket-1"],
                },
              ]
            : [
                {
                  tagId: "vpn",
                  objectType: "work_record" as const,
                  date: "2026-08-08",
                  count: 3,
                },
              ],
      }),
    );
    render(
      <ClassificationInsightsPage
        clientID="client-a"
        api={{ report }}
        technicianOptions={[{ id: "tech-a", label: "Alex" }]}
      />,
    );
    fireEvent.change(screen.getByLabelText("Technician"), {
      target: { value: "tech-a" },
    });
    fireEvent.change(screen.getByLabelText("Tag match"), {
      target: { value: "all" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Run insights" }));
    await waitFor(() => expect(report).toHaveBeenCalledTimes(5));
    expect(report.mock.calls[0][1]).toMatchObject({
      clientID: "client-a",
      technicianID: "tech-a",
      match: "all",
    });
    expect(screen.getByText(/Projected through/)).toBeInTheDocument();
    expect(screen.getByText(/Projected through/)).toHaveTextContent(
      new Date("2026-08-08T10:00:00Z").toLocaleString(),
    );
    const link = screen.getByRole("link", {
      name: "Open work record ticket-1",
    });
    expect(link.getAttribute("href")).toContain("classification-insights");
    expect(decodeURIComponent(link.getAttribute("href") ?? "")).toContain(
      "technician_id=tech-a",
    );
  });

  it("renders governed labels, an accessible trend chart, and an exact table", async () => {
    const report = vi.fn(
      async (
        kind: ClassificationReportKind,
      ): Promise<ClassificationReport> => ({
        kind,
        projectionAsOf: "2026-08-08T10:00:00Z",
        rows:
          kind === "trends"
            ? [
                {
                  tagId: "tag-vpn",
                  objectType: "work_record",
                  date: "2026-08-08",
                  count: 2,
                  addedCount: 3,
                  removedCount: 1,
                  activeCount: 2,
                },
              ]
            : [],
      }),
    );
    const catalog = vi.fn(async () => ({
      groups: [{ id: "network", label: "Network", description: "" }],
      tags: [
        {
          id: "tag-vpn",
          label: "VPN",
          groupId: "network",
          state: "active" as const,
          synonyms: [],
          version: 1,
        },
      ],
    }));
    render(
      <ClassificationInsightsPage
        clientID="client-a"
        api={{ report, catalog }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run insights" }));
    expect(
      await screen.findByRole("img", { name: /Classification trend/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("table", { name: "Classification trend values" }),
    ).toHaveTextContent("VPN");
    expect((await axe.run(document.body)).violations).toEqual([]);
  });

  it("uses the common watermark and never paints a late previous-Client batch", async () => {
    const pending: Array<(report: ClassificationReport) => void> = [];
    const report = vi.fn(
      (kind: ClassificationReportKind, filter: ClassificationReportFilter) =>
        new Promise<ClassificationReport>((resolve) => pending.push(resolve)),
    );
    const view = render(
      <ClassificationInsightsPage clientID="client-a" api={{ report }} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run insights" }));
    await waitFor(() => expect(report).toHaveBeenCalledTimes(5));
    view.rerender(
      <ClassificationInsightsPage clientID="client-b" api={{ report }} />,
    );
    await act(async () => {
      pending.forEach((resolve, index) =>
        resolve({
          kind: kinds[index],
          projectionAsOf:
            index === 0 ? "2026-08-08T10:00:00Z" : "2026-08-08T12:00:00Z",
          rows: [{ tagId: "a-only", objectType: "work_record", count: 1 }],
        }),
      );
      await Promise.resolve();
    });
    expect(screen.queryByText(/a-only/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Projected through/)).not.toBeInTheDocument();
  });
});

const kinds: ClassificationReportKind[] = [
  "usage",
  "combinations",
  "trends",
  "classification-health",
  "recurring-issues",
];
