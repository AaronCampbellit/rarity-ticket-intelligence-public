import type { ReactNode } from "react";

import type { DirectoryClient } from "../../api/browserSession";
import type { ClientResourceKind } from "./types";
import type { StructuredActionController } from "./useStructuredActionController";
import {
  catalogGroups,
  isClientResourceAction,
  resourceKinds,
  type WorkspaceAction,
} from "./workspaceCatalog";

export function StructuredActionForm({
  controller,
  clients,
  busy,
  clientResourceCreateFields,
  clientResourceUpdateFields,
  onCancel,
}: {
  controller: StructuredActionController;
  clients: DirectoryClient[];
  busy: boolean;
  clientResourceCreateFields: ReactNode;
  clientResourceUpdateFields: ReactNode;
  onCancel: () => void;
}) {
  const {
    action,
    actionIsRead,
    actionNeedsClient,
    activityDetails,
    activityKind,
    activitySummary,
    assetExternalIdentityIncomplete,
    availableCatalog,
    availableResourceKinds,
    contractReference,
    limit,
    opportunityStage,
    pipelineID,
    projectID,
    projectTasks,
    projectTitle,
    proposalOpportunityID,
    proposalState,
    query,
    queueReference,
    reason,
    reference,
    resourceKind,
    resourceReference,
    reviewAction,
    selectAction,
    selectedCatalogAction,
    serviceReference,
    selectResourceKind,
    selectTargetClient,
    setActivityDetails,
    setActivityKind,
    setActivitySummary,
    setContractReference,
    setLimit,
    setOpportunityStage,
    setPipelineID,
    setProjectID,
    setProjectTasks,
    setProjectTitle,
    setProposalOpportunityID,
    setProposalState,
    setQuery,
    setQueueReference,
    setReason,
    setReference,
    setResourceReference,
    setServiceReference,
    setStageID,
    setStructuredBody,
    setStructuredDisplayID,
    setStructuredEmail,
    setStructuredName,
    setStructuredPhone,
    setTechnicianReference,
    setTicketID,
    setTicketPriority,
    setTicketStatus,
    setTicketType,
    setValue,
    setVersion,
    structuredBody,
    structuredDisplayID,
    structuredEmail,
    structuredName,
    structuredPhone,
    targetClientID,
    technicianReference,
    ticketID,
    ticketPriority,
    ticketStatus,
    ticketType,
    value,
    version,
    stageID,
  } = controller;

  return (
    <form className="ai-workspace__action-form" onSubmit={reviewAction}>
      <h3>Prepare an action</h3>
      <label>
        Action
        <select
          value={action}
          onChange={(event) =>
            selectAction(event.target.value as WorkspaceAction)
          }
        >
          {catalogGroups.map((group) => {
            const actions = availableCatalog.filter(
              (item) => item.group === group,
            );
            return actions.length ? (
              <optgroup key={group} label={group}>
                {actions.map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </optgroup>
            ) : null;
          })}
        </select>
      </label>

      {actionNeedsClient ? (
        <label>
          Target client
          <select
            value={targetClientID}
            onChange={(event) => selectTargetClient(event.target.value)}
            required
            disabled={!clients.length}
          >
            {!clients.length ? (
              <option value="">No authorized clients available</option>
            ) : null}
            {clients.map((client) => (
              <option key={client.id} value={client.id}>
                {client.name} ({client.display_id})
              </option>
            ))}
          </select>
        </label>
      ) : null}
      {actionNeedsClient && !clients.length ? (
        <p className="ai-workspace__directory-guidance">
          No authorized clients are available for structured actions. You can
          continue using chat.
        </p>
      ) : null}

      {isClientResourceAction(action) ? (
        <label>
          Resource kind
          <select
            value={resourceKind}
            onChange={(event) =>
              selectResourceKind(event.target.value as ClientResourceKind)
            }
            required
          >
            {(action === "client_resource_list"
              ? resourceKinds
              : availableResourceKinds
            ).map(({ kind, label }) => (
              <option key={kind} value={kind}>
                {label}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      {action === "client_resource_add" ? clientResourceCreateFields : null}
      {action === "client_resource_update" ||
      action === "client_resource_deactivate" ||
      action === "client_resource_reactivate" ? (
        <label>
          Resource
          <input
            value={resourceReference}
            onChange={(event) => setResourceReference(event.target.value)}
            placeholder="Exact display ID or name"
            required
          />
        </label>
      ) : null}
      {action === "client_resource_update" ? clientResourceUpdateFields : null}

      {action === "ticket_get" ? (
        <label>
          Ticket ID
          <input
            value={reference}
            onChange={(event) => setReference(event.target.value)}
            required
          />
        </label>
      ) : null}
      {action === "ticket_search" || action === "knowledge_search" ? (
        <label>
          Search query
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            required={action === "ticket_search"}
          />
        </label>
      ) : null}
      {action === "project_search" ||
      action === "ticket_search" ||
      action === "knowledge_search" ||
      action === "prospect_list" ||
      action === "opportunity_list" ||
      action === "proposal_list" ? (
        <label>
          Result limit
          <input
            type="number"
            min="1"
            max={action === "ticket_search" ? "20" : "50"}
            value={limit}
            onChange={(event) => setLimit(event.target.value)}
            required
          />
        </label>
      ) : null}
      {action === "project_get" || action === "knowledge_get" ? (
        <label>
          {action === "project_get" ? "Project" : "Knowledge article"}
          <input
            value={reference}
            onChange={(event) => setReference(event.target.value)}
            placeholder="Exact display ID or name"
            required
          />
        </label>
      ) : null}
      {action === "opportunity_list" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Opportunity filters</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Pipeline ID
              <input
                value={pipelineID}
                onChange={(event) => setPipelineID(event.target.value)}
              />
            </label>
            <label>
              Stage ID
              <input
                value={stageID}
                onChange={(event) => setStageID(event.target.value)}
              />
            </label>
          </div>
        </fieldset>
      ) : null}
      {action === "opportunity_get" ? (
        <label>
          Opportunity
          <input
            value={reference}
            onChange={(event) => setReference(event.target.value)}
            placeholder="Exact display ID or name"
            required
          />
        </label>
      ) : null}
      {action === "proposal_list" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Proposal filters</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Proposal state
              <select
                value={proposalState}
                onChange={(event) =>
                  setProposalState(
                    event.target.value as "" | "draft" | "issued" | "accepted",
                  )
                }
              >
                <option value="">All states</option>
                <option value="draft">Draft</option>
                <option value="issued">Issued</option>
                <option value="accepted">Accepted</option>
              </select>
            </label>
            <label>
              Opportunity ID
              <input
                value={proposalOpportunityID}
                onChange={(event) =>
                  setProposalOpportunityID(event.target.value)
                }
              />
            </label>
          </div>
        </fieldset>
      ) : null}
      {action === "proposal_get" ? (
        <label>
          Proposal
          <input
            value={reference}
            onChange={(event) => setReference(event.target.value)}
            placeholder="Exact display ID"
            required
          />
        </label>
      ) : null}

      {action === "project" ? (
        <>
          <label>
            Project title
            <input
              value={projectTitle}
              onChange={(event) => setProjectTitle(event.target.value)}
              required
            />
          </label>
          <label htmlFor="ai-workspace-project-tasks">
            Project tasks
            <textarea
              id="ai-workspace-project-tasks"
              aria-describedby="ai-workspace-project-tasks-help"
              value={projectTasks}
              onChange={(event) => setProjectTasks(event.target.value)}
              placeholder="Enter one task per line"
              required
            />
          </label>
          <small id="ai-workspace-project-tasks-help">
            Enter only the tasks the user requested, one per line.
          </small>
        </>
      ) : null}
      {action === "task" ? (
        <>
          <div className="ai-workspace__action-grid">
            <label>
              Project ID
              <input
                value={projectID}
                onChange={(event) => setProjectID(event.target.value)}
                required
              />
            </label>
            <label>
              Project reference
              <input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="Exact display ID or name"
                required
              />
            </label>
          </div>
          <label>
            Task title
            <input
              value={structuredName}
              onChange={(event) => setStructuredName(event.target.value)}
              required
            />
          </label>
        </>
      ) : null}
      {action === "client" ? (
        <div className="ai-workspace__action-grid">
          <label>
            Display ID
            <input
              value={structuredDisplayID}
              onChange={(event) => setStructuredDisplayID(event.target.value)}
              required
            />
          </label>
          <label>
            Client name
            <input
              value={structuredName}
              onChange={(event) => setStructuredName(event.target.value)}
              required
            />
          </label>
        </div>
      ) : null}
      {action === "ticket_create" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Ticket details</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Ticket display ID
              <input
                value={structuredDisplayID}
                onChange={(event) => setStructuredDisplayID(event.target.value)}
                required
              />
            </label>
            <label>
              Ticket type
              <select
                value={ticketType}
                onChange={(event) =>
                  setTicketType(
                    event.target.value as
                      "" | "incident" | "request" | "change" | "problem",
                  )
                }
                required
              >
                <option value="">Select type</option>
                <option value="incident">Incident</option>
                <option value="request">Request</option>
                <option value="change">Change</option>
                <option value="problem">Problem</option>
              </select>
            </label>
          </div>
          <label>
            Ticket title
            <input
              value={structuredName}
              onChange={(event) => setStructuredName(event.target.value)}
              required
            />
          </label>
          <label>
            Ticket description
            <textarea
              value={structuredBody}
              onChange={(event) => setStructuredBody(event.target.value)}
              required
            />
          </label>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Ticket status
              <input
                value={ticketStatus}
                onChange={(event) => setTicketStatus(event.target.value)}
                required
              />
            </label>
            <label>
              Ticket priority
              <select
                value={ticketPriority}
                onChange={(event) =>
                  setTicketPriority(
                    event.target.value as
                      "" | "low" | "normal" | "high" | "critical",
                  )
                }
                required
              >
                <option value="">Select priority</option>
                <option value="low">Low</option>
                <option value="normal">Normal</option>
                <option value="high">High</option>
                <option value="critical">Critical</option>
              </select>
            </label>
          </div>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Service
              <input
                value={serviceReference}
                onChange={(event) => setServiceReference(event.target.value)}
                placeholder="Optional exact display ID or name"
              />
            </label>
            <label>
              Contract
              <input
                value={contractReference}
                onChange={(event) => setContractReference(event.target.value)}
                placeholder="Optional exact display ID or name"
              />
            </label>
          </div>
        </fieldset>
      ) : null}
      {action === "ticket_assign" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Ticket assignment</legend>
          <TicketIdentityFields
            ticketID={ticketID}
            version={version}
            onTicketID={setTicketID}
            onVersion={setVersion}
          />
          <label>
            Technician
            <input
              value={technicianReference}
              onChange={(event) => setTechnicianReference(event.target.value)}
              placeholder="Exact display name or workforce email"
              required
            />
          </label>
          <label>
            Assignment reason
            <input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              required
            />
          </label>
        </fieldset>
      ) : null}
      {action === "ticket_route" ? (
        <>
          <TicketIdentityFields
            ticketID={ticketID}
            version={version}
            onTicketID={setTicketID}
            onVersion={setVersion}
          />
          <label>
            Queue
            <input
              value={queueReference}
              onChange={(event) => setQueueReference(event.target.value)}
              placeholder="Exact queue key or name"
              required
            />
          </label>
        </>
      ) : null}
      {action === "opportunity_transition" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Opportunity transition</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Opportunity
              <input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="Exact display ID or name"
                required
              />
            </label>
            <label>
              Current version
              <input
                type="number"
                min="1"
                value={version}
                onChange={(event) => setVersion(event.target.value)}
                required
              />
            </label>
          </div>
          <label>
            Destination stage
            <input
              value={opportunityStage}
              onChange={(event) => setOpportunityStage(event.target.value)}
              placeholder="Exact stage key or name"
              required
            />
          </label>
          <label>
            Transition reason
            <input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              required
            />
          </label>
        </fieldset>
      ) : null}
      {action === "opportunity_activity_create" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Opportunity activity</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Opportunity
              <input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="Exact display ID or name"
                required
              />
            </label>
            <label>
              Activity kind
              <select
                value={activityKind}
                onChange={(event) =>
                  setActivityKind(
                    event.target.value as
                      "" | "note" | "call" | "email" | "meeting",
                  )
                }
                required
              >
                <option value="">Select kind</option>
                <option value="note">Note</option>
                <option value="call">Call</option>
                <option value="email">Email</option>
                <option value="meeting">Meeting</option>
              </select>
            </label>
          </div>
          <label>
            Activity summary
            <input
              value={activitySummary}
              onChange={(event) => setActivitySummary(event.target.value)}
              required
            />
          </label>
          <label>
            Activity details
            <textarea
              value={activityDetails}
              onChange={(event) => setActivityDetails(event.target.value)}
              required
            />
          </label>
          <div className="ai-workspace__action-timing">
            <strong>Time</strong>
            <span>Recorded when confirmed</span>
          </div>
        </fieldset>
      ) : null}
      {action === "proposal_create" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Proposal draft</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Opportunity
              <input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="Exact display ID or name"
                required
              />
            </label>
            <label>
              Proposal display ID
              <input
                value={structuredDisplayID}
                onChange={(event) => setStructuredDisplayID(event.target.value)}
                required
              />
            </label>
          </div>
        </fieldset>
      ) : null}

      {action === "knowledge_create" || action === "knowledge_revise" ? (
        <>
          <div className="ai-workspace__action-grid">
            {action === "knowledge_create" ? (
              <label>
                Display ID
                <input
                  value={structuredDisplayID}
                  onChange={(event) =>
                    setStructuredDisplayID(event.target.value)
                  }
                  required
                />
              </label>
            ) : (
              <label>
                Knowledge article
                <input
                  value={reference}
                  onChange={(event) => setReference(event.target.value)}
                  placeholder="Exact display ID or title"
                  required
                />
              </label>
            )}
            {action === "knowledge_revise" ? (
              <label>
                Current version
                <input
                  type="number"
                  min="1"
                  value={version}
                  onChange={(event) => setVersion(event.target.value)}
                  required
                />
              </label>
            ) : null}
          </div>
          <label>
            Article title
            <input
              value={structuredName}
              onChange={(event) => setStructuredName(event.target.value)}
              required
            />
          </label>
          <label>
            Article body
            <textarea
              value={structuredBody}
              onChange={(event) => setStructuredBody(event.target.value)}
              required
            />
          </label>
        </>
      ) : null}
      {action === "knowledge_publish" ? (
        <fieldset className="ai-workspace__action-region">
          <legend>Internal publication</legend>
          <div className="ai-workspace__action-grid ai-workspace__action-grid--wide">
            <label>
              Knowledge article
              <input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="Exact display ID or title"
                required
              />
            </label>
            <label>
              Current version
              <input
                type="number"
                min="1"
                value={version}
                onChange={(event) => setVersion(event.target.value)}
                required
              />
            </label>
          </div>
          <label>
            Publication reason
            <input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              required
            />
          </label>
          <small>Internal only; no client-visible or external delivery.</small>
        </fieldset>
      ) : null}

      {action === "prospect_create" ? (
        <>
          <div className="ai-workspace__action-grid">
            <label>
              Display ID
              <input
                value={structuredDisplayID}
                onChange={(event) => setStructuredDisplayID(event.target.value)}
                required
              />
            </label>
            <label>
              Prospect name
              <input
                value={structuredName}
                onChange={(event) => setStructuredName(event.target.value)}
                required
              />
            </label>
          </div>
          <div className="ai-workspace__action-grid">
            <label>
              Optional email
              <input
                type="email"
                value={structuredEmail}
                onChange={(event) => setStructuredEmail(event.target.value)}
              />
            </label>
            <label>
              Optional phone
              <input
                value={structuredPhone}
                onChange={(event) => setStructuredPhone(event.target.value)}
              />
            </label>
          </div>
        </>
      ) : null}

      {action === "transition" ||
      action === "priority" ||
      action === "note" ||
      action === "email" ? (
        <>
          <TicketIdentityFields
            ticketID={ticketID}
            version={version}
            onTicketID={setTicketID}
            onVersion={setVersion}
          />
          <label>
            New value
            <textarea
              value={value}
              onChange={(event) => setValue(event.target.value)}
              required
            />
          </label>
        </>
      ) : null}
      {action === "transition" ||
      action === "priority" ||
      action === "ticket_route" ||
      action === "client_resource_update" ||
      action === "client_resource_deactivate" ||
      action === "client_resource_reactivate" ? (
        <label>
          Reason
          <input
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            required
          />
        </label>
      ) : null}

      <div className="ai-proposal__actions">
        <button
          type="submit"
          disabled={
            busy ||
            (actionNeedsClient && !targetClientID) ||
            assetExternalIdentityIncomplete ||
            !selectedCatalogAction
          }
        >
          {action === "client_resource_list"
            ? "Look up resources"
            : actionIsRead
              ? "Run read"
              : "Review action"}
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function TicketIdentityFields({
  ticketID,
  version,
  onTicketID,
  onVersion,
}: {
  ticketID: string;
  version: string;
  onTicketID: (value: string) => void;
  onVersion: (value: string) => void;
}) {
  return (
    <div className="ai-workspace__action-grid">
      <label>
        Ticket ID
        <input
          value={ticketID}
          onChange={(event) => onTicketID(event.target.value)}
          required
        />
      </label>
      <label>
        Current version
        <input
          type="number"
          min="1"
          value={version}
          onChange={(event) => onVersion(event.target.value)}
          required
        />
      </label>
    </div>
  );
}
