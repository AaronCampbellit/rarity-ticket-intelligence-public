# Authorization and RBAC

**Status:** Draft

Capabilities such as `WorkRecord.Read`, `WorkRecord.Assign`, `WorkRecord.Close`, `Client.Edit`, and `Automation.Publish` compose roles. Authorization also evaluates client scope, object relationships, record state, assignment, and contextual policy.

Default deny applies. Permission checks occur in shared application services, not only routes or UI. Role changes, grants, denials, administrative overrides, and policy decisions are audited and testable.
