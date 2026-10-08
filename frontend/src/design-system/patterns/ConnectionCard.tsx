import type { ReactNode } from "react";

import { Panel } from "../components/containers/Panel";
import {
  StatusBadge,
  type FeedbackTone,
} from "../components/feedback/StatusBadge";

export type ConnectionHealth = "unknown" | "healthy" | "degraded" | "failed";

const healthTone: Record<ConnectionHealth, FeedbackTone> = {
  unknown: "neutral",
  healthy: "success",
  degraded: "warning",
  failed: "danger",
};

export function ConnectionCard({
  name,
  providerType,
  endpoint,
  enabled,
  health,
  credentialConfigured,
  credentialRequired = true,
  lastCheckedAt,
  actions,
}: {
  name: string;
  providerType: string;
  endpoint?: string;
  enabled: boolean;
  health: ConnectionHealth;
  credentialConfigured: boolean;
  credentialRequired?: boolean;
  lastCheckedAt?: string;
  actions: ReactNode;
}) {
  const credentialState = !credentialRequired
    ? "Credential not required"
    : credentialConfigured
      ? "Credential configured"
      : "Credential not configured";

  return (
    <Panel title={name} actions={actions}>
      <div className="rti-connection">
        <div className="rti-connection__badges">
          <StatusBadge>{providerType}</StatusBadge>
          <StatusBadge tone={enabled ? "success" : "neutral"}>
            {enabled ? "Enabled" : "Disabled"}
          </StatusBadge>
          <StatusBadge tone={healthTone[health]}>{health}</StatusBadge>
        </div>
        <dl>
          {endpoint ? (
            <>
              <dt>Endpoint</dt>
              <dd>
                <code>{endpoint}</code>
              </dd>
            </>
          ) : null}
          <dt>Credential</dt>
          <dd>{credentialState}</dd>
          {lastCheckedAt ? (
            <>
              <dt>Last checked</dt>
              <dd>{lastCheckedAt}</dd>
            </>
          ) : null}
        </dl>
      </div>
    </Panel>
  );
}
