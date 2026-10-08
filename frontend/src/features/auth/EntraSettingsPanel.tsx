import { type FormEvent, useEffect, useState } from "react";

import {
  configureEntraSettings,
  disableEntraSettings,
  loadEntraSettings,
  type EntraSettings,
  verifyEntraSettings,
} from "../../api/browserSession";
import {
  Button,
  ButtonGroup,
  Notice,
  Panel,
  StatePanel,
  StatusBadge,
  WriteOnlySecretField,
} from "../../design-system";

function status(settings: EntraSettings) {
  switch (settings.state) {
    case "connected":
      return { label: "Connected", tone: "success" as const };
    case "verification_required":
      return { label: "Verification required", tone: "warning" as const };
    case "action_required":
      return { label: "Action required", tone: "danger" as const };
    default:
      return { label: "Not connected", tone: "neutral" as const };
  }
}

export function EntraSettingsPanel() {
  const [settings, setSettings] = useState<EntraSettings | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void loadEntraSettings()
      .then((loaded) => {
        setSettings(loaded);
        setState("ready");
      })
      .catch(() => setState("error"));
  }, []);

  async function configure(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const updated = await configureEntraSettings({
        expected_version: settings.version,
        tenant_id: String(data.get("tenantID") ?? ""),
        client_id: String(data.get("clientID") ?? ""),
        client_secret: String(data.get("clientSecret") ?? ""),
        redirect_url: String(data.get("redirectURL") ?? ""),
        reason: String(data.get("reason") ?? ""),
      });
      form.reset();
      setSettings(updated);
      setNotice(
        "Microsoft Entra settings are staged. Verify tenant discovery before SSO becomes active.",
      );
    } catch {
      setState("error");
    }
  }

  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings) return;
    const form = event.currentTarget;
    try {
      const updated = await verifyEntraSettings(
        settings.version,
        String(new FormData(form).get("reason") ?? ""),
      );
      form.reset();
      setSettings(updated);
      setNotice(
        "Microsoft Entra discovery is verified. A controlled Microsoft sign-in remains an acceptance step.",
      );
    } catch {
      setNotice("");
      setState("error");
    }
  }

  async function disable(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings) return;
    const form = event.currentTarget;
    try {
      const updated = await disableEntraSettings(
        settings.version,
        String(new FormData(form).get("reason") ?? ""),
      );
      form.reset();
      setSettings(updated);
      setNotice(
        "Microsoft Entra is disabled. Local Platform Administrator sign-in remains available.",
      );
    } catch {
      setNotice("");
      setState("error");
    }
  }

  if (state === "loading") {
    return <StatePanel state="loading" title="Loading identity settings…" />;
  }
  if (state === "error" || !settings) {
    return (
      <StatePanel
        state="error"
        title="Identity settings could not be updated"
        description="Refresh to load the latest version, then retry."
      />
    );
  }

  const currentStatus = status(settings);
  return (
    <Panel
      title="Microsoft Entra"
      description="Optional workforce single sign-on. Local Platform Administrators remain available whether Entra is connected or not."
      actions={
        <StatusBadge tone={currentStatus.tone}>
          {currentStatus.label}
        </StatusBadge>
      }
    >
      {notice ? <Notice title="Identity updated">{notice}</Notice> : null}
      <form
        key={`configure-${settings.version}`}
        className="recovery-account-form"
        onSubmit={(event) => void configure(event)}
      >
        <h3>
          {settings.state === "not_connected"
            ? "Connect Microsoft Entra"
            : "Replace Microsoft Entra configuration"}
        </h3>
        <label htmlFor="entra-tenant-id">
          <span>Tenant ID</span>
          <input
            id="entra-tenant-id"
            name="tenantID"
            defaultValue={settings.tenant_id}
            autoComplete="off"
            required
          />
        </label>
        <label htmlFor="entra-client-id">
          <span>Client ID</span>
          <input
            id="entra-client-id"
            name="clientID"
            defaultValue={settings.client_id}
            autoComplete="off"
            required
          />
        </label>
        <WriteOnlySecretField
          id="entra-client-secret"
          name="clientSecret"
          label="Client secret"
          configured={settings.credential_configured}
          configuredLabel="A protected credential is configured"
          replacementLabel="Client secret"
          required
        />
        <label htmlFor="entra-redirect-url">
          <span>Redirect URL</span>
          <input
            id="entra-redirect-url"
            name="redirectURL"
            type="url"
            defaultValue={settings.redirect_url}
            required
          />
        </label>
        <label htmlFor="entra-configuration-reason">
          <span>Configuration reason</span>
          <input id="entra-configuration-reason" name="reason" required />
        </label>
        <Button type="submit" intent="primary">
          Stage configuration
        </Button>
      </form>

      {settings.state === "verification_required" ? (
        <form
          className="recovery-account-form"
          onSubmit={(event) => void verify(event)}
        >
          <h3>Verify staged configuration</h3>
          <label htmlFor="entra-verification-reason">
            <span>Verification reason</span>
            <input id="entra-verification-reason" name="reason" required />
          </label>
          <Button type="submit" intent="primary">
            Verify configuration
          </Button>
        </form>
      ) : null}

      {settings.state !== "not_connected" ? (
        <form
          className="recovery-account-form"
          onSubmit={(event) => void disable(event)}
        >
          <h3>Disable Microsoft Entra</h3>
          <p>
            This ends new Microsoft sign-ins. It does not disable any local
            Platform Administrator.
          </p>
          <label htmlFor="entra-disable-reason">
            <span>Disable reason</span>
            <input id="entra-disable-reason" name="reason" required />
          </label>
          <ButtonGroup label="Microsoft Entra disable action">
            <Button type="submit" intent="danger">
              Disable Microsoft Entra
            </Button>
          </ButtonGroup>
        </form>
      ) : null}
    </Panel>
  );
}
