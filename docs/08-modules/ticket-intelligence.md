# Ticket Intelligence Module

**Status:** V1 product definition

## Purpose

Deliver a polished technician operating surface for intake, triage, routing, collaboration, resolution, search, auditing, and integration.

## V1

- Email, authenticated API, monitoring integration, and technician-created intake
- Shared work records with Incident, Service Request, Problem, Change, Alert, and General Ticket
- Clients, locations, contacts, teams, departments, queues, technicians, assets, services, and contracts
- One primary owner plus collaborators, reviewers, escalation participants, and watchers
- Configurable workflows, SLAs, approvals, typed automations, webhooks, and external HTTP calls
- Comments, internal notes, client-visible replies, attachments, time, relationships, parent/child work, audit, history, search, reporting foundations, and dependency impact
- One MSP-global governed tag catalog across Work Records, Tasks, Projects,
  Assets, Knowledge Articles, and Time Entries; keyboard-accessible search and
  selection; derived Project/Phase-to-Task inheritance; AI suggestions; and
  scoped classification insights
- Structured internal staff and organizational-team mentions from ticket,
  task, and project details, comments, and notes; a Dashboard-only Mentions
  widget with unread/read/archive state; exact authorized source links; and
  content-free email/Teams alerts
- Entra SSO, break-glass access, RBAC, API/browser security, HA, backups, observability, and upgrade/recovery operations

## Deferred

Full client portal, native mobile application, arbitrary endpoint/host command execution, mature AI autonomy, billing, remote access, password vault, and Kubernetes-first deployment.

## Experience goal

A technician can understand what needs attention, why it was routed, who owns it, what changed, what is affected, and what action is permitted without navigating inconsistent modules.

Type, Status, and Tags remain visibly separate. Tag chips expose whether an
assignment is direct, inherited, automated, or awaiting an AI decision without
showing internal keys. Interactive creation requires a meaningful tag, while
trusted intake may visibly fall back to protected `Unclassified`. Settings
preview rename/move/merge/archive impact before a reasoned, version-checked
change. Insights default to effective tags, disclose projection freshness, and
link recurring-issue aggregates to scoped evidence.

Mentions remain separate from owners, assignees, collaborators, participants,
watchers, tasks, and approvals. One item is collapsed per recipient and parent
object; a later occurrence points to the latest source and returns it to
unread, including from archive. Group mentions snapshot only current eligible
members of the existing organization team. There are no mention intents, AI
mention targets, full Mentions inbox, generic in-app notification duplicate,
or customer/public mention surfaces.
