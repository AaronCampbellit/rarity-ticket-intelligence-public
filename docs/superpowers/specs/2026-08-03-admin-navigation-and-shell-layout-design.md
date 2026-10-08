# Admin Navigation and Shell Layout Design

**Status:** Approved for implementation on 2026-08-03

## Goal

Reduce primary-navigation density, keep administration and provider
configuration discoverable, and make the application shell render consistently
at desktop and narrow widths.

## Navigation model

The primary sidebar keeps the Work, Sales, Delivery, and Operations groups.
Administration becomes one disclosure labeled **Admin**. It is collapsed by
default and automatically expands whenever its current route is active.

The disclosure contains four labeled subgroups:

- **Access:** Sessions, Local administrators, Roles, and Audit.
- **Platform:** Setup center, Service API keys, and Billing review.
- **Organization:** Knowledge, Clients and directory, and Client resources.
- **Integrations:** Teams settings, Datto connections, Graph mailboxes,
  Webhooks, and Automation.

AI assist, AI settings, integration health, Datto reconciliation, and forwarding
intake remain operational workflows outside Admin. The active nested link keeps
the existing page indication. Selecting a link closes the narrow-screen drawer.

## Interaction and accessibility

Admin uses a real button with `aria-expanded` and an associated controlled
region. Keyboard and pointer activation use the same toggle. The nested links
remain ordinary links with visible focus states and `aria-current="page"` on
the active route. When the active route belongs to Admin, the disclosure stays
open so the current location is visible.

## Shell layout

The sidebar owns a continuous navigation-colored surface for the full viewport.
Its navigation area scrolls independently when links exceed the available
height; the brand and build label remain visible.

At the narrow breakpoint, the fixed sidebar must not consume a CSS grid row.
The workspace is explicitly placed in the first row and column so the top bar
and page content begin at the top of the viewport in Codex, mobile, and regular
browsers.

## Verification

Component tests cover disclosure state, active-route expansion, nested-link
navigation, Escape/drawer behavior, and accessible attributes. CSS contract
tests or rendered measurements cover the continuous sidebar surface,
independent navigation scrolling, and the narrow workspace origin.

Rendered QA covers desktop and a width below 760 pixels. It verifies page
identity, content at the top of the viewport, sidebar background continuity,
Admin expansion/collapse, active link visibility, mobile drawer closure,
console health, and screenshots.
