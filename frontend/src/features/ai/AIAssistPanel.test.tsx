import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AIAssistPanel } from "./AIAssistPanel";
import type { AIAssistAPI, AIAssistJob, AIAssistRecommendation } from "./types";

const queuedJob: AIAssistJob = {
  id: "job-1",
  workRecordID: "work-1",
  feature: "summary",
  state: "queued",
  attempt: 0,
  maxAttempts: 3,
  createdAt: "2026-07-30T12:00:00Z",
  updatedAt: "2026-07-30T12:00:00Z",
  version: 2,
};
const failedJob: AIAssistJob = {
  ...queuedJob,
  state: "failed",
  attempt: 1,
  safeErrorCode: "provider_timeout",
  completedAt: "2026-07-30T12:00:01Z",
  version: 4,
};
const recommendation: AIAssistRecommendation = {
  id: "recommendation-1",
  jobID: "job-1",
  clientID: "client-1",
  workRecordID: "work-1",
  feature: "summary",
  text: "Plain text recommendation.",
  candidateIDs: ["1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50"],
  confidence: 0.8,
  relevantInputs: ["title", "description"],
  state: "pending_human",
  generatedAt: "2026-07-30T12:00:01Z",
  version: 1,
};

function stub(overrides: Partial<AIAssistAPI> = {}): AIAssistAPI {
  return {
    submit: vi.fn().mockResolvedValue(queuedJob),
    getJob: vi.fn().mockResolvedValue(queuedJob),
    cancel: vi
      .fn()
      .mockResolvedValue({ ...queuedJob, state: "cancelled", version: 3 }),
    retry: vi.fn().mockResolvedValue(queuedJob),
    getRecommendation: vi.fn().mockResolvedValue(recommendation),
    decide: vi.fn().mockResolvedValue({
      id: "recommendation-1",
      state: "accepted",
      applied: false,
      sent: false,
    }),
    ...overrides,
  };
}

afterEach(() => {
  vi.useRealTimers();
  cleanup();
  sessionStorage.clear();
});

describe("AIAssistPanel", () => {
  it("keeps a slow local generation visible and requires a reasoned versioned cancellation", async () => {
    const user = userEvent.setup();
    const api = stub();
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Generate summary" }));
    expect(await screen.findByText("Generating summary…")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Generate reply draft" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Cancel generation" }),
    ).toBeDisabled();
    await user.type(
      screen.getByLabelText("Cancellation reason"),
      "Work already updated",
    );
    await user.click(screen.getByRole("button", { name: "Cancel generation" }));

    expect(api.cancel).toHaveBeenCalledWith(
      "job-1",
      { expectedVersion: 2, reason: "Work already updated" },
      expect.any(AbortSignal),
    );
  });

  it("reloads a completed recommendation from minimal session state and makes it reviewable", async () => {
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    const api = stub({
      getJob: vi.fn().mockResolvedValue({
        ...queuedJob,
        state: "completed",
        recommendationID: "recommendation-1",
        completedAt: "2026-07-30T12:00:01Z",
      }),
    });
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );

    expect(await screen.findByText("Pending human review")).toBeVisible();
    expect(screen.getByText("Plain text recommendation.")).toBeVisible();
    expect(screen.getByText("title, description")).toBeVisible();
    expect(
      screen.getByRole("link", {
        name: "1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50",
      }),
    ).toHaveAttribute(
      "href",
      "#/ai-assist?workRecordID=1e6c0b21-2d1b-4e0a-a1fd-a865803f1a50",
    );
    expect(sessionStorage.getItem("rarity.ai-assist.v1:work-1")).toBe(
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    expect(
      screen.queryByRole("button", { name: /apply|send/i }),
    ).not.toBeInTheDocument();
  });

  it("offers retry only for safe retryable failures and records a human decision without applying or sending", async () => {
    const user = userEvent.setup();
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    const api = stub({
      getJob: vi.fn().mockResolvedValue({
        ...failedJob,
        recommendationID: "recommendation-1",
      }),
      retry: vi.fn().mockResolvedValue({
        ...queuedJob,
        state: "completed",
        recommendationID: "recommendation-1",
        completedAt: "2026-07-30T12:00:02Z",
        version: 5,
      }),
    });
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );

    expect(
      await screen.findByText(
        "Generation could not complete (provider_timeout).",
      ),
    ).toBeVisible();
    expect(sessionStorage.getItem("rarity.ai-assist.v1:work-1")).toBe(
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    await user.type(
      screen.getByLabelText("Retry reason"),
      "Connection is back",
    );
    await user.click(screen.getByRole("button", { name: "Retry generation" }));
    expect(api.retry).toHaveBeenCalledWith(
      "job-1",
      { expectedVersion: 4, reason: "Connection is back" },
      expect.any(AbortSignal),
    );

    await waitFor(() =>
      expect(screen.getByText("Pending human review")).toBeVisible(),
    );
    await user.type(
      screen.getByLabelText("Decision reason"),
      "Technician reviewed the draft",
    );
    await user.click(
      screen.getByRole("button", { name: "Accept recommendation" }),
    );
    expect(api.decide).toHaveBeenCalledWith(
      "recommendation-1",
      { decision: "accepted", reason: "Technician reviewed the draft" },
      expect.any(AbortSignal),
    );
    expect(
      await screen.findByText(
        "Accepted recorded. No changes were applied or sent.",
      ),
    ).toBeVisible();
  });

  it("does not offer retry for a non-retryable safe failure", async () => {
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    const api = stub({
      getJob: vi
        .fn()
        .mockResolvedValue({ ...failedJob, safeErrorCode: "invalid_output" }),
    });
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );

    expect(
      await screen.findByText(
        "Generation could not complete (invalid_output).",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Retry generation" }),
    ).not.toBeInTheDocument();
    expect(sessionStorage.getItem("rarity.ai-assist.v1:work-1")).toBe(
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
  });

  it("keeps the durable job visible after a transient refresh error and recovers on manual refresh", async () => {
    const user = userEvent.setup();
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    const api = stub({
      getJob: vi
        .fn()
        .mockResolvedValueOnce(queuedJob)
        .mockRejectedValueOnce(new Error("temporary network failure"))
        .mockResolvedValueOnce({ ...queuedJob, state: "running", version: 3 }),
    });
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );

    expect(await screen.findByText("Generating summary…")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Refresh status" }));
    expect(
      await screen.findByText(
        "Generation status is unavailable (request_failed).",
      ),
    ).toBeVisible();
    expect(screen.getByText("Generating summary…")).toBeVisible();
    expect(sessionStorage.getItem("rarity.ai-assist.v1:work-1")).toBe(
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );

    await user.click(screen.getByRole("button", { name: "Refresh status" }));
    expect(await screen.findByText(/running •/)).toBeVisible();
  });

  it("does not overlap polls and aborts the in-flight status request on unmount", async () => {
    vi.useFakeTimers();
    let pollSignal: AbortSignal | undefined;
    const api = stub({
      getJob: vi
        .fn()
        .mockResolvedValueOnce(queuedJob)
        .mockImplementation((_id: string, signal?: AbortSignal) => {
          pollSignal = signal;
          return new Promise<AIAssistJob>(() => undefined);
        }),
    });
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    const view = render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );
    await vi.advanceTimersByTimeAsync(1);
    expect(screen.getByText("Generating summary…")).toBeVisible();
    await vi.advanceTimersByTimeAsync(2_000);
    expect(api.getJob).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(api.getJob).toHaveBeenCalledTimes(2);

    view.unmount();
    expect(pollSignal?.aborted).toBe(true);
  });

  it("keeps cancellation actionable while a status poll is pending", async () => {
    vi.useFakeTimers();
    let pollSignal: AbortSignal | undefined;
    const api = stub({
      getJob: vi
        .fn()
        .mockResolvedValueOnce(queuedJob)
        .mockImplementation((_id: string, signal?: AbortSignal) => {
          pollSignal = signal;
          return new Promise<AIAssistJob>(() => undefined);
        }),
    });
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );
    await vi.advanceTimersByTimeAsync(1);
    await vi.advanceTimersByTimeAsync(2_000);

    fireEvent.change(screen.getByLabelText("Cancellation reason"), {
      target: { value: "Technician stopped the request" },
    });
    expect(
      screen.getByRole("button", { name: "Cancel generation" }),
    ).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel generation" }));

    expect(pollSignal?.aborted).toBe(true);
    expect(api.cancel).toHaveBeenCalledWith(
      "job-1",
      { expectedVersion: 2, reason: "Technician stopped the request" },
      expect.any(AbortSignal),
    );
  });

  it("lets cancellation preempt a pending manual refresh", async () => {
    let refreshSignal: AbortSignal | undefined;
    const api = stub({
      getJob: vi
        .fn()
        .mockResolvedValueOnce(queuedJob)
        .mockImplementation((_id: string, signal?: AbortSignal) => {
          refreshSignal = signal;
          return new Promise<AIAssistJob>(() => undefined);
        }),
    });
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );
    expect(await screen.findByText("Generating summary…")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Refresh status" }));
    fireEvent.change(screen.getByLabelText("Cancellation reason"), {
      target: { value: "Technician stopped the request" },
    });
    const cancelButton = screen.getByRole("button", {
      name: "Cancel generation",
    });
    expect(cancelButton).toBeEnabled();
    fireEvent.click(cancelButton);

    expect(refreshSignal?.aborted).toBe(true);
    expect(api.cancel).toHaveBeenCalledWith(
      "job-1",
      { expectedVersion: 2, reason: "Technician stopped the request" },
      expect.any(AbortSignal),
    );
  });

  it("restarts saved-job restoration when the work record changes and ignores the old response", async () => {
    let resolveFirst: ((value: AIAssistJob) => void) | undefined;
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-1",
      JSON.stringify({ version: 1, jobID: "job-1" }),
    );
    sessionStorage.setItem(
      "rarity.ai-assist.v1:work-2",
      JSON.stringify({ version: 1, jobID: "job-2" }),
    );
    const api = stub({
      getJob: vi.fn((id: string) => {
        if (id === "job-1") {
          return new Promise<AIAssistJob>((resolve) => {
            resolveFirst = resolve;
          });
        }
        return Promise.resolve({
          ...queuedJob,
          id: "job-2",
          workRecordID: "work-2",
        });
      }),
    });
    const view = render(
      <AIAssistPanel
        workRecordID="work-1"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );
    view.rerender(
      <AIAssistPanel
        workRecordID="work-2"
        supportedFeatures={["summary"]}
        api={api}
      />,
    );
    expect(await screen.findByText("Generating summary…")).toBeVisible();
    expect(api.getJob).toHaveBeenLastCalledWith(
      "job-2",
      expect.any(AbortSignal),
    );
    resolveFirst?.(queuedJob);

    await waitFor(() =>
      expect(sessionStorage.getItem("rarity.ai-assist.v1:work-2")).toBe(
        JSON.stringify({ version: 1, jobID: "job-2" }),
      ),
    );
  });
});
