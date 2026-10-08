# Local Administrator Recovery

**Status:** Implemented operator runbook

Use this procedure only when normal HTTPS local-administrator password management is unavailable. It is a direct database recovery boundary for an operator who already controls the Rarity host and Compose deployment. It does not create an HTTP bypass, reveal an existing password, enable a disabled account, or recover an ambiguous tenant match.

## Preconditions

- Confirm the incident and the human authorized to perform recovery.
- Work from the installation directory on the Rarity host through an approved privileged shell.
- Confirm PostgreSQL is healthy and the intended release image digest has already passed release verification.
- Identify the exact MSP display ID and local-administrator username.
- Choose a non-secret reason suitable for the immutable audit ledger.
- Ensure the shell has an interactive TTY. Never place the new password in a command, environment variable, pipe, redirected file, shell history, ticket, or chat.

For the pilot/release profile, verify the active services and image before mutation:

```bash
docker compose --env-file .env \
  -f infrastructure/compose/compose.yaml \
  -f infrastructure/compose/compose.pilot.yaml \
  -f infrastructure/compose/compose.release.yaml \
  ps
```

## Reset the password

Run the one-shot operator service from the same verified API image:

```bash
docker compose --env-file .env \
  -f infrastructure/compose/compose.yaml \
  -f infrastructure/compose/compose.pilot.yaml \
  -f infrastructure/compose/compose.release.yaml \
  --profile operator run --rm --no-deps rarity-admin \
  local-admin reset-password \
  --msp 'MSP-DISPLAY-ID' \
  --username 'local-admin-username' \
  --reason 'incident reference and authorized operator'
```

The command reads the password twice from `/dev/tty` with terminal echo disabled. Passwords must be 16–72 bytes. Unknown or password-bearing flags are rejected. The target must be one enabled local administrator in one active MSP; zero or multiple case-insensitive MSP matches fail closed with the same unavailable-target error.

On success, the command prints the resolved MSP display ID, normalized username, new account version, and number of revoked sessions. It never prints the password or password hash.

For the constrained demo profile, substitute `compose.demo.yaml` for the pilot and release overrides. Local source builds are acceptable only in that non-production environment.

## What commits atomically

One PostgreSQL transaction:

1. locks and revalidates the enabled account;
2. replaces the password with a bcrypt hash at the platform minimum cost;
3. increments the account version;
4. revokes every active session for the account's technician;
5. appends `security.local_admin.password_reset` to the audit ledger and event outbox with source `operator_cli`, system actor `00000000-0000-0000-0000-000000000000`, the supplied reason, and the revoked-session count.

Any failure rolls back all five effects. The system actor records that host/database recovery authority performed the mutation; it does not claim that an operating-system username was cryptographically authenticated by Rarity.

## Validate and close

1. Sign in through the installation's HTTPS URL with the new password from an allowed network.
2. Confirm sessions that existed before recovery no longer authorize requests.
3. Confirm the correlated audit and outbox facts exist and contain no password material.
4. Record the command result, release digest, reason, authorizer, operator, time, and validation outcome in the incident record. Do not record the password.
5. If the command reported failure, preserve the error and investigate before retrying. If the new password is subsequently lost, rerun the same audited reset; there is no password rollback or disclosure path.
