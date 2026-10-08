# Automatic Remote Bootstrap URL Design

**Status:** Approved direction pending written-spec review

## Purpose

The first Rarity startup must give a deployment operator a copyable setup link
without requiring a separate token-generation command. The operator commonly
reads deployment output through SSH or another remote console and opens the
link on a different workstation, so the link must use the browser-reachable
server address rather than `localhost` or the server's bind address.

## Considered approaches

1. **Automatic startup issuance — selected.** When installation setup is
   incomplete, the API issues a token and prints one absolute setup URL. This
   gives the smallest operator workflow while retaining the existing
   single-use database authority.
2. **Manual helper command.** The existing `rarity-bootstrap-token` command
   remains useful for deliberate recovery or rotation, but it is no longer
   required for normal installation.
3. **Deployment-provided token.** Supplying a secret through Compose or another
   orchestrator would move token generation outside Rarity and create another
   long-lived secret-handling path, so it is rejected.

## Runtime configuration

`RARITY_PUBLIC_URL` is the canonical browser-reachable origin and optional path
prefix for the installation. It must be an absolute URL with no user
information, query, or fragment. `pilot` and `production` require HTTPS.
`local`, `test`, and `demo` may use HTTP only for `localhost`, `127.0.0.1`, or
`[::1]`.

On startup, after migrations and installation-state lookup:

- a completed installation starts normally and never issues or prints a
  bootstrap token;
- an incomplete installation requires a valid `RARITY_PUBLIC_URL`;
- the API issues a new 32-byte, 15-minute token through the existing setup
  service, atomically invalidating any earlier unused token; and
- the API prints one clearly labeled setup URL and expiry to deployment output.

An incomplete-installation restart rotates the link. A failure to inspect
installation state, issue the token, or construct an allowed public URL stops
startup. The API must not start with an unusable or insecure first-run path.

The printed link is:

```text
https://rarity.example/#/setup?bootstrap_token=<single-use-token>
```

When `RARITY_PUBLIC_URL` contains a path prefix, the setup path is appended
without discarding that prefix. The listen address and request host are never
used to guess the public URL.

## Browser flow

The setup page reads `bootstrap_token` only from the hash-router fragment. A URL
fragment is not sent in the HTTP request or reverse-proxy access line. On first
render the page:

1. copies the token into in-memory component state;
2. immediately replaces browser history with the same `#/setup` route without
   the token; and
3. shows that deployment authority was accepted without rendering the token.

The token is never placed in local storage, session storage, cookies, page
markup, analytics, errors, audit records, or application logs. Bootstrap
submission continues to send it only as `Authorization: Bootstrap <token>` over
HTTPS. Refreshing after the fragment is scrubbed intentionally loses the
in-memory token; the operator must reuse the current printed link or explicitly
rotate it with the helper command.

Opening `#/setup` without a token shows a concise instruction to use the setup
link from deployment output. The GUI does not ask the operator to run the
helper command during normal installation. The existing helper remains
available for recovery and prints the same absolute URL format when
`RARITY_PUBLIC_URL` is configured.

## Authority and completion

This design does not change the existing bootstrap transaction. The database
stores only the SHA-256 token hash. Successful setup consumes the token and
atomically creates the organization and first local Platform Administrator.
Failed validation does not consume the token. Replay, expired tokens, and
tokens rotated by a restart are rejected. Once setup commits, bootstrap closes
permanently and later restarts do not issue new authority.

## Deployment output

The setup link is the sole intentional plaintext exposure of the bootstrap
secret. It is written once at startup to the API's deployment output with a
warning that anyone holding the link can initialize the installation and with
the RFC 3339 expiry. It is not repeated by health endpoints, readiness
endpoints, status APIs, or subsequent log records.

Operators must treat startup logs as secret-bearing until the token expires or
is consumed. Product documentation must make that boundary explicit.

## Verification

Automated tests must prove:

- public URL validation, including HTTPS enforcement and path-prefix handling;
- completed installations neither issue nor print a token;
- incomplete installations issue exactly one token and print a copyable
  externally reachable URL with its expiry;
- restart rotation invalidates the previous unused token;
- token issuance or public-URL failure prevents startup;
- the helper command emits the same URL contract;
- the setup page consumes the fragment, scrubs it from browser history, keeps
  it only in memory, and submits it in the authorization header;
- rendered UI, request bodies, status responses, logs other than the one
  intentional startup line, audit data, and events do not contain the token;
- expiry, replay, failed validation, and permanent closure retain their current
  behavior; and
- a browser running on a different host can open the printed URL and complete
  Entra-free setup with the first local administrator.

The final item requires deployed HTTPS acceptance; local unit and browser tests
prove construction and consumption but do not substitute for that live check.
