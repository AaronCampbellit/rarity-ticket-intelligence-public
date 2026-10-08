# Technician Information Architecture

**Status:** Drafted for Milestone 0 review

## Design goals

The V1 information architecture makes daily technician work visible before administration. It follows progressive complexity: work, queues, and search are immediately available; automation, integrations, and platform settings appear only to authorized roles.

## Primary navigation

```text
Rarity
├── Home
│   ├── My Work
│   ├── Unassigned Queues
│   ├── SLA Risk
│   ├── Scheduled Work
│   ├── Mentions widget
│   └── Recent Activity
├── Work
│   ├── All Work
│   ├── Queues
│   ├── Scheduled
│   ├── Approvals
│   └── Saved Searches
├── Clients
│   ├── Clients
│   ├── Contacts
│   ├── Assets
│   ├── Services
│   └── Contracts
├── Knowledge
├── Reports
│   ├── Dashboards
│   └── Exports
├── Operations
│   ├── Integrations
│   ├── Automation
│   ├── Health
│   └── Audit
└── Settings
    ├── Organization and Identity
    ├── Roles and Access
    ├── Workflows and SLAs
    ├── Routing and Notifications
    ├── Storage and Backups
    └── Setup and Help
```

The Home view opens by default. A technician with no administrative capability does not see inaccessible operational areas, but authorization remains enforced by every route and API.

## Global controls

The persistent top bar contains global search, a create-work action, notifications, current user/session menu, and an environment/health indicator for authorized operators. Search opens a keyboard-accessible result surface categorized by work, clients, contacts, assets, knowledge, and attachments; unauthorized results never appear.

## Core routes

| Route | Job to be done | Primary audience |
|---|---|---|
| Home | Understand today’s work and risk | Technician |
| Queue | Triage and claim unassigned work | Technician, manager |
| Work detail | Diagnose, collaborate, communicate, and resolve | Technician |
| Scheduled | Plan and execute future work | Technician, manager |
| Client workspace | Understand support context and coverage | Technician, manager |
| Asset/service | Understand dependencies and related work | Technician |
| Knowledge | Find, draft, publish internal guidance | Technician |
| Dashboard | Observe workload, SLA, intake, and health | Scoped audience |
| Setup and Help | Configure and maintain the installation | Administrator |

## Responsive and accessibility rule

Desktop supports dense worklists and a persistent context pane. Tablet collapses the secondary pane. Mobile preserves create, search, queue, ticket detail, and reply flows using a single-column layout. Every pointer interaction has keyboard and screen-reader alternatives; drag/drop is never required to triage or reorder work.

## Mentions widget and deep links

The Home Dashboard widget is the sole in-app mention surface. It provides
Unread, Read, and Archived lenses, counts, newest-first cursor loading, mark
read/unread, archive, and open-source actions. It is notification-sized rather
than a full inbox and does not duplicate entries in the generic notification
center. The three state controls use the ARIA tab/tabpanel pattern, expose one
tab stop, and support Left/Right/Home/End keyboard activation.

Opening an item calls the authenticated occurrence resolver. The returned hash
route selects the authorized Client, opens the ticket/task/project, expands the
internal details/comment/note region, focuses and temporarily outlines the
exact current token selected by authenticated `token_id`, and marks the
projection read in one version-checked operation. If that token no longer
exists, focus moves to the source-unavailable notice rather than a nearby token
or stale offset. If the source was removed, redacted, or publicized, the
authorized parent opens with the same no-content notice. Lost access returns
the standard non-disclosing not-found response.
