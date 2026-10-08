# Technician Workspace Wireframes

**Status:** Drafted for Milestone 0 review

These wireframes establish interaction hierarchy and data requirements; they are not final visual design.

## 1. Home

```text
+--------------------------------------------------------------------------------+
| Rarity | Search…                         + Create | Notifications | Profile  |
+----------+---------------------------------------------------------------------+
| Home     | Good morning, Alex                         Today / This week         |
| Work     | +-------------------+ +-------------------+ +-------------------+    |
| Clients  | | My active work 12 | | SLA risk 3      | | Scheduled 4       |    |
| Knowledge| +-------------------+ +-------------------+ +-------------------+    |
| Reports  |                                                                     |
| Ops      | Assigned work                 Unassigned queues                     |
| Settings | INC-10482 VPN unavailable     Network Alerts (7)                    |
|          | SR-10470 New user              Service Desk (12)                    |
|          | CHG-10453 Firewall update      VIP Support (2)                       |
+----------+---------------------------------------------------------------------+
```

Cards link to filtered worklists; they never rely on color alone to convey risk. A user can save a current filter/search from any worklist.

## 2. Queue triage

```text
+--------------------------------------------------------------------------------+
| Queue: Network Alerts   [Views] [Filters] [Sort] [Save view]                  |
+--------------------------------------+-----------------------------------------+
| Unassigned (7)                       | Context / quick preview                 |
| ● INC-10482 VPN unavailable           | Client: Acme Architecture                |
|   Critical · 14m to response breach   | Service: VPN · Dallas                    |
| ● INC-10479 Router packet loss        | Assets: FW-01, CORE-SW-01                |
| ○ INC-10477 Backup warning            | [Claim] [Assign] [Open]                  |
|                                      | Routing explanation                       |
| In progress (4)                      | "Datto alert → Network Alerts"           |
+--------------------------------------+-----------------------------------------+
```

Tickets may remain unassigned during New/Triage. Claiming establishes the primary owner when the record moves into active work. Bulk actions preview scope and denied records before execution.

## 3. Work detail

```text
+--------------------------------------------------------------------------------+
| INC-10482  VPN unavailable at Dallas  [Critical] [In Progress] [•••]          |
| Acme Architecture · Network Alerts · Owner: Alex · SLA: response met / 2h      |
+----------------------------------------------+---------------------------------+
| Activity                                     | Context                         |
| [Public reply] [Internal note] [Time]        | Client / contact                |
| -------------------------------------------- | Assets (2) · Services (1)       |
| 10:18 Alex claimed and began triage          | Contract / SLA                  |
| 10:16 Datto: gateway unreachable             | Related / blocked / duplicate   |
| 10:12 Support mailbox created ticket         | Knowledge suggestions           |
|                                              | Dependency impact               |
| Compose public reply…                         |                                 |
| [Attach] [Send reply]                         |                                 |
+----------------------------------------------+---------------------------------+
```

Public replies and internal notes are visually and semantically distinct before composition. All activity identifies actor, source, and visibility. The context pane supports multiple assets, contacts, services, contracts, and knowledge links.

## 4. Scheduled work

```text
+--------------------------------------------------------------------------------+
| Scheduled work     [Calendar] [List] [Create scheduled work]                  |
+------------------------+-------------------------------------------------------+
| Week of July 27        | Mon       Tue       Wed       Thu       Fri            |
| Filters                | 09:00 CHG-10453  | 14:00 SR-10470                       |
| Team / Queue / Owner   | Firewall update  | New-user onboarding                  |
| Client / Status        | Ends 10:00       | Reminder: 30m                        |
+------------------------+-------------------------------------------------------+
```

Scheduled status requires planned start. Planned end is optional. Authorized calendar views show reminders and apply the same client/permission filtering as worklists.

## 5. First-run setup

```text
+---------------------------------------------------------------+
| Welcome to Rarity                                  Step 2 of 6 |
|---------------------------------------------------------------|
| ✓ Organization   ● Local admin   ○ Entra (optional)            |
| ○ Intake         ○ Storage       ○ Backups                      |
|                                                               |
| Create the first local Platform Administrator                  |
| Email [_______________________]                                |
| Username [____________________]                                |
| Password [____________________]                                |
| Allowed networks [192.168.0.0/16________]                     |
|                                                               |
| [Back]                                          [Save & next]  |
+---------------------------------------------------------------+
```

The wizard validates each step and records who accepted it. Entra offers
**Configure now** or **Skip for now** and is not required for completion. It
never displays a submitted secret again. The completed setup/help center stays
accessible from Settings.

## 6. Datto reconciliation

```text
+--------------------------------------------------------------------------------+
| Asset reconciliation: 18 candidates                         [Sync now]        |
+--------------------------------------+-----------------------------------------+
| Candidate: ACME-DC01                 | Differences                             |
| Confidence: 87%                      | Hostname: same                          |
| Rarity asset: DC-01                  | Serial: Rarity blank / Datto 1234       |
| Datto device: ACME-DC01              | OS: Windows Server 2022 / 2022          |
|                                      |                                         |
| [Link] [Use Rarity] [Use Datto] [Keep separate]               |
+--------------------------------------+-----------------------------------------+
```

Automated rules handle routine matches. Ambiguous or conflicting candidates surface here, and every choice is auditable. Missing/retired Datto assets become stale/inactive for review rather than being deleted.
