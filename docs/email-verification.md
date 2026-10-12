# Email verification slice

The app exposes these proposed additions under `/api/v1`:

| Method | Path | Body | Success |
| --- | --- | --- | --- |
| POST | `/auth/register` | `firstName`, `lastName`, `email`, `password` | 201, unverified account summary; encrypted verification email queued |
| POST | `/auth/verification/verify` | `email`, `code` | 200, `{"data":{"status":"email_verified"}}` |
| POST | `/auth/verification/resend` | `email` | 202, generic eligibility message |

Signup remains closed by default while registration policy and the remaining auth controls are unfinished. Tests explicitly enable it. Verification does not issue access tokens or refresh cookies; login/session implementation follows in another slice.

All three endpoints require JSON, reject unknown fields, null objects, and trailing JSON, and cap request bodies at 16 KiB. Validation errors use 422; malformed requests use 400; unknown/expired/consumed/exhausted verification codes share a generic 400 response. HTTP responses never include OTPs, hashes, or encrypted outbox contents. Missing/invalid verification configuration makes auth unavailable with 503; closed signup is still 403.

## Challenge rules

- Codes contain eight cryptographically random digits, including possible leading zeros.
- Codes expire ten minutes after issue; they are invalid at the exact expiry instant.
- At most five guesses are allowed per issued code. Failed guesses are committed to PostgreSQL. A correct fifth guess succeeds; a correct guess after five failures is rejected.
- Successful verification sets the account's verification timestamp, consumes the challenge, clears its keyed digest, and removes its queued email.
- Resend waits sixty seconds after the last successful issue; the boundary is eligible. It creates a different code, resets that code's attempts, and replaces the pending email. Unknown and already verified accounts queue nothing.
- Resend's public response is identical for cooldown, unknown, verified and eligible accounts. It intentionally exposes no account-specific `Retry-After` header. Separate account/IP rate limits remain pending.
- PostgreSQL account row locks serialize all verification and resend transactions. One concurrent verification can succeed, one concurrent resend can issue a replacement, and concurrent invalid guesses cannot exceed five persisted attempts.

## Secret handling and delivery

The verification configuration requires a securely generated 32-byte server secret. HKDF-SHA256 derives separate keys for HMAC-SHA256 challenge digests and AES-GCM email encryption. These operations use Go's maintained standard cryptographic libraries; the application implements no cryptographic algorithms. AES-GCM generates and carries its nonce via `NewGCMWithRandomNonce`. Challenge identifiers and account identifiers bind digests and authenticated ciphertext to their purpose.

Persist the configured secret across restarts. Changing it invalidates outstanding challenges and makes their existing outbox payloads unreadable; key rotation/reissue must be an explicit operational action, never a data reset. No secret is generated automatically by the app.

Account creation, challenge creation, and encrypted email queuing commit together. A queue failure rolls back the account. A failed resend preserves the old challenge and email. `OpenEmail` is an internal trusted-adapter hook for trusted worker/inbox use; it is not an HTTP endpoint.

The Resend SDK adapter and opt-in delivery worker now implement retry/claim coordination and expired-payload cleanup. The protected staging inbox and live provider setup remain pending. The outbox stores only the latest pending encrypted email per account; it is replaced on resend and removed on provider acceptance or successful verification. Provider acceptance leaves the challenge usable. See [Resend onboarding](resend.md). No external mail was sent during tests.

## Verified tests

The failing service tests were run before implementation. Once the service existed, tests failed against real PostgreSQL before the migration existed. The route tests also failed before wiring. After implementation and refactoring, `go test -race ./... -count=1 -timeout=5m` passed, including the existing foundation tests, and `go vet ./...` passed.

The disposable PostgreSQL harness does not consume staging/application URLs. No staging data was accessed, migrated, reset, or otherwise changed.
