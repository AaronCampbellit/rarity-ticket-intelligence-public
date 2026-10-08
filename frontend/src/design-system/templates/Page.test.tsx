import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Button } from "../components/actions/Button";
import { PresentationProvider } from "../foundations/presentation";
import { Page } from "./Page";
import { DirectionPage } from "./DirectionPage";

afterEach(cleanup);

describe("Page", () => {
  it("provides one labeled main region and preserves action order", () => {
    render(
      <Page
        eyebrow="Service operations"
        title="Technician work"
        description="Assigned and unassigned Client work."
        actions={
          <>
            <Button intent="primary">Create work</Button>
            <Button>Save view</Button>
          </>
        }
      >
        <p>Worklist</p>
      </Page>,
    );

    expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
    expect(screen.getByRole("main")).toHaveAccessibleName("Technician work");
    expect(
      screen.getAllByRole("button").map((button) => button.textContent),
    ).toEqual(["Create work", "Save view"]);
  });

  it("composes page families in the Signal console layout", () => {
    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <DirectionPage family="worklist">
          <Page title="Work">
            <p>Worklist</p>
          </Page>
        </DirectionPage>
      </PresentationProvider>,
    );

    expect(screen.getByTestId("direction-page")).toHaveAttribute(
      "data-layout",
      "console",
    );
    expect(screen.getByTestId("direction-page")).toHaveAttribute(
      "data-family",
      "worklist",
    );
  });
});
