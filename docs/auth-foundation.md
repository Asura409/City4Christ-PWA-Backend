# Auth foundation

Source: `City4Christ_PWA_Backend_PRD.docx` (11 October 2026), plus the requested auth requirements.

## Implemented

- Argon2id adapter using `github.com/alexedwards/argon2id` and current `golang.org/x/crypto`. Parameters: v19, 64 MiB memory, three iterations, two lanes, 16-byte random salt, 32-byte key. Empty or malformed stored credentials fail closed.
- PostgreSQL account repository using pgx. Unique normalized emails, nullable local passwords for later Google-only identities, unverified accounts, and database-generated UUIDs/timestamps.
- Embedded Goose forward migration. Rerunning it preserves existing accounts. There is no automatic database reset or destructive down migration.
- Registration service and `POST /api/v1/auth/register` through the global app. Public registration defaults to disabled, pending product policy and remaining authentication controls. Explicitly enabled tests persist unverified accounts without cookies or access credentials. Registration now queues an encrypted verification email transactionally; external delivery is not implemented yet.
- Email verification OTP: eight digits, ten-minute expiry, single use, five attempts, sixty-second resend cooldown. Account row locks serialize verification and resend. Previous codes are invalidated, consumed verifiers removed, and queued email payloads authenticated and encrypted using Go standard cryptographic libraries.
- Resend adapter using the official SDK, bounded delivery retries, PostgreSQL delivery claims, expiry cleanup, and an opt-in email-worker executable. Staging external sending requires approved recipients. Live provider delivery is unverified.
- Global app liveness and readiness routes. Database failure affects readiness, while liveness remains healthy. Errors omit dependency details and include server-generated request IDs.
- GitHub Actions workflow with pinned official checkout/setup-go commits. It installs PostgreSQL binaries and runs formatting, vet, and tests with the race detector. Its remote execution has not been observed.

Registration validates names (1–100 characters), email syntax (maximum 254 bytes), and passwords (minimum 15 characters, maximum 1024 bytes). These are provisional technical limits. Passwords are preserved exactly. JSON bodies are capped at 16 KiB, reject unknown fields, trailing JSON, and null objects. Successful responses omit passwords and hashes.

## Disposable PostgreSQL tests

Each harness caller gets a freshly initialized PostgreSQL cluster and a separate `c4c_test` database. Private temporary Unix sockets are the only connection path; TCP is disabled. Neither `DATABASE_URL` nor any existing application/staging connection is consumed. Cleanup closes connections, stops the owned cluster, and removes only its temporary directory. PostgreSQL 16.15 was installed locally for these tests.

Run tests as a non-root user. PostgreSQL binaries must be on PATH, under Ubuntu's usual `/usr/lib/postgresql/*/bin`, or selected with `TEST_POSTGRES_BIN`. Missing binaries fail rather than skip integration coverage. The repository tests exercise concurrent duplicate inserts, nullable passwords, normalized-email constraints and migration reruns against real PostgreSQL.

## Executed TDD evidence

1. Password tests failed with `undefined: NewPasswordHasher`; added the OSS adapter, then tests passed.
2. Repository tests failed with missing implementation; after repository plumbing, real PostgreSQL returned `relation "accounts" does not exist`. Added the account migration, then tests passed.
3. Health route tests failed with `undefined: New`; added the global app, then tests passed.
4. Registration service and route tests failed with missing service/config symbols; implemented them, then tests passed.
5. Added a null-object regression test; it failed with HTTP 422 instead of 400. Fixed decoding and refactored shared error handling, then reran the full suite.
6. OTP service tests failed with missing service symbols. After service plumbing, real PostgreSQL failed with `relation "verification_email_outbox" does not exist`. Added the forward migration, then service tests passed.
7. OTP global-app tests failed with missing configuration fields. Wired verification/resend routes and transactional registration; the full suite passed.
8. Refactored cryptographic preparation separately from SQL transactions, removed redundant registration wiring, and shared strict JSON decoding across all auth routes. The full race-enabled suite and vet passed again. Tests cover concurrency, exact expiry/cooldown boundaries, encrypted payload tampering, account binding, and simulated outbox failures that must roll back registration/resend.

9. Resend adapter and worker tests failed before implementation and before delivery schema columns existed. Added the adapter, forward migration, and worker, then fixed the failing slow-provider backoff regression. The full race suite, vet, and worker build passed after refactoring.

Final local checks verified:

```sh
go test -race ./... -count=1 -timeout=5m
go vet ./...
git diff --check
```

All passed. `actionlint` v1.7.12 also validated `.github/workflows/test.yml`. These checks do not establish a remote CI result or deployment success.

## Remaining slices

- Live Resend domain/sender/secret setup and a deliberate delivery check; protected staging email inbox and provider delivery/bounce monitoring. The adapter and worker are implemented; no real email has been sent.
- Login, account/IP rate limits, access credentials, refresh rotation/replay, logout and `/api/v1/me`.
- Google OIDC with state, nonce and PKCE; explicit secure account linking, no default local password.
- Runnable API server, Docker, persistent Render staging PostgreSQL, mobile auth test page and protected staging inbox.

Each slice begins with executed failing tests, followed by implementation and refactoring. No deployment or merge has occurred. No staging database has been accessed or changed.

## Open product decisions

- Registration: open, invitation, or approval following verification. Signup remains disabled until decided and completed.
- Exact PWA/API origins and domain relationship for cookie, CORS and CSRF policy.
- Staging inbox operator authentication. Resend is selected; sender domain/address and credentials still need configuration.

## Environment

The repository initially contained only a README and the PRD. No applicable AGENTS.md was found. The cloud runtime skill was read; its environment-status tool and network-policy snapshot were unavailable. Shell execution required escalation because sandbox startup failed with `bwrap: loopback: Failed RTM_NEWADDR`. Official Go 1.27.2 was downloaded into `/tmp/c4c-toolchain` and checked against its release SHA-256. Docker remains absent.
