# Direct messaging

The first messaging slice supports durable one-to-one conversations over REST.
Only verified accounts can participate. Any verified member may start a chat by
recipient account ID; the React picker discovers members through the existing
opt-in directory, which omits accounts whose owners keep their profiles hidden.
The API does not return email addresses or other private account fields.

## Privacy boundary

Every history, send, and read-receipt operation checks the caller's participant
row. Non-participants receive the same `404` response as an unknown conversation.
Staff, pastors, administrators, and ministry leaders receive no backdoor access to
private conversations. Conversation creation is idempotent per member pair, so
opening the same pair from either direction returns the same conversation.

Message bodies are durable in PostgreSQL and are not included in API request logs.
The API does not yet provide message deletion, moderation, attachment upload,
search, group conversations, realtime delivery, typing indicators, or presence.
Retention and any exceptional access policy must be decided before those features
are added. Read and send operations remain available over HTTPS REST without a
realtime service.

## API

All routes require `Authorization: Bearer <access token>`. Writes also require an
allowed `Origin` header. Error responses use the common City4Christ error shape.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/conversations` | List the caller's conversations, last message, unread count, and both read cursors |
| `POST` | `/api/v1/conversations` | Start or reopen a one-to-one conversation |
| `GET` | `/api/v1/conversations/{conversationId}/messages` | Read participant-only history with older-message pagination |
| `POST` | `/api/v1/conversations/{conversationId}/messages` | Send an idempotent message |
| `PUT` | `/api/v1/conversations/{conversationId}/read` | Advance the caller's monotonic read receipt |

Start a conversation:

```json
{"recipientId":"22222222-2222-4222-8222-222222222222"}
```

Send a message with a client-generated UUID. Reuse the same `clientMessageId`
when retrying the same body after a network timeout. Reusing the ID with different
content returns `409 idempotency_conflict`.

```json
{
  "clientMessageId":"7219a6ea-b436-48ca-9d75-b2b8f5dd18ef",
  "body":"Good morning. How can I pray with you today?"
}
```

Message bodies are trimmed, must contain 1–4,000 Unicode characters, and allow
newlines and tabs. History returns messages in ascending sequence order. Use the
`meta.nextBeforeSequence` cursor to retrieve older messages; zero means there is
no older page. The client marks the latest visible message read by sending an
empty JSON object or `{"throughMessageId":"<message UUID>"}`. The stored read
sequence only advances, even if older messages are marked afterward.

The OpenAPI contract is served at `/openapi.json` and through `/docs/`.

## Group conversations

Each existing church group has one durable group chat. `GET /api/v1/group-conversations`
returns only chats the verified caller can currently access. Church-audience chats
require active group membership. Leadership-audience chats require a current
Super Admin, Assistant Pastor, or Ministry Leader role, following the existing
group audience policy. All group chat history/send/read requests recheck that
eligibility; leaving a church group immediately hides its history. Staff do not
receive a history bypass.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/group-conversations` | List currently accessible group chats and unread counts |
| `GET` | `/api/v1/groups/{groupId}/messages` | Read group history with older-message pagination |
| `POST` | `/api/v1/groups/{groupId}/messages` | Send an idempotent group message |
| `PUT` | `/api/v1/groups/{groupId}/read` | Advance the caller's group read receipt |
| `DELETE` | `/api/v1/groups/{groupId}/messages/{messageId}` | Redact a message when authorized to moderate that group |

Super Admin, Assistant Pastor, and Staff may moderate group content; a Ministry
Leader may moderate only the group in their role scope. Moderation replaces the
message body with an empty value and records an audit event. Other participants
see a removed-message placeholder. A moderator role does not grant group history
access. Group messages use REST and PostgreSQL; realtime delivery, attachments,
notifications, typing indicators and presence remain future work.

The group chat UI is available from the Chat screen's Group chats tab. The current
`API_URL` environment setting is sufficient; the backend must allow the frontend
origin and apply migration 00028 before the UI is released.
