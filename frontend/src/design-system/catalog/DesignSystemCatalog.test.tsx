import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { findA11yViolations } from "../testing/renderA11y";
import { DesignSystemCatalog } from "./DesignSystemCatalog";

afterEach(cleanup);

describe("DesignSystemCatalog", () => {
  it("organizes the catalog and switches preview density", () => {
    render(<DesignSystemCatalog />);

    expect(
      screen.getByRole("navigation", { name: "Catalog sections" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "Foundations" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Components" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Patterns" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Templates" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "States" })).toBeVisible();

    fireEvent.change(screen.getByLabelText("Preview density"), {
      target: { value: "compact" },
    });
    expect(screen.getByTestId("catalog-preview")).toHaveAttribute(
      "data-density",
      "compact",
    );

    expect(screen.getByText("Raised surface")).toBeVisible();
    expect(screen.getByText("Floating surface")).toBeVisible();
    expect(screen.getByLabelText("Signal console template")).toBeVisible();
    expect(screen.queryByLabelText(/Atlas/)).not.toBeInTheDocument();
  });

  it("renders the implemented action, field, secret, and state contracts", () => {
    render(<DesignSystemCatalog />);

    expect(screen.getByRole("button", { name: "Create work" })).toBeVisible();
    expect(screen.getByLabelText(/Connection name/)).toBeVisible();
    expect(screen.getByLabelText("Due date")).toBeVisible();
    expect(screen.getByLabelText("Assigned technician")).toBeVisible();
    expect(screen.getByLabelText("Internal note")).toBeVisible();
    expect(screen.getByLabelText("Replace API key")).toHaveValue("");
    expect(screen.getByText("States and transitions")).toBeVisible();
    expect(screen.getByText("No work matches these filters")).toBeVisible();
    expect(screen.getByText("Work could not be loaded")).toBeVisible();
    expect(
      screen.getByRole("region", { name: "Classification component examples" }),
    ).toBeVisible();
    expect(screen.getByLabelText("Loading classification")).toHaveAttribute(
      "aria-busy",
      "true",
    );
    expect(screen.getByText("Archived classification")).toBeVisible();
  });

  it("renders provider-agnostic operational patterns", () => {
    render(<DesignSystemCatalog />);

    expect(screen.getByText("Local Ollama")).toBeVisible();
    expect(screen.getByText("http://host.docker.internal:11434")).toBeVisible();
    expect(screen.getByText("Credential not required")).toBeVisible();
    expect(screen.getByText("Discovering models")).toBeVisible();
    expect(screen.getByText("This record changed")).toBeVisible();
  });

  it("has no detectable WCAG A or AA violations", async () => {
    const view = render(<DesignSystemCatalog />);
    expect(await findA11yViolations(view.container)).toEqual([]);
  });
});
