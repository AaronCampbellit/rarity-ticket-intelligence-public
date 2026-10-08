import { createRoot } from "react-dom/client";

import { App, type BuildInfo } from "./App";
import { createSessionAwareFetch } from "./api/sessionExpiry";
import { Button, StatePanel } from "./design-system";
import "./design-system/foundations/tokens.css";
import "./design-system/foundations/reset.css";

window.fetch = createSessionAwareFetch(window.fetch.bind(window));

async function loadBuild(): Promise<BuildInfo> {
  const response = await fetch("/v1/system/build");
  const isLocalDevelopment = ["127.0.0.1", "localhost"].includes(
    window.location.hostname,
  );
  const isJSON = response.headers
    .get("content-type")
    ?.includes("application/json");

  if (response.ok && isJSON) {
    return (await response.json()) as BuildInfo;
  }
  if (isLocalDevelopment) {
    return {
      revision: "local-vite",
    };
  }
  throw new Error("Build information is unavailable.");
}

const root = createRoot(document.getElementById("root")!);

loadBuild()
  .then((build) => root.render(<App build={build} />))
  .catch(() =>
    root.render(
      <main className="rti-environment-state">
        <StatePanel
          state="error"
          title="Rarity environment unavailable"
          description="Build information could not be loaded."
          action={
            <Button intent="primary" onClick={() => window.location.reload()}>
              Retry
            </Button>
          }
          supportCode="BUILD-INFO-UNAVAILABLE"
        />
      </main>,
    ),
  );
