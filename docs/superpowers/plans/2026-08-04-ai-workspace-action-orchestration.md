# AI Workspace Action Orchestration Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

**Goal:** Turn an explicit natural-language project request into the existing
typed `project.create` proposal without inventing business data or bypassing
confirmation.

**Architecture:** Add a small workspace-message planner that recognizes the
approved project command and otherwise leaves messages on the existing product
help path. The planner extracts only user-supplied project title, client name,
and task names. The HTTP route resolves the named client against the
authenticated active-client directory entry, then calls the existing tool
registry to create an expiring exact preview. Missing or mismatched data returns
an assistant clarification and creates no proposal.

**Deliberate boundary:** This slice establishes the planner interface and safe
message-to-tool seam. It does not grant a model arbitrary tool selection,
search across clients, create clients, or execute a write without confirmation.
A provider-backed structured planner can replace the bounded parser behind the
same contract later.

## Tasks

- [x] Add failing planner tests for a complete project command, missing user
  data, and ordinary help questions.
- [x] Implement the bounded project intent parser and clarification responses.
- [x] Add failing route tests proving active-client name resolution, proposal
  provenance, and no proposal on incomplete or mismatched requests.
- [x] Route recognized intents through `project.create`; retain `product.help`
  for non-action messages.
- [x] Return optional proposals from the message API and display them
  immediately in the AI workspace.
- [x] Update operator/user documentation and the active Kanban work log.
- [x] Run focused and full verification, publish to `main`, deploy the exact
  revision to the demo server, and perform full-width live acceptance.

## Verification

- Go unit and HTTP route tests cover extraction, clarification, active-client
  matching, message provenance, and tool selection.
- Vitest covers proposal rendering directly from a chat response.
- Full Go and frontend suites, formatting, vetting, build, documentation
  validation, and migration contracts pass.
- Live demo acceptance creates a preview from chat, confirms exact supplied
  values, and rejects the proposal without creating a project.
