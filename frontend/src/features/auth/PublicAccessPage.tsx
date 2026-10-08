import { type FormEvent, useState } from "react";

import { loginLocalAdministrator } from "../../api/browserSession";
import { RarityBrand } from "../../design-system";

export type PublicAccessState = "loading" | "error" | "login" | "recovery";

export function PublicAccessPage({
  state,
  entraAvailable = false,
  loginError,
  onRetry,
  onLocalAuthenticated,
}: {
  state: PublicAccessState;
  entraAvailable?: boolean;
  loginError?: "entra" | "unavailable";
  onRetry?: () => void;
  onLocalAuthenticated?: () => void;
}) {
  return (
    <main id="main-content" className="public-access">
      <div className="public-access__brand public-access__brand--full">
        <RarityBrand variant="full" />
      </div>
      <div className="public-access__brand public-access__brand--compact">
        <RarityBrand variant="compact" />
      </div>

      <section className="public-access__card">
        {state === "loading" ? (
          <div className="public-access__state" role="status">
            <span className="public-access__spinner" aria-hidden="true" />
            <h1>Loading Rarity</h1>
            <p>Confirming installation and session access.</p>
          </div>
        ) : null}

        {state === "error" ? (
          <div className="public-access__state">
            <p className="record-id">Connection</p>
            <h1>Rarity is unavailable</h1>
            <p>
              Installation or session status could not be confirmed. Protected
              content remains closed.
            </p>
            <button
              className="public-access__primary"
              type="button"
              onClick={onRetry}
            >
              Retry
            </button>
          </div>
        ) : null}

        {state === "login" && entraAvailable ? (
          <div className="public-access__state">
            <p className="record-id">Secure workspace</p>
            <h1>Sign in to Rarity</h1>
            <p>Use your organization’s Microsoft account to continue.</p>
            <a className="public-access__primary" href="/auth/entra/login">
              Sign in with Microsoft
            </a>
            {loginError ? (
              <p className="public-access__error" role="alert">
                {loginError === "entra"
                  ? "Microsoft sign-in was not completed. Try again."
                  : "Microsoft sign-in is temporarily unavailable."}
              </p>
            ) : null}
          </div>
        ) : null}

        {state === "login" && !entraAvailable ? (
          <LocalAdministratorLogin
            heading="Sign in to Rarity"
            description="Use the local Platform Administrator account for this installation."
            onAuthenticated={onLocalAuthenticated}
          />
        ) : null}

        {state === "recovery" ? (
          <LocalAdministratorLogin
            heading="Local administrator sign-in"
            description="Use a local Platform Administrator account. Every successful use is audited."
            onAuthenticated={onLocalAuthenticated}
          />
        ) : null}
      </section>
    </main>
  );
}

function LocalAdministratorLogin({
  heading,
  description,
  onAuthenticated,
}: {
  heading: string;
  description: string;
  onAuthenticated?: () => void;
}) {
  const [state, setState] = useState<"idle" | "submitting" | "failed">("idle");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setState("submitting");
    try {
      await loginLocalAdministrator(
        String(form.get("username") ?? ""),
        String(form.get("password") ?? ""),
      );
      setState("idle");
      onAuthenticated?.();
    } catch {
      setState("failed");
    }
  }

  return (
    <form
      className="public-access__form"
      onSubmit={(event) => void submit(event)}
    >
      <p className="record-id">Installation administration</p>
      <h1>{heading}</h1>
      <p>{description}</p>
      <label>
        <span>Username</span>
        <input name="username" autoComplete="username" required />
      </label>
      <label>
        <span>Password</span>
        <input
          name="password"
          type="password"
          autoComplete="current-password"
          required
        />
      </label>
      {state === "failed" ? <p role="alert">Authentication failed.</p> : null}
      <button
        className="public-access__primary"
        type="submit"
        disabled={state === "submitting"}
      >
        {state === "submitting" ? "Signing in…" : "Sign in locally"}
      </button>
    </form>
  );
}
