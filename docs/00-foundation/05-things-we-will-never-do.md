# Things We Will Never Do

**Status:** Foundational draft

We will never:

- bypass authorization for convenience or create a hidden administrator backdoor;
- create browser-only business capabilities or undocumented privileged endpoints;
- hardcode client-specific business processes into shared application code;
- store credentials, tokens, recovery keys, or connection secrets in plaintext;
- allow one client’s data to enter another client’s search, report, cache, automation, export, or AI context;
- delete or rewrite immutable audit history;
- permanently delete recoverable business objects without explicit policy and authorization;
- break supported APIs, events, or SDK contracts without versioning and migration guidance;
- publish workflows or automations without validation, versioning, audit, and failure controls;
- permit V1 automation to run arbitrary code on the host or managed endpoints;
- treat backups as successful without monitored jobs and routine restoration tests;
- ship critical platform features without telemetry, documentation, and proportionate automated tests;
- add a competing identity, permission, audit, or object model for a new module;
- obscure whether output is inferred, predicted, automated, or human-confirmed;
- let AI silently make material customer-data changes;
- expose internal notes through client-visible email or future portal surfaces.

Exceptions require a time-bounded ADR, named owner, explicit risk acceptance, and removal plan.
