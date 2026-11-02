# Direct messaging

The messaging API supports durable one-to-one conversations over REST.
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
Typing indicators and online status are ephemeral WebSocket signals; they are not
stored or used as proof that a person read a message. Message attachments reuse
the existing private R2 upload and malware-scan pipeline. Read and send operations
remain available over HTTPS REST without a realtime service.

## API

All routes require `Authorization: Bearer <access token>`. Writes also require an
allowed `Origin` header. Error responses use the common City4Christ error shape.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/conversations` | List the caller's conversations, last message, unread count, and both read cursors |
| `POST` | `/api/v1/conversations` | Start or reopen a one-to-one conversation |
| `GET` | `/api/v1/conversations/{conversationId}/messages` | Read or search participant-only history with older-message pagination |
| `POST` | `/api/v1/conversations/{conversationId}/messages` | Send an idempotent message |
| `PATCH` | `/api/v1/conversations/{conversationId}/messages/{messageId}` | Edit your own message within 10 minutes |
| `DELETE` | `/api/v1/conversations/{conversationId}/messages/{messageId}` | Redact your own message within 10 minutes |
| `PUT` | `/api/v1/conversations/{conversationId}/read` | Advance the caller's monotonic read receipt |

Start a conversation:

```json
{"recipientId":"22222222-2222-4222-8222-222222222222"}
```

Send a message with a client-generated UUID. Reuse the same `clientMessageId`
when retrying the same body and attachment IDs after a network timeout. Reusing
the ID with different content or files returns `409 idempotency_conflict`.

```json
{
  "clientMessageId":"7219a6ea-b436-48ca-9d75-b2b8f5dd18ef",
  "body":"Good morning. How can I pray with you today?",
  "attachmentIds":["a40c381c-2f34-40a7-a31b-11c66213fb72"]
}
```

`attachmentIds` is optional and accepts up to four unique upload IDs. Reserve
each upload with purpose `message_attachment`, upload its bytes to the returned
private R2 URL, complete the upload, and wait until its scan status is `available`.
The message send verifies ownership, purpose, scan state, and object availability
in the same transaction as the message. Responses include signed attachment URLs
that expire after 30 minutes; storage object keys are never returned. The existing
allowlist and limits apply: JPEG/PNG/WebP/GIF images up to 20 MiB, MPEG/MP4/WAV/OGG
audio up to 50 MiB, and MP4/WebM video up to 100 MiB. Messages still require text.
Group moderation removes the message-to-upload association, so removed messages
no longer expose attachments. When a deleted or moderated message was the last
message using a clean private object, a durable worker job deletes that object
from R2; shared objects remain until their final message reference is removed.

Message bodies are trimmed, must contain 1–4,000 Unicode characters, and allow
newlines and tabs. History returns messages in ascending sequence order. Use the
`meta.nextBeforeSequence` cursor to retrieve older messages; zero means there is
no older page. The client marks the latest visible message read by sending an
empty JSON object or `{"throughMessageId":"<message UUID>"}`. The stored read
sequence only advances, even if older messages are marked afterward.

Search within a conversation with `?query=prayer&limit=20`. Search is
case-insensitive substring matching, accepts 2–120 characters, and uses the same
participant authorization and sequence cursor as history. It only searches message
bodies; attachment contents are not indexed.

The sender may edit or delete their direct message strictly before ten minutes
after its server-recorded creation time. The server enforces the window even if a
client clock is wrong. Edits replace the visible body and show an `editedAt` time;
an audit row records the actor, action, and time without keeping prior body text.
Deletion redacts the body for both participants, removes its attachment links,
queues unshared clean files for asynchronous R2 deletion, and leaves a
deleted-message marker in history. Attempts after the window return
`409 mutation_window_expired`; another participant receives `404`. Group moderator
removal remains a separate permission-controlled action.

The OpenAPI contract is served at `/openapi.json` and through `/docs/`.

## Group conversations

Each existing church group has one durable group chat. `GET /api/v1/group-conversations`
returns only chats the verified caller can currently access. Church-audience chats
require active group membership. Leadership-audience chats require a current
Super Admin, Assistant Pastor, or Ministry Leader role, following the existing
group audience policy. All group chat history/send/read requests recheck that
eligibility; leaving a church group immediately hides its history. Staff do not
receive a history bypass.

Group history accepts the same optional `query`, `beforeSequence`, and `limit`
parameters. Search includes only non-redacted message bodies and rechecks current
group chat eligibility on every request.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/group-conversations` | List currently accessible group chats and unread counts |
| `GET` | `/api/v1/groups/{groupId}/messages` | Read or search group history with older-message pagination |
| `POST` | `/api/v1/groups/{groupId}/messages` | Send an idempotent group message |
| `PUT` | `/api/v1/groups/{groupId}/read` | Advance the caller's group read receipt |
| `DELETE` | `/api/v1/groups/{groupId}/messages/{messageId}` | Redact a message when authorized to moderate that group |

Super Admin, Assistant Pastor, and Staff may moderate group content; a Ministry
Leader may moderate only the group in their role scope. Moderation replaces the
message body with an empty value, detaches files from the message, and records an
audit event. Other participants see a removed-message placeholder. A moderator
role does not grant group history access. Group messages use REST and PostgreSQL;
realtime delivery is available for connected, currently authorized group members.
Typing indicators and chat-scoped online status are ephemeral WebSocket signals.
The broader notification pipeline remains separate.

The group chat UI is available from the Chat screen's Group chats tab. The current
`API_URL` environment setting is sufficient; the backend must allow the frontend
origin and apply migration 00028 before the UI is released.

## Realtime connection tickets

`POST /api/v1/realtime/tickets` requires the same Bearer access token and an
allowed `Origin`. It returns an opaque 32-byte ticket with a 30-second lifetime.
The server stores only its SHA-256 digest and binds the ticket to the current
verified account, session, and access-token expiry. Account and IP rate limits
apply. Tickets are single-use, and logout/revocation invalidates an unconsumed
ticket.

Connect to `wss://<api-origin>/api/v1/realtime` from an allowed browser origin.
Do not put the ticket in the URL or a subprotocol. Send it in the first JSON
frame immediately after the WebSocket handshake:

```json
{"type":"authenticate","ticket":"<ticket from POST /api/v1/realtime/tickets>"}
```

After authentication the server sends `{"type":"ready"}`. A client may send
`{"type":"ping"}` and receives `{"type":"pong"}`. The server also sends
WebSocket control pings to detect dead connections. Message writes continue to
use the REST endpoints so their validation and idempotency rules remain in
effect.

Typing is sent as an ephemeral control frame, scoped to a direct conversation or
an authorized group. The server verifies current membership, excludes the sender
from fanout, and rate-limits signals to one per 300 ms per socket. These frames
never contain message text and are not persisted:

```json
{"type":"typing.start","scope":"direct","conversationId":"<conversation UUID>"}
{"type":"typing.stop","scope":"group","groupId":"<group UUID>"}
```

Recipients receive `typing.start` or `typing.stop` with the scope ID and sender
account ID. The client should clear a typing indicator after a short timeout if
the stop signal is lost during a disconnect.

Online status is visible only inside an existing direct conversation or a group
chat the member can currently access. After selecting a chat, the client sends a
`presence.subscribe` frame using the same scope ID fields; it sends
`presence.unsubscribe` when leaving that chat. The initial direct snapshot reports
whether the other participant currently has a chat socket. A group snapshot reports
the number of other connected members. Later `presence.online` and
`presence.offline` events update that state when an account's first chat socket
connects or its last one disconnects. The server rechecks current chat eligibility
before fanout. Online means connected to this single API instance's chat socket,
not a guarantee that the person is available. Presence is held in memory only and
is cleared on disconnect or server restart; multi-instance fanout remains deferred.

New direct and group messages produce a `message.created` event containing the
scope, conversation or group ID, message ID, and monotonic sequence. Group
moderation produces `message.deleted`. Event frames deliberately omit message
bodies; fetch content from the corresponding REST history endpoint. Events are
sent only to connected participants or currently eligible group members. The
server rechecks session revocation periodically and closes the socket by access
token expiry. A slow client is disconnected when its bounded event queue fills.

Delivery is an online notification, not a durable event log. On connect or after
reconnect, fetch the relevant REST history and use its sequence cursor to catch
up; duplicate notification or history data should be reconciled by message ID.
There is no cross-instance WebSocket fanout yet, so deploy more than one API
instance only after adding shared pub/sub. PostgreSQL remains the durable source
of truth.
