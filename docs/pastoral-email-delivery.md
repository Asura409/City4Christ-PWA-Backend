# Appointment emails and reminders

The API writes email intent in the same PostgreSQL transaction as a booking change.
The existing VPS email worker delivers it through the official Resend Go SDK.
There is no new public endpoint, SMTP listener, cron job or separate worker service.
No real accounts or historical bookings are backfilled.

## Delivery rules

- A new pending request queues an update for the assigned pastor.
- Approval or decline queues an update for the requesting member.
- Cancellation queues updates for both participants, including the cancelling actor.
- Approval also queues reminders for both participants at 24 hours and 1 hour before
  the appointment. Thresholds already reached at approval time are skipped; the
  approval update covers short-notice confirmations.
- Unlisting does not cancel existing appointments or their reminders. Revoking
  pastoral authority suppresses pending/approved updates and reminders, and all
  emails addressed to that former provider. The verified member can still receive
  a cancellation or decline update.

Emails use the purple responsive City4Christ template. Subjects are generic account
updates. Neither HTML nor plain text includes names, dates, booking identifiers,
contact details of the other participant, confidential reasons or care notes. The
button links to the protected frontend `/#/pastors` route without credentials or
sign-in tokens. Users sign in to see current appointment details.

## Reliability and privacy

Migration 41 adds a metadata-only outbox. Each booking version/event/recipient has
one unique row. Repeated booking requests and decisions do not create duplicate
messages. Workers claim rows with `FOR UPDATE SKIP LOCKED`, acquire the same
provider advisory lock used by booking mutations, and recheck the current booking
version, status, account verification and pastoral authority before dispatch.
Cancellation waits for an already in-flight dispatch; an email already accepted by
the provider cannot be recalled. Unsent superseded messages are discarded.

Provider retries reuse `pastoral-email/<outbox UUID>`. A successful provider call
followed by a lost database commit therefore retains the same deduplication key.
This is at-least-once processing with provider deduplication, not an exactly-once
email-delivery guarantee. [Resend retains keys for 24 hours](https://resend.com/docs/dashboard/emails/idempotency-keys).
Queue eligibility is bounded to 23 hours after its scheduled time; pending/approved
messages and reminders also expire no later than the appointment start. An overdue
24-hour reminder expires by the 1-hour reminder threshold.

Delivery makes at most five attempts, with exponential backoff, a ten-second send
limit, and a bounded provider Retry-After. Non-retryable rejections stop immediately.
Expired, sent, failed and discarded rows remain as delivery metadata and are not
automatically requeued. Errors and worker logs contain safe classifications only.
Recipient email addresses are loaded from currently verified accounts at send time;
no email bodies or copied recipient addresses are stored in this outbox.

## Opt-in rollout

Keep the existing worker's Resend key, sender, TLS database configuration and
`EMAIL_RECIPIENT_ALLOWLIST` policy. No new secrets are needed. These additional
non-secret settings go in the existing private worker environment file:

```dotenv
PASTORAL_EMAIL_ENABLED=true
PASTORAL_EMAIL_APP_URL=https://city4christ-pwa.vercel.app/
```

The app URL must be HTTPS with no user credentials, query or fragment; a hosting
base path such as `/City4Christ-PWA/` is supported. Use the frontend address rather
than the API address. The worker appends the protected appointments route.

The flag defaults to false. `EMAIL_DELIVERY_MODE=disabled` still disables the whole
worker. When only the pastoral flag is false, verification/recovery delivery keeps
working and the worker never queries the new outbox. New booking mutations still
queue rows, which expire normally; there is no historical booking scan.

Release order: apply migration 41 through the existing forward-migration deployment,
install the updated worker binary, configure the frontend URL and enable the flag,
then restart the existing service. The worker does not migrate or reset a database.
This implementation does not modify the running VPS worker or staging environment.
See [VPS worker operations](vps-email-worker.md) for its existing service commands.

## Local checks

Tests cover transactional enqueue/rollback, repeated mutations, timed reminders,
short-notice approval, cancellation, expiry, revoked roles, account verification,
concurrent workers, lost commits, retry limits, Retry-After, recipient allowlists,
URL validation, private template content and enqueue through global HTTP routes.
All database fixtures use separate disposable PostgreSQL clusters; provider calls
use fakes or loopback HTTP. No live email is sent by the tests.

Verified locally for this slice: all tests in `internal/pastoral`,
`internal/mailresend`, `cmd/email-worker` and `internal/database` passed with the
race detector. Targeted pastoral HTTP tests through `internal/app` passed with the
race detector. Go vet for affected packages, formatting and diff checks passed.
The updated Docker image built successfully and its disposable PostgreSQL smoke
checks passed, including repeated forward migrations preserving a fixture record.
These results do not claim a fresh full-repository test run or live delivery
acceptance. No live worker settings, secrets or staging records were changed.
