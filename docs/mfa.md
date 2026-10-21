# Authenticator protection for sensitive actions

This slice adds optional, app-specific TOTP enrollment and recovery to the fresh
primary-authentication foundation. It does not appoint administrators, implement
Super Admin bootstrap, or turn ordinary login into a two-stage login flow.
An authenticated first-factor session can still read its own profile. Once an
account enrolls, role grant/revoke and Google linking additionally require recent
app-specific MFA confirmation. Mandatory MFA for future privileged church roles
remains part of their protected appointment lifecycle.

## Cryptography and storage

TOTP generation, provisioning URI/QR and validation use
[pquerna/otp v1.5.0](https://github.com/pquerna/otp). Parameters are a random 32-byte
secret, six digits, 30-second periods, SHA-1 for authenticator compatibility and
one adjacent time step of tolerance. Accepted counters are persisted and serialized
across sessions, preventing concurrent replay. No OTP algorithm is implemented in
this application.

The standard Go AES-GCM implementation encrypts setup secrets with random nonces.
A domain-separated HKDF key derives from the existing stable
`VERIFICATION_KEY_BASE64`; authenticated data binds account and factor UUID.
Copying ciphertext between accounts/factors or changing the key fails closed.
Preserve the server key; changing it requires an explicit key migration/recovery
plan, not automatic factor resets. No new environment variable is required.

Ten recovery codes are generated with 256 bits of randomness each. Only SHA-256
digests are stored. Codes are exact, case-sensitive 43-character values, displayed
once at activation or regeneration. They are never retrievable afterward.

## HTTP contract

All endpoints require an active verified Bearer session and reject query
parameters and target identifiers. POSTs require an exact allowed Origin,
`application/json`, and primary password/Google authentication within five minutes.
Every response is private and `Cache-Control: no-store`. No endpoint issues or
rotates application tokens or refresh cookies.

| Method and route | Request body | Result |
| --- | --- | --- |
| GET `/api/v1/auth/mfa` | None | `enabled`, `pending`, nullable `freshUntil`, `recoveryCodesRemaining`; no secrets |
| POST `/api/v1/auth/mfa/enroll` | `{}` | Enrollment UUID, private setup key, `otpauth` URI, local PNG QR and expiry |
| POST `/api/v1/auth/mfa/confirm` | `enrollmentId`, six-digit `code` | Activates factor and returns ten recovery codes once |
| POST `/api/v1/auth/mfa/verify` | Six-digit `code` | Confirms this session using the authenticator |
| POST `/api/v1/auth/mfa/recovery` | `recoveryCode` | Atomically consumes one recovery code and confirms this session |
| POST `/api/v1/auth/mfa/recovery/regenerate` | `{}` | Replaces all recovery codes; returns new codes once |
| POST `/api/v1/auth/mfa/disable` | `{}` | Deletes factor, codes and proofs |

Enrollment is session-bound and expires in ten minutes. A new setup replaces only
pending setup, never an active factor. Activation requires a valid authenticator
code in the initiating session. Its code cannot be reused for subsequent MFA
verification. A refresh preserves the session binding but never extends primary
or MFA freshness.

MFA proofs last at most five minutes, capped by primary authentication and session
expiry. A Google-only account first confirms its already-linked Google identity,
then supplies an app authenticator/recovery code. Provider `amr` claims are not
assumed to satisfy the app factor. Reconfirming only the password or Google identity
does not renew an expired MFA proof.

A recovery code cannot log in by itself, reset a password, renew primary proof or
disable the factor. It clears previous MFA proofs, then verifies only the current
session. Regeneration and disabling require both fresh primary and MFA proofs.
Regeneration invalidates every old code and clears other session proofs. Disabling
removes the factor and all codes/proofs atomically. Password recovery does not
remove an enrolled authenticator; revoked sessions cannot use previous proofs.

Status reads are limited to 60/account/minute and 120/IP/minute. Confirm, verify
and recovery share five attempts/account/15 minutes. Enrollment, regeneration and
disabling share five management requests/account/15 minutes. Existing auth IP
limits remain 60/minute. Attempt counters are persisted separately from the
operation transaction, so failed operations do not undo throttling.

Errors include `400 invalid_mfa_code`, `403 reauthentication_required`,
`403 mfa_required`, `409 mfa_already_enabled`, `429 rate_limited`, and generic
`503 unavailable` for storage/key failures. An invalid session returns 401.
Sensitive actions must be explicitly resubmitted after confirmation.

## Transactions, audit and frontend

Mutations lock the verified account before factor/session rows, serialize code
consumption and commit changes with `mfa_audit_events`. Audit failure rolls back
activation, consumption, regeneration or disabling. Audits contain account/session
identifiers, action and time; no seeds, OTPs, recovery codes or provider tokens.
Role mutations verify the still-valid primary and MFA proofs inside their existing
assignment/audit transaction. Google linking checks at both start and callback.
Factor changes cannot race a sensitive action past an obsolete proof.

The independent frontend uses small controller/view modules in
`js/components/mfa/`. Profile, Settings, Staff and signed-in auth pages expose
manual/QR setup, verification, one-time backup-code display, recovery, regeneration
and explicit disable confirmation. Inputs clear after requests; setup and recovery
details clear on hiding, logout and navigation. No secret is placed in URLs,
local/session storage, third-party QR services or service-worker caches.

If activation/regeneration commits but its response is lost, codes cannot be
retrieved. Verify the authenticator and regenerate a new set. Lost-device users
can use one saved recovery code, then disable/re-enroll or regenerate as needed.
Loss of both device and all codes requires a future audited assisted-recovery
procedure; email password reset does not bypass this protection.

## Release boundaries

Migration 16 is additive, preserves existing accounts/sessions/roles and seeds no
factors, codes or proofs. Deploy the backend before the accompanying frontend only
after separate release approval. This task does not deploy, touch staging or
appoint any administrator.

Before production-wide/mandatory rollout, complete privileged-role bootstrap and
last-admin safeguards, audited assisted recovery and factor-change email alerts.
Actual authenticator-device and real Google acceptance remain staging checks after
an authorized release. Local browser tests use disposable data and a synthetic
Google screen; signed Google token verification is covered separately by the
OIDC adapter tests. The lifecycle follows the separation of primary authentication,
second-factor confirmation and recovery in the
[OWASP MFA guidance](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html).

## Verified local checks

TDD red runs observed missing MFA service methods, missing global routes, missing
frontend client methods/controls, a Google-link MFA bypass, an expired primary
proof accepted after a database lock wait, and late private setup data after
navigation. Implementations were added or corrected before the green reruns.

- Full backend `go test -race -p 1 ./... -count=1 -timeout=10m` passed.
  Focused MFA/Google/reauthentication/role race tests passed after the final callback
  deadline correction; normal affected service, migration and global HTTP tests
  passed on the final source as well.
- Disposable PostgreSQL tests verify encrypted/tamper-resistant factors, one-use
  OTP counters, concurrent recovery consumption, proof expiry, session isolation,
  callback rechecking, atomic audit rollback, recovery rotation and safe upgrades.
- `go vet ./...`, Go formatting, Swagger configuration, whitespace and changed
  documentation-link checks passed. OpenAPI 1.9.0 validated with four pre-existing
  warnings; no new MFA contract warnings were introduced.
- Frontend: 18 unit tests and 30 mocked desktop/mobile browser scenarios passed,
  including late-response suppression and browser-storage privacy.
- Six real-API desktop/mobile workflows passed against TLS loopback/global app and
  disposable PostgreSQL. Password and Google-only accounts exercise authenticator
  enrollment, private QR/manual setup, activation, recovery consumption and secret
  clearing. The password workflow also disables the factor. Real-API artifacts use
  a separate temporary output directory to avoid Playwright trace collisions.
- Docker image `city4christ:mfa-verify` built successfully; container smoke checks
  passed, including repeatable forward migrations, test-data preservation,
  anonymous MFA rejection, non-root runtime and clean API/worker shutdown.

These results are local verification. No push, merge, deployment, staging reset,
real email or administrator appointment occurred.
