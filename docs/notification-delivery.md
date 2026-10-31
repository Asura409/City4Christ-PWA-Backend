# Notification completion and optional Web Push

In-app notices remain durable and private. Preferences affect push delivery only;
disabling push never removes the inbox. Migration 45 adds preferences, immutable
broadcast drafts/audits, session-bound subscriptions and durable delivery jobs.
Existing notices and accounts remain unchanged. No worker, credentials, broadcasts
or live subscriptions are installed or enabled by applying this source change.

## Member API

All routes require a verified active Bearer session. Writes also require an allowed
Origin. Unknown JSON fields/queries are rejected; account/IP limits apply.

| Method | Route | Purpose |
| --- | --- | --- |
| GET / PUT | `/api/v1/notifications/preferences` | Read/replace all six push preferences |
| PUT | `/api/v1/notifications/read-all` | Mark only the caller's currently visible inbox read; body `{}` |
| GET | `/api/v1/notifications/push/config` | Availability and public VAPID key, never a private key |
| POST | `/api/v1/notifications/push/subscriptions` | Bind browser subscription to the authenticated account/session |
| DELETE | `/api/v1/notifications/push/subscriptions` | Remove the caller's endpoint; body `{ "endpoint": "..." }` |

Preferences: `pushEnabled` defaults false; `groups`, `events`, `pastoral`, `messages`
and `announcements` default true. PUT requires all six booleans. The Notifications
page provides categories, mark-all-read, explicit device enable/disable controls and
fallback messages when push is unavailable. Permission is requested only after a
user clicks enable. Browser support/permission and installation requirements vary;
real supported-device acceptance remains necessary.

Subscriptions accept `{endpoint, keys: {p256dh, auth}}` from the browser Push API.
The HTTPS provider allowlist covers Google/FCM, Mozilla, Apple and Windows Push.
Redirects, arbitrary hosts, invalid keys and oversized inputs are rejected. At most
ten subscriptions are retained per account. An endpoint owned by another live
session/account cannot be stolen. Expired/revoked bindings can be replaced through
explicit opt-in. Delivery rechecks session validity, so logout/recovery revocation
stops subsequent worker sends. A previously accepted/in-flight push cannot be recalled.
Subscription endpoints and auth secrets are sensitive: keep them out of logs,
exports and frontend configuration; store them only in the protected database.

## Broadcast workflow

- `GET /api/v1/admin/broadcasts[?groupId=<UUID>]`: latest 50 for an authorized audience.
- `POST /api/v1/admin/broadcasts`: immutable draft `{title, body, audience, groupId?}`.
- `GET /api/v1/admin/broadcasts/{id}`: review content and current recipient count.
- `POST /api/v1/admin/broadcasts/{id}/send`: `{ "version": 1, "confirm": true }`.

`audience` is `church` (no group ID) or `group` (required canonical group UUID).
Global `broadcasts.send` belongs to Super Admin, Assistant Pastor and Staff; an
exact-scope ministry leader may send only to their active group. Leadership audience
eligibility is checked before group operations. No appointment powers are added.

Draft creation sends nothing. To correct a draft, create a replacement; this slice
does not edit or delete broadcast history. The UI shows the selected audience,
requires review and explicit confirmation, then sends. Recipients are selected at
send time, not frozen by a preview. One transaction locks the draft, rechecks
current authority, creates deduplicated inbox notices/jobs, records the recipient
count and writes the audit. Retries/concurrent sends do not duplicate inbox records.
The audience is limited to 10,000 verified accounts per broadcast. Group recipients
must have current visible membership; their private broadcast text disappears from
inbox reads/counts after membership, eligibility or group availability is lost.
A successful response counts inbox records, not provider delivery or human receipt.

Existing membership decisions, coordinator assignments and pastoral transitions use
the same transactional producer. Direct/group messages now create generic notices
without message text, and event changes/cancellations notify eligible RSVP accounts.
Message retries do not duplicate notices. Visitor welcome automation remains deferred
with the visitor domain; SMS is not implemented or enabled.

## Delivery worker and privacy

The maintained [webpush-go library](https://github.com/SherClockHolmes/webpush-go)
handles Web Push encryption and VAPID. The backend does not implement the protocol's
cryptography. Push payloads and the service worker use only this generic preview:
“City4Christ — You have a new update. Open the app to view it.” Clicking always opens
the PWA's own Notifications page; provider-supplied text/URLs are ignored.

A newly committed notice queues jobs only for opted-in subscribed devices and
selected categories. No old inbox is backfilled when someone opts in. The worker
leases jobs with SKIP LOCKED, checks preferences/verification/session/group access
again before sending, and skips read or older-than-24-hour notices. It uses ten-second
provider timeouts, six bounded attempts, exponential retry for network/429/5xx
failures, and removes expired subscriptions on 404/410. Other provider failures
remain failed for operator review. The inbox remains available during outages.

Web Push is at least once around a crash after provider acceptance; a fixed topic
and notification tag coalesce generic previews. Provider acceptance does not prove
that a phone displayed a notification. Delivery rows/audits currently have no
automatic retention purge. Operators can inspect aggregate job states without
exporting endpoints, secrets or private content:

```sql
SELECT state, count(*) FROM notification_push_jobs GROUP BY state;
```

## Rollout: explicit operator work, not performed here

1. Apply normal forward migrations using the approved deployment procedure.
2. Build/install `cmd/notification-worker` as `~/.local/bin/city4christ-notification-worker`.
   The Docker image also includes `/app/notification-worker`.
3. Generate settings directly into a **new** private file on the server:

   ```sh
   city4christ-notification-worker generate-keys ~/.config/city4christ/notifications.env
   ```

   The parent directory must exist. The command uses the OSS key generator, creates
   the file with mode 0600, refuses to overwrite it and never prints either key.
4. Privately add `DATABASE_URL` with `sslmode=verify-full` to that worker file.
   It already contains `PUSH_ENABLED=true`, `PUSH_VAPID_PUBLIC_KEY`,
   `PUSH_VAPID_PRIVATE_KEY`, and `PUSH_VAPID_SUBJECT=mailto:info@city4christ.org`.
5. Configure **only** `PUSH_ENABLED=true` and the matching public key on the API.
   No VAPID private key belongs in the API or frontend environment. The frontend
   still needs only its existing public `API_URL`.
6. Install the user unit `deploy/city4christ-notification-worker.service`, enable it
   after operator approval, check redacted logs and opt in on an approved test device.
   The worker requires verified database TLS, never runs migrations and never resets
   data. Outbound HTTPS to the browser push providers must be available.

Keep key files backed up privately. Rotating VAPID keys requires browser
resubscription; after browser subscription expiry, reopen and explicitly enable
this device again. Real provider/device acceptance has not been claimed by local
stub/browser tests. No live push, broadcast or email is sent by the automated tests.
