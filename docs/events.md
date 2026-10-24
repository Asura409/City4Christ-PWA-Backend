# Church events API

Events are church-wide. Only published events appear in the public event list and detail endpoints. Times are supplied as RFC 3339 instants and paired with an IANA timezone for display.

## Public and member endpoints

- `GET /api/v1/events` returns published events, ordered by start time. An optional bearer token adds `myAttendeeCount` for the signed-in member.
- `GET /api/v1/events/{eventId}` returns one published event.
- `POST /api/v1/events/{eventId}/rsvp` requires a signed-in member and JSON `{ "attendeeCount": 1 }`. The count includes the member and guests and is limited to 1–10. Each account has one RSVP per event; repeating the same count is safe.
- `DELETE /api/v1/events/{eventId}/rsvp` cancels that account's RSVP before the event starts and releases its seats.

An event may have a positive integer `capacity` or no limit. Capacity is enforced transactionally. If the requested RSVP would exceed remaining seats, the API returns HTTP 409 with code `event_full`; there is no waitlist. RSVP changes are closed once the event starts or is cancelled.

Event records include `id`, `title`, `description`, `location`, `startsAt`, `timeZone`, `capacity`, `attendeeCount`, `myAttendeeCount`, `status`, `version`, `createdAt`, and `updatedAt`. The public list only returns `status: "published"`.

## Event management

Accounts with the global `events.manage` capability can use:

- `GET /api/v1/admin/events` to list drafts, published, and cancelled events.
- `POST /api/v1/admin/events` to create a draft. Required fields are `title`, `startsAt`, and `timeZone`; `description`, `location`, and `capacity` are optional.
- `PATCH /api/v1/admin/events/{eventId}` to replace event details and set `status` to `draft`, `published`, or `cancelled`. Supply the current `version`; stale versions return a conflict.

Super Administrators, Assistant Pastors, and Staff receive `events.manage`. Event creation, publication, edits, cancellation, and RSVP changes are recorded in audit tables. Cancelled events cannot be restored.

OpenAPI schemas and response details are in `internal/app/apidocs/openapi.json`.
