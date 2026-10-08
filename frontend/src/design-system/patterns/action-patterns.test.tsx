import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button } from "../components/actions/Button";
import { ConflictRecovery } from "./ConflictRecovery";
import { ReasonRequiredDialog } from "./ReasonRequiredDialog";
import { ScopedAction } from "./ScopedAction";
import { VersionedEditor } from "./VersionedEditor";

afterEach(cleanup);

const scope = {
  organization: "Campbell MSP",
  client: "Northwind Legal",
  record: "CHG-1042",
};

describe("ScopedAction", () => {
  it("shows scope and explains an unavailable action", () => {
    render(
      <ScopedAction
        scope={scope}
        permitted={false}
        disabledReason="Requires change approval."
      >
        <Button intent="primary">Apply change</Button>
      </ScopedAction>,
    );

    const action = screen.getByRole("button", { name: "Apply change" });
    expect(action).toBeDisabled();
    expect(action).toHaveAccessibleDescription("Requires change approval.");
    expect(
      screen.getByText(/Campbell MSP.*Northwind Legal.*CHG-1042/),
    ).toBeVisible();
  });
});

describe("ReasonRequiredDialog", () => {
  it("rejects whitespace and returns the unchanged expected version", () => {
    const confirm = vi.fn();
    render(
      <ReasonRequiredDialog
        open
        action="Override approval"
        target="Change Order CHG-1042"
        scope={scope}
        expectedVersion={7}
        onConfirm={confirm}
        onClose={() => {}}
      />,
    );

    fireEvent.change(screen.getByLabelText(/Reason/), {
      target: { value: "   " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Override approval" }));
    expect(confirm).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a reason.")).toBeVisible();

    fireEvent.change(screen.getByLabelText(/Reason/), {
      target: { value: "Emergency security remediation" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Override approval" }));
    expect(confirm).toHaveBeenCalledWith({
      reason: "Emergency security remediation",
      expectedVersion: 7,
    });
  });
});

describe("VersionedEditor and conflict recovery", () => {
  it("keeps the current version visible beside the editor", () => {
    render(
      <VersionedEditor version={4} label="Proposal">
        <p>Commercial terms</p>
      </VersionedEditor>,
    );
    expect(screen.getByText("Proposal version 4")).toBeVisible();
  });

  it("offers explicit reload and review choices without retrying", () => {
    const reload = vi.fn();
    const review = vi.fn();
    render(
      <ConflictRecovery
        expectedVersion={4}
        currentVersion={6}
        onReload={reload}
        onReview={review}
      />,
    );

    expect(
      screen.getByText(/expected version 4.*current version is 6/i),
    ).toBeVisible();
    expect(reload).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Reload current version" }),
    );
    expect(reload).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Review differences" }));
    expect(review).toHaveBeenCalledOnce();
  });
});
