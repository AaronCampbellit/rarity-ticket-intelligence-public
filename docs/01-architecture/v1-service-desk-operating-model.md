# V1 Service Desk Operating Model

**Status:** Accepted V1 direction

## Record types and classifications

V1 work-record types are Incident, Service Request, Change, and Problem. Tasks are child work items. Monitoring alerts are incidents with alert-source metadata. Maintenance, security, onboarding, and procurement use classifications, templates, and workflows rather than new base record types.

## Default lifecycle

The default workflow states are New, Triage, In Progress, Scheduled, Waiting on Client, Waiting on Vendor, Resolved, Closed, and Cancelled. MSPs may add or rename statuses without weakening lifecycle, audit, or SLA semantics. Priorities default to Critical, High, Normal, and Low and are configurable.

Tickets may remain unassigned in a queue while in triage. A primary owner is required once work becomes active; collaborators remain supported. Routing may evaluate client, mailbox, sender, subject, service, alert source, queue, team, and department. Individual automatic assignment is optional.

Scheduled work requires a planned start, may have a planned end, supports reminders, and appears on authorized calendar views.

## SLA, contracts, and time

Contracts define covered services, coverage, billing rules, response/resolution SLAs, and calendars. SLA policy can vary by MSP, client, contract, or service, with business hours, holidays, time zones, and optional 24/7 coverage. Warning and breach notifications/escalations default on but are configurable.

Time entries support timer/manual capture and billable/non-billable classification. Approval before export is configurable by MSP and contract. V1 calculates included and billable work and exports approved billing data as clean CSV; it does not invoice, receive payments, or write to accounting systems.

## Collaboration and relationships

Tickets link multiple first-class assets, users/contacts, contracts, services, and knowledge articles. Supported record links are related, duplicate, blocked-by, and blocking. Merging duplicates preserves both histories, does not notify requesters, and creates redirects from source records rather than deleting them.

## Communications, attachments, and knowledge

Technician-first V1 defers the client portal and client-facing knowledge base. Public replies are separate from internal notes, use the configured support mailbox, and preserve threading. Direct web/API uploads are supported to a configurable 250 MB default per file. Rarity has no internal malware scanner in V1; mailbox protection covers email attachments. Safe common image formats can render inline only for authorized users; active/non-safe types are download-only and access-controlled.

Knowledge is internal-only in V1, with drafts and version history; authorized technicians may publish directly.

## Dashboards and search

The default technician dashboard shows assigned work, unassigned queues, SLA risk, scheduled work, and recent activity. Users can save/share searches and filters. Editable dashboards are shareable with authorized team, department, queue, or MSP audiences. Global search is permission-aware across tickets, clients, contacts, assets, knowledge, and attachments.
