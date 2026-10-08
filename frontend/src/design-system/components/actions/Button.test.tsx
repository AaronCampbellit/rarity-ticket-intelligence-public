import { cleanup, render, screen } from "@testing-library/react";
import { Plus } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";

import { findA11yViolations } from "../../testing/renderA11y";
import { Button } from "./Button";
import { ButtonGroup } from "./ButtonGroup";
import { IconButton } from "./IconButton";

afterEach(cleanup);

describe("Button", () => {
  it("defaults to a non-submitting secondary action", () => {
    render(<Button>Review details</Button>);
    const button = screen.getByRole("button", { name: "Review details" });

    expect(button).toHaveAttribute("type", "button");
    expect(button).toHaveAttribute("data-intent", "secondary");
  });

  it("announces loading and prevents duplicate activation", () => {
    render(
      <Button loading loadingLabel="Saving changes">
        Save
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Saving changes" });

    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(screen.getByText("Saving changes")).toHaveClass("sr-only");
  });

  it("associates a disabled explanation", () => {
    render(
      <>
        <Button disabled disabledReasonID="missing-capability">
          Delete key
        </Button>
        <p id="missing-capability">Requires service-key administration.</p>
      </>,
    );

    expect(screen.getByRole("button", { name: "Delete key" })).toHaveAttribute(
      "aria-describedby",
      "missing-capability",
    );
  });

  it("renders a leading icon as decorative", () => {
    render(<Button leadingIcon={Plus}>Create work</Button>);
    expect(screen.getByRole("button", { name: "Create work" })).toBeVisible();
    expect(document.querySelector("svg")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
  });
});

describe("IconButton", () => {
  it("requires an accessible label and exposes a native tooltip", () => {
    render(<IconButton icon={Plus} label="Create work" />);
    const button = screen.getByRole("button", { name: "Create work" });

    expect(button).toHaveAttribute("title", "Create work");
  });
});

describe("ButtonGroup", () => {
  it("groups related actions without changing their DOM order", async () => {
    const view = render(
      <ButtonGroup label="Record actions">
        <Button intent="primary">Save</Button>
        <Button>Cancel</Button>
      </ButtonGroup>,
    );

    expect(screen.getByRole("group", { name: "Record actions" })).toBeVisible();
    expect(
      screen.getAllByRole("button").map((button) => button.textContent),
    ).toEqual(["Save", "Cancel"]);
    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});
