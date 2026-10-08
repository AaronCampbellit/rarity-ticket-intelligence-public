import type { DirectoryClient } from "../../api/browserSession";
import type { AIWorkspaceProposal } from "./types";

export function AIProposal({
  proposal,
  targetClient,
  busy,
  onConfirm,
  onReject,
}: {
  proposal: AIWorkspaceProposal;
  targetClient?: DirectoryClient;
  busy: boolean;
  onConfirm: () => void;
  onReject: () => void;
}) {
  const changes = Object.entries(proposal.preview.changes ?? {});
  const targetIdentity = proposalTargetIdentity(proposal, targetClient);
  const locationIdentity = proposalLocationIdentity(proposal);
  const locationReference = proposal.preview.changes?.location?.after;
  const unresolvedTargetID =
    proposal.target_client_id || proposal.preview.target_id;
  return (
    <section className="ai-proposal" aria-label="AI action preview">
      <div>
        <p className="ai-workspace__eyebrow">Approval required</p>
        <h3>{proposal.preview.summary}</h3>
        <p>
          Review the exact changes below. Rarity will check your current
          permissions and target state again before it runs.
        </p>
      </div>
      {targetIdentity ? (
        <div className="ai-proposal__target">
          <strong>{targetIdentity.label}</strong>
          <span>{targetIdentity.value}</span>
        </div>
      ) : (
        <div
          className="ai-proposal__target ai-proposal__target--unavailable"
          role="alert"
          aria-label="Target client unavailable"
        >
          <strong>Target client unavailable</strong>
          <span>{unresolvedTargetID}</span>
          <small>
            Refresh the authorized Client directory before confirming.
          </small>
        </div>
      )}
      {locationIdentity ? (
        <div className="ai-proposal__target">
          <strong>{locationIdentity.label}</strong>
          <span>{locationIdentity.value}</span>
        </div>
      ) : typeof locationReference === "string" && locationReference ? (
        <div
          className="ai-proposal__target ai-proposal__target--unavailable"
          role="alert"
          aria-label="Target location unavailable"
        >
          <strong>Target location unavailable</strong>
          <span>{locationReference}</span>
          <small>Refresh the active Location list before confirming.</small>
        </div>
      ) : null}
      {changes.length ? (
        <dl className="ai-proposal__changes">
          {changes.map(([field, change]) => (
            <div className="ai-proposal__change" key={field}>
              <dt>
                <strong>{field.replaceAll("_", " ")}</strong>
              </dt>
              <dd>
                <DisplayValue label={field} value={change.before} />
              </dd>
              <dd className="ai-proposal__after">
                <span aria-hidden="true">→</span>
                <div>
                  <DisplayValue label={field} value={change.after} />
                </div>
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      <div className="ai-proposal__actions">
        <button
          type="button"
          disabled={
            busy ||
            !targetIdentity ||
            (Boolean(locationReference) && !locationIdentity)
          }
          onClick={onConfirm}
          aria-label={`Confirm ${proposal.preview.summary}`}
        >
          Confirm action
        </button>
        <button type="button" disabled={busy} onClick={onReject}>
          Reject
        </button>
      </div>
    </section>
  );
}

export function proposalLocationIdentity(
  proposal: AIWorkspaceProposal,
): { label: string; value: string } | undefined {
  const reference = proposal.preview.changes?.location?.after;
  const targetLocation = proposal.preview.location_target;
  if (
    typeof reference !== "string" ||
    !reference ||
    !targetLocation ||
    typeof targetLocation.id !== "string" ||
    !targetLocation.id.trim() ||
    typeof targetLocation.client_id !== "string" ||
    !targetLocation.client_id.trim() ||
    targetLocation.kind !== "location" ||
    targetLocation.client_id !== proposal.target_client_id ||
    typeof targetLocation.display_id !== "string" ||
    targetLocation.display_id !== reference ||
    typeof targetLocation.name !== "string" ||
    !targetLocation.name.trim()
  ) {
    return undefined;
  }
  return {
    label: "Target location",
    value: `${targetLocation.name} (${targetLocation.display_id})`,
  };
}

export function proposalTargetIdentity(
  proposal: AIWorkspaceProposal,
  targetClient?: DirectoryClient,
): { label: string; value: string } | undefined {
  if (proposal.tool_name === "client.create") {
    return newClientIdentity(proposal);
  }
  if (proposal.tool_name === "prospect.create") {
    return newProspectIdentity(proposal);
  }
  if (
    !proposal.target_client_id ||
    targetClient?.id !== proposal.target_client_id
  ) {
    return undefined;
  }
  return {
    label: "Target client",
    value: `${targetClient.name} (${targetClient.display_id})`,
  };
}

function newClientIdentity(
  proposal: AIWorkspaceProposal,
): { label: string; value: string } | undefined {
  if (proposal.tool_name !== "client.create") return undefined;
  const name = proposal.preview.changes?.name?.after;
  const displayID = proposal.preview.changes?.display_id?.after;
  if (typeof name !== "string" || typeof displayID !== "string") {
    return undefined;
  }
  return { label: "New client", value: `${name} (${displayID})` };
}

function newProspectIdentity(
  proposal: AIWorkspaceProposal,
): { label: string; value: string } | undefined {
  if (proposal.tool_name !== "prospect.create") return undefined;
  const name = proposal.preview.changes?.name?.after;
  const displayID = proposal.preview.changes?.display_id?.after;
  if (typeof name !== "string" || typeof displayID !== "string") {
    return undefined;
  }
  return { label: "New prospect", value: `${name} (${displayID})` };
}

function DisplayValue({ label, value }: { label: string; value: unknown }) {
  if (value === null || value === undefined || value === "") {
    return <span>Not set</span>;
  }
  if (Array.isArray(value)) {
    return (
      <ul
        className="ai-proposal__nested-list"
        aria-label={`${plainLabel(label)} values`}
      >
        {value.map((item, index) => (
          <li key={index}>
            <DisplayValue label={`${label} ${index + 1}`} value={item} />
          </li>
        ))}
      </ul>
    );
  }
  if (typeof value === "object") {
    return (
      <dl
        className="ai-proposal__nested-data"
        role="group"
        aria-label={`${plainLabel(label)} details`}
      >
        {Object.entries(value).map(([field, item]) => (
          <div key={field}>
            <dt>{plainLabel(field)}</dt>
            <dd>
              <DisplayValue label={`${label} ${field}`} value={item} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return <span>{String(value)}</span>;
}

function plainLabel(value: string): string {
  const text = value
    .split("_")
    .map((part) => (part === "id" ? "ID" : part))
    .join(" ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}
