# Devotional publication foundation

Editors can create and edit plain-text drafts, publish immediately, and withdraw
published entries back to draft. Readers see only currently published content.
There are no seeded devotionals, external scripture fetches or staging mutations.

## Permission and publication rules

The new global `devotionals.manage` capability follows the existing sermon editorial
roles: Super Admin, Assistant Pastor, Staff, and the existing legacy content-editor
and system-administrator assignments. Finance, access-administrator and ministry
leadership assignments alone do not grant devotional editing. Role delegation is
unchanged; this feature does not assign roles. Every editorial operation rechecks
verified-account authority in PostgreSQL, including inside write transactions.

Creating an entry always produces a draft with version 1. PATCH requires its current
positive version and at least one editable field or status. A stale version returns
409. Publishing sets server-controlled publishedAt; withdrawal clears it. Editing
an entry while its status stays published changes its public content immediately.
The frontend makes this action explicit as Publish changes; Save as draft withdraws
it. Each successful mutation records actor, entry, action, version and UTC time in
an audit transaction. Audit failure rolls back the content mutation. Audit records
do not contain a copy of the devotional body; full revision history is deferred.

All text is plain text. Title and body are required after trimming, with maximums
of 200 and 20,000 Unicode characters. Optional author and scriptureReference allow
200 characters each. Unsupported control characters are rejected; body newlines
and tabs are allowed. Readers escape HTML rather than execute it.

## API

All paths have the `/api/v1` prefix. Responses use the existing data/error envelope
and no-store cache policy. Public DTOs omit editorial status, version and timestamps
other than publishedAt. Author is editorial display text, not an account identity.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/devotionals` | Published entries, latest publication first |
| GET | `/devotionals/{devotionalId}` | Published detail; draft/withdrawn/missing returns 404 |
| POST | `/admin/devotionals` | Create a draft; returns 201 and Location |
| GET | `/admin/devotionals` | Editorial list including drafts |
| GET | `/admin/devotionals/{devotionalId}` | Editorial detail and current version |
| PATCH | `/admin/devotionals/{devotionalId}` | Edit, publish or withdraw with current version |

Published reads are public, like the sermon catalog. Editorial routes require a
verified Bearer session and current global capability. Mutations also require an
exact allowed Origin. Unknown JSON fields and fields such as publishedAt or actorId
are rejected. No draft-visibility query flag is accepted on public endpoints.

Create body:

```json
{
  "title": "Hope today",
  "body": "A reflection and prayer for today.",
  "scriptureReference": "Psalm 28:7",
  "author": "City4Christ"
}
```

Publish with `{"version":1,"status":"published"}`. Withdraw using the latest version
and status draft. Editable content fields can accompany status in a PATCH.

Lists accept limit (default 20, maximum 100) and cursor. Public order is descending
publishedAt/id; editorial order is descending createdAt/id. Use nextCursor from the
same endpoint; empty means the end. The cursor is a pagination position, never an
authority credential. Visibility is reapplied on every page. Unknown and duplicate
query parameters are rejected; detail and write operations accept no query string.

Limits: public reads 120/IP/minute; editorial reads 60/account/minute; writes
30/account/minute, with a 120/IP/minute ceiling per editorial operation class.
Malformed input returns field-specific 422, missing session 401, denied authority
403, stale version 409, rate limit 429, and unavailable storage 503.

See `/openapi.json` (1.18.0) and `/docs/` for schemas and interactive examples.

## Frontend and rollout

`/#/devotionals` is linked from Home and Word. The PWA requires sign-in for that
screen, while the API's published catalog remains public. Editorial tools appear
only for the explicit capability. The form creates a draft before publishing,
keeps unsaved text on conflicts, and offers an explicit latest-version reload.
No draft bodies are stored in localStorage or service-worker caches. Reloading
clears a previously opened reader entry before refetching publication visibility.

The frontend's existing permission parser validates capability names. Ship its
`devotionals.manage` compatibility addition before the backend starts advertising
the new capability, or coordinate the two deployments. Then apply additive
migration 42 and release the devotional routes, followed by the reader/editor UI.
Do not serve the new UI as a fake fallback when the API is unavailable.

No push, merge, deployment, live editor assignment or live content seeding is part
of this local implementation. Staging acceptance remains with the testing team.
Daily-verse selection and timezone rules, scheduled publication, rich text, media,
notifications and full content revision history remain later slices.

## Verified local checks

- TDD red/green checks for the service/repository, publication/privacy boundaries,
  editorial capability, global HTTP routes, OpenAPI contract and browser editor.
- Full devotional, access-policy and migration packages passed with the race
  detector against disposable PostgreSQL. Targeted global HTTP, current-access,
  origin and rate-limit checks passed with the race detector.
- 39 frontend unit tests passed. The broader React/access/devotional browser run
  passed 34 scenarios; the final devotional run passed all 12 desktop/mobile
  scenarios, including field feedback and selection-state regressions.
- The build-tagged browser harness passed the real Go API workflow with separate
  editor/reader sessions on desktop and mobile using disposable PostgreSQL.
- Production frontend build, Go vet, formatting, diff checks and Swagger editor
  checks passed. OpenAPI validates with the same eight existing warnings.
- Docker build and smoke checks passed, including forward migration 42 applied
  twice while preserving the disposable fixture record.

These results do not claim a fresh full-repository test run or staging acceptance.
No release, live role assignment or live content creation was performed.
