# Visitor welcome and private follow-up: first slice

The owner selected **90 days** for visitor contact retention. This slice covers
an optional welcome form and authorized staff follow-up, separate from login
registration, church membership and the ordinary member directory.

The public frontend entry is `/#/welcome`, also linked from Sign in. A Welcome Desk
QR code can point to that same URL; it contains no identity token or personal data.
The form asks for a full name and at least one email/phone contact. Phone numbers
use international `+` country-code format. Contact methods remain unverified.

The checkbox explicitly permits storage for up to 90 days and contact about the
visit, not marketing. The server supplies the exact consent text and version;
both are retained with receipt time. Unsupported family, children, prayer, baptism,
account-linking and member-management fields are rejected in this first slice.
No welcome email/SMS is sent to an unverified supplied address or number. Secure
account claiming, family relationships and further welcome-team task delegation
remain later work. The existing verified login/signup flows are unchanged.

## Privacy and authority

- Only verified **Super Admin, Assistant Pastor and Staff** accounts receive
  global `visitors.follow_up`. Finance, technical administrators, content editors,
  ministry leaders and ordinary members receive no access through those roles.
- Lists return names and workflow metadata, not email or phone. Opening a detail
  record is a separate audited read. Staff cannot search contact values, export
  contacts, edit identities or attach a submission to an account through these APIs.
- Names, email and phone are stored together as authenticated ciphertext using
  Go's maintained AES-GCM implementation and automatic random nonces. HKDF
  separates encryption and retry-digest keys; HMAC protects request fingerprints.
  The dedicated visitor root key is separate from the auth/OTP configuration.
  Ciphertexts are bound to their record UUID and key ID.
- Key IDs support rotation with retained previous read keys. Missing/wrong keys or
  tampered data fail closed; there is no plaintext fallback. No personal contact
  values appear in audit rows, in-app staff notices, push previews or URLs.
- Every disclosed name/contact read and workflow change writes metadata audit in
  the same transaction. Audit failure aborts the response/change. Current roles
  are checked again for each service operation; same-session revocation applies.

Public receipt is generic, whether an email is new or already used by an account.
A matching email never creates, verifies, changes or links an account. Client UUIDs
make unchanged retries safe; changed content under the same UUID returns 409.
Distinct submissions/shared contact methods are not silently merged into one person.
IP limits (60 attempts/hour and 300/day, allowing shared Welcome Desk Wi-Fi) bound public submissions. Form state is
kept in memory, cleared on success, and never stored by the app in localStorage.

## Staff workflow

The Profile page shows **Visitor follow-up** only for the relevant capability.
The private `/#/visitors` page lists New, Contacted or Closed records with bounded
pagination. Opening details shows the supplied contact methods and consent notice.
Staff record status using optimistic version checks; stale changes require reload.
The UI requires explicit confirmation before removing contact details on a confirmed
request to stop follow-up. Staff should verify identity separately before any future
account changes; the visitor form is not identity proof.

A fresh submission records one generic staff inbox notice transactionally. Push,
if separately enabled/consented, stays generic and uses the announcements preference.
No real notice, email or push is sent by disposable tests.

## Retention and withdrawal

All visitor contacts in this slice expire exactly **90 × 24 hours** after receipt;
follow-up/closure does not extend that period. Reads and changes are denied once
expired. Withdrawal immediately clears ciphertext, key ID and request digest.
Replaying an old submission UUID cannot resurrect withdrawn or expired contacts.
Consent/workflow metadata and non-identifying tombstones remain for auditing.

The running API performs bounded expiry passes at startup and every minute,
scrubbing only expired visitor contacts. Batches use row locks/SKIP LOCKED, so
multiple API processes are safe. This is the approved visitor retention policy,
not a reset of staging or another domain. Manual catch-up is available as:

```sh
api visitors-purge
```

The command uses the API environment/database and does not run migrations. A pass
handles at most 1,000 records; repeat when catching up a larger backlog. Keep the
API/scheduled cleanup running and alert on its redacted retention errors. During
an outage, API reads still reject expired data when service resumes; the next pass
scrubs overdue ciphertext. Backups, restored copies and sensitive browser handling
still require the church's operational retention/recovery controls. Never restore
an old date to bypass expiry.

## API

All responses are no-store. Public POST and staff writes require an allowed Origin.
Staff routes also require a verified Bearer session, current permission and rate
limits. No account, contact lookup or public status endpoint exists.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/visitors/config` | Availability, 90-day notice and policy version |
| POST | `/api/v1/visitors` | Consented intake; generic 202 acknowledgment |
| GET | `/api/v1/admin/visitors?status=new&limit=20&cursor=...` | Audited names-only queue |
| GET | `/api/v1/admin/visitors/{visitorId}` | Audited contact detail |
| PATCH | `/api/v1/admin/visitors/{visitorId}` | Versioned status/withdrawal |

Example public input (generate a new UUID for a new form; preserve it for retries):

```json
{
  "submissionId": "11111111-1111-4111-8111-111111111111",
  "fullName": "Synthetic Visitor",
  "email": "visitor@example.invalid",
  "phone": "",
  "consent": true,
  "policyVersion": "visitor-welcome-v1"
}
```

PATCH example: `{"status":"contacted","version":1}`. Valid statuses are `new`,
`contacted`, `closed` and `withdrawn`. Errors use the normal 400/401/403/404/409/
422/429/503 envelopes; field validation includes guidance. Unknown fields and
queries are rejected. The public form and staff queue show genuine unavailable,
empty, validation and conflict states rather than reporting a false receipt.

## Configuration and rollout

Intake is **disabled by default**. Generate a dedicated private settings file on
the server, without printing a key into the terminal/chat:

```sh
api visitor-keygen NEW_PRIVATE_FILE
```

It creates a new mode-0600 file, refuses overwrite, and writes directly usable
hosting/Docker settings: `VISITOR_INTAKE_ENABLED=false`, `VISITOR_DATA_KEY_ID=v1`,
`VISITOR_DATA_KEY_BASE64`, and optional `VISITOR_DATA_PREVIOUS_KEYS_JSON`.
Keep the generated key private and backed up separately from database exports.
The frontend still needs only its existing public `API_URL`; no visitor key goes
into browser configuration.

Apply additive migration 46 using the normal forward deployment procedure. Configure
these API settings privately, verify protected staff access and retention operation,
then explicitly set `VISITOR_INTAKE_ENABLED=true` for approved acceptance. No real
intake, key generation, migration or feature enablement is performed by the local
implementation/testing steps. For rotation, choose a new ID/current key and retain
old ID/base64 entries in `VISITOR_DATA_PREVIOUS_KEYS_JSON` until their records expire.
Do not remove required read keys or extend retention by re-encrypting old records.
