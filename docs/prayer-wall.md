# Prayer Wall

The first Prayer Wall slice lets verified members create and read prayer requests, acknowledge that they prayed, and mark their own request answered or archived.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/prayer/requests?status=open&limit=20&cursor=...` | List requests visible to the signed-in member |
| `POST` | `/api/v1/prayer/requests` | Create a request with `body`, `visibility`, optional `groupId`, and `anonymous` |
| `GET` | `/api/v1/prayer/requests/{requestId}` | Read one visible, non-archived request |
| `PATCH` | `/api/v1/prayer/requests/{requestId}` | Owner marks `answered` or `archived` |
| `PUT` | `/api/v1/prayer/requests/{requestId}/prayed` | Idempotently acknowledge an open, visible request |

Every endpoint requires a verified member Bearer session. Community requests are available to signed-in members. Group requests are visible only to active members of that church group. Private requests are visible only to their owner. Staff roles have no privacy bypass. A request marked anonymous hides its author from other members; its owner sees `author: "You"` and `isOwner: true`. Inaccessible records return 404. Collections use a stable cursor and enforce visibility in SQL.

Request bodies are trimmed plain text of 1–3,000 Unicode characters. `groupId` is required only for group visibility. Status filters support open and answered; archived requests are hidden from lists and detail. Reads/writes have account and IP limits.

This slice does not include comments, file/media attachments, prayer notifications, or staff moderation.
