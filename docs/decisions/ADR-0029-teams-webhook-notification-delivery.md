# ADR-0029: Teams webhook notification delivery

**Status:** Accepted\
**Date:** 2026-07-27

## Decision

Rarity V1 delivers Microsoft Teams notifications through named, encrypted Incoming Webhook connections. Each connection targets one configured Teams channel and posts compact Adaptive Cards. Rarity does not use delegated technician credentials or broad Microsoft Graph message permissions for unattended notification delivery.

## Notification behavior

Rules may target Teams by MSP, client, contract, workflow, queue, team, department, record type, event, and permitted user preference. Cards contain only a ticket number, priority, status, short subject, SLA state, and an authenticated link back to Rarity. Attachments, credentials, internal notes, email bodies, and AI context are excluded by default.

Assignments default to in-app and email, with Teams enabled by queue policy. SLA warning/breach and major-incident policies default to in-app, email, and Teams. Routine changes default to in-app only. One notification is emitted per ticket/event type during a configurable quiet period; critical escalations bypass suppression.

## Reliability and scope

Teams delivery retries for 24 hours and then remains visible as a failure in delivery history and the health dashboard. Teams is a notification surface, not a source of truth or a work-action channel. Direct-message bots, interactive Teams approvals, and Slack are deferred from V1.

## Consequences

The Teams integration is simple to configure, least-privileged, and does not need unattended access to a technician identity. Each destination webhook remains a sensitive connection secret and must be rotatable, auditable, and scoped to its intended notification policy.
