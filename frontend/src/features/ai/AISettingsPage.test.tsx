import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AISettingsPage, isPublicRemoteURL } from "./AISettingsPage";
import type { AISettingsAPI, AIPolicy, ProviderConnection } from "./types";

afterEach(cleanup);

const provider: ProviderConnection = {
  id: "provider-1",
  name: "Remote models",
  adapter: "openai_compatible",
  networkMode: "remote",
  baseUrl: "https://models.example.test",
  credentialConfigured: true,
  enabled: true,
  timeoutSeconds: 300,
  requestLimitBytes: 1_048_576,
  responseLimitBytes: 5_242_880,
  health: "healthy",
  version: 4,
};

const policy: AIPolicy = {
  enabled: false,
  providerDisclosureAccepted: false,
  promptVersion: "v1",
  allowedFeatures: [],
  summaryModelProfileId: "",
  replyDraftModelProfileId: "",
  similarSuggestionsModelProfileId: "",
  calendarRecommendationModelProfileId: "",
  costLimitEnabled: true,
  allowUnmeteredUnknown: false,
  monthlyCostLimitMinor: 0,
  version: 2,
};

function apiStub(overrides: Partial<AISettingsAPI> = {}): AISettingsAPI {
  return {
    listConnections: vi.fn().mockResolvedValue([provider]),
    createConnection: vi
      .fn()
      .mockResolvedValue({ ...provider, id: "provider-2" }),
    updateConnection: vi.fn().mockResolvedValue(provider),
    setConnectionEnabled: vi.fn().mockResolvedValue(provider),
    replaceCredential: vi.fn().mockResolvedValue(provider),
    testConnection: vi.fn().mockResolvedValue(provider),
    discoverModels: vi.fn().mockResolvedValue([]),
    listModels: vi.fn().mockResolvedValue([]),
    updateModels: vi.fn().mockResolvedValue([]),
    getPolicy: vi.fn().mockResolvedValue(policy),
    updatePolicy: vi.fn().mockResolvedValue(policy),
    ...overrides,
  };
}

describe("AISettingsPage", () => {
  it("does not offer provider mutations while signed out", () => {
    const api = apiStub();

    render(<AISettingsPage api={api} authenticated={false} />);

    expect(
      screen.getByRole("heading", { name: "Sign in to manage AI providers" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Add provider" }),
    ).not.toBeInTheDocument();
    expect(api.listConnections).not.toHaveBeenCalled();
  });

  it.each([
    ["https://models.example.test", true],
    ["http://models.example.test", false],
    ["https://localhost", false],
    ["https://models", false],
    ["https://10.0.0.4", false],
    ["https://100.64.1.1", false],
    ["https://169.254.1.1", false],
    ["https://[::]", false],
    ["https://[::1]", false],
    ["https://[ff02::1]", false],
    ["https://[fe80::1]", false],
    ["https://[fd00::1]", false],
    ["https://[2001:db8::1]", false],
    ["https://[::ffff:127.0.0.1]", false],
    ["https://[::ffff:10.0.0.1]", false],
    ["https://[::ffff:169.254.1.1]", false],
    ["https://[::ffff:100.64.1.1]", false],
    ["https://[::ffff:0.0.0.0]", false],
    ["https://[::ffff:224.0.0.1]", false],
    ["https://[::ffff:8.8.8.8]", true],
    ["https://[::127.0.0.1]", false],
    ["https://[::10.0.0.1]", false],
    ["https://[::0.0.0.0]", false],
    ["https://[::100.64.0.1]", false],
    ["https://[::8.8.8.8]", true],
    ["https://[2606:4700:4700::1111]", true],
    ["https://8.8.8.8", true],
  ])("validates public remote URL shape for %s", (value, expected) => {
    expect(isPublicRemoteURL(value)).toBe(expected);
  });

  it("creates a local Ollama connection without retaining its credential", async () => {
    const api = apiStub();
    render(<AISettingsPage api={api} />);

    await screen.findByRole("button", { name: "Add provider" });
    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    fireEvent.change(screen.getByLabelText("Provider name"), {
      target: { value: "Local Ollama" },
    });
    fireEvent.change(screen.getByLabelText("Provider adapter"), {
      target: { value: "ollama" },
    });
    fireEvent.change(screen.getByLabelText("Network mode"), {
      target: { value: "local" },
    });
    fireEvent.change(screen.getByLabelText("Base URL"), {
      target: { value: "http://127.0.0.1:11434" },
    });
    fireEvent.click(screen.getByLabelText(/allow private-network access/i));
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Initial configuration" },
    });
    fireEvent.change(screen.getByLabelText("Credential"), {
      target: { value: "api.key.synthetic" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save provider" }));

    await waitFor(() =>
      expect(api.createConnection).toHaveBeenCalledWith(
        expect.objectContaining({
          adapter: "ollama",
          networkMode: "local",
          credential: "api.key.synthetic",
          timeoutSeconds: 900,
          requestLimitBytes: 1_048_576,
          responseLimitBytes: 5_242_880,
        }),
        expect.any(AbortSignal),
      ),
    );
    expect(
      screen.queryByDisplayValue("api.key.synthetic"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Provider saved");
  });

  it("explains local acknowledgement and remote public HTTPS requirements", async () => {
    render(<AISettingsPage api={apiStub()} />);
    await screen.findByRole("button", { name: "Add provider" });
    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));

    expect(
      screen.getByText(/Remote providers must use public HTTPS/i),
    ).toBeVisible();
    fireEvent.change(screen.getByLabelText("Network mode"), {
      target: { value: "local" },
    });
    expect(
      screen.getByText("Local network does not necessarily mean this machine."),
    ).toBeVisible();
    expect(
      screen.getByLabelText(/allow private-network access/i),
    ).toBeVisible();
  });

  it("focuses limits that exceed the hard maximum", async () => {
    render(<AISettingsPage api={apiStub()} />);
    await screen.findByRole("button", { name: "Add provider" });
    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    fireEvent.change(screen.getByLabelText("Response limit (MiB)"), {
      target: { value: "11" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save provider" }));

    expect(screen.getByRole("alert")).toHaveTextContent("10 MiB");
    expect(screen.getByLabelText("Response limit (MiB)")).toHaveFocus();
  });

  it("replaces a credential through a write-only dialog and clears it on cancel", async () => {
    const api = apiStub();
    render(<AISettingsPage api={api} />);
    await screen.findByRole("button", { name: "Replace credential" });
    fireEvent.click(screen.getByRole("button", { name: "Replace credential" }));
    fireEvent.change(screen.getByLabelText("New credential"), {
      target: { value: "replacement.synthetic" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.queryByDisplayValue("replacement.synthetic"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Replace credential" }));
    fireEvent.change(screen.getByLabelText("New credential"), {
      target: { value: "replacement.synthetic" },
    });
    fireEvent.change(screen.getByLabelText("Credential replacement reason"), {
      target: { value: "Routine rotation" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save credential" }));
    await waitFor(() =>
      expect(api.replaceCredential).toHaveBeenCalledWith(
        "provider-1",
        expect.objectContaining({
          credential: "replacement.synthetic",
          expectedVersion: 4,
        }),
        expect.any(AbortSignal),
      ),
    );
    expect(
      screen.queryByDisplayValue("replacement.synthetic"),
    ).not.toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Replace credential" }),
      ).toHaveFocus(),
    );
  });

  it("tests a provider and announces the safe result", async () => {
    const api = apiStub();
    render(<AISettingsPage api={api} />);

    await screen.findByRole("button", { name: "Test connection" });
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() =>
      expect(api.testConnection).toHaveBeenCalledWith(
        "provider-1",
        expect.any(AbortSignal),
      ),
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Provider test completed",
    );
  });

  it("keeps discovered models disabled until an administrator enables and saves them", async () => {
    const model = {
      id: "model-1",
      connectionId: "provider-1",
      providerModelId: "llama3",
      displayName: "Llama 3",
      supportedFeatures: ["summary"] as const,
      contextLimit: 8192,
      outputLimit: 2048,
      zeroCost: true,
      inputCostPerMillionMinor: 15,
      outputCostPerMillionMinor: 30,
      enabled: false,
      version: 3,
    };
    let discovered = false;
    const api = apiStub({
      discoverModels: vi.fn().mockImplementation(() => {
        discovered = true;
        return Promise.resolve([model]);
      }),
      listModels: vi
        .fn()
        .mockImplementation(() => Promise.resolve(discovered ? [model] : [])),
    });
    render(<AISettingsPage api={api} />);
    await screen.findByRole("button", { name: "Discover models" });
    fireEvent.change(screen.getByLabelText("Discovery reason"), {
      target: { value: "Check local capacity" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Discover models" }));
    await screen.findByText("llama3");
    expect(screen.getByLabelText("Enable Llama 3")).not.toBeChecked();
    fireEvent.click(screen.getByLabelText("Enable Llama 3"));
    fireEvent.change(screen.getByLabelText("Model change reason"), {
      target: { value: "Approved for summaries" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save models" }));
    await waitFor(() =>
      expect(api.updateModels).toHaveBeenCalledWith(
        "provider-1",
        expect.arrayContaining([
          expect.objectContaining({
            id: "model-1",
            enabled: true,
            displayName: "Llama 3",
            supportedFeatures: ["summary"],
            contextLimit: 8192,
            outputLimit: 2048,
            zeroCost: true,
            inputCostPerMillionMinor: 15,
            outputCostPerMillionMinor: 30,
            expectedVersion: 3,
          }),
        ]),
        "Approved for summaries",
        expect.any(AbortSignal),
      ),
    );
  });

  it("saves feature mappings only from enabled models with cost governance", async () => {
    const api = apiStub({
      listModels: vi.fn().mockResolvedValue([
        {
          id: "model-disabled",
          connectionId: "provider-1",
          providerModelId: "old",
          displayName: "Old",
          supportedFeatures: ["summary"],
          contextLimit: 1,
          outputLimit: 1,
          zeroCost: false,
          enabled: false,
          version: 1,
        },
        {
          id: "model-enabled",
          connectionId: "provider-1",
          providerModelId: "new",
          displayName: "New",
          supportedFeatures: ["summary", "reply_draft"],
          contextLimit: 1,
          outputLimit: 1,
          zeroCost: false,
          inputCostPerMillionMinor: 15,
          outputCostPerMillionMinor: 30,
          enabled: true,
          version: 1,
        },
      ]),
    });
    render(<AISettingsPage api={api} />);
    await screen.findByLabelText("Enable AI assistance");
    expect(
      screen.queryByRole("option", { name: /Old/ }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Allow ticket summaries"));
    fireEvent.change(screen.getByLabelText("Summary model"), {
      target: { value: "model-enabled" },
    });
    fireEvent.change(screen.getByLabelText("Policy reason"), {
      target: { value: "Pilot approval" },
    });
    fireEvent.change(
      screen.getByLabelText("Monthly cost limit (minor units)"),
      { target: { value: "12500" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save policy" }));
    await waitFor(() =>
      expect(api.updatePolicy).toHaveBeenCalledWith(
        expect.objectContaining({
          allowedFeatures: ["summary"],
          summaryModelProfileId: "model-enabled",
          monthlyCostLimitMinor: 12500,
          reason: "Pilot approval",
        }),
        expect.any(AbortSignal),
      ),
    );
  });

  it("preserves unsaved provider values and announces a conflict", async () => {
    const api = apiStub({
      updateConnection: vi.fn().mockRejectedValue(
        Object.assign(new Error("conflict"), {
          code: "version_conflict",
          status: 409,
        }),
      ),
    });
    render(<AISettingsPage api={api} />);
    await screen.findByLabelText("Provider name");
    fireEvent.change(screen.getByLabelText("Provider name"), {
      target: { value: "Edited remote" },
    });
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Rename" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save provider" }));

    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent(
        "changed by someone else",
      ),
    );
    expect(screen.getByLabelText("Provider name")).toHaveValue("Edited remote");
  });

  it("supports keyboard activation and labels every primary control", async () => {
    const user = userEvent.setup();
    render(
      <AISettingsPage
        api={apiStub({ listConnections: vi.fn().mockResolvedValue([]) })}
      />,
    );
    const add = await screen.findByRole("button", { name: "Add provider" });
    add.focus();
    await user.keyboard("{Enter}");
    expect(screen.getByLabelText("Provider name")).toBeVisible();
    expect(screen.getByRole("status")).toBeVisible();
  });

  it("offers MSP-wide policy choices only for enabled models on enabled providers", async () => {
    const enabledElsewhere: ProviderConnection = {
      ...provider,
      id: "provider-2",
      name: "Local models",
    };
    const api = apiStub({
      listConnections: vi.fn().mockResolvedValue([provider, enabledElsewhere]),
      listModels: vi.fn().mockImplementation((id: string) =>
        Promise.resolve(
          id === "provider-1"
            ? []
            : [
                {
                  id: "model-2",
                  connectionId: "provider-2",
                  providerModelId: "remote-enabled",
                  displayName: "Enabled elsewhere",
                  supportedFeatures: ["summary"],
                  contextLimit: 8192,
                  outputLimit: 1024,
                  zeroCost: false,
                  enabled: true,
                  version: 1,
                },
              ],
        ),
      ),
    });
    render(<AISettingsPage api={api} />);

    await screen.findByRole("option", { name: "Enabled elsewhere" });
    fireEvent.click(screen.getByLabelText("Allow ticket summaries"));
    fireEvent.change(screen.getByLabelText("Summary model"), {
      target: { value: "model-2" },
    });
    expect(screen.getByLabelText("Summary model")).toHaveValue("model-2");
  });

  it("offers only zero-cost models for synchronous calendar recommendations", async () => {
    const models = [
      {
        id: "paid",
        connectionId: provider.id,
        providerModelId: "paid",
        displayName: "Paid calendar",
        supportedFeatures: ["calendar_recommendation" as const],
        contextLimit: 8192,
        outputLimit: 1024,
        zeroCost: false,
        enabled: true,
        version: 1,
      },
      {
        id: "free",
        connectionId: provider.id,
        providerModelId: "free",
        displayName: "Free calendar",
        supportedFeatures: ["calendar_recommendation" as const],
        contextLimit: 8192,
        outputLimit: 1024,
        zeroCost: true,
        enabled: true,
        version: 1,
      },
    ];
    render(
      <AISettingsPage
        api={apiStub({ listModels: vi.fn().mockResolvedValue(models) })}
      />,
    );
    await screen.findByRole("option", { name: "Free calendar" });
    expect(
      screen.queryByRole("option", { name: "Paid calendar" }),
    ).not.toBeInTheDocument();
  });

  it("keeps a discovered result with its provider after switching providers", async () => {
    let resolveDiscovery: ((models: never[]) => void) | undefined;
    const second: ProviderConnection = {
      ...provider,
      id: "provider-2",
      name: "Second provider",
    };
    const api = apiStub({
      listConnections: vi.fn().mockResolvedValue([provider, second]),
      discoverModels: vi.fn().mockImplementation(
        () =>
          new Promise<never[]>((resolve) => {
            resolveDiscovery = resolve;
          }),
      ),
      listModels: vi.fn().mockResolvedValue([]),
    });
    render(<AISettingsPage api={api} />);
    await screen.findByRole("button", { name: "Discover models" });
    fireEvent.change(screen.getByLabelText("Discovery reason"), {
      target: { value: "Refresh" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Discover models" }));
    fireEvent.click(screen.getByRole("button", { name: /Second provider/ }));
    resolveDiscovery?.([]);
    await waitFor(() =>
      expect(screen.getByLabelText("Provider name")).toHaveValue(
        "Second provider",
      ),
    );
    expect(screen.queryByText(/discovery completed/i)).not.toBeInTheDocument();
  });

  it("keeps a newer model refresh when the bootstrap request settles later", async () => {
    const staleModel = {
      id: "model-stale",
      connectionId: "provider-1",
      providerModelId: "stale",
      displayName: "Stale model",
      supportedFeatures: ["summary"] as const,
      contextLimit: 8192,
      outputLimit: 1024,
      zeroCost: true,
      enabled: true,
      version: 1,
    };
    const freshModel = {
      ...staleModel,
      id: "model-fresh",
      providerModelId: "fresh",
      displayName: "Fresh model",
    };
    let rejectBootstrap: ((error: Error) => void) | undefined;
    let modelCalls = 0;
    const api = apiStub({
      listModels: vi.fn().mockImplementation(() => {
        modelCalls += 1;
        if (modelCalls === 1) {
          return new Promise((_, reject) => {
            rejectBootstrap = reject;
          });
        }
        return Promise.resolve([freshModel]);
      }),
      discoverModels: vi.fn().mockResolvedValue([freshModel]),
    });
    render(<AISettingsPage api={api} />);
    await screen.findByRole("button", { name: "Discover models" });
    fireEvent.change(screen.getByLabelText("Discovery reason"), {
      target: { value: "Refresh inventory" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Discover models" }));
    await screen.findByText("fresh");

    rejectBootstrap?.(new Error("old bootstrap failed"));
    await waitFor(() => expect(api.listModels).toHaveBeenCalledTimes(2));
    expect(screen.getByText("fresh")).toBeVisible();
    expect(screen.queryByText("stale")).not.toBeInTheDocument();
  });

  it("traps and restores keyboard focus for credential replacement", async () => {
    render(<AISettingsPage api={apiStub()} />);
    const replace = await screen.findByRole("button", {
      name: "Replace credential",
    });
    replace.focus();
    fireEvent.click(replace);
    const credential = screen.getByLabelText("New credential");
    await waitFor(() => expect(credential).toHaveFocus());
    fireEvent.keyDown(credential, { key: "Tab", shiftKey: true });
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("button", { name: "Cancel" }), {
      key: "Tab",
    });
    expect(credential).toHaveFocus();
    fireEvent.keyDown(credential, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(replace).toHaveFocus();
  });

  it("rejects non-HTTPS remote provider URLs before calling the API", async () => {
    const api = apiStub({ listConnections: vi.fn().mockResolvedValue([]) });
    render(<AISettingsPage api={api} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Add provider" }),
    );
    fireEvent.change(screen.getByLabelText("Provider name"), {
      target: { value: "Unsafe remote" },
    });
    fireEvent.change(screen.getByLabelText("Base URL"), {
      target: { value: "http://models.example.test" },
    });
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Test" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save provider" }));
    expect(screen.getByRole("alert")).toHaveTextContent("public HTTPS");
    expect(api.createConnection).not.toHaveBeenCalled();
  });

  it("shows provider operational details and lets an unavailable model list retry", async () => {
    const detailed = {
      ...provider,
      lastSucceededAt: "2026-07-30T12:01:00Z",
      lastErrorCode: "timeout",
    };
    const api = apiStub({
      listConnections: vi.fn().mockResolvedValue([detailed]),
      listModels: vi
        .fn()
        .mockRejectedValueOnce(new Error("offline"))
        .mockResolvedValueOnce([]),
    });
    render(<AISettingsPage api={api} />);

    await screen.findByRole("button", { name: "Retry models" });
    expect(screen.getByText(/Safe error: timeout/)).toBeVisible();
    expect(screen.getByText(/Last successful test/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry models" }));
    await waitFor(() => expect(api.listModels).toHaveBeenCalledTimes(2));
  });
});
