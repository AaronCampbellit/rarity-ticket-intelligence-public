import {
  isAuthenticationError,
  loadPrincipalNavigation,
  loadSetupStatus,
  type PrincipalNavigation,
} from "../../api/browserSession";
import { isRecognizedProtectedHash } from "../../navigation";

const returnHashKey = "rarity.post-authentication-return";

export type ApplicationAccess =
  | { kind: "setup"; entraAvailable: boolean }
  | { kind: "unauthenticated"; entraAvailable: boolean }
  | {
      kind: "authenticated";
      entraAvailable: boolean;
      principal: PrincipalNavigation;
    };

export async function loadApplicationAccess(
  signal?: AbortSignal,
): Promise<ApplicationAccess> {
  const setup = await loadSetupStatus(signal);
  if (!setup.completed) {
    return {
      kind: "setup",
      entraAvailable: setup.entra_available,
    };
  }

  try {
    return {
      kind: "authenticated",
      entraAvailable: setup.entra_available,
      principal: await loadPrincipalNavigation(signal),
    };
  } catch (error) {
    if (isAuthenticationError(error)) {
      return {
        kind: "unauthenticated",
        entraAvailable: setup.entra_available,
      };
    }
    throw error;
  }
}

export function safeReturnHash(
  hash: string,
  catalogEnabled: boolean,
): string | undefined {
  return isRecognizedProtectedHash(hash, catalogEnabled) ? hash : undefined;
}

export function rememberReturnHash(
  hash: string,
  catalogEnabled: boolean,
): void {
  if (window.sessionStorage.getItem(returnHashKey)) return;
  const safe = safeReturnHash(hash, catalogEnabled);
  if (safe) window.sessionStorage.setItem(returnHashKey, safe);
}

export function takeReturnHash(catalogEnabled: boolean): string | undefined {
  const remembered = window.sessionStorage.getItem(returnHashKey);
  window.sessionStorage.removeItem(returnHashKey);
  return remembered ? safeReturnHash(remembered, catalogEnabled) : undefined;
}

export function peekReturnHash(catalogEnabled: boolean): string | undefined {
  const remembered = window.sessionStorage.getItem(returnHashKey);
  return remembered ? safeReturnHash(remembered, catalogEnabled) : undefined;
}

export function clearReturnHash(): void {
  window.sessionStorage.removeItem(returnHashKey);
}
