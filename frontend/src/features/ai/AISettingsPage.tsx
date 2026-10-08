import { useEffect, useRef, useState } from "react";

import {
  Button,
  Dialog,
  FormActions,
  Notice,
  Page,
  StatePanel,
} from "../../design-system";
import { AISettingsAPIError, aiSettingsAPI } from "./api";
import type {
  AIFeature,
  AIPolicy,
  AISettingsAPI,
  ModelProfile,
  NetworkMode,
  ProviderConnection,
} from "./types";
import "./ai.css";

const MEBIBYTE = 1024 * 1024;
const features: Array<{ id: AIFeature; label: string; modelLabel: string }> = [
  {
    id: "summary",
    label: "Allow ticket summaries",
    modelLabel: "Summary model",
  },
  {
    id: "reply_draft",
    label: "Allow reply drafts",
    modelLabel: "Reply draft model",
  },
  {
    id: "similar_suggestions",
    label: "Allow similar-ticket suggestions",
    modelLabel: "Similar-ticket suggestions model",
  },
  {
    id: "calendar_recommendation",
    label: "Allow calendar recommendations",
    modelLabel: "Calendar recommendation model",
  },
];

type ProviderDraft = {
  id?: string;
  name: string;
  adapter: "ollama" | "openai_compatible";
  networkMode: NetworkMode;
  baseUrl: string;
  timeoutSeconds: string;
  requestLimitMiB: string;
  responseLimitMiB: string;
  localNetworkAcknowledged: boolean;
  reason: string;
};

type PolicyDraft = Omit<AIPolicy, "currentMonthlyCostMinor"> & {
  reason: string;
};

type ConnectionModels =
  | { state: "loading"; models: ModelProfile[] }
  | { state: "loaded"; models: ModelProfile[] }
  | { state: "error"; models: ModelProfile[] };

function emptyProviderDraft(): ProviderDraft {
  return {
    name: "",
    adapter: "openai_compatible",
    networkMode: "remote",
    baseUrl: "",
    timeoutSeconds: "300",
    requestLimitMiB: "1",
    responseLimitMiB: "5",
    localNetworkAcknowledged: false,
    reason: "",
  };
}

function providerDraft(connection: ProviderConnection): ProviderDraft {
  return {
    id: connection.id,
    name: connection.name,
    adapter: connection.adapter,
    networkMode: connection.networkMode,
    baseUrl: connection.baseUrl,
    timeoutSeconds: String(connection.timeoutSeconds),
    requestLimitMiB: String(connection.requestLimitBytes / MEBIBYTE),
    responseLimitMiB: String(connection.responseLimitBytes / MEBIBYTE),
    localNetworkAcknowledged: connection.networkMode === "local",
    reason: "",
  };
}

function policyDraft(policy: AIPolicy): PolicyDraft {
  return { ...policy, reason: "" };
}

function errorCode(error: unknown) {
  return error instanceof AISettingsAPIError ||
    (error && typeof error === "object" && "code" in error)
    ? String((error as { code: unknown }).code)
    : "request_failed";
}

function changedBySomeoneElse(error: unknown) {
  const status =
    error && typeof error === "object" && "status" in error
      ? (error as { status?: unknown }).status
      : undefined;
  return status === 409 || errorCode(error) === "version_conflict";
}

function isAbortError(error: unknown) {
  return (
    (error instanceof DOMException && error.name === "AbortError") ||
    (typeof error === "object" &&
      error !== null &&
      "name" in error &&
      (error as { name?: unknown }).name === "AbortError")
  );
}

function updateModel(
  models: ModelProfile[],
  id: string,
  apply: (model: ModelProfile) => ModelProfile,
) {
  return models.map((model) => (model.id === id ? apply(model) : model));
}

function featureMapping(policy: PolicyDraft, feature: AIFeature) {
  if (feature === "summary") return policy.summaryModelProfileId;
  if (feature === "reply_draft") return policy.replyDraftModelProfileId;
  if (feature === "calendar_recommendation")
    return policy.calendarRecommendationModelProfileId;
  return policy.similarSuggestionsModelProfileId;
}

function withFeatureMapping(
  policy: PolicyDraft,
  feature: AIFeature,
  value: string,
): PolicyDraft {
  if (feature === "summary") return { ...policy, summaryModelProfileId: value };
  if (feature === "reply_draft")
    return { ...policy, replyDraftModelProfileId: value };
  if (feature === "calendar_recommendation")
    return { ...policy, calendarRecommendationModelProfileId: value };
  return { ...policy, similarSuggestionsModelProfileId: value };
}

function isPublicIPv4(octets: number[]) {
  return !(
    octets.length !== 4 ||
    octets.some((octet) => octet < 0 || octet > 255) ||
    octets[0] === 10 ||
    octets[0] === 127 ||
    octets[0] === 0 ||
    octets[0] >= 224 ||
    (octets[0] === 169 && octets[1] === 254) ||
    (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) ||
    (octets[0] === 192 && octets[1] === 168) ||
    (octets[0] === 100 && octets[1] >= 64 && octets[1] <= 127)
  );
}

function parseIPv6Hextets(host: string): number[] | null {
  const parts = host.split("::");
  if (parts.length > 2) return null;
  const left = parts[0] ? parts[0].split(":") : [];
  const right = parts.length === 2 && parts[1] ? parts[1].split(":") : [];
  if (parts.length === 1 && left.length !== 8) return null;
  const missing = 8 - left.length - right.length;
  if (missing < (parts.length === 2 ? 1 : 0)) return null;
  const raw = [...left, ...Array(missing).fill("0"), ...right];
  if (raw.length !== 8 || raw.some((part) => !/^[0-9a-f]{1,4}$/i.test(part)))
    return null;
  return raw.map((part) => Number.parseInt(part, 16));
}

function isPublicIPv6(host: string) {
  const groups = parseIPv6Hextets(host);
  if (!groups) return false;
  const allZero = groups.every((group) => group === 0);
  const loopback =
    groups.slice(0, 7).every((group) => group === 0) && groups[7] === 1;
  const ipv4Mapped =
    groups.slice(0, 5).every((group) => group === 0) && groups[5] === 0xffff;
  const ipv4Compatible =
    !allZero && groups.slice(0, 6).every((group) => group === 0);
  if (ipv4Mapped || ipv4Compatible) {
    return isPublicIPv4([
      groups[6] >> 8,
      groups[6] & 0xff,
      groups[7] >> 8,
      groups[7] & 0xff,
    ]);
  }
  const multicast = (groups[0] & 0xff00) === 0xff00;
  const linkLocal = (groups[0] & 0xffc0) === 0xfe80;
  const uniqueLocal = (groups[0] & 0xfe00) === 0xfc00;
  const documentation =
    (groups[0] === 0x2001 && groups[1] === 0x0db8) ||
    (groups[0] === 0x3fff && (groups[1] & 0xf000) === 0);
  const nonGlobal =
    (groups[0] === 0x0100 && groups[1] === 0) ||
    (groups[0] === 0x2001 && groups[1] === 0x0002) ||
    (groups[0] === 0x2001 && (groups[1] & 0xfff0) === 0x0010);
  return !(
    allZero ||
    loopback ||
    multicast ||
    linkLocal ||
    uniqueLocal ||
    documentation ||
    nonGlobal
  );
}

export function isPublicRemoteURL(value: string) {
  try {
    const endpoint = new URL(value);
    if (
      endpoint.protocol !== "https:" ||
      !endpoint.hostname ||
      endpoint.username ||
      endpoint.password
    )
      return false;
    const host = endpoint.hostname.toLowerCase().replace(/^\[|\]$/g, "");
    if (host.includes(":")) return isPublicIPv6(host);
    const ipv4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
    if (ipv4) return isPublicIPv4(ipv4.slice(1).map(Number));
    return host !== "localhost" && host.includes(".");
  } catch {
    return false;
  }
}

function formatSafeTimestamp(value: string) {
  const timestamp = new Date(value);
  return Number.isNaN(timestamp.getTime())
    ? "recorded"
    : timestamp.toLocaleString();
}

export function AISettingsPage({
  api = aiSettingsAPI,
  authenticated = true,
  entraAvailable = true,
}: {
  api?: AISettingsAPI;
  authenticated?: boolean;
  entraAvailable?: boolean;
}) {
  const [connections, setConnections] = useState<ProviderConnection[] | null>(
    null,
  );
  const [policy, setPolicy] = useState<PolicyDraft | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [draft, setDraft] = useState<ProviderDraft | null>(null);
  const [credential, setCredential] = useState("");
  const [credentialDialog, setCredentialDialog] = useState(false);
  const [replacementCredential, setReplacementCredential] = useState("");
  const [replacementReason, setReplacementReason] = useState("");
  const [modelsByConnection, setModelsByConnection] = useState<
    Record<string, ConnectionModels>
  >({});
  const [modelReason, setModelReason] = useState("");
  const [discoveryReason, setDiscoveryReason] = useState("");
  const [notice, setNotice] = useState("Ready");
  const [error, setError] = useState("");
  const errorRef = useRef<HTMLParagraphElement>(null);
  const credentialRef = useRef("");
  const replacementCredentialRef = useRef("");
  const selectedIDRef = useRef<string | null>(null);
  const requestGeneration = useRef(new Map<string, number>());
  const requestAbort = useRef(new Map<string, AbortController>());
  const operationGeneration = useRef(new Map<string, number>());
  const replaceButtonRef = useRef<HTMLButtonElement>(null);
  const credentialInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!authenticated) return;
    let active = true;
    const controller = new AbortController();
    void Promise.all([
      api.listConnections(controller.signal),
      api.getPolicy(controller.signal),
    ])
      .then(([loadedConnections, loadedPolicy]) => {
        if (!active) return;
        setConnections(loadedConnections);
        setPolicy(policyDraft(loadedPolicy));
        setModelsByConnection(
          Object.fromEntries(
            loadedConnections.map((connection) => [
              connection.id,
              { state: "loading", models: [] },
            ]),
          ),
        );
        const first = loadedConnections[0];
        if (first) {
          selectedIDRef.current = first.id;
          setSelectedID(first.id);
          setDraft(providerDraft(first));
        }
        const bootstrap = loadedConnections.map((connection) => {
          const generation =
            (requestGeneration.current.get(connection.id) ?? 0) + 1;
          requestGeneration.current.set(connection.id, generation);
          const requestKey = `models:${connection.id}`;
          const { controller, generation: operation } =
            beginOperation(requestKey);
          return {
            id: connection.id,
            generation,
            operation,
            requestKey,
            controller,
            promise: api.listModels(connection.id, controller.signal),
          };
        });
        void Promise.allSettled(
          bootstrap.map((request) => request.promise),
        ).then((results) => {
          if (!active) return;
          setModelsByConnection((current) => {
            const next = { ...current };
            results.forEach((result, index) => {
              const request = bootstrap[index];
              if (
                requestGeneration.current.get(request.id) !==
                  request.generation ||
                request.controller.signal.aborted ||
                !isCurrentOperation(
                  request.requestKey,
                  request.operation,
                  request.controller,
                )
              )
                return;
              next[request.id] =
                result.status === "fulfilled"
                  ? { state: "loaded", models: result.value }
                  : { state: "error", models: [] };
            });
            return next;
          });
        });
      })
      .catch((failure) => {
        if (!active || controller.signal.aborted || isAbortError(failure))
          return;
        setConnections([]);
        setPolicy(null);
        setError("AI settings could not be loaded.");
        setNotice("Loading failed");
      });
    return () => {
      active = false;
      controller.abort();
      requestAbort.current.forEach((controller) => controller.abort());
      requestAbort.current.clear();
    };
  }, [api, authenticated]);

  useEffect(
    () => () => {
      credentialRef.current = "";
      replacementCredentialRef.current = "";
    },
    [],
  );

  useEffect(() => {
    if (!credentialDialog) return;
    queueMicrotask(() => credentialInputRef.current?.focus());
  }, [credentialDialog]);

  if (!authenticated) {
    return (
      <Page
        eyebrow="Administration"
        title="AI providers and policy"
        description="Configure provider connections, model profiles, and technician-controlled AI assistance."
      >
        <StatePanel
          state="permission"
          title="Sign in to manage AI providers"
          description="Provider settings are protected. Sign in before adding or changing a provider."
          action={
            <div className="ai-actions">
              {entraAvailable ? (
                <a href="/auth/entra/login">Sign in with Microsoft</a>
              ) : null}
              <a href="#/break-glass">Local administrator sign-in</a>
            </div>
          }
        />
      </Page>
    );
  }

  function showError(message: string) {
    setError(message);
    setNotice(message);
    queueMicrotask(() => errorRef.current?.focus());
  }

  function beginOperation(key: string) {
    const generation = (operationGeneration.current.get(key) ?? 0) + 1;
    operationGeneration.current.set(key, generation);
    requestAbort.current.get(key)?.abort();
    const controller = new AbortController();
    requestAbort.current.set(key, controller);
    return { controller, generation };
  }

  function isCurrentOperation(
    key: string,
    generation: number,
    controller: AbortController,
  ) {
    return (
      !controller.signal.aborted &&
      operationGeneration.current.get(key) === generation
    );
  }

  function clearCredential() {
    credentialRef.current = "";
    setCredential("");
  }

  function updateConnectionModels(
    connectionID: string,
    apply: (current: ConnectionModels) => ConnectionModels,
  ) {
    setModelsByConnection((current) => {
      const existing = current[connectionID] ?? { state: "loaded", models: [] };
      return { ...current, [connectionID]: apply(existing) };
    });
  }

  function refreshModels(connectionID: string) {
    const generation = (requestGeneration.current.get(connectionID) ?? 0) + 1;
    requestGeneration.current.set(connectionID, generation);
    const requestKey = `models:${connectionID}`;
    const { controller, generation: operation } = beginOperation(requestKey);
    updateConnectionModels(connectionID, (current) => ({
      ...current,
      state: "loading",
    }));
    void api.listModels(connectionID, controller.signal).then(
      (loaded) => {
        if (
          controller.signal.aborted ||
          requestGeneration.current.get(connectionID) !== generation ||
          !isCurrentOperation(requestKey, operation, controller)
        )
          return;
        updateConnectionModels(connectionID, () => ({
          state: "loaded",
          models: loaded,
        }));
      },
      () => {
        if (
          controller.signal.aborted ||
          requestGeneration.current.get(connectionID) !== generation ||
          !isCurrentOperation(requestKey, operation, controller)
        )
          return;
        updateConnectionModels(connectionID, (current) => ({
          ...current,
          state: "error",
        }));
      },
    );
  }

  function updateSelectedModels(
    apply: (models: ModelProfile[]) => ModelProfile[],
  ) {
    if (!selectedIDRef.current) return;
    const connectionID = selectedIDRef.current;
    updateConnectionModels(connectionID, (current) => ({
      ...current,
      models: apply(current.models),
    }));
  }

  function setModels(next: React.SetStateAction<ModelProfile[] | null>) {
    updateSelectedModels((current) => {
      const updated = typeof next === "function" ? next(current) : next;
      return updated ?? current;
    });
  }

  function selectConnection(connection: ProviderConnection) {
    selectedIDRef.current = connection.id;
    setSelectedID(connection.id);
    setDraft(providerDraft(connection));
    clearCredential();
    setError("");
  }

  function addProvider() {
    selectedIDRef.current = null;
    setSelectedID(null);
    setDraft(emptyProviderDraft());
    clearCredential();
    setError("");
    setNotice("New provider form ready");
  }

  function updateDraft<K extends keyof ProviderDraft>(
    key: K,
    value: ProviderDraft[K],
  ) {
    setDraft((current) => (current ? { ...current, [key]: value } : current));
  }

  function validateProvider(): {
    timeoutSeconds: number;
    requestLimitBytes: number;
    responseLimitBytes: number;
  } | null {
    if (!draft) return null;
    const timeoutSeconds = Number(draft.timeoutSeconds);
    const requestMiB = Number(draft.requestLimitMiB);
    const responseMiB = Number(draft.responseLimitMiB);
    if (
      !Number.isInteger(timeoutSeconds) ||
      timeoutSeconds < 1 ||
      timeoutSeconds > 3600
    ) {
      showError("Timeout must be between 1 and 3600 seconds.");
      return null;
    }
    if (!Number.isFinite(requestMiB) || requestMiB <= 0 || requestMiB > 5) {
      showError("Request limit cannot exceed 5 MiB.");
      return null;
    }
    if (!Number.isFinite(responseMiB) || responseMiB <= 0 || responseMiB > 10) {
      showError("Response limit cannot exceed 10 MiB.");
      document.getElementById("ai-response-limit")?.focus();
      return null;
    }
    if (!draft.name.trim() || !draft.baseUrl.trim() || !draft.reason.trim()) {
      showError("Provider name, base URL, and reason are required.");
      return null;
    }
    try {
      const endpoint = new URL(draft.baseUrl);
      const validRemote =
        draft.networkMode === "remote" && isPublicRemoteURL(draft.baseUrl);
      const validLocal =
        draft.networkMode === "local" &&
        (endpoint.protocol === "http:" || endpoint.protocol === "https:") &&
        !!endpoint.hostname &&
        !endpoint.username &&
        !endpoint.password;
      if (!validRemote && !validLocal) throw new Error("invalid_provider_url");
    } catch {
      showError(
        draft.networkMode === "remote"
          ? "Remote providers must use a public HTTPS endpoint."
          : "Local providers must use an HTTP or HTTPS endpoint.",
      );
      document.getElementById("ai-base-url")?.focus();
      return null;
    }
    if (draft.networkMode === "local" && !draft.localNetworkAcknowledged) {
      showError("Local-network acknowledgment is required.");
      return null;
    }
    return {
      timeoutSeconds,
      requestLimitBytes: Math.round(requestMiB * MEBIBYTE),
      responseLimitBytes: Math.round(responseMiB * MEBIBYTE),
    };
  }

  async function saveProvider(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const limits = validateProvider();
    if (!draft || !limits) return;
    const connectionID = draft.id;
    let savedConnectionID = connectionID;
    const requestKey = `provider:${connectionID ?? "create"}`;
    const { controller, generation } = beginOperation(requestKey);
    setError("");
    try {
      if (connectionID) {
        const saved = await api.updateConnection(
          connectionID,
          {
            ...draft,
            ...limits,
            expectedVersion:
              connections?.find((item) => item.id === connectionID)?.version ??
              0,
          },
          controller.signal,
        );
        savedConnectionID = saved.id;
        if (!isCurrentOperation(requestKey, generation, controller)) return;
        setConnections(
          (current) =>
            current?.map((item) => (item.id === saved.id ? saved : item)) ??
            current,
        );
        if (selectedIDRef.current === connectionID)
          setDraft(providerDraft(saved));
      } else {
        const saved = await api.createConnection(
          {
            ...draft,
            ...limits,
            credential: credential || undefined,
          },
          controller.signal,
        );
        savedConnectionID = saved.id;
        if (!isCurrentOperation(requestKey, generation, controller)) return;
        setConnections((current) => [...(current ?? []), saved]);
        if (selectedIDRef.current === null) {
          selectedIDRef.current = saved.id;
          setSelectedID(saved.id);
          setDraft(providerDraft(saved));
        }
        clearCredential();
      }
      if (
        isCurrentOperation(requestKey, generation, controller) &&
        selectedIDRef.current === savedConnectionID
      )
        setNotice("Provider saved");
    } catch (failure) {
      if (
        !isCurrentOperation(requestKey, generation, controller) ||
        isAbortError(failure) ||
        (connectionID !== null && selectedIDRef.current !== connectionID)
      )
        return;
      showError(
        changedBySomeoneElse(failure)
          ? "This provider was changed by someone else. Your unsaved values are still here."
          : `Provider could not be saved (${errorCode(failure)}).`,
      );
    }
  }

  async function setEnabled() {
    if (!draft?.id) return;
    if (!draft.reason.trim()) {
      showError("A reason is required to enable or disable a provider.");
      return;
    }
    const current = connections?.find((item) => item.id === draft.id);
    if (!current) return;
    const requestKey = `enabled:${current.id}`;
    const { controller, generation } = beginOperation(requestKey);
    try {
      const saved = await api.setConnectionEnabled(
        current.id,
        {
          enabled: !current.enabled,
          expectedVersion: current.version,
          reason: draft.reason,
        },
        controller.signal,
      );
      if (!isCurrentOperation(requestKey, generation, controller)) return;
      setConnections(
        (items) =>
          items?.map((item) => (item.id === saved.id ? saved : item)) ?? items,
      );
      if (selectedIDRef.current === current.id) {
        setDraft(providerDraft(saved));
        setNotice(saved.enabled ? "Provider enabled" : "Provider disabled");
      }
    } catch (failure) {
      if (
        !isCurrentOperation(requestKey, generation, controller) ||
        isAbortError(failure) ||
        selectedIDRef.current !== current.id
      )
        return;
      showError(
        changedBySomeoneElse(failure)
          ? "This provider was changed by someone else. Your unsaved values are still here."
          : `Provider state could not be changed (${errorCode(failure)}).`,
      );
    }
  }

  async function testConnection() {
    if (!draft?.id) return;
    const connectionID = draft.id;
    const requestKey = `test:${connectionID}`;
    const { controller, generation } = beginOperation(requestKey);
    try {
      const saved = await api.testConnection(connectionID, controller.signal);
      if (!isCurrentOperation(requestKey, generation, controller)) return;
      setConnections(
        (items) =>
          items?.map((item) => (item.id === saved.id ? saved : item)) ?? items,
      );
      if (selectedIDRef.current === connectionID) {
        setDraft(providerDraft(saved));
        setNotice("Provider test completed");
      }
    } catch (failure) {
      if (
        !isCurrentOperation(requestKey, generation, controller) ||
        isAbortError(failure) ||
        selectedIDRef.current !== connectionID
      )
        return;
      showError(`Provider test failed (${errorCode(failure)}).`);
    }
  }

  async function discoverModels() {
    if (!draft?.id) return;
    if (!discoveryReason.trim()) {
      showError("A discovery reason is required.");
      return;
    }
    const current = connections?.find((item) => item.id === draft.id);
    if (!current) return;
    const connectionID = current.id;
    const generation = (requestGeneration.current.get(connectionID) ?? 0) + 1;
    requestGeneration.current.set(connectionID, generation);
    const requestKey = `discover:${connectionID}`;
    const { controller, generation: operation } = beginOperation(requestKey);
    try {
      const discovered = await api.discoverModels(
        connectionID,
        {
          expectedVersion: current.version,
          reason: discoveryReason,
        },
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        requestGeneration.current.get(connectionID) !== generation ||
        !isCurrentOperation(requestKey, operation, controller)
      )
        return;
      updateConnectionModels(connectionID, () => ({
        state: "loaded",
        models: discovered,
      }));
      refreshModels(connectionID);
      if (selectedIDRef.current === connectionID) {
        setNotice(
          "Model discovery completed. Discovered models remain disabled until you enable them.",
        );
      }
    } catch (failure) {
      if (
        !isCurrentOperation(requestKey, operation, controller) ||
        isAbortError(failure) ||
        requestGeneration.current.get(connectionID) !== generation ||
        selectedIDRef.current !== connectionID
      )
        return;
      showError(`Model discovery failed (${errorCode(failure)}).`);
    }
  }

  async function saveModels() {
    const connectionID = draft?.id;
    const modelState = connectionID
      ? modelsByConnection[connectionID]
      : undefined;
    if (!connectionID || !modelState) return;
    if (!modelReason.trim()) {
      showError("A model change reason is required.");
      return;
    }
    const generation = (requestGeneration.current.get(connectionID) ?? 0) + 1;
    requestGeneration.current.set(connectionID, generation);
    const requestKey = `models-save:${connectionID}`;
    const { controller, generation: operation } = beginOperation(requestKey);
    try {
      const saved = await api.updateModels(
        connectionID,
        modelState.models.map(
          ({
            connectionId: _connectionID,
            providerModelId: _providerID,
            ...model
          }) => ({ ...model, expectedVersion: model.version }),
        ),
        modelReason,
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        requestGeneration.current.get(connectionID) !== generation ||
        !isCurrentOperation(requestKey, operation, controller)
      )
        return;
      updateConnectionModels(connectionID, () => ({
        state: "loaded",
        models: saved,
      }));
      refreshModels(connectionID);
      if (selectedIDRef.current === connectionID)
        setNotice("Model profiles saved");
    } catch (failure) {
      if (
        controller.signal.aborted ||
        isAbortError(failure) ||
        requestGeneration.current.get(connectionID) !== generation ||
        !isCurrentOperation(requestKey, operation, controller) ||
        selectedIDRef.current !== connectionID
      )
        return;
      showError(
        changedBySomeoneElse(failure)
          ? "These models were changed by someone else. Your unsaved values are still here."
          : `Models could not be saved (${errorCode(failure)}).`,
      );
    }
  }

  async function saveCredential(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft?.id || !replacementCredential || !replacementReason.trim()) {
      showError("A new credential and reason are required.");
      return;
    }
    const current = connections?.find((item) => item.id === draft.id);
    if (!current) return;
    const requestKey = `credential:${current.id}`;
    const { controller, generation } = beginOperation(requestKey);
    try {
      const saved = await api.replaceCredential(
        current.id,
        {
          credential: replacementCredential,
          expectedVersion: current.version,
          reason: replacementReason,
        },
        controller.signal,
      );
      if (!isCurrentOperation(requestKey, generation, controller)) return;
      setConnections(
        (items) =>
          items?.map((item) => (item.id === saved.id ? saved : item)) ?? items,
      );
      if (selectedIDRef.current !== current.id) return;
      setDraft(providerDraft(saved));
      setReplacementCredential("");
      replacementCredentialRef.current = "";
      setReplacementReason("");
      setCredentialDialog(false);
      setNotice("Credential replaced");
      requestAnimationFrame(() => replaceButtonRef.current?.focus());
    } catch (failure) {
      if (
        !isCurrentOperation(requestKey, generation, controller) ||
        isAbortError(failure) ||
        selectedIDRef.current !== current.id
      )
        return;
      showError(`Credential could not be replaced (${errorCode(failure)}).`);
    }
  }

  function cancelCredential() {
    if (draft?.id) requestAbort.current.get(`credential:${draft.id}`)?.abort();
    replaceButtonRef.current?.focus();
    setReplacementCredential("");
    replacementCredentialRef.current = "";
    setReplacementReason("");
    setCredentialDialog(false);
    setNotice("Credential replacement cancelled");
  }

  function trapCredentialDialog(event: React.KeyboardEvent<HTMLElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      cancelCredential();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(
      event.currentTarget.querySelectorAll<HTMLElement>(
        "input, button:not([disabled]), [href], select, textarea, [tabindex]:not([tabindex='-1'])",
      ),
    ).filter((element) => !element.hasAttribute("disabled"));
    const first = focusable[0];
    const last = focusable.at(-1);
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  async function savePolicy(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!policy || !policy.reason.trim()) {
      showError("A policy reason is required.");
      return;
    }
    if (invalidPolicyMappings.length > 0) {
      showError(
        "A mapped model is disabled or unavailable. Select an eligible model before saving the policy.",
      );
      return;
    }
    const { controller, generation } = beginOperation("policy");
    try {
      const { reason, ...request } = policy;
      const saved = await api.updatePolicy(
        {
          ...request,
          expectedVersion: policy.version,
          reason,
        },
        controller.signal,
      );
      if (!isCurrentOperation("policy", generation, controller)) return;
      setPolicy(policyDraft(saved));
      setNotice("AI policy saved");
    } catch (failure) {
      if (
        !isCurrentOperation("policy", generation, controller) ||
        isAbortError(failure)
      )
        return;
      showError(
        changedBySomeoneElse(failure)
          ? "The AI policy was changed by someone else. Your unsaved values are still here."
          : `AI policy could not be saved (${errorCode(failure)}).`,
      );
    }
  }

  const selectedConnection = connections?.find(
    (connection) => connection.id === selectedID,
  );
  const selectedModelState = selectedID
    ? modelsByConnection[selectedID]
    : undefined;
  const models = selectedModelState?.models ?? [];
  const enabledModels = (connections ?? []).flatMap((connection) =>
    connection.enabled
      ? (modelsByConnection[connection.id]?.models ?? []).filter(
          (model) => model.enabled,
        )
      : [],
  );
  const invalidPolicyMappings = policy
    ? features.filter((feature) => {
        const mapping = featureMapping(policy, feature.id);
        return (
          mapping !== "" &&
          !enabledModels.some(
            (model) =>
              model.id === mapping &&
              model.supportedFeatures.includes(feature.id) &&
              (feature.id !== "calendar_recommendation" || model.zeroCost),
          )
        );
      })
    : [];
  const unavailableModelProviders = (connections ?? []).filter(
    (connection) => modelsByConnection[connection.id]?.state === "error",
  );

  return (
    <Page
      eyebrow="Administration"
      title="AI providers and policy"
      description="Configure provider connections, model profiles, and technician-controlled AI assistance."
      actions={
        <Button intent="primary" onClick={addProvider}>
          Add provider
        </Button>
      }
    >
      <div className="ai-settings-page">
        <p className="ai-status" role="status" aria-live="polite">
          {notice}
        </p>
        {error ? (
          <div tabIndex={-1} ref={errorRef}>
            <Notice tone="danger" title="AI settings action failed" urgent>
              {error}
            </Notice>
          </div>
        ) : null}

        {connections === null ? (
          <StatePanel
            state="loading"
            title="Loading AI settings"
            description="Retrieving provider connections, model profiles, and policy."
          />
        ) : (
          <div className="ai-settings-grid">
            <aside className="ai-provider-list" aria-label="AI providers">
              <h2>Providers</h2>
              {connections.length === 0 ? (
                <p className="ai-empty">
                  No providers configured. Add a provider to begin.
                </p>
              ) : (
                <ul>
                  {connections.map((connection) => {
                    const modelState = modelsByConnection[connection.id];
                    const modelCount =
                      modelState?.state === "loaded"
                        ? `${modelState.models.length} model${modelState.models.length === 1 ? "" : "s"}`
                        : modelState?.state === "error"
                          ? "Models unavailable"
                          : "Models loading";
                    return (
                      <li key={connection.id}>
                        <button
                          type="button"
                          className={
                            connection.id === selectedID ? "selected" : ""
                          }
                          onClick={() => selectConnection(connection)}
                        >
                          <strong>{connection.name}</strong>
                          <span>
                            {connection.adapter.replace("_", " ")} ·{" "}
                            {connection.networkMode}
                          </span>
                          <span>
                            {connection.enabled ? "Enabled" : "Disabled"} ·{" "}
                            {connection.credentialConfigured
                              ? "Configured"
                              : "Not configured"}
                          </span>
                          <span>
                            {connection.health} · {modelCount}
                          </span>
                          {connection.lastSucceededAt ? (
                            <span>
                              Last successful test{" "}
                              {formatSafeTimestamp(connection.lastSucceededAt)}
                            </span>
                          ) : null}
                          {connection.lastErrorCode ? (
                            <span>Safe error: {connection.lastErrorCode}</span>
                          ) : null}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </aside>

            <div className="ai-workspace">
              {draft ? (
                <section
                  className="ai-panel"
                  aria-labelledby="provider-form-heading"
                >
                  <div className="panel-heading">
                    <div>
                      <h2 id="provider-form-heading">
                        {draft.id ? "Provider connection" : "New provider"}
                      </h2>
                      <p>
                        Credentials are write-only. Only their configuration
                        status is shown.
                      </p>
                    </div>
                    {selectedConnection ? (
                      <span className="ai-health">
                        {selectedConnection.health}
                      </span>
                    ) : null}
                  </div>
                  <form className="ai-form" noValidate onSubmit={saveProvider}>
                    <label>
                      <span>Provider name</span>
                      <input
                        value={draft.name}
                        onChange={(event) =>
                          updateDraft("name", event.target.value)
                        }
                      />
                    </label>
                    <label>
                      <span>Provider adapter</span>
                      <select
                        value={draft.adapter}
                        onChange={(event) =>
                          updateDraft(
                            "adapter",
                            event.target.value as ProviderDraft["adapter"],
                          )
                        }
                      >
                        <option value="openai_compatible">
                          OpenAI compatible
                        </option>
                        <option value="ollama">Ollama</option>
                      </select>
                    </label>
                    <label>
                      <span>Network mode</span>
                      <select
                        value={draft.networkMode}
                        onChange={(event) => {
                          const networkMode = event.target.value as NetworkMode;
                          setDraft((current) =>
                            current
                              ? {
                                  ...current,
                                  networkMode,
                                  timeoutSeconds:
                                    current.timeoutSeconds === "300" ||
                                    current.timeoutSeconds === "900"
                                      ? networkMode === "local"
                                        ? "900"
                                        : "300"
                                      : current.timeoutSeconds,
                                  localNetworkAcknowledged:
                                    networkMode === "remote"
                                      ? false
                                      : current.localNetworkAcknowledged,
                                }
                              : current,
                          );
                        }}
                      >
                        <option value="remote">Remote</option>
                        <option value="local">Local</option>
                      </select>
                    </label>
                    <label>
                      <span>Base URL</span>
                      <input
                        id="ai-base-url"
                        type="url"
                        value={draft.baseUrl}
                        onChange={(event) =>
                          updateDraft("baseUrl", event.target.value)
                        }
                      />
                    </label>
                    {draft.networkMode === "remote" ? (
                      <p className="ai-help">
                        Remote providers must use public HTTPS endpoints. The
                        server verifies DNS and public routing before
                        connecting.
                      </p>
                    ) : (
                      <div className="ai-local-notice">
                        <p>
                          Local network does not necessarily mean this machine.
                        </p>
                        <label>
                          <input
                            type="checkbox"
                            checked={draft.localNetworkAcknowledged}
                            onChange={(event) =>
                              updateDraft(
                                "localNetworkAcknowledged",
                                event.target.checked,
                              )
                            }
                          />{" "}
                          Allow private-network access for this provider
                        </label>
                      </div>
                    )}
                    {!draft.id ? (
                      <label>
                        <span>Credential</span>
                        <input
                          type="password"
                          autoComplete="new-password"
                          value={credential}
                          onChange={(event) => {
                            credentialRef.current = event.target.value;
                            setCredential(event.target.value);
                          }}
                        />
                      </label>
                    ) : (
                      <p className="ai-credential-status">
                        Credential:{" "}
                        {selectedConnection?.credentialConfigured
                          ? "Configured"
                          : "Not configured"}
                      </p>
                    )}
                    <label>
                      <span>Timeout (seconds)</span>
                      <input
                        type="number"
                        min="1"
                        max="3600"
                        value={draft.timeoutSeconds}
                        onChange={(event) =>
                          updateDraft("timeoutSeconds", event.target.value)
                        }
                      />
                    </label>
                    <label>
                      <span>Request limit (MiB)</span>
                      <input
                        type="number"
                        min="1"
                        max="5"
                        value={draft.requestLimitMiB}
                        onChange={(event) =>
                          updateDraft("requestLimitMiB", event.target.value)
                        }
                      />
                    </label>
                    <label>
                      <span>Response limit (MiB)</span>
                      <input
                        id="ai-response-limit"
                        type="number"
                        min="1"
                        max="10"
                        value={draft.responseLimitMiB}
                        onChange={(event) =>
                          updateDraft("responseLimitMiB", event.target.value)
                        }
                      />
                    </label>
                    <label className="ai-form-wide">
                      <span>Reason</span>
                      <input
                        value={draft.reason}
                        onChange={(event) =>
                          updateDraft("reason", event.target.value)
                        }
                        placeholder="Why is this provider being changed?"
                      />
                    </label>
                    <div className="ai-actions ai-form-wide">
                      <button className="ai-primary" type="submit">
                        Save provider
                      </button>
                      {draft.id ? (
                        <>
                          <button type="button" onClick={setEnabled}>
                            {selectedConnection?.enabled
                              ? "Disable provider"
                              : "Enable provider"}
                          </button>
                          <button
                            type="button"
                            ref={replaceButtonRef}
                            onClick={() => setCredentialDialog(true)}
                          >
                            Replace credential
                          </button>
                          <button type="button" onClick={testConnection}>
                            Test connection
                          </button>
                        </>
                      ) : null}
                    </div>
                  </form>
                </section>
              ) : (
                <section className="ai-panel ai-empty">
                  <h2>Select or add a provider</h2>
                  <p>Provider settings are scoped to this MSP.</p>
                </section>
              )}

              {draft?.id ? (
                <section className="ai-panel" aria-labelledby="models-heading">
                  <div className="panel-heading">
                    <div>
                      <h2 id="models-heading">Model profiles</h2>
                      <p>
                        Discovery does not enable models or assign them to
                        technician features.
                      </p>
                    </div>
                  </div>
                  <div className="ai-inline-form">
                    <label>
                      <span>Discovery reason</span>
                      <input
                        value={discoveryReason}
                        onChange={(event) =>
                          setDiscoveryReason(event.target.value)
                        }
                      />
                    </label>
                    <button type="button" onClick={discoverModels}>
                      Discover models
                    </button>
                  </div>
                  {selectedModelState?.state === "loading" ? (
                    <p className="ai-empty">Loading models…</p>
                  ) : selectedModelState?.state === "error" ? (
                    <div className="ai-model-error" role="status">
                      <p>Models are unavailable for this provider.</p>
                      <button
                        type="button"
                        onClick={() => refreshModels(draft.id!)}
                      >
                        Retry models
                      </button>
                    </div>
                  ) : models.length === 0 ? (
                    <p className="ai-empty">No models discovered yet.</p>
                  ) : (
                    <>
                      <div className="ai-table-wrap">
                        <table>
                          <thead>
                            <tr>
                              <th>Enabled</th>
                              <th>Provider model ID</th>
                              <th>Display name</th>
                              <th>Features</th>
                              <th>Context</th>
                              <th>Output</th>
                              <th>Zero cost</th>
                              <th>Input / M</th>
                              <th>Output / M</th>
                            </tr>
                          </thead>
                          <tbody>
                            {models.map((model) => (
                              <tr key={model.id}>
                                <td>
                                  <input
                                    aria-label={`Enable ${model.displayName}`}
                                    type="checkbox"
                                    checked={model.enabled}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                enabled: event.target.checked,
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <code>{model.providerModelId}</code>
                                </td>
                                <td>
                                  <input
                                    aria-label={`Display name for ${model.providerModelId}`}
                                    value={model.displayName}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                displayName: event.target.value,
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <fieldset
                                    aria-label={`Supported features for ${model.displayName}`}
                                  >
                                    {features.map((feature) => (
                                      <label key={feature.id}>
                                        <input
                                          type="checkbox"
                                          checked={model.supportedFeatures.includes(
                                            feature.id,
                                          )}
                                          onChange={(event) =>
                                            setModels((current) =>
                                              current
                                                ? updateModel(
                                                    current,
                                                    model.id,
                                                    (item) => ({
                                                      ...item,
                                                      supportedFeatures: event
                                                        .target.checked
                                                        ? [
                                                            ...item.supportedFeatures,
                                                            feature.id,
                                                          ]
                                                        : item.supportedFeatures.filter(
                                                            (value) =>
                                                              value !==
                                                              feature.id,
                                                          ),
                                                    }),
                                                  )
                                                : current,
                                            )
                                          }
                                        />
                                        {feature.id}
                                      </label>
                                    ))}
                                  </fieldset>
                                </td>
                                <td>
                                  <input
                                    aria-label={`Context limit for ${model.displayName}`}
                                    type="number"
                                    value={model.contextLimit}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                contextLimit: Number(
                                                  event.target.value,
                                                ),
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <input
                                    aria-label={`Output limit for ${model.displayName}`}
                                    type="number"
                                    value={model.outputLimit}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                outputLimit: Number(
                                                  event.target.value,
                                                ),
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <input
                                    aria-label={`Zero cost for ${model.displayName}`}
                                    type="checkbox"
                                    checked={model.zeroCost}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                zeroCost: event.target.checked,
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <input
                                    aria-label={`Input price for ${model.displayName}`}
                                    type="number"
                                    value={model.inputCostPerMillionMinor ?? ""}
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                inputCostPerMillionMinor:
                                                  event.target.value === ""
                                                    ? undefined
                                                    : Number(
                                                        event.target.value,
                                                      ),
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                                <td>
                                  <input
                                    aria-label={`Output price for ${model.displayName}`}
                                    type="number"
                                    value={
                                      model.outputCostPerMillionMinor ?? ""
                                    }
                                    onChange={(event) =>
                                      setModels((current) =>
                                        current
                                          ? updateModel(
                                              current,
                                              model.id,
                                              (item) => ({
                                                ...item,
                                                outputCostPerMillionMinor:
                                                  event.target.value === ""
                                                    ? undefined
                                                    : Number(
                                                        event.target.value,
                                                      ),
                                              }),
                                            )
                                          : current,
                                      )
                                    }
                                  />
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                      <div className="ai-inline-form">
                        <label>
                          <span>Model change reason</span>
                          <input
                            value={modelReason}
                            onChange={(event) =>
                              setModelReason(event.target.value)
                            }
                          />
                        </label>
                        <button type="button" onClick={saveModels}>
                          Save models
                        </button>
                      </div>
                    </>
                  )}
                </section>
              ) : null}

              {policy ? (
                <section className="ai-panel" aria-labelledby="policy-heading">
                  <div className="panel-heading">
                    <div>
                      <h2 id="policy-heading">MSP AI policy</h2>
                      <p>
                        AI results remain technician-reviewed recommendations;
                        they do not send replies or change work records.
                      </p>
                    </div>
                  </div>
                  {invalidPolicyMappings.length > 0 ? (
                    <p className="ai-policy-warning" role="alert">
                      A current model mapping is disabled or unavailable. Remap
                      it before saving.
                    </p>
                  ) : null}
                  {unavailableModelProviders.length > 0 ? (
                    <p className="ai-policy-warning" role="status">
                      Models are unavailable for:{" "}
                      {unavailableModelProviders
                        .map((connection) => connection.name)
                        .join(", ")}
                      . They are not available for policy selection.
                    </p>
                  ) : null}
                  <form className="ai-form" onSubmit={savePolicy}>
                    <label className="ai-check ai-form-wide">
                      <input
                        type="checkbox"
                        checked={policy.enabled}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? { ...current, enabled: event.target.checked }
                              : current,
                          )
                        }
                      />{" "}
                      Enable AI assistance
                    </label>
                    <label className="ai-check ai-form-wide">
                      <input
                        type="checkbox"
                        checked={policy.providerDisclosureAccepted}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? {
                                  ...current,
                                  providerDisclosureAccepted:
                                    event.target.checked,
                                }
                              : current,
                          )
                        }
                      />{" "}
                      Provider disclosure accepted
                    </label>
                    <label className="ai-form-wide">
                      <span>Prompt version</span>
                      <input
                        value={policy.promptVersion}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? {
                                  ...current,
                                  promptVersion: event.target.value,
                                }
                              : current,
                          )
                        }
                      />
                    </label>
                    <fieldset className="ai-form-wide">
                      <legend>Allowed features</legend>
                      {features.map((feature) => (
                        <label className="ai-check" key={feature.id}>
                          <input
                            type="checkbox"
                            checked={policy.allowedFeatures.includes(
                              feature.id,
                            )}
                            onChange={(event) =>
                              setPolicy((current) =>
                                current
                                  ? {
                                      ...current,
                                      allowedFeatures: event.target.checked
                                        ? [
                                            ...current.allowedFeatures,
                                            feature.id,
                                          ]
                                        : current.allowedFeatures.filter(
                                            (value) => value !== feature.id,
                                          ),
                                    }
                                  : current,
                              )
                            }
                          />{" "}
                          {feature.label}
                        </label>
                      ))}
                    </fieldset>
                    {features.map((feature) => (
                      <label key={feature.id}>
                        <span>{feature.modelLabel}</span>
                        <select
                          value={featureMapping(policy, feature.id)}
                          onChange={(event) =>
                            setPolicy((current) =>
                              current
                                ? withFeatureMapping(
                                    current,
                                    feature.id,
                                    event.target.value,
                                  )
                                : current,
                            )
                          }
                        >
                          <option value="">No model selected</option>
                          {enabledModels
                            .filter(
                              (model) =>
                                model.supportedFeatures.includes(feature.id) &&
                                (feature.id !== "calendar_recommendation" ||
                                  model.zeroCost),
                            )
                            .map((model) => (
                              <option key={model.id} value={model.id}>
                                {model.displayName}
                              </option>
                            ))}
                        </select>
                      </label>
                    ))}
                    <label className="ai-check ai-form-wide">
                      <input
                        type="checkbox"
                        checked={policy.costLimitEnabled}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? {
                                  ...current,
                                  costLimitEnabled: event.target.checked,
                                }
                              : current,
                          )
                        }
                      />{" "}
                      Enforce a monthly cost limit
                    </label>
                    <label>
                      <span>Monthly cost limit (minor units)</span>
                      <input
                        type="number"
                        min="0"
                        value={policy.monthlyCostLimitMinor}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? {
                                  ...current,
                                  monthlyCostLimitMinor: Number(
                                    event.target.value,
                                  ),
                                }
                              : current,
                          )
                        }
                      />
                    </label>
                    <label className="ai-check">
                      <input
                        type="checkbox"
                        checked={policy.allowUnmeteredUnknown}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? {
                                  ...current,
                                  allowUnmeteredUnknown: event.target.checked,
                                }
                              : current,
                          )
                        }
                      />{" "}
                      Allow unmetered unknown-cost providers
                    </label>
                    <label className="ai-form-wide">
                      <span>Policy reason</span>
                      <input
                        value={policy.reason}
                        onChange={(event) =>
                          setPolicy((current) =>
                            current
                              ? { ...current, reason: event.target.value }
                              : current,
                          )
                        }
                      />
                    </label>
                    <div className="ai-actions ai-form-wide">
                      <button type="submit" className="ai-primary">
                        Save policy
                      </button>
                    </div>
                  </form>
                </section>
              ) : null}
            </div>
          </div>
        )}
        <Dialog
          open={credentialDialog && Boolean(draft?.id)}
          title="Replace credential"
          description="The existing credential is not shown and this new value will not be retained after submission."
          initialFocusRef={credentialInputRef}
          onClose={cancelCredential}
          onKeyDown={trapCredentialDialog}
          actions={
            <FormActions>
              <Button
                form="replace-provider-credential"
                type="submit"
                intent="primary"
              >
                Save credential
              </Button>
              <Button intent="tertiary" onClick={cancelCredential}>
                Cancel
              </Button>
            </FormActions>
          }
        >
          <form id="replace-provider-credential" onSubmit={saveCredential}>
            <label>
              <span>New credential</span>
              <input
                ref={credentialInputRef}
                type="password"
                autoComplete="new-password"
                value={replacementCredential}
                onChange={(event) => {
                  replacementCredentialRef.current = event.target.value;
                  setReplacementCredential(event.target.value);
                }}
              />
            </label>
            <label>
              <span>Credential replacement reason</span>
              <input
                value={replacementReason}
                onChange={(event) => setReplacementReason(event.target.value)}
              />
            </label>
          </form>
        </Dialog>
      </div>
    </Page>
  );
}
