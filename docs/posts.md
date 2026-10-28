# Church community posts

The community slice provides a persistent church-wide feed. A
verified member can create a post and read the feed after signing in. Each post
shows its author’s display name and creation time. The feed uses a stable cursor
and defaults to 20 posts per page (maximum 100).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/posts?limit=20&cursor=...` | Read the signed-in member feed |
| `POST` | `/api/v1/posts` | Publish `{ "body": "..." }` |
| `PUT` | `/api/v1/posts/{postId}` | Replace your post body and full attachment list |
| `DELETE` | `/api/v1/posts/{postId}` | Remove your own post from the feed |
| `PUT` | `/api/v1/posts/{postId}/reaction` | Add the signed-in member’s `{ "reaction": "amen" }` reaction |
| `DELETE` | `/api/v1/posts/{postId}/reaction` | Remove the signed-in member’s Amen reaction |
| `GET` | `/api/v1/posts/{postId}/comments?limit=20&cursor=...` | Read church-wide comments on a post |
| `POST` | `/api/v1/posts/{postId}/comments` | Add `{ "body": "..." }` to a post |
| `DELETE` | `/api/v1/posts/{postId}/comments/{commentId}` | Delete your own comment |
| `POST` | `/api/v1/content-reports` | Report a post or comment with a reason |
| `GET` | `/api/v1/moderation/content-reports?status=pending&limit=20&cursor=...` | Read the authorized staff review queue |
| `POST` | `/api/v1/moderation/content-reports/{reportId}/review` | Hide reported content or dismiss the report with a reason |

Writes require a valid Bearer access token, an allowed `Origin`, and JSON. Text
is trimmed and must contain 1–5,000 Unicode characters. Reads and writes have
per-account and shared-IP rate limits. The feed is church-wide; this slice does
not introduce private group audiences. Each post includes `amenCount` and a
viewer-specific `reacted` boolean. Amen writes are idempotent and return the
current count and the caller’s reaction state.

Comments are visible to signed-in members, limited to 2,000 Unicode characters,
and returned newest first with stable cursor pagination. Members can delete only
their own comments; requests to delete another member’s comment return not found.
Deleted comments are scrubbed from member reads.

Post creation accepts up to four owned uploads after malware scanning; feed
responses include short-lived signed URLs for attached media. Editing uses full
replacement semantics: omitted `attachmentIds` clears the attachment list. Only
the author can edit or delete a post. Deletion removes the post from public feed,
scrubs its text and removes its comments, reactions, and media associations while
retaining a minimal tombstone. Existing clean uploads remain owned by their
uploader. Migration `00037_post_soft_delete.sql` adds a nullable deletion timestamp
without changing existing posts.

Any verified member can report another member’s visible post or comment using a
documented reason category. Members cannot report their own content; one pending
report per member and target is accepted. Only verified Super Admins, Assistant
Pastors and Staff with the global `groups.content.moderate` capability can read
or act on church-wide reports. A ministry leader's scoped capability does not
grant access to the church-wide queue. Group-specific moderation will require an
exact group-scope check when that content is implemented.

Reports retain a snapshot of the reported text so staff can review it. Moderators
must record a reason to hide content or dismiss a report. Hiding a post scrubs
its text and comments, removes reactions and media associations, removes it from
member feeds, resolves pending reports on the post and its comments, and records
an audit event in one transaction. Hiding a comment scrubs it from comment reads
and writes an audit event. Previously issued short-lived media URLs may remain
valid until expiry; hiding prevents new URLs from being issued. Report snapshots
and audit rows currently have no automatic purge; a retention policy is still
needed before production rollout. Migration `00038_post_content_moderation.sql`
adds reports, audit events and comment tombstones without changing existing
content. See the OpenAPI contract for request and response schemas.
