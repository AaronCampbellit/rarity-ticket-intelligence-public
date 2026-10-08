import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { findA11yViolations } from "../../testing/renderA11y";
import { Field } from "./Field";
import { Switch } from "./Switch";
import { TextInput } from "./TextInput";
import { ValidationSummary } from "./ValidationSummary";

afterEach(cleanup);

describe("Field", () => {
  it("associates its label, hint, and error with the control", async () => {
    const view = render(
      <Field
        id="provider-name"
        label="Connection name"
        hint="Use a name technicians will recognize."
        error="A connection name is required."
        required
      >
        <TextInput aria-describedby="external-context" />
      </Field>,
    );
    const input = screen.getByRole("textbox", { name: /Connection name/ });

    expect(input).toHaveAttribute("id", "provider-name");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute(
      "aria-describedby",
      "external-context provider-name-hint provider-name-error",
    );
    expect(screen.getByText("Required")).toBeVisible();
    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});

describe("Switch", () => {
  it("uses a native checkbox and visible state text", () => {
    render(
      <Switch label="Enable connection" checked={false} onChange={() => {}} />,
    );
    const control = screen.getByRole("checkbox", {
      name: "Enable connection Off",
    });

    expect(control).toHaveAttribute("type", "checkbox");
    expect(screen.getByText("Off")).toBeVisible();
  });
});

describe("ValidationSummary", () => {
  it("focuses a new failed-submit summary and links to invalid fields", () => {
    render(
      <ValidationSummary
        title="Review two fields"
        errors={[
          { fieldID: "name", message: "Enter a name." },
          { fieldID: "endpoint", message: "Enter an endpoint." },
        ]}
        focusOnMount
      />,
    );

    expect(screen.getByRole("alert")).toHaveFocus();
    expect(screen.getByRole("link", { name: "Enter a name." })).toHaveAttribute(
      "href",
      "#name",
    );
  });
});
