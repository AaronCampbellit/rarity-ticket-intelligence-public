# Authentication

**Status:** Draft

Workforce authentication may use Entra ID with current standards and the tenant's MFA policy. Entra is optional during and after installation. When connected, Rarity validates issuer, audience, signature, nonce/state, time claims, and tenant configuration.

Local Platform Administrators use strong password hashing, restricted network policy, bounded and revocable sessions, and audited authentication. At least one enabled local administrator must remain; Entra activation never disables local access. Service principals use scoped, rotatable credentials; long-lived shared credentials are avoided.

If every local administrator password is unavailable, an authorized host operator can use the TTY-only `rarity-admin local-admin reset-password` recovery command. It connects directly to PostgreSQL over the private Compose network; there is no HTTP recovery endpoint and no password flag, environment variable, or standard-input mode. A successful reset atomically replaces the bcrypt hash, revokes every active session for that administrator, and appends correlated system-actor audit and outbox facts with the operator-supplied reason. See [Local Administrator Recovery](../05-infrastructure/local-administrator-recovery.md).
