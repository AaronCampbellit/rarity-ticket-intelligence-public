import { useRef, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { Button, Notice } from "../../design-system";

export type ReadinessState =
  | "not_started"
  | "action_required"
  | "ready_to_verify"
  | "verified"
  | "attention";

type Section = {
  key: string;
  label: string;
  state: ReadinessState;
  summary: string;
  remediation_href?: string;
};

export type GuidedCenterStatus = {
  msp_id: string;
  public_url: string;
  completed_at: string;
  configuration_version: number;
  intake_status: {
    graph_configured: boolean;
    api_key_configured: boolean;
    forwarding_configured: boolean;
  };
  object_storage_status: {
    provider: string;
    endpoint: string;
    bucket: string;
    region: string;
    access_key_configured: boolean;
    credential_configured: boolean;
  };
  backup_status: { evidence_key_configured: boolean };
  sections: Section[];
};

const labels: Record<ReadinessState, string> = {
  not_started: "Not started",
  action_required: "Action required",
  ready_to_verify: "Ready to verify",
  verified: "Verified",
  attention: "Attention",
};

export function GuidedSetupCenter({ center }: { center: GuidedCenterStatus }) {
  const [open, setOpen] = useState<string | null>(null);
  const [current, setCurrent] = useState(center);
  const panelHeading = useRef<HTMLHeadingElement>(null);

  function openPanel(key: string) {
    setOpen(key);
    window.setTimeout(() => panelHeading.current?.focus(), 0);
  }

  return (
    <>
      <div className="setup-health-grid">
        {current.sections.map((section) => (
          <article key={section.key}>
            <span className={`setup-state setup-${section.state}`}>
              {labels[section.state]}
            </span>
            <h3>{section.label}</h3>
            <p>{section.summary}</p>
            {["intake", "object_storage", "backups"].includes(section.key) ? (
              <Button onClick={() => openPanel(section.key)}>
                {section.state === "ready_to_verify" ? "Continue" : "Start"}{" "}
                {section.label} setup
              </Button>
            ) : section.remediation_href ? (
              <a href={section.remediation_href}>Review and remediate</a>
            ) : null}
          </article>
        ))}
      </div>
      {open ? (
        <section
          className="setup-guided-panel"
          role="region"
          aria-labelledby={`setup-${open}-title`}
        >
          {open === "intake" ? (
            <IntakePanel
              headingRef={panelHeading}
              status={center.intake_status}
            />
          ) : null}
          {open === "object_storage" ? (
            <StoragePanel
              headingRef={panelHeading}
              center={current}
              onUpdated={setCurrent}
            />
          ) : null}
          {open === "backups" ? (
            <BackupPanel
              headingRef={panelHeading}
              center={current}
              configured={current.backup_status.evidence_key_configured}
            />
          ) : null}
          <Button type="button" onClick={() => setOpen(null)}>
            Close{" "}
            {current.sections.find((section) => section.key === open)?.label}{" "}
            setup
          </Button>
        </section>
      ) : null}
    </>
  );
}

function IntakePanel({
  headingRef,
  status,
}: {
  headingRef: React.RefObject<HTMLHeadingElement | null>;
  status: GuidedCenterStatus["intake_status"];
}) {
  return (
    <>
      <h3 id="setup-intake-title" ref={headingRef} tabIndex={-1}>
        Mailbox and API intake setup
      </h3>
      <p>
        Choose how requests reach Rarity. You can configure more than one path.
      </p>
      <div className="setup-guidance-options">
        <article>
          <h4>Microsoft 365 mailbox</h4>
          <p>
            Connect a support mailbox using an Entra application. Rarity stores
            its client secret through the protected, write-only credential flow.
          </p>
          <p>
            Status: {status.graph_configured ? "Configured" : "Not configured"}
          </p>
          <a href="#/graph-settings">Set up a Microsoft 365 mailbox</a>
        </article>
        <article>
          <h4>Service API</h4>
          <p>
            Create a scoped key for a trusted system that will submit work
            directly to Rarity.
          </p>
          <p>
            Status:{" "}
            {status.api_key_configured ? "Configured" : "Not configured"}
          </p>
          <a href="#/service-keys">Create a service API key</a>
        </article>
        <article>
          <h4>Email forwarding</h4>
          <p>
            Forward messages from an existing mail system and restrict accepted
            sender domains.
          </p>
          <p>
            Status:{" "}
            {status.forwarding_configured ? "Configured" : "Not configured"}
          </p>
          <a href="#/forwarding-settings">Set up email forwarding</a>
        </article>
      </div>
    </>
  );
}

function StoragePanel({
  headingRef,
  center,
  onUpdated,
}: {
  headingRef: React.RefObject<HTMLHeadingElement | null>;
  center: GuidedCenterStatus;
  onUpdated: (center: GuidedCenterStatus) => void;
}) {
  const status = center.object_storage_status;
  const [reason, setReason] = useState("");
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState("");

  async function verify() {
    setTesting(true);
    setResult("");
    try {
      const response = await fetch(
        "/api/v1/setup/center/object-storage/verify",
        {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            expected_version: center.configuration_version,
            reason,
          }),
        },
      );
      if (!response.ok) throw new Error("storage_verification_failed");
      const updated = (await response.json()) as GuidedCenterStatus;
      onUpdated(updated);
      setResult("Object storage verification completed.");
    } catch {
      setResult(
        "The storage test could not be completed. Verify the deployment values and bucket permissions, then retry.",
      );
    } finally {
      setTesting(false);
    }
  }

  return (
    <>
      <h3 id="setup-object_storage-title" ref={headingRef} tabIndex={-1}>
        Object storage setup
      </h3>
      <p>
        Rarity uses a dedicated S3-compatible bucket for attachments and large
        inbound payloads. These values are read from the running deployment.
      </p>
      <dl className="setup-effective-values">
        <div>
          <dt>Provider</dt>
          <dd>{status.provider || "Not detected"}</dd>
        </div>
        <div>
          <dt>Endpoint</dt>
          <dd>{status.endpoint || "Missing"}</dd>
        </div>
        <div>
          <dt>Bucket</dt>
          <dd>{status.bucket || "Missing"}</dd>
        </div>
        <div>
          <dt>Region</dt>
          <dd>{status.region || "Missing"}</dd>
        </div>
        <div>
          <dt>Access key</dt>
          <dd>{status.access_key_configured ? "Configured" : "Missing"}</dd>
        </div>
        <div>
          <dt>Secret key</dt>
          <dd>
            {status.credential_configured ? "Configured (hidden)" : "Missing"}
          </dd>
        </div>
      </dl>
      <Notice tone="warning" title="Deployment-managed settings">
        Set S3_ENDPOINT, S3_BUCKET, S3_REGION, S3_ACCESS_KEY_ID, and
        S3_SECRET_ACCESS_KEY in the deployment environment, then restart the
        Rarity API. Use a credential limited to this bucket.
      </Notice>
      <label className="setup-test-reason">
        <span>Reason for storage test</span>
        <input
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder="Why this verification is being run"
        />
      </label>
      <Button
        type="button"
        intent="primary"
        disabled={testing || reason.trim() === ""}
        onClick={() => void verify()}
      >
        {testing ? "Testing object storage…" : "Test object storage"}
      </Button>
      {result ? <p role="status">{result}</p> : null}
    </>
  );
}

function BackupPanel({
  headingRef,
  center,
  configured,
}: {
  headingRef: React.RefObject<HTMLHeadingElement | null>;
  center: GuidedCenterStatus;
  configured: boolean;
}) {
  const evidenceCommand = [
    "go run ./backend/cmd/rarity-backup-evidence",
    "--pgbackrest-evidence ./pgbackrest-evidence",
    "--restore-evidence ./restore-evidence",
    `--msp-id ${center.msp_id}`,
    `--api-url ${center.public_url}`,
    `--expected-version ${center.configuration_version}`,
  ].join(" ");

  return (
    <>
      <h3 id="setup-backups-title" ref={headingRef} tabIndex={-1}>
        Backup and PITR setup
      </h3>
      <p>
        Store pgBackRest backups and WAL archives in a separate failure domain
        from the live PostgreSQL server.
      </p>
      <ol>
        <li>Configure the remote repository endpoint, bucket, and region.</li>
        <li>
          Configure a dedicated repository credential and cipher passphrase.
        </li>
        <li>
          Enable WAL archiving and install the documented backup schedule.
        </li>
        <li>Run a restore verification and submit its evidence.</li>
      </ol>
      <p>
        Verification requires that the latest backup is less than 24 hours old,
        WAL evidence is less than 15 minutes old, and restore proof is less than
        31 days old.
      </p>
      <p>Evidence authority: {configured ? "Configured" : "Not configured"}</p>
      <p>
        After running pgBackRest and a clean restore test, submit the evidence:
      </p>
      <code className="setup-command">{evidenceCommand}</code>
    </>
  );
}
