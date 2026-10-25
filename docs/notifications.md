# In-app notifications

Members can list their own notifications, check unread count, and mark an item
read. Clients cannot create notices or choose another account as the recipient.
Domain code inserts notifications inside its transaction, so the related action
and notice can commit or roll back together. Optional deduplication keys prevent
retries from creating duplicate notices.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/notifications?limit=20&cursor=...` | List the caller's newest notices |
| `GET` | `/api/v1/notifications/unread-count` | Return the caller's unread count |
| `PUT` | `/api/v1/notifications/{notificationId}/read` | Idempotently mark the caller's notice read |

Every route requires a verified member Bearer session. Reads are account-scoped;
attempting to mark another account's notification read returns 404. Writes require
an allowed browser Origin. Pagination uses a sequence cursor; clients should return it unchanged. Each notification contains plain text and an
optional same-origin React hash route. It does not expose donor, prayer, message,
email or other private content by design. Delivery is currently in-app only;
there is no email, SMS, Web Push, broadcast or retry worker in this slice.

Use `Repository.CreateTx` from a domain transaction. `DedupKey` should be stable
for one domain event and recipient, for example a group decision ID or coordinator
audit event ID. Event coordinator assignments notify only the assigned member and
link to Events. The UI refreshes the unread count when opening a page and after
marking an item read.
