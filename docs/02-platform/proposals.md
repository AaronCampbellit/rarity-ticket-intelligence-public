# Proposals

**Status:** Approved native PSA design

Proposals are first-class commercial records attached to Opportunities. An issued Proposal Version is immutable; revisions create a new version while preserving every earlier version and response.

## Line types

Proposal Versions support:

- fixed-fee services;
- time-and-materials labor;
- products and licenses; and
- recurring services.

Lines may carry quantity, unit cost, unit price, discount, margin, tax treatment, and planned hours where applicable.

## Approval and acceptance

Configurable internal approval rules may evaluate value, margin, discount, or risk. Customer acceptance supports built-in electronic acceptance and manually recorded offline acceptance.

Both methods preserve the signer, timestamp, acceptance method, exact Proposal Version, and immutable PDF snapshot. Electronic acceptance derives customer identity and evidence from a single-use, Proposal-Version-bound grant verified at the service boundary; caller-supplied signer fields are not proof of identity. Offline acceptance also records the user who entered it.

## Project baseline

The accepted Proposal Version seeds the Project's original commercial baseline. Its lines map through an editable conversion preview into Phases, scope, budgets, and planned hours. Later commercial changes use versioned Change Orders and never rewrite the accepted Proposal Version.
