# Errors

**Status:** Draft

Errors use a consistent envelope containing stable code, safe message, correlation ID, optional field details, and documentation reference. HTTP status codes retain standard meaning.

Validation, authentication, authorization, conflict, concurrency, rate-limit, and workflow errors are distinct. Internal stack traces, SQL, secrets, and sensitive existence details never cross the API boundary.
