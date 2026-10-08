import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AIAssistAPI } from "../../features/ai/types";
import { PresentationProvider } from "../foundations/presentation";
import { AIRail } from "./AIRail";

afterEach(cleanup);

describe("AIRail", () => {
  it("uses the Signal multitasking dock", () => {
    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <AIRail context={{ routeID: "work", routeLabel: "Work" }} />
      </PresentationProvider>,
    );

    expect(screen.getByLabelText("Rarity AI")).toHaveAttribute(
      "data-mode",
      "dock",
    );
  });

  it("submits a durable AI job from the attached record context", async () => {
    const submit = vi.fn().mockResolvedValue({
      id: "job-1",
      workRecordID: "1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50",
      feature: "summary",
      state: "queued",
      attempt: 0,
      maxAttempts: 3,
      createdAt: "2026-08-05T00:00:00Z",
      updatedAt: "2026-08-05T00:00:00Z",
      version: 1,
    });
    const api: AIAssistAPI = {
      submit,
      getJob: vi.fn(),
      cancel: vi.fn(),
      retry: vi.fn(),
      getRecommendation: vi.fn(),
      decide: vi.fn(),
    };

    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <AIRail
          context={{
            routeID: "work",
            routeLabel: "Work",
            recordID: "1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50",
          }}
          api={api}
        />
      </PresentationProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Ask Rarity" }));
    fireEvent.click(screen.getByRole("button", { name: "Generate summary" }));

    await waitFor(() =>
      expect(submit).toHaveBeenCalledWith(
        "1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50",
        expect.objectContaining({ feature: "summary" }),
        expect.any(AbortSignal),
      ),
    );
  });

  it("does not submit work-record generation for project or task context", () => {
    const submit = vi.fn();
    const api: AIAssistAPI = {
      submit,
      getJob: vi.fn(),
      cancel: vi.fn(),
      retry: vi.fn(),
      getRecommendation: vi.fn(),
      decide: vi.fn(),
    };

    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <AIRail
          context={{
            routeID: "project",
            routeLabel: "Projects",
            recordID: "project-1",
            entityType: "project",
          }}
          api={api}
        />
      </PresentationProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Ask Rarity" }));
    expect(
      screen.getByText("Delivery record assistance is coming next"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Generate summary" }),
    ).not.toBeInTheDocument();
    expect(submit).not.toHaveBeenCalled();
  });
});
