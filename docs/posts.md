# Church community posts

The first community slice provides a persistent, text-only church-wide feed. A
verified member can create a post and read the feed after signing in. Each post
shows its author’s display name and creation time. The feed uses a stable cursor
and defaults to 20 posts per page (maximum 100).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/posts?limit=20&cursor=...` | Read the signed-in member feed |
| `POST` | `/api/v1/posts` | Publish `{ "body": "..." }` |

Writes require a valid Bearer access token, an allowed `Origin`, and JSON. Text
is trimmed and must contain 1–5,000 Unicode characters. Reads and writes have
per-account and shared-IP rate limits. The feed is church-wide; this slice does
not introduce private group audiences.

Comments, reactions, media uploads, editing/deletion, reports, and moderation
remain future M5 work. Until the media upload pipeline is in place, posts do not
accept image, audio, or video URLs.
