import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button } from "../actions/Button";
import { findA11yViolations } from "../../testing/renderA11y";
import { Dialog } from "./Dialog";
import { Disclosure } from "./Disclosure";
import { Panel } from "./Panel";

afterEach(cleanup);

describe("containers", () => {
  it("labels panels and uses native disclosure behavior", async () => {
    const view = render(
      <>
        <Panel title="Connection health">
          <p>Healthy</p>
        </Panel>
        <Disclosure summary="Technical details">
          <p>Support code CONNECTION-FAILED</p>
        </Disclosure>
      </>,
    );

    expect(
      screen.getByRole("region", { name: "Connection health" }),
    ).toBeVisible();
    expect(
      screen.getByText("Technical details").closest("summary"),
    ).not.toBeNull();
    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});

describe("Dialog", () => {
  it("enters focus, traps Tab, closes on Escape, and restores focus", () => {
    const close = vi.fn();
    const { rerender } = render(
      <>
        <button type="button">Open editor</button>
        <Dialog open={false} title="Edit connection" onClose={close}>
          <Button>Save</Button>
          <Button>Cancel</Button>
        </Dialog>
      </>,
    );
    const trigger = screen.getByRole("button", { name: "Open editor" });
    trigger.focus();

    rerender(
      <>
        <button type="button">Open editor</button>
        <Dialog open title="Edit connection" onClose={close}>
          <Button>Save</Button>
          <Button>Cancel</Button>
        </Dialog>
      </>,
    );

    expect(screen.getByRole("button", { name: "Save" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("dialog"), {
      key: "Tab",
      shiftKey: true,
    });
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(close).toHaveBeenCalledOnce();

    rerender(
      <>
        <button type="button">Open editor</button>
        <Dialog open={false} title="Edit connection" onClose={close}>
          <Button>Save</Button>
        </Dialog>
      </>,
    );
    expect(screen.getByRole("button", { name: "Open editor" })).toHaveFocus();
  });

  it("does not dismiss a destructive confirmation from Escape or backdrop", () => {
    const close = vi.fn();
    render(
      <Dialog
        open
        title="Revoke service key"
        onClose={close}
        dismissible={false}
      >
        <Button intent="danger">Revoke</Button>
      </Dialog>,
    );

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    fireEvent.click(screen.getByTestId("dialog-backdrop"));
    expect(close).not.toHaveBeenCalled();
  });
});
