# Logging

**Status:** Structured API logging and core redaction implemented; retention/export acceptance pending

Application logs are structured and include service, version, environment, severity, timestamp, correlation/trace context, safe actor/object references, and error codes.

Logs exclude credentials, session material, full authorization headers, sensitive payloads, and client content unless explicitly designed and protected. Retention, access, export, redaction, and clock synchronization are operational requirements.

The shared JSON logger redacts URL/key/secret/token/password attributes plus authorization and cookie headers. HTTP completion logs use registered route patterns instead of raw paths and exclude queries, headers, and bodies. Deployed log collection must remain private, enforce role-based access and retention, preserve synchronized timestamps, and verify that exporter failures cannot block request processing.
