# Notifications

**Status:** Accepted V1 direction

V1 notification channels are in-app, email, Microsoft Teams, and signed outbound webhooks. Slack is deferred. Notification policies are configurable by MSP, client, contract, workflow, queue, team, department, record type, event, and user preference where permitted.

Teams delivery uses named, encrypted Incoming Webhook connections to configured channels and compact Adaptive Cards. Cards contain a ticket number, priority, status, short subject, SLA state, and an authenticated Rarity link. Attachments, credentials, internal notes, email bodies, and AI context are excluded by default. Teams is notification-only; direct-message bots and interactive Teams approvals are deferred.

Default policies warn and escalate on SLA warning/breach. Assignments default to in-app and email, with Teams enabled by queue policy. SLA warning/breach and major incidents default to in-app, email, and Teams. Routine updates default to in-app only. A configurable quiet period suppresses duplicate ticket/event notifications, while critical escalations bypass suppression. Teams delivery retries for 24 hours. Public ticket replies use the configured support mailbox and preserve conversation threading. Internal notes are never delivered through a client-facing channel. Delivery attempts, recipients, content classification, retries, suppression, and failures are audited and visible to authorized users.
