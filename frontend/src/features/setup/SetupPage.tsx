import { type FormEvent, useEffect, useState } from "react";

import {
  Button,
  Notice,
  Page,
  StatePanel,
  TagInput,
} from "../../design-system";
import "../../features/work/work.css";
import {
  GuidedSetupCenter,
  type GuidedCenterStatus,
} from "./GuidedSetupCenter";

type SetupCenterStatus = GuidedCenterStatus & {
  intake: Record<string, unknown>;
  object_storage: Record<string, unknown>;
  backups: Record<string, unknown>;
};

function consumeBootstrapToken(): string {
  const [route, query = ""] = window.location.hash.split("?", 2);
  if (route !== "#/setup") return "";
  const token = new URLSearchParams(query).get("bootstrap_token")?.trim() ?? "";
  if (token) {
    window.history.replaceState(
      null,
      "",
      `${window.location.pathname}${window.location.search}#/setup`,
    );
  }
  return token;
}

export function SetupPage({
  installationComplete = false,
}: {
  installationComplete?: boolean | null;
}) {
  const [state, setState] = useState<
    "loading" | "ready" | "saving" | "complete" | "error"
  >(
    installationComplete === null || installationComplete ? "loading" : "ready",
  );
  const [center, setCenter] = useState<SetupCenterStatus | null>(null);
  const [configureEntra, setConfigureEntra] = useState(false);
  const [bootstrapToken, setBootstrapToken] = useState(consumeBootstrapToken);

  useEffect(() => {
    if (installationComplete !== true) {
      if (installationComplete === false) setState("ready");
      return;
    }
    const controller = new AbortController();
    setState("loading");
    void fetch("/api/v1/setup/center", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(`setup_center_${response.status}`);
        return (await response.json()) as SetupCenterStatus;
      })
      .then((value) => {
        setCenter(value);
        setState("complete");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [installationComplete]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setState("saving");
    try {
      const payload: Record<string, unknown> = {
        organization_id: form.get("organization_id"),
        organization_name: form.get("organization_name"),
        organization_display_id: form.get("organization_display_id"),
        admin_email: form.get("admin_email"),
        admin_display_name: form.get("admin_display_name"),
        recovery_username: form.get("recovery_username"),
        recovery_password: form.get("recovery_password"),
        recovery_allowed_cidrs: form
          .getAll("recovery_allowed_cidrs")
          .map(String)
          .map((value) => value.trim())
          .filter(Boolean),
        intake: {},
        object_storage: {},
        backups: {},
      };
      if (configureEntra) {
        Object.assign(payload, {
          entra_tenant_id: form.get("entra_tenant_id"),
          entra_client_id: form.get("entra_client_id"),
          entra_client_secret: form.get("entra_client_secret"),
          entra_redirect_url: form.get("entra_redirect_url"),
          admin_entra_subject: form.get("admin_entra_subject"),
        });
      }
      const response = await fetch("/api/v1/setup/bootstrap", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bootstrap ${bootstrapToken}`,
        },
        body: JSON.stringify(payload),
      });
      if (!response.ok) throw new Error(`bootstrap_${response.status}`);
      formElement.reset();
      setBootstrapToken("");
      setState("complete");
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      className="service-desk-settings setup-center"
      eyebrow={
        installationComplete ? "Platform readiness" : "Secure first-run setup"
      }
      title={
        installationComplete
          ? "Setup center"
          : "Configure this Rarity installation"
      }
      description={
        installationComplete
          ? "Follow the guided steps to connect intake, verify storage, and prove that backup and recovery are working."
          : "Open the single-use setup link from deployment output. It expires after 15 minutes and bootstrap permanently closes after success."
      }
    >
      {state === "loading" ? (
        <StatePanel state="loading" title="Loading setup status…" />
      ) : null}
      {state === "complete" ? (
        <section>
          <h2>{installationComplete ? "Platform setup" : "Setup complete"}</h2>
          <Notice tone="success" title="Bootstrap authority closed">
            Bootstrap authority is permanently closed.
            {center
              ? ` Configuration recorded ${new Date(center.completed_at).toLocaleString()}.`
              : " Local administrator sign-in is ready. Microsoft Entra can be connected later."}
          </Notice>
          {center ? <GuidedSetupCenter center={center} /> : null}
        </section>
      ) : state !== "loading" ? (
        <form className="setup-form" onSubmit={(event) => void submit(event)}>
          <fieldset>
            <legend>Deployment authority</legend>
            {bootstrapToken ? (
              <Notice tone="success" title="Deployment authority accepted">
                This setup link is ready. Its secret has been removed from the
                browser address and will be submitted only when setup is
                completed.
              </Notice>
            ) : (
              <Notice
                tone="warning"
                title="Open the setup link from deployment output"
              >
                Copy the complete first-run URL printed by the Rarity API and
                open it in this browser. If it expired, restart the incomplete
                installation or explicitly rotate it with the recovery helper.
              </Notice>
            )}
          </fieldset>
          <fieldset>
            <legend>Organization</legend>
            <label>
              <span>Installation MSP ID</span>
              <input
                name="organization_id"
                placeholder="The UUID configured as RARITY_MSP_ID"
                required
              />
            </label>
            <label>
              <span>Organization name</span>
              <input name="organization_name" required />
            </label>
            <label>
              <span>Organization display ID</span>
              <input name="organization_display_id" required />
            </label>
          </fieldset>
          <fieldset>
            <legend>Microsoft Entra (optional)</legend>
            <p>
              Skip this for local evaluation or development. You can connect
              Microsoft Entra later from Identity settings.
            </p>
            <label>
              <input
                type="radio"
                name="entra_choice"
                checked={!configureEntra}
                onChange={() => setConfigureEntra(false)}
              />
              <span>Skip Microsoft Entra for now</span>
            </label>
            <label>
              <input
                type="radio"
                name="entra_choice"
                checked={configureEntra}
                onChange={() => setConfigureEntra(true)}
              />
              <span>Configure Microsoft Entra now</span>
            </label>
            {configureEntra ? (
              <>
                <label>
                  <span>Entra tenant ID</span>
                  <input name="entra_tenant_id" required />
                </label>
                <label>
                  <span>Entra application client ID</span>
                  <input name="entra_client_id" required />
                </label>
                <label>
                  <span>Entra application client secret</span>
                  <input
                    name="entra_client_secret"
                    type="password"
                    minLength={16}
                    autoComplete="new-password"
                    required
                  />
                </label>
                <label>
                  <span>Entra redirect URL</span>
                  <input name="entra_redirect_url" type="url" required />
                </label>
                <label>
                  <span>Administrator Entra object ID</span>
                  <input name="admin_entra_subject" required />
                </label>
              </>
            ) : (
              <Notice
                tone="neutral"
                title="Local administration will remain available"
              >
                RTI will be fully usable without Microsoft credentials.
              </Notice>
            )}
          </fieldset>
          <fieldset>
            <legend>Initial local Platform Administrator</legend>
            <label>
              <span>Administrator email</span>
              <input name="admin_email" type="email" required />
            </label>
            <label>
              <span>Administrator display name</span>
              <input name="admin_display_name" required />
            </label>
            <label>
              <span>Local administrator username</span>
              <input
                name="recovery_username"
                autoComplete="username"
                required
              />
            </label>
            <label>
              <span>Local administrator password</span>
              <input
                name="recovery_password"
                type="password"
                minLength={16}
                maxLength={72}
                autoComplete="new-password"
                required
              />
            </label>
            <TagInput
              label="Allowed administrator networks"
              name="recovery_allowed_cidrs"
              defaultValues={["10.0.0.0/8"]}
              placeholder="10.0.0.0/8"
            />
          </fieldset>
          <Notice tone="info" title="Operational setup continues after sign-in">
            After the local administrator is created, the guided Setup Center
            will walk you through mailbox or API intake, object storage, and
            backup verification with plain-English instructions.
          </Notice>
          {state === "error" ? (
            <StatePanel
              state="error"
              title="Setup was not accepted"
              description="Verify HTTPS, token age, required fields, and recovery networks."
            />
          ) : null}
          <button
            type="submit"
            disabled={state === "saving" || !bootstrapToken}
          >
            {state === "saving" ? "Completing setup…" : "Complete secure setup"}
          </button>
        </form>
      ) : null}
    </Page>
  );
}
