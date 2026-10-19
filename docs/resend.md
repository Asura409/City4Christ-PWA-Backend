# Resend onboarding

Resend is the selected provider. The official Go SDK adapter, PostgreSQL delivery loop, and opt-in worker command are implemented. Live Resend delivery has not been exercised. The chosen sender is `City4christ <info@city4christ.org>`. Domain verification, API key configuration, and approved test recipients remain unconfirmed.

## Provider setup

1. Add `city4christ.org` in Resend and publish the DNS records its dashboard provides. Wait for verification.
2. Use the selected sender: `City4christ <info@city4christ.org>`.
3. Create a Sending access API key restricted to that domain. Store it in backend/worker secrets; never commit it or paste it into chat.
4. Choose exact staging recipient addresses, or set `EMAIL_RECIPIENT_ALLOWLIST='*'` explicitly to allow all beta recipients. Empty staging policy remains invalid.

Official references: [Go setup](https://resend.com/docs/send-with-go), [domain verification](https://resend.com/docs/dashboard/domains/introduction), [API key permissions](https://resend.com/changelog/new-api-key-permissions).

## Worker environment

`.env.example` lists variable names and safe defaults. It contains no credentials and is not loaded automatically.

| Variable | Purpose |
| --- | --- |
| `EMAIL_DELIVERY_MODE` | Default empty/`disabled`; explicitly set `resend` to enable external sending |
| `APP_ENV` | `staging` or `production` when live sending is enabled |
| `DATABASE_URL` | Existing application database; forward migrations through 00003 must already be applied |
| `VERIFICATION_KEY_BASE64` | Base64 encoding of the same persistent 32-byte verification secret used by the API |
| `RESEND_API_KEY` | Sending-only Resend API key stored as a secret |
| `EMAIL_FROM` | Exact sender on your verified domain |
| `EMAIL_RECIPIENT_ALLOWLIST` | Comma-separated exact addresses, or standalone `*` for all recipients; required for staging; optional restriction in production |

Do not generate a new verification key for worker startup or restart. The worker must share the API's existing key to decrypt pending messages. Never use fixed test-fixture keys in a deployed environment.

Once the schema and environment are intentionally configured, the worker command is:

```sh
go run ./cmd/email-worker
```

The command does not run migrations, reset data, or enable signup. Empty/disabled delivery mode stays idle until shutdown without connecting to a database or provider. It supports graceful shutdown, polls once per second, and redacts provider/database details from logs. The API executable and staging Blueprint are described in [Render staging](render-staging.md). Authentication sessions and other auth features remain pending.

## Email appearance

Verification emails include a responsive HTML layout and a plain-text alternative. The purple palette follows the official [City4Christ website](https://city4christ.org/): deep plum `#2B0831` with `#542361` accents and a soft lavender code panel. The embedded template lives in `internal/mailresend/verification.html`; it uses inline styles, presentation tables, a prominent selectable eight-digit code, and an absolute UTC expiry. No remote fonts, images, tracking pixels, or verification links are added. HTML is rendered with Go's escaping template library and remains identical across retries for the same challenge.

Changing the template changes the provider payload. Apply it before issuing a fresh test challenge, and avoid changing message settings while retrying a pending challenge under the same idempotency key.

## Delivery behavior

- The worker locks the account before reading its pending email, matching verification/resend lock ordering. Concurrent workers use `SKIP LOCKED` and do not dispatch the same current challenge concurrently.
- It revalidates challenge identity, attempts, verification state, and expiry before dispatch. Stale, expired, consumed, or exhausted emails are discarded. Invalid ciphertext is marked as a permanent failure.
- Outbound requests are bounded to ten seconds or remaining challenge lifetime, whichever is shorter. Account verification/resend for the same user can wait for this bounded send to finish; this preserves ordering.
- The adapter uses the official SDK with stable `email-verification/<challenge-id>` idempotency keys. Text contains the absolute expiry rather than a changing countdown. Provider idempotency lasts 24 hours, longer than the ten-minute OTP lifetime. [Resend idempotency](https://resend.com/docs/dashboard/emails/idempotency-keys).
- Timeouts, network errors, 408/409/429 and 5xx responses are retryable. Backoff starts after the failed request finishes, with five attempts maximum. The retry schedule starts at five seconds and doubles; numeric `Retry-After` hints can extend it.
- Permanent failures or exhausted delivery retries are retained with a safe error code and failure timestamp until challenge expiry. Later worker passes remove expired payloads. Manual resend creates a fresh challenge; no automatic failed-record reset occurs.
- Successful provider acceptance removes the outbox entry but leaves the OTP available for verification. Acceptance does not prove inbox delivery. Delivery/bounce webhooks and receipt monitoring remain pending.
- Keep sender/template settings stable while retrying a challenge, because Resend requires the same payload for an idempotency key. A fresh resend issues a new challenge/key when message settings change.
- Staging recipient restrictions are checked before contacting Resend. A blocked address is marked as a permanent rejection; the provider sees no request for it.

## TDD verification

Adapter tests failed before implementation. They run the official SDK against a local HTTP mock and verify authentication, fixed endpoint selection, idempotency keys, message content, safe errors, and response classification. PostgreSQL worker tests failed first with a missing method, then with missing delivery columns; the forward migration made them pass. A further failing regression test caught backoff being measured before a slow provider call finished; the implementation was corrected.

The global app integration test registers an account, replaces its code through resend, dispatches through the SDK to the local mock, and verifies the received code through the global app. No real provider requests or external emails are used by automated tests. Live sending needs separately configured domain/sender/secrets and a deliberate delivery check.

Final local verification after refactoring passed: `go test -race ./... -count=1 -timeout=5m`, `go vet ./...`, the `cmd/email-worker` build, formatting, and `git diff --check`. Remote CI and live Resend delivery have not been observed. Nothing was deployed or merged, and no staging database was accessed.

For open beta email delivery, set `EMAIL_RECIPIENT_ALLOWLIST='*'` in the VPS worker environment and restart the worker. Do not mix `*` with addresses. API registration separately requires `REGISTRATION_ENABLED=true`; email verification and rate limits still apply. A previously blocked OTP needs a fresh resend request.

Workers revalidate queue entries after acquiring the account lock. If another worker has drained an entry, it is ignored without an error. Retry delays and permanent-failure flags are checked again before dispatch, so stale queue candidates cannot bypass them. Expired or consumed challenges are still cleaned up. PostgreSQL regressions exercise all three stale-claim states and concurrent single dispatch.
