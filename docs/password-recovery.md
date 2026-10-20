# Email password recovery

Password recovery requires the API migration, updated VPS email worker and frontend from this release. Follow the release order below before testing live delivery.

## User flow

On the frontend sign-in page, choose **Forgot password?**, enter your verified account email, and request a reset link. Open the email link, enter and confirm a new password, then sign in normally. The new password must have at least 15 characters and at most 1024 UTF-8 bytes. Passwords are not trimmed.

Links expire after 15 minutes and work once. Resending after the 60-second cooldown invalidates the previous link. Requesting a reset does not change the password or end any sessions. Completing a reset atomically replaces the Argon2id hash, consumes the token, revokes every existing session (including access JWTs and refresh cookies), and queues a password-change notification. An in-flight login using the old hash cannot create a session after the reset commits.

Google-only accounts have no local password. They must use **Continue with Google**, and recover their Google account through Google if necessary. Recovery cannot add a local password to these accounts. Unverified email/password accounts must verify their email first.

## API

All requests require JSON and an allowed `Origin`. Browser requests use `credentials: 'include'`. No Bearer token is required. Responses have `Cache-Control: no-store`.

`POST /api/v1/auth/password/forgot`

```json
{"email":"member@example.invalid","returnTo":"https://city4christ-pwa.vercel.app/auth.html"}
```

The `returnTo` URL must end with `/auth.html`, use HTTPS, and have an exact origin listed in `APP_ORIGIN` or `CORS_ORIGINS`. Credentials, query strings and fragments are forbidden. This also supports a GitHub Pages project prefix. The server never constructs links from the request Host header. Unknown, unverified, Google-only and cooldown accounts receive the same 202 response. There are five requests per normalized email per 15 minutes, in addition to the shared IP auth limit of 60 requests per minute.

`POST /api/v1/auth/password/reset`

```json
{"token":"token extracted privately from the email link","password":"your new private password"}
```

Success returns 200 with `data.status=password_reset` and clears the refresh cookie. Invalid, expired, superseded or consumed tokens return 400 `invalid_reset_token`. Invalid passwords return 422. There are ten submissions per token per 15 minutes plus the shared IP auth limit. Unknown JSON fields are rejected.

The 256-bit random credential uses the existing standard-library secure random generator; only its SHA-256 digest is stored in the challenge table. The email outbox is encrypted and authenticated using the existing AES-GCM implementation with a separate recovery binding. The link carries the token in its fragment, so it is not sent in page HTTP requests. The frontend removes the fragment from the current history entry on initialization, keeps the token only in memory, and uses a no-referrer policy. Reloading that page requires reopening the email link. No passwords, reset tokens or provider responses should be logged.

## Release order

1. Release the API with additive migration `00013_password_recovery.sql`. Do not reset any database.
2. Rebuild and restart the existing VPS email worker from the same release, after the migration is present. The old binary delivers verification OTPs only; recovery emails will remain queued until the new worker is running. The worker never runs migrations.
3. Release the frontend. Its existing `API_URL` is sufficient; no new environment variable or secret is needed. Its HTTPS origin must already be allowlisted by the API.
4. Use an approved test account to verify actual Resend receipt, reset, rejection of the old password and existing sessions, successful new-password login, and rejection of a reused link.

The recovery sender uses the official Resend Go SDK, provider idempotency keys, the existing recipient policy, bounded delivery timeouts and five-attempt retry limits. Reset emails expire with their challenge; password-change notifications expire after 24 hours. Worker failures contain safe classification codes only. Provider acceptance does not guarantee inbox delivery.

The design follows the [OWASP password recovery guidance](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html). Local tests use isolated PostgreSQL and captured provider requests; they send no real email and never connect to staging.

## TDD verification

Before implementation, the service test failed because recovery did not exist, global-app requests returned 404, delivery/sender/recipient-guard tests failed on missing implementations, the OpenAPI test failed on missing paths, the frontend client test failed on missing methods, and both real browser projects failed on the missing Forgot password control. Subsequent failing regression tests exposed an in-flight old-password login race, an existing-tab fragment navigation issue, a broadcast self-redirect, and PostgreSQL timestamp precision differences. These were corrected and the corresponding tests rerun.

The regression suite covers token expiry, cooldown and supersession, concurrent single use, encrypted outbox binding and tampering, provider retries, password validation, old-password rejection, JWT/refresh revocation, transactional rollback when notification insertion fails, Google-only and unverified account exclusion, origin/target restrictions, strict JSON, account limits, schema 12-to-13 data preservation, and real frontend recovery on desktop/mobile. Live Resend inbox acceptance remains a separate release check.

Final local verification on 20 October 2026: `go test -race -p 1 ./... -count=1 -timeout=10m` passed, as did `go vet ./...`, Swagger configuration checks, 11 frontend unit tests, eight mocked browser scenarios, four real-API desktop/mobile workflows, and the final Docker build plus disposable PostgreSQL smoke script. An earlier concurrent race run hit its five-minute timeout during Argon2id work; the final run used one package at a time and a larger local timeout. Hash parameters and CI settings were not weakened. The concurrency test observation window accommodates race-instrumented hashing. These results do not establish live email delivery or deployment acceptance.
