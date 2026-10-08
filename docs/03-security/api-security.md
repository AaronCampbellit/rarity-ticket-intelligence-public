# API Security

**Status:** Draft

APIs require authenticated principals, explicit scopes, object authorization, input validation, bounded queries, rate/resource limits, and audit. Credentials are hashed or encrypted as appropriate, displayed once, rotatable, expirable, and attributable.

Mutation endpoints support idempotency and concurrency controls. Errors avoid secret or internal detail leakage. Public endpoints are inventoried, documented, tested, and protected against enumeration and cross-client access.
