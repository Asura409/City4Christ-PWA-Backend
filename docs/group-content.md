# Group posts, events and permission audit

This local slice completes group content before extending notifications. Existing
church-wide content and memberships are preserved. Migration 44 adds nullable,
immutable group audiences; omitting `groupId` keeps the previous church-wide behavior.
The migrations do not reset, reclassify or seed existing data.

## Posts and moderation

- Create with `POST /api/v1/posts` and `{ "body": "Group update", "groupId": "<UUID>" }`.
- Read the exact feed with `GET /api/v1/posts?groupId=<UUID>&limit=20`.
- Omit `groupId` to read the church feed. Group posts never appear in that feed.
- Existing comments, reactions, owned clean attachments, author edits/deletion and
  reports work on group posts. Every operation rechecks the parent audience.
- Edits cannot move a post to another group or to the church feed.
- Report queues accept `groupId`. Group leaders moderate only their assigned scope;
  global moderators retain church/group oversight subject to leadership eligibility.
  Church-wide queues exclude group report snapshots. Reports retain their existing
  reason, audit and snapshot policy; the production retention decision remains open.

Reading or participating requires a verified account, active membership, an active
group and, for leadership groups, current leadership eligibility. Knowing a post,
comment, group or upload association ID does not authorize access. Membership loss,
role loss and archiving take effect on subsequent requests. Server denials use the
normal not-found result; already-issued signed media URLs retain their short TTL.
Management authority is not automatic membership or permission to post as a member.

## Events

Create an event with the existing `/api/v1/admin/events` input plus `groupId`.
The existing edit/publish/cancel workflow, optimistic version checks and capacity
rules are reused; the audience is immutable. Global event managers or leaders with
`ministry.members.manage` for that exact active group can manage its events.
Leadership eligibility is checked before management, even for global staff.

`GET /api/v1/events?groupId=<UUID>` selects a group's published events for active
members. `/api/v1/admin/events?groupId=<UUID>` is its authorized editorial list.
The unfiltered public list includes church-wide events and only those group events
visible to its authenticated viewer; anonymous callers never see group events.
The unfiltered editorial list remains church-wide.

Group detail, RSVP, coordinator reads and calendar export enforce the audience.
Authorized group managers can review coordinator metadata without becoming group
members. Coordinator assignments for a group event cannot use a different group.
Private calendar downloads send Bearer headers and use `Cache-Control: no-store`.
Registered attendees receive a generic transactional notice when a published event
changes or is cancelled; group notice reads recheck current membership.

## Frontend

Group details link to **Group posts** and **Group events**. The scoped feed reuses
comments, reports, moderation and attachment rendering. Its own text composer and
the shared upload composer preserve the selected group audience and show it before
sharing. Event creation is fixed to the group selected by the route. Existing
church-wide pages remain available.

## Verification boundary

Repository/service tests use disposable PostgreSQL. Global-app tests exercise
private reads, writes, calendars, coordinator scope, membership/verification loss
and exact-group moderation. Desktop/mobile browser coverage includes group posting,
leader event creation and a real-API group broadcast received by another member.
Provider/device acceptance and deployment are separate steps.
