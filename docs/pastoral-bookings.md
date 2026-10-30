# Book a pastor — approval foundation

Members request an explicit available slot. The owner selected **pastor approval**
on 30 October 2026. A pending request holds the slot; approval retains it. Decline
or cancellation releases it. A fresh request always needs its own approval.

## Access and privacy

- Verified members may browse explicitly listed pastors and request slots.
- Super Admins can publish/replace directory profiles only for an already verified
  Assistant Pastor or Super Admin. Fresh primary confirmation and application MFA
  are mandatory. This operation does not appoint a church role.
- A listed pastor manages only their own slots. Pastoral role revocation takes
  effect on subsequent requests and removes the directory entry. Unlisting stops
  new reservations; an eligible pastor can still review existing assignments.
- Booking lists are limited to the requesting member and assigned eligible pastor.
  Super Admin, Staff, finance and group roles grant no exceptional care access.
- Only names, identifiers, UTC instants, display time zone, status and version are
  stored for appointments. No email/contact details are returned in directory or
  booking DTOs. Confidential purposes and care notes are not accepted in this slice.
- In-app notifications contain a generic update and a private appointments link;
  they omit names, dates and care content. Writes and notices share a transaction.

## HTTP contract

All paths start with `/api/v1`. Every route requires a verified Bearer session;
mutations also require an exact allowed `Origin`. Send JSON for POST/PATCH.

| Method | Path | Action |
| --- | --- | --- |
| GET | `/pastors` | Listed eligible pastors, without contact details |
| POST | `/admin/pastors` | Protected directory profile creation/replacement |
| GET | `/pastors/{pastorId}/availability` | Future unreserved slots |
| GET | `/me/pastoral/profile` | Own eligible provider profile, including unlisted |
| GET / POST | `/me/pastoral/availability` | Own active slots / publish a slot |
| DELETE | `/me/pastoral/availability/{slotId}` | Withdraw an unreserved own slot |
| POST | `/pastor-bookings` | Request `{ "slotId": "uuid" }`; returns pending |
| GET | `/me/pastor-bookings` | Own requests and history |
| DELETE | `/pastor-bookings/{bookingId}` | Cancel an own future appointment |
| GET | `/me/pastoral/bookings` | Assigned provider queue and history |
| PATCH | `/me/pastoral/bookings/{bookingId}` | Decide with `{ "status": "approved", "version": 1 }` |

Directory settings are `{accountId,bio,timeZone,listed}`. Bio is at most 1000
characters; timeZone must be a loadable IANA zone (`Local` is rejected). Omitted/null
listed defaults to true; POST replaces settings. A slot supplies `startsAt` and
`endsAt` with offsets: 15–120 minutes long, in the future and starting within 180
days. Active slots for a pastor cannot overlap; adjacent slots are allowed.

Lists accept limit (default 20, maximum 100) and cursor. Results use stable ascending
UUID order; nextCursor is only a pagination position and never grants access.
Availability also accepts `from` and `to`, defaults to now through 30 days, and returns
slots wholly inside a positive range of at most 31 days, ending within 180 days.
The initial React view shows the next 30 days and formats in the pastor's time zone;
slot-creation inputs use the device's local time.

Pending requests require assigned-pastor approval or decline. An assigned pastor
may also cancel a pending/approved appointment. Member cancellation remains possible
if the provider is unlisted or its role revoked. Decisions/cancellation must occur
before the start time. Stale decision versions return 409; repeating an unchanged
outcome is safe. Terminal records and audit are retained. No confidential reason
is accepted for rejection/cancellation in this foundation.

A repeated reservation for the member's current active slot returns the existing
booking. Competing members and overlapping appointments for one member are rejected.
PostgreSQL serializes provider actions and member reservations; a partial unique
index additionally permits only one pending/approved booking per slot. Calendar
withdrawal conflicts with an active reservation rather than silently cancelling it.

Read limits are 60/account/minute, writes 30/account/minute, with 120/IP/minute per
operation class. Malformed IDs/queries fail 422, unauthorized private targets fail 404,
forbidden authority fails 403, state conflicts fail 409 and rate limits fail 429.
Unknown JSON fields fail 400; existing envelope/request-ID rules apply. API responses
are no-store. See `/openapi.json` for schemas and examples.

## React and rollout

`/#/pastors` is linked from Home and Profile. It provides directory/availability,
own booking requests/cancellation, and an eligible pastor's calendar and decision
queue. Directory administration is shown only for the relevant Super Admin
capability; the server independently enforces the protected operation.

Migrations 39–40 only add profiles, slots, bookings, indexes and metadata audit.
They do not appoint pastors, create test users or reset any existing records.
A real Super Admin must explicitly publish an already appointed pastor profile
before availability can be offered. No live bootstrap is performed by this feature.

Confidential notes, service types, recurring availability, explicit blocked periods,
reassignment, attendance/no-show outcomes, email reminders and exceptional care
access remain later slices. Care-data retention/encryption rules must be agreed
before collecting free-text care details. This foundation does not close milestone M9.

## Local verification

TDD checks covered repository transactions against disposable PostgreSQL, service
validation and concurrent reservations, HTTP routes through the global application,
and the React booking flow. Focused pastoral race tests and route/contract tests
passed. Desktop and mobile browser checks passed, including two workflows against
the real Go API. Frontend unit tests and the production build passed. OpenAPI
validation passed with eight existing warnings.

The broad race run exceeded the five-minute package limit in authentication-heavy
application and auth tests; the application retry also exceeded ten minutes. These
runs do not establish a full-suite pass. All other packages in that broad run
passed.

The Docker image build and smoke checks passed, including forward migrations run
twice without deleting the disposable fixture record. Go vet and Swagger
configuration checks passed. The complete application and authentication packages
also passed in separate runs without the race detector using a ten-minute package
budget; a redundant authentication rerun was stopped after collecting both results. This
does not replace the unresolved full race-suite verification. No staging data was
reset and no release was performed.
