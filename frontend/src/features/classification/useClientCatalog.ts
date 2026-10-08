import { useEffect, useState } from "react";

import { createClassificationAPI } from "./api";
import type { ClassificationCatalog } from "./types";

type CatalogState = "idle" | "loading" | "ready" | "error";

const catalogByClient = new Map<string, Promise<ClassificationCatalog>>();

function catalogForClient(clientID: string) {
  let request = catalogByClient.get(clientID);
  if (!request) {
    request = createClassificationAPI(globalThis.fetch, clientID)
      .catalog()
      .catch((error: unknown) => {
        catalogByClient.delete(clientID);
        throw error;
      });
    catalogByClient.set(clientID, request);
  }
  return request;
}

/**
 * Catalog labels are MSP-global, but this cache is deliberately keyed by the
 * active Client session so changing client scope never reuses a stale request.
 */
export function useClientClassificationCatalog(clientID: string) {
  const [catalog, setCatalog] = useState<ClassificationCatalog>();
  const [state, setState] = useState<CatalogState>(
    clientID ? "loading" : "idle",
  );

  useEffect(() => {
    if (!clientID) {
      setCatalog(undefined);
      setState("idle");
      return;
    }
    let active = true;
    setCatalog(undefined);
    setState("loading");
    void catalogForClient(clientID)
      .then((loaded) => {
        if (!active) return;
        setCatalog(loaded);
        setState("ready");
      })
      .catch(() => {
        if (active) setState("error");
      });
    return () => {
      active = false;
    };
  }, [clientID]);

  return { catalog, state };
}

export function __resetClientClassificationCatalogForTests() {
  catalogByClient.clear();
}
