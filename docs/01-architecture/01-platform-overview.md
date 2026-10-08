# Platform Overview

**Status:** Draft

Rarity is a modular platform with a shared control plane and domain services. A single installation hosts one MSP and isolates many clients.

```text
Browser / SDK / Integration / Email
              |
       API and Intake Edge
              |
 Identity + Authorization + Application Services
              |
 Objects | Work Records | Workflow | Automation | Graph
              |
 Events | Search | Audit | Notifications | AI
              |
 PostgreSQL | Cache/Queue | Object Storage | Search
```

The UI is an API client. Intake is normalized before routing. All mutations pass through shared application services so permissions, validation, audit, events, and isolation remain consistent. Modules may be deployed together initially, but boundaries and contracts must support later scaling without premature microservice fragmentation.
