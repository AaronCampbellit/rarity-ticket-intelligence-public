# Opportunities

**Status:** Implemented source contract; constrained demo PostgreSQL acceptance complete

Opportunities are first-class native PSA sales records. They belong to either a lightweight Prospect or an existing Client and retain the complete sales history after conversion into a Project.

## Pipelines

Administrators may create multiple pipelines, including New Business, Existing Client Expansion, and Renewal. Each pipeline defines configurable stages, allowed transitions, probability, forecast classification, required fields, aging targets, and Proposal or approval gates.

Weighted and committed forecasts are derived from pipeline configuration and authoritative commercial records.

## Opportunity workspace

An Opportunity supports Contacts, owner and team, expected close date, forecast category, expected value, activities, notes, attachments, custom fields, and tasks.

Opportunity tasks are normal first-class Tasks. During conversion, selected incomplete tasks retain their identity and move to the Project. Completed and unselected tasks remain as Opportunity history.

An Opportunity may name one participating MSP team and an ordered set of
Contacts from its Client. Authorized users replace that participant set with
the exact Opportunity version. PostgreSQL rejects a team from another MSP and
Contacts outside the Opportunity Client before atomically updating the record,
audit ledger, and outbox.

Custom fields are a bounded string map stored with the Opportunity. Authorized
users replace the complete map using optimistic concurrency; reserved core
fields cannot be shadowed. The mutation writes matching audit and outbox facts,
and configured stage gates evaluate the merged core and custom field set.

Attachments use the same bounded, content-type allowlisted streaming path as
service-desk files. Object bytes are stored outside PostgreSQL while scoped
metadata, checksum, uploader, audit, and outbox facts commit together.
Opportunity readers receive a bounded metadata list; cross-Client parents are
rejected before metadata is written.

## Prospect conversion

A Prospect is a lightweight MSP-scoped sales record. Opportunity conversion creates or matches a full Client and carries forward Contacts, activity, attachments, and lineage without creating duplicate Clients.

## Lifecycle

The native commercial lifecycle is:

```text
Prospect or Client -> Opportunity -> Proposal Version -> Approval
-> Conversion Preview -> Project
```

Conversion requires an accepted Proposal Version, complete Client information, ownership, mappings, and valid financial values. It is atomic and idempotent. The won Opportunity becomes read-only historical sales context linked to the resulting Project.

## Implementation evidence

The current source includes configurable Pipeline and guarded stage-transition
services, bounded custom fields, streamed attachments, currency-safe
forecasting, four Proposal Line types, immutable issued snapshots, exact-version
electronic and offline acceptance, deterministic conversion preview, and
selective incomplete-task movement. The shared browser workspace verifies the
accepted Opportunity-to-Project journey with synthetic fixtures.

See [Native PSA Opportunities and Projects API](../04-api/psa-opportunities-projects.md) and [Native PSA Acceptance](../06-development/psa-acceptance.md). PostgreSQL migration and transaction acceptance remains pending an isolated `TEST_DATABASE_URL`.
