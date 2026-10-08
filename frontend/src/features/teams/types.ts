export type TeamsHealth =
  "pending" | "healthy" | "degraded" | "failed" | "disabled";

export type TeamsConnection = {
  id: string;
  clientID?: string;
  name: string;
  credentialConfigured: boolean;
  enabled: boolean;
  health: TeamsHealth;
  lastTestedAt?: string;
  lastSuccessAt?: string;
  lastErrorCode?: string;
  version: number;
};

export type TeamsSettingsAPI = {
  list(signal?: AbortSignal): Promise<TeamsConnection[]>;
  create(
    input: { name: string; webhookURL: string; reason: string },
    signal?: AbortSignal,
  ): Promise<TeamsConnection>;
  updateName(
    id: string,
    input: { expectedVersion: number; name: string; reason: string },
    signal?: AbortSignal,
  ): Promise<TeamsConnection>;
  setEnabled(
    id: string,
    input: { expectedVersion: number; enabled: boolean; reason: string },
    signal?: AbortSignal,
  ): Promise<TeamsConnection>;
  replaceCredential(
    id: string,
    input: { expectedVersion: number; webhookURL: string; reason: string },
    signal?: AbortSignal,
  ): Promise<TeamsConnection>;
  test(
    id: string,
    input: { reason: string },
    signal?: AbortSignal,
  ): Promise<TeamsConnection>;
};
