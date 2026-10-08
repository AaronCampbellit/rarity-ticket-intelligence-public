import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { WriteOnlySecretField } from "./WriteOnlySecretField";

afterEach(cleanup);

describe("WriteOnlySecretField", () => {
  it("represents an existing credential without receiving or rendering it", () => {
    const { container } = render(
      <WriteOnlySecretField
        id="api-key"
        label="API key"
        name="credential"
        configured
      />,
    );

    expect(screen.getByText("Credential configured")).toBeVisible();
    expect(screen.getByLabelText("Replace API key")).toHaveValue("");
    expect(container.textContent).not.toContain("sk-existing-secret");
    expect(container.querySelector("input")).not.toHaveAttribute("value");
  });

  it("requires a new credential when none is configured", () => {
    render(
      <WriteOnlySecretField
        id="api-key"
        label="API key"
        name="credential"
        configured={false}
        required
      />,
    );

    expect(screen.getByLabelText(/^API key/)).toBeRequired();
  });

  it("reveals only the locally entered replacement value", () => {
    render(
      <WriteOnlySecretField
        id="api-key"
        label="API key"
        name="credential"
        configured
      />,
    );
    const input = screen.getByLabelText("Replace API key");

    fireEvent.change(input, { target: { value: "new-local-value" } });
    fireEvent.click(screen.getByRole("button", { name: "Show API key" }));

    expect(input).toHaveAttribute("type", "text");
    expect(input).toHaveValue("new-local-value");
    expect(screen.getByRole("button", { name: "Hide API key" })).toBeVisible();
  });
});
