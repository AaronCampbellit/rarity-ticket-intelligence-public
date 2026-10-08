import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Button } from "../components/actions/Button";
import { ApprovalDecision } from "./ApprovalDecision";
import { AuditEvidence } from "./AuditEvidence";
import { ConnectionCard } from "./ConnectionCard";
import { DurableJobProgress } from "./DurableJobProgress";

afterEach(cleanup);

describe("DurableJobProgress", () => {
  it("keeps queued and running work distinct from final failure", () => {
    render(
      <DurableJobProgress
        state="running"
        label="Discovering models"
        startedAt="Aug 1, 2026, 5:20 PM"
        progress={{ completed: 3, total: 8 }}
        actions={<Button>Cancel discovery</Button>}
      />,
    );
    expect(screen.getByText("Running")).toBeVisible();
    expect(screen.getByText("3 of 8 complete")).toBeVisible();
    expect(screen.queryByText("Failed")).not.toBeInTheDocument();
  });
});

describe("ConnectionCard", () => {
  it("renders a local Ollama connection without requiring a credential", () => {
    render(
      <ConnectionCard
        name="Local inference"
        providerType="Ollama"
        endpoint="http://host.docker.internal:11434"
        enabled
        health="healthy"
        credentialConfigured={false}
        credentialRequired={false}
        actions={
          <>
            <Button>Test connection</Button>
            <Button>Discover models</Button>
          </>
        }
      />,
    );

    expect(screen.getByText("Ollama")).toBeVisible();
    expect(screen.getByText("http://host.docker.internal:11434")).toBeVisible();
    expect(screen.getByText("Credential not required")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Test connection" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Discover models" }),
    ).toBeVisible();
  });

  it("represents a hosted secret only by configured state", () => {
    const { container } = render(
      <ConnectionCard
        name="Hosted provider"
        providerType="OpenAI-compatible"
        endpoint="https://models.example.com/v1"
        enabled
        health="unknown"
        credentialConfigured
        credentialRequired
        actions={<Button>Replace credential</Button>}
      />,
    );
    expect(screen.getByText("Credential configured")).toBeVisible();
    expect(container.textContent).not.toContain("sk-");
  });
});

describe("approval and audit evidence", () => {
  it("marks override evidence distinctly", () => {
    render(
      <ApprovalDecision
        actor="Alex Morgan"
        decision="Approved"
        reason="Emergency security remediation"
        decidedAt="Aug 1, 2026, 5:25 PM"
        override
      />,
    );
    expect(screen.getByText("Override")).toBeVisible();
    expect(screen.getByText("Emergency security remediation")).toBeVisible();
  });

  it("preserves correlation and version identifiers", () => {
    render(
      <AuditEvidence
        entries={[
          { label: "Correlation ID", value: "corr-1042" },
          { label: "Version", value: "7" },
        ]}
      />,
    );
    expect(screen.getByText("corr-1042")).toBeVisible();
    expect(screen.getByText("7")).toBeVisible();
  });
});
