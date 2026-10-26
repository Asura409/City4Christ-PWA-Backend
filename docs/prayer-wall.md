# Prayer Wall

The first Prayer Wall slice lets verified members create and read prayer requests, acknowledge that they prayed, and mark their own request answered or archived.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/prayer/requests?status=open&limit=20&cursor=...` | List requests visible to the signed-in member |
| `POST` | `/api/v1/prayer/requests` | Create a request with `body`, `visibility`, optional `groupId`, and `anonymous` |
| `GET` | `/api/v1/prayer/requests/{requestId}` | Read one visible, non-archived request |
| `PATCH` | `/api/v1/prayer/requests/{requestId}` | Owner marks `answered` or `archived` |
| `PUT` | `/api/v1/prayer/requests/{requestId}/prayed` | Idempotently acknowledge an open, visible request |
| `GET` | `/api/v1/prayer/requests/{requestId}/encouragements` | List encouragements under the parent request's visibility rules |
| `POST` | `/api/v1/prayer/requests/{requestId}/encouragements` | Add a plain-text encouragement and notify the request owner |

Every endpoint requires a verified member Bearer session. Community requests are available to signed-in members. Group requests are visible only to active members of that church group. Private requests are visible only to their owner. Staff roles have no privacy bypass. A request marked anonymous hides its author from other members; its owner sees `author: "You"` and `isOwner: true`. Inaccessible records return 404. Collections use a stable cursor and enforce visibility in SQL.

Request bodies are trimmed plain text of 1–3,000 Unicode characters. `groupId` is required only for group visibility. Status filters support open and answered; archived requests are hidden from lists and detail. Reads/writes have account and IP limits.

Encouragement bodies are trimmed plain text of 1–1,000 Unicode characters.
Every encouragement list and create checks the parent request's current visibility:
community, active group membership, or owner-only private access. Archived and
expired requests cannot receive encouragements. Replies expose display names but
never account IDs. Creating a reply writes a generic in-app notice to the request
owner in the same PostgreSQL transaction; private request text is not copied into
the notice. Notifications are not sent to the author when they encourage their own
request.

This slice does not include file/media attachments or staff moderation of encouragements.

## Reports, moderation and expiry

Members may report a visible community or group request once, with reason
`safety`, `spam`, `inappropriate`, or `other` and optional details (up to 1,000
characters). Private requests are not reportable through this API. Report
responses and moderator queues omit reporter identity and the author's identity;
anonymous display is preserved.

- `POST /api/v1/prayer/requests/{requestId}/reports` files a report.
- `GET /api/v1/prayer/reports?status=pending&limit=20` lists reports in the
  caller's moderation scope.
- `PATCH /api/v1/prayer/reports/{reportId}` accepts `{ "decision": "hide",
  "reason": "..." }` or `dismiss`. Hiding soft archives the request. Dismissing
  closes the report without changing the request.

An active group leader can review reports about that leader's own group.
Super Administrators can review community and group reports. Assistant Pastors
and Staff can review community reports only. Removed leaders lose access when
their active group membership ends. No role can read or report another person's
private request.

Requests expire 180 days after creation. Expiry soft archives rather than deleting
text, acknowledgements, or audit history. Expired pending reports are closed as
`expired`. Expiry is processed in bounded batches during prayer API activity;
expired requests are excluded from reads immediately by the database query. Data
retention/deletion policy beyond this archive period still requires a church-wide
privacy decision.

Moderation and expiry actions are written to `prayer_moderation_events`. Report
and moderation history is retained with the request. This slice does not expose
any donor/member directory fields or support restoring hidden requests.
