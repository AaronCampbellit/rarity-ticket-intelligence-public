# API Authentication

**Status:** Bearer principal and Client-context boundary implemented

V1 integrations use scoped service API keys and technician scripts use expiring personal access tokens. OAuth 2.0/OIDC is deferred. Managed credentials support expiration, rotation, attribution, scope, and last-used visibility.

Authentication method never replaces authorization. Every request produces a principal, installation scope, credential identifier, and correlation context.

Technician session bearer requests may select an active Client with `X-Rarity-Client-ID`. The value is never trusted directly: the server validates MSP ownership and loads only current global plus matching Client role grants. MSP-global requests omit the header. Scoped service API keys cannot use the header to widen or replace their configured Client.
