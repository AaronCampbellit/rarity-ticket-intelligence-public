# Entra SSO

**Status:** Draft

An installation may connect to the MSP’s approved Entra tenant configuration during setup or later from Identity settings. RTI remains fully usable with local Platform Administrators when Entra is absent or unhealthy. Initial sign-in and account linking require explicit issuer/tenant validation; automatic cross-tenant acceptance is forbidden.

V1 provisions Entra-backed workforce users just in time on first successful sign-in. SCIM is deferred. Group/role mapping is versioned and explainable. Deprovisioning, disabled users, token revocation, conditional access, clock skew, key rotation, and identity-provider outage behavior require tested runbooks. Activating, replacing, disabling, or losing Entra never silently changes local administrators.
