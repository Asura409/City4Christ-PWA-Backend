# Reviewed account-deletion requests

A verified member can request account deletion from their own Profile. This is a
review request: it immediately disables sign-in, revokes every session and hides
the member from the directory. It does **not** permanently erase the account,
shared posts, messages, bookings or audit history. The email address remains
reserved. There is no automatic reactivation, rejection, completion or purge flow.
No permanent-erasure deadline or new retention period is promised by this slice.

The server provides the exact notice and policy version
`account-deletion-request-v1`; the member must explicitly confirm that version.
An ordinary current session alone is insufficient: request submission requires
fresh primary authentication, plus a recent app MFA proof when the account has
an enrolled factor. Google-only members reauthenticate with Google and do not need
to create a local password. A request must not leave the church without an
effective Super Admin. Pending or approved pastoral appointments whose end time
is still in the future must be cancelled or resolved through their existing
workflows before either participant can request account deletion. Historical
appointments remain intact.

## Member API

All paths begin with `/api/v1`, require verified Bearer authentication and return
`Cache-Control: no-store`. Writes require an allowed `Origin`, JSON and browser
`credentials: 'include'`. Unknown JSON fields and query parameters are rejected.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/me/deletion-request` | Current availability, notice, policy version and own request state |
| DELETE | `/me` | Submit a reviewed request; does not permanently delete data |

```json
{"confirm":true,"policyVersion":"account-deletion-request-v1"}
```

An accepted request returns `202` with `data` containing its ID, account ID,
`status: "requested"`, version, confirmed policy and timestamps. The response clears
the Secure, HttpOnly refresh cookie and issues no replacement token. The frontend
must clear credentials and show the accepted-request confirmation. Its next use of
the old session returns `401`, including a retry after a response was lost.

One transaction records the request and audit metadata, sets
`accounts.deletion_requested_at`, hides the directory profile, revokes all
sessions, consumes verification/reset challenges, removes queued OTP/recovery
mail and deletes push subscriptions and their delivery jobs. Subsequent sign-in,
refresh, protected operations and relevant deliveries must exclude disabled
accounts. Existing roles remain historical records and confer no effective
capability while the account is disabled.

Repeated eligible internal requests reuse the same unique account request rather
than duplicating it. No public account lookup or email-existence endpoint exists.
Submission permits five attempts per account and twenty per IP per 15 minutes.

## Super Admin review API

Every review list, detail and status change independently requires current global
`accounts.deletion.review`, fresh primary authentication and a recent app MFA
proof. Only Super Admin receives this capability; Staff, Assistant Pastor, finance,
technical administrator and ministry roles alone do not. Permission revocation
takes effect on the next operation within the existing session.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/admin/account-deletion-requests` | Names and workflow metadata; no email |
| GET | `/admin/account-deletion-requests/{requestId}` | Audited private detail, including email |
| PATCH | `/admin/account-deletion-requests/{requestId}` | Mark a request as being reviewed |

List parameters are `status=requested|reviewing`, `limit=1..100` (default 20) and a
UUID `cursor`. Default status is `requested`. Responses contain a `data` array and
`meta.nextCursor`. Contact details are never included in the queue. Each disclosed
queue row, detail view and status change writes metadata audit in the same
transaction; audit failure aborts disclosure or mutation. Audit records contain
identifiers, action, status, version and time, without copied contact information.

```json
{"status":"reviewing","version":1}
```

PATCH requires the current version and returns the updated request with incremented
version. A stale version returns `409 version_conflict`; reload before making a
new decision. The only statuses are `requested` and `reviewing`. Reviewing does not
reactivate an account or claim that its data was erased. These review operations
permit thirty attempts per account and 120 per IP per minute.

## Errors and release

- `400`: malformed JSON, invalid field types or unknown fields.
- `401`: missing, expired, revoked or disabled-account session.
- `403`: forbidden capability, disallowed origin, required fresh primary proof or
  required MFA proof (`reauthentication_required` / `mfa_required`).
- `404`: unknown review request.
- `409`: `last_administrator`, `blocked_active_commitments` or `version_conflict`.
- `422`: missing confirmation, stale policy, invalid query/version or unsupported status.
- `429`: account/IP rate limit; `503`: required dependency unavailable.

Migration `00047_account_deletion_requests.sql` is additive. It does not disable
existing accounts, seed requests, purge data or reset staging. Apply migration 47 before installing or starting updated API or worker binaries:
their eligibility checks reference the new account column. Release the API and
coordinate a separately authorized worker update so authentication and delivery
consistently enforce the active-account policy. Request transactions remove queued
OTP/recovery mail even before the worker update, but a provider call already in
flight cannot be recalled. No worker is upgraded automatically. No new provider
or secret is required. Configure backup,
retention and any future permanent-erasure procedure separately through the
approved operational policy. Local disposable-PostgreSQL tests and fake-delivery
checks do not establish live deletion or production acceptance.
