export type DirectoryClient = {
  id: string;
  display_id: string;
  name: string;
};

export type DirectoryEntry = {
  id: string;
  display_id: string;
  name: string;
  key: string;
  client_id?: string;
  department_id?: string;
  team_id?: string;
};

export type Directory = {
  clients: DirectoryClient[];
  departments: DirectoryEntry[];
  teams: DirectoryEntry[];
  queues: DirectoryEntry[];
};

type DeployedDirectoryEntry = Record<string, unknown> & {
  id?: string;
  ID?: string;
  display_id?: string;
  DisplayID?: string;
  name?: string;
  Name?: string;
  key?: string;
  Key?: string;
  client_id?: string;
  ClientID?: string;
  department_id?: string;
  DepartmentID?: string;
  team_id?: string;
  TeamID?: string;
};

export type PrincipalNavigation = {
  id: string;
  navigation: string[];
  capabilities: string[];
};

/** The server owns navigation grants; the browser only renders granted routes. */
export function principalHasNavigation(
  principal: PrincipalNavigation,
  route: string,
): boolean {
  return principal.navigation.includes(route);
}

export type SetupStatus = {
  completed: boolean;
  bootstrap_available: boolean;
  entra_available: boolean;
};

export class BrowserRequestError extends Error {
  constructor(
    readonly resource: string,
    readonly status: number,
  ) {
    super(`${resource}_${status}`);
    this.name = "BrowserRequestError";
  }
}

export function isAuthenticationError(error: unknown): boolean {
  return error instanceof BrowserRequestError && error.status === 401;
}

export type BrowserSession = {
  id: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  network_prefix?: string;
  device_fingerprint?: string;
  current: boolean;
};

export type RecoveryAccount = {
  id: string;
  technician_id: string;
  username: string;
  allowed_cidrs: string[];
  enabled: boolean;
  created_at: string;
  last_used_at?: string;
  last_used_ip?: string;
  version: number;
};
export type LocalAdministrator = RecoveryAccount;

export type EntraSettings = {
  state:
    "not_connected" | "verification_required" | "connected" | "action_required";
  version: number;
  tenant_id?: string;
  client_id?: string;
  redirect_url?: string;
  credential_configured: boolean;
};

function cookieValue(name: string): string {
  const prefix = `${name}=`;
  for (const part of document.cookie.split(";")) {
    const candidate = part.trim();
    if (candidate.startsWith(prefix)) {
      return decodeURIComponent(candidate.slice(prefix.length));
    }
  }
  return "";
}

export function csrfHeaders(): Record<string, string> {
  const token = cookieValue("__Host-rarity_csrf") || cookieValue("rarity_csrf");
  return token ? { "X-Rarity-CSRF": token } : {};
}

export async function loadDirectory(signal?: AbortSignal): Promise<Directory> {
  const response = await fetch("/api/v1/directory", {
    credentials: "same-origin",
    signal,
  });
  if (!response.ok) throw new Error(`directory_${response.status}`);
  const directory = (await response.json()) as {
    clients: DeployedDirectoryEntry[];
    departments?: DeployedDirectoryEntry[];
    teams?: DeployedDirectoryEntry[];
    queues?: DeployedDirectoryEntry[];
  };
  return {
    ...directory,
    clients: directory.clients.map(
      normalizeDirectoryEntry,
    ) as DirectoryClient[],
    departments:
      (directory.departments?.map(
        normalizeDirectoryEntry,
      ) as DirectoryEntry[]) ?? [],
    teams:
      (directory.teams?.map(normalizeDirectoryEntry) as DirectoryEntry[]) ?? [],
    queues:
      (directory.queues?.map(normalizeDirectoryEntry) as DirectoryEntry[]) ??
      [],
  };
}

function normalizeDirectoryEntry(entry: DeployedDirectoryEntry) {
  return {
    ...entry,
    id: entry.id ?? entry.ID ?? "",
    display_id: entry.display_id ?? entry.DisplayID ?? "",
    name: entry.name ?? entry.Name ?? "",
    key: entry.key ?? entry.Key ?? "",
    client_id: entry.client_id ?? entry.ClientID,
    department_id: entry.department_id ?? entry.DepartmentID,
    team_id: entry.team_id ?? entry.TeamID,
  };
}

export async function loadPrincipalNavigation(
  signal?: AbortSignal,
): Promise<PrincipalNavigation> {
  const response = await fetch("/api/v1/me", {
    credentials: "same-origin",
    signal,
  });
  if (!response.ok) {
    throw new BrowserRequestError("principal", response.status);
  }
  return (await response.json()) as PrincipalNavigation;
}

export async function loadSetupStatus(
  signal?: AbortSignal,
): Promise<SetupStatus> {
  const response = await fetch("/api/v1/setup/status", {
    credentials: "same-origin",
    signal,
  });
  if (!response.ok) {
    throw new BrowserRequestError("setup", response.status);
  }
  return (await response.json()) as SetupStatus;
}

export async function loginLocalAdministrator(
  username: string,
  password: string,
): Promise<void> {
  const body = new URLSearchParams({
    username: username.trim(),
    password,
  });
  const response = await fetch("/auth/local/login", {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      ...csrfHeaders(),
    },
    body: body.toString(),
  });
  if (!response.ok) throw new Error(`local_admin_${response.status}`);
}

export const loginBreakGlass = loginLocalAdministrator;

export async function listSessions(): Promise<BrowserSession[]> {
  const response = await fetch("/api/v1/sessions", {
    credentials: "same-origin",
  });
  if (!response.ok) throw new Error(`sessions_${response.status}`);
  return (await response.json()) as BrowserSession[];
}

export async function revokeSession(sessionID: string): Promise<void> {
  const response = await fetch(
    `/api/v1/sessions/${encodeURIComponent(sessionID)}`,
    {
      method: "DELETE",
      credentials: "same-origin",
      headers: csrfHeaders(),
    },
  );
  if (!response.ok) throw new Error(`session_revoke_${response.status}`);
}

export async function logout(): Promise<void> {
  const response = await fetch("/auth/logout", {
    method: "POST",
    credentials: "same-origin",
    headers: csrfHeaders(),
  });
  if (!response.ok) throw new Error(`logout_${response.status}`);
}

export async function refreshSession(): Promise<void> {
  const response = await fetch("/auth/session/refresh", {
    method: "POST",
    credentials: "same-origin",
    headers: csrfHeaders(),
  });
  if (!response.ok) throw new Error(`session_refresh_${response.status}`);
}

export async function listRecoveryAccounts(): Promise<RecoveryAccount[]> {
  const response = await fetch("/api/v1/admin/local-administrators", {
    credentials: "same-origin",
  });
  if (!response.ok) throw new Error(`recovery_accounts_${response.status}`);
  return (await response.json()) as RecoveryAccount[];
}

export async function createRecoveryAccount(input: {
  email?: string;
  display_name?: string;
  username: string;
  password: string;
  allowed_cidrs: string[];
  reason: string;
}): Promise<RecoveryAccount> {
  const response = await fetch("/api/v1/admin/local-administrators", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify(input),
  });
  if (!response.ok)
    throw new Error(`recovery_account_create_${response.status}`);
  return (await response.json()) as RecoveryAccount;
}

export async function disableRecoveryAccount(
  account: RecoveryAccount,
  reason: string,
): Promise<RecoveryAccount> {
  const response = await fetch(
    `/api/v1/admin/local-administrators/${encodeURIComponent(account.id)}/disable`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...csrfHeaders() },
      body: JSON.stringify({ expected_version: account.version, reason }),
    },
  );
  if (!response.ok)
    throw new Error(`recovery_account_disable_${response.status}`);
  return (await response.json()) as RecoveryAccount;
}

export async function resetRecoveryAccountPassword(
  account: RecoveryAccount,
  password: string,
  reason: string,
): Promise<RecoveryAccount> {
  const response = await fetch(
    `/api/v1/admin/local-administrators/${encodeURIComponent(account.id)}/password`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...csrfHeaders() },
      body: JSON.stringify({
        expected_version: account.version,
        password,
        reason,
      }),
    },
  );
  if (!response.ok)
    throw new Error(`recovery_account_password_${response.status}`);
  return (await response.json()) as RecoveryAccount;
}

export async function updateRecoveryAccountNetworks(
  account: RecoveryAccount,
  allowedCIDRs: string[],
  reason: string,
): Promise<RecoveryAccount> {
  const response = await fetch(
    `/api/v1/admin/local-administrators/${encodeURIComponent(account.id)}/networks`,
    {
      method: "PATCH",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...csrfHeaders() },
      body: JSON.stringify({
        expected_version: account.version,
        allowed_cidrs: allowedCIDRs,
        reason,
      }),
    },
  );
  if (!response.ok)
    throw new Error(`recovery_account_networks_${response.status}`);
  return (await response.json()) as RecoveryAccount;
}

export async function loadEntraSettings(): Promise<EntraSettings> {
  const response = await fetch("/api/v1/admin/identity/entra", {
    credentials: "same-origin",
  });
  if (!response.ok) throw new Error(`entra_settings_${response.status}`);
  return (await response.json()) as EntraSettings;
}

export async function configureEntraSettings(input: {
  expected_version: number;
  tenant_id: string;
  client_id: string;
  client_secret: string;
  redirect_url: string;
  reason: string;
}): Promise<EntraSettings> {
  const response = await fetch("/api/v1/admin/identity/entra", {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify(input),
  });
  if (!response.ok)
    throw new Error(`entra_settings_configure_${response.status}`);
  return (await response.json()) as EntraSettings;
}

async function mutateEntraSettings(
  operation: "verify" | "disable",
  expectedVersion: number,
  reason: string,
): Promise<EntraSettings> {
  const response = await fetch(`/api/v1/admin/identity/entra/${operation}`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify({
      expected_version: expectedVersion,
      reason,
    }),
  });
  if (!response.ok)
    throw new Error(`entra_settings_${operation}_${response.status}`);
  return (await response.json()) as EntraSettings;
}

export function verifyEntraSettings(
  expectedVersion: number,
  reason: string,
): Promise<EntraSettings> {
  return mutateEntraSettings("verify", expectedVersion, reason);
}

export function disableEntraSettings(
  expectedVersion: number,
  reason: string,
): Promise<EntraSettings> {
  return mutateEntraSettings("disable", expectedVersion, reason);
}
