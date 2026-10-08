import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PublicAccessPage } from "./PublicAccessPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("PublicAccessPage", () => {
  it("shows only branded loading state while access resolves", () => {
    render(<PublicAccessPage state="loading" />);

    expect(screen.getByRole("img", { name: "Rarity" })).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent("Loading Rarity");
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
  });

  it("offers a retry without exposing the application shell", async () => {
    const retry = vi.fn();
    render(<PublicAccessPage state="error" onRetry={retry} />);

    expect(
      screen.getByRole("heading", { name: "Rarity is unavailable" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
  });

  it("shows Microsoft as the only action when Entra is available", () => {
    render(<PublicAccessPage state="login" entraAvailable />);

    expect(
      screen.getByRole("link", { name: "Sign in with Microsoft" }),
    ).toHaveAttribute("href", "/auth/entra/login");
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Local administrator sign-in"),
    ).not.toBeInTheDocument();
  });

  it("shows local administrator fields for an Entra-free installation", () => {
    render(<PublicAccessPage state="login" entraAvailable={false} />);

    expect(screen.getByLabelText("Username")).toBeVisible();
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "type",
      "password",
    );
    expect(
      screen.queryByRole("link", { name: "Sign in with Microsoft" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the recovery form available independently of Entra", () => {
    render(<PublicAccessPage state="recovery" entraAvailable />);

    expect(
      screen.getByRole("heading", { name: "Local administrator sign-in" }),
    ).toBeVisible();
    expect(screen.getByLabelText("Username")).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Sign in with Microsoft" }),
    ).not.toBeInTheDocument();
  });

  it("announces a generic local authentication failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 401 })),
    );
    render(<PublicAccessPage state="recovery" entraAvailable />);

    await userEvent.type(screen.getByLabelText("Username"), "local-admin");
    await userEvent.type(screen.getByLabelText("Password"), "not-the-password");
    await userEvent.click(
      screen.getByRole("button", { name: "Sign in locally" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Authentication failed.",
    );
  });

  it("notifies the boundary after successful local authentication", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );
    const authenticated = vi.fn();
    render(
      <PublicAccessPage
        state="login"
        entraAvailable={false}
        onLocalAuthenticated={authenticated}
      />,
    );

    await userEvent.type(screen.getByLabelText("Username"), "local-admin");
    await userEvent.type(
      screen.getByLabelText("Password"),
      "correct horse battery staple",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Sign in locally" }),
    );

    await waitFor(() => expect(authenticated).toHaveBeenCalledOnce());
  });
});
