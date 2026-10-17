# Sermon publishing and media links

Editors use a verified, active Bearer session. The backend checks PostgreSQL for `content_editor` or `system_administrator` on every editor request; roles are not trusted from request bodies or JWT claims. No existing user is promoted by migrations, and no public role-assignment API is provided.

## Routes

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/v1/admin/sermons` | Create a draft; always starts at version 1 |
| GET | `/api/v1/admin/sermons/{sermonId}` | Read current metadata, status and version, including drafts |
| PATCH | `/api/v1/admin/sermons/{sermonId}` | Edit metadata or change publication state using the current version |

POST/PATCH require an allowed Origin, application/json, an active editor session and are limited to 60 attempts per editor per minute. Bodies are capped at 16 KiB; unknown fields are rejected. CORS preflight supports PATCH. Visitors continue to use the public catalog endpoints, which hide drafts and future publications.

Create a draft:

```json
{
  "title": "Living in Faith",
  "summary": "A message about faith and community.",
  "speaker": "Pastor Joy",
  "series": "Foundations",
  "topic": "Faith",
  "preachedAt": "2026-10-11T14:00:00Z",
  "videoUrl": "https://youtu.be/AbCdEf12345"
}
```

The video ID is illustrative; use a church-approved video. Title and speaker are required (1–200 characters after trimming). Summary, series and topic default to empty strings, bounded to 10,000/200/100 characters. preachedAt is a required timestamp with an explicit offset and is normalized to UTC microsecond precision. Drafts do not appear publicly.

Publish using the returned ID and current version:

```json
{ "version": 1, "status": "published" }
```

The response includes `version: 2`, `status: "published"`, and the server-assigned publishedAt. An already-published sermon retains its original publication timestamp during edits or repeated publish actions.

Schedule publication:

```json
{ "version": 2, "status": "scheduled", "publishAt": "2027-01-10T15:00:00Z" }
```

Supply a timestamp that is future when the request is validated. Sub-microsecond values are truncated before checking the boundary. Scheduling an already-published sermon hides it immediately until the new instant. Visibility is evaluated during reads; no cron job is needed to publish on schedule.

Unpublish:

```json
{ "version": 3, "status": "draft" }
```

This clears publishedAt and hides the sermon immediately. Editor detail returns publishedAt=null for drafts.

Omitted fields retain their stored values exactly, including existing summary formatting. Normalization applies only to fields explicitly supplied.

Metadata updates use the same PATCH endpoint, e.g. `{ "version": 4, "title": "Updated title" }`. Supply at least one change. Optional null fields are treated as omitted; use an empty string to clear summary, series, topic or videoUrl. Title and speaker cannot be cleared. publishAt must accompany status=scheduled and cannot be supplied for other statuses. publishedAt, role and actor cannot be assigned by clients.

If another update committed first, return 409 `version_conflict` without modifying the sermon or adding an audit event. Reload editor detail and deliberately reconcile changes. Do not blindly retry with an incremented version.

## Media policy

This slice supports public HTTPS YouTube watch and youtu.be links only. Links normalize to `https://www.youtube.com/watch?v=VIDEO_ID`, where the ID has the supported eleven-character YouTube form. Start/tracking parameters are discarded. Credentials, ports, fragments, lookalike hosts and other providers are rejected. The database independently constrains stored URLs to the canonical form.

No request fetches, probes, embeds raw HTML or streams the supplied video. The frontend decides how to display the canonical link. Validation confirms the URL format, not that a video exists, belongs to the church, or remains accessible. videoUrl is optional and omitted from public responses when absent. Audio, Vimeo, private media and uploads require later explicit adapters/access policy.

## Authorization and audit operations

Only an approved database operator may provision staff roles outside this API. A role row belongs to a specific verified account ID. Use approved change tracking for grants/revocations; never grant editor access automatically during signup or staging seed scripts. Removing the role denies subsequent calls without refreshing the access token. Logout/replay revocation also invalidates editor authentication.

Every successful create/edit/publication writes `sermon_audit_events` in the same transaction as the sermon change. Audit records contain actor, sermon, action, version, status at the time and UTC timestamp. They omit the sermon body, tokens and credentials. Audit insertion failure rolls back the change; row locks and version checks prevent lost updates. The API exposes no audit-edit/delete route. Foreign keys retain actor/sermon references, so future account-deletion and retention workflows must account for audit retention.

Migration 00008 adds roles, version/media columns and audit records forward-only. Existing sermons retain metadata and get version 1 with no video. No roles or content are seeded, and no staging reset is performed.

## Executed verification

Publishing tests first failed on missing implementation and then the missing role schema. A failing read-only transaction regression led to using lock-compatible authorization transactions. Tests cover verified editor checks, member denial, revoked roles, the full draft/publish/unpublish/schedule lifecycle, publication boundaries, canonical media links, stale and concurrent updates, and rollback of both create and edit on audit failure.

Global-app tests first failed with 404 before the editor routes were wired. They exercise real sessions and PostgreSQL, Origin checks, public draft visibility, PATCH preflight, validation, privilege-field rejection, audit errors, role revocation and logout. An additional failing regression caught PostgreSQL timestamp rounding at the scheduling boundary. All tests use disposable PostgreSQL rather than staging.

Swagger UI enables PATCH in Try it out. Run `node scripts/test-swagger-config.cjs` to verify the actual initializer supports all editor methods and retains same-origin credentials without persisting authorization.
