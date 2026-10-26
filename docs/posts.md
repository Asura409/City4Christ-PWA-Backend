# Church community posts

The community slice provides a persistent church-wide feed. A
verified member can create a post and read the feed after signing in. Each post
shows its author’s display name and creation time. The feed uses a stable cursor
and defaults to 20 posts per page (maximum 100).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/posts?limit=20&cursor=...` | Read the signed-in member feed |
| `POST` | `/api/v1/posts` | Publish `{ "body": "..." }` |
| `PUT` | `/api/v1/posts/{postId}/reaction` | Add the signed-in member’s `{ "reaction": "amen" }` reaction |
| `DELETE` | `/api/v1/posts/{postId}/reaction` | Remove the signed-in member’s Amen reaction |

Writes require a valid Bearer access token, an allowed `Origin`, and JSON. Text
is trimmed and must contain 1–5,000 Unicode characters. Reads and writes have
per-account and shared-IP rate limits. The feed is church-wide; this slice does
not introduce private group audiences. Each post includes `amenCount` and a
viewer-specific `reacted` boolean. Amen writes are idempotent and return the
current count and the caller’s reaction state.

Post creation accepts up to four owned uploads after malware scanning; feed
responses include short-lived signed URLs for attached media. Comments,
post editing/deletion, reports, and moderation remain future M5 work. See the
OpenAPI contract for request and response schemas.
