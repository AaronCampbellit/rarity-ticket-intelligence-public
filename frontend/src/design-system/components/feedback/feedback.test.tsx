import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button } from "../actions/Button";
import { findA11yViolations } from "../../testing/renderA11y";
import { Notice } from "./Notice";
import { StatePanel } from "./StatePanel";
import { StatusBadge } from "./StatusBadge";
import { ToastRegion } from "./ToastRegion";

afterEach(cleanup);

describe("shared feedback", () => {
  it("announces loading with readable text", () => {
    render(
      <StatePanel
        state="loading"
        title="Loading work"
        description="Retrieving the selected Client queue."
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Loading work");
  });

  it("renders an empty state without false alert semantics", () => {
    render(
      <StatePanel
        state="empty"
        title="No work matches these filters"
        description="Reset filters to see all work."
      />,
    );
    expect(screen.getByText("No work matches these filters")).toBeVisible();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("offers a real recovery action for a blocking error", () => {
    const retry = vi.fn();
    render(
      <StatePanel
        state="error"
        title="Work could not be loaded"
        description="Try the request again."
        action={<Button onClick={retry}>Retry</Button>}
        supportCode="WORK-LIST-UNAVAILABLE"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledOnce();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "WORK-LIST-UNAVAILABLE",
    );
  });

  it("uses text as well as tone for status", async () => {
    const view = render(
      <>
        <StatusBadge tone="success">Connected</StatusBadge>
        <Notice tone="warning" title="Approval required">
          A second approver must review this change.
        </Notice>
      </>,
    );

    expect(screen.getByText("Connected")).toBeVisible();
    expect(screen.getByText("Approval required")).toBeVisible();
    expect(await findA11yViolations(view.container)).toEqual([]);
  });

  it("queues non-blocking updates in one polite live region", () => {
    render(
      <ToastRegion
        messages={[
          { id: "one", message: "Work saved." },
          { id: "two", message: "Audit evidence recorded." },
        ]}
      />,
    );

    const region = screen.getByRole("status");
    expect(region).toHaveAttribute("aria-live", "polite");
    expect(region).toHaveTextContent("Work saved.");
    expect(region).toHaveTextContent("Audit evidence recorded.");
  });
});
