import { type FormEvent, useEffect, useState } from "react";

import {
  createRecoveryAccount,
  disableRecoveryAccount,
  listRecoveryAccounts,
  resetRecoveryAccountPassword,
  type RecoveryAccount,
  updateRecoveryAccountNetworks,
} from "../../api/browserSession";
import {
  Dialog,
  Notice,
  Page,
  StatePanel,
  TagInput,
} from "../../design-system";
import { EntraSettingsPanel } from "./EntraSettingsPanel";

export function RecoveryAccessPage({
  authenticated = true,
}: {
  authenticated?: boolean;
}) {
  const [accounts, setAccounts] = useState<RecoveryAccount[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [notice, setNotice] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [editor, setEditor] = useState<{
    account: RecoveryAccount;
    mode: "password" | "networks";
  } | null>(null);

  useEffect(() => {
    if (!authenticated) return;
    void listRecoveryAccounts()
      .then((found) => {
        setAccounts(found);
        setState("ready");
      })
      .catch(() => setState("error"));
  }, [authenticated]);

  if (!authenticated) {
    return (
      <Page
        className="session-page recovery-access-page"
        eyebrow="Account security"
        title="Local Platform Administrators"
        description="Maintain local installation administrators. Every change and successful sign-in is audited."
      >
        <StatePanel
          state="permission"
          title="Sign in to manage local administrators"
          description="This installation is already configured. An existing Platform Administrator must sign in before another local administrator can be created."
          action={<a href="#/break-glass">Local administrator sign-in</a>}
        />
      </Page>
    );
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setNotice("");
    try {
      const account = await createRecoveryAccount({
        email: String(data.get("email") ?? ""),
        display_name: String(data.get("displayName") ?? ""),
        username: String(data.get("username") ?? ""),
        password: String(data.get("password") ?? ""),
        allowed_cidrs: data.getAll("allowedCIDRs").map(String).filter(Boolean),
        reason: String(data.get("reason") ?? ""),
      });
      setAccounts((current) => [account, ...current]);
      form.reset();
      setState("ready");
      setNotice(
        "Local Platform Administrator created. The password will not be shown again.",
      );
    } catch {
      setState("error");
    }
  }

  async function disable(account: RecoveryAccount) {
    const reason = window.prompt(
      "Why is this local administrator being disabled?",
    );
    if (!reason?.trim()) return;
    try {
      const disabled = await disableRecoveryAccount(account, reason);
      setAccounts((current) =>
        current.map((candidate) =>
          candidate.id === account.id
            ? { ...candidate, ...disabled }
            : candidate,
        ),
      );
      setNotice("Local administrator disabled.");
    } catch {
      setState("error");
    }
  }

  function mergeAccount(updated: RecoveryAccount) {
    setAccounts((current) =>
      current.map((candidate) =>
        candidate.id === updated.id ? { ...candidate, ...updated } : candidate,
      ),
    );
  }

  async function resetPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editor || editor.mode !== "password") return;
    const data = new FormData(event.currentTarget);
    const password = String(data.get("password") ?? "");
    if (password !== String(data.get("confirmPassword") ?? "")) {
      setPasswordError("Passwords do not match.");
      return;
    }
    setPasswordError("");
    try {
      mergeAccount(
        await resetRecoveryAccountPassword(
          editor.account,
          password,
          String(data.get("reason") ?? ""),
        ),
      );
      setEditor(null);
      setNotice("Local administrator password reset.");
    } catch {
      setEditor(null);
      setState("error");
    }
  }

  async function updateNetworks(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editor || editor.mode !== "networks") return;
    const data = new FormData(event.currentTarget);
    try {
      mergeAccount(
        await updateRecoveryAccountNetworks(
          editor.account,
          data.getAll("allowedCIDRs").map(String).filter(Boolean),
          String(data.get("reason") ?? ""),
        ),
      );
      setEditor(null);
      setNotice("Local administrator networks updated.");
    } catch {
      setEditor(null);
      setState("error");
    }
  }

  return (
    <Page
      className="session-page recovery-access-page"
      eyebrow="Account security"
      title="Local Platform Administrators"
      description="Maintain local installation administrators. Every change and successful sign-in is audited. At least one enabled account must remain."
    >
      {notice ? (
        <Notice title="Local administration updated">{notice}</Notice>
      ) : null}
      <EntraSettingsPanel />
      {state === "error" ? (
        <StatePanel
          state="error"
          title="Local administration could not be updated"
          description="Check your permissions and input."
        />
      ) : null}
      <form
        className="recovery-account-form"
        onSubmit={(event) => void create(event)}
      >
        <h2>Add local Platform Administrator</h2>
        <label htmlFor="local-admin-email">
          <span>Email</span>
          <input id="local-admin-email" name="email" type="email" required />
        </label>
        <label htmlFor="local-admin-display-name">
          <span>Display name</span>
          <input id="local-admin-display-name" name="displayName" required />
        </label>
        <label htmlFor="recovery-username">
          <span>Username</span>
          <input
            id="recovery-username"
            name="username"
            autoComplete="off"
            required
          />
        </label>
        <label htmlFor="recovery-password">
          <span>Initial password</span>
          <input
            id="recovery-password"
            name="password"
            type="password"
            minLength={16}
            maxLength={72}
            autoComplete="new-password"
            required
          />
        </label>
        <TagInput
          label="Allowed networks"
          name="allowedCIDRs"
          placeholder="10.20.0.0/16"
        />
        <label htmlFor="recovery-reason">
          <span>Reason</span>
          <input
            id="recovery-reason"
            name="reason"
            placeholder="Add an installation administrator"
            required
          />
        </label>
        <button type="submit">Create local administrator</button>
      </form>
      <section aria-label="Local Platform Administrators">
        {state === "loading" ? (
          <StatePanel state="loading" title="Loading local administrators…" />
        ) : null}
        {accounts.map((account) => (
          <article className="session-card" key={account.id}>
            <div>
              <h2>
                {account.username}
                {account.enabled ? "" : " · Disabled"}
              </h2>
              <p>Networks: {account.allowed_cidrs.join(", ")}</p>
              <small>
                {account.last_used_at
                  ? `Last used ${new Date(account.last_used_at).toLocaleString()} from ${account.last_used_ip || "unknown network"}`
                  : "Never used"}
              </small>
            </div>
            {account.enabled ? (
              <div className="session-card__actions">
                <button
                  type="button"
                  onClick={() => {
                    setPasswordError("");
                    setEditor({ account, mode: "password" });
                  }}
                >
                  Reset password
                </button>
                <button
                  type="button"
                  onClick={() => setEditor({ account, mode: "networks" })}
                >
                  Edit networks
                </button>
                <button type="button" onClick={() => void disable(account)}>
                  Disable
                </button>
              </div>
            ) : null}
          </article>
        ))}
      </section>
      <Dialog
        open={editor?.mode === "password"}
        title="Reset local administrator password"
        description={editor?.account.username}
        onClose={() => {
          setPasswordError("");
          setEditor(null);
        }}
      >
        <form
          className="recovery-account-form"
          onSubmit={(event) => void resetPassword(event)}
        >
          <label htmlFor="local-admin-new-password">
            <span>New password</span>
            <input
              id="local-admin-new-password"
              name="password"
              type="password"
              minLength={16}
              maxLength={72}
              autoComplete="new-password"
              required
            />
          </label>
          <label htmlFor="local-admin-confirm-password">
            <span>Confirm new password</span>
            <input
              id="local-admin-confirm-password"
              name="confirmPassword"
              type="password"
              minLength={16}
              maxLength={72}
              autoComplete="new-password"
              required
            />
          </label>
          {passwordError ? <p role="alert">{passwordError}</p> : null}
          <label htmlFor="local-admin-password-reason">
            <span>Password reset reason</span>
            <input id="local-admin-password-reason" name="reason" required />
          </label>
          <div className="session-card__actions">
            <button type="submit">Save new password</button>
            <button
              type="button"
              onClick={() => {
                setPasswordError("");
                setEditor(null);
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      </Dialog>
      <Dialog
        open={editor?.mode === "networks"}
        title="Edit allowed networks"
        description={editor?.account.username}
        onClose={() => setEditor(null)}
      >
        <form
          className="recovery-account-form"
          onSubmit={(event) => void updateNetworks(event)}
        >
          <TagInput
            label="Updated allowed networks"
            name="allowedCIDRs"
            defaultValues={editor?.account.allowed_cidrs}
            placeholder="192.168.86.0/24"
          />
          <label htmlFor="local-admin-network-reason">
            <span>Network change reason</span>
            <input id="local-admin-network-reason" name="reason" required />
          </label>
          <div className="session-card__actions">
            <button type="submit">Save networks</button>
            <button type="button" onClick={() => setEditor(null)}>
              Cancel
            </button>
          </div>
        </form>
      </Dialog>
    </Page>
  );
}
