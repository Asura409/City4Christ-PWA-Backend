# Church roles, operations and privacy requirements

Recorded 20 October 2026 from the owner's requirements in this conversation.
This supplements `City4Christ_PWA_Backend_PRD.docx` (11 October 2026); it does not
replace the original domains or existing auth/security requirements. This document
defines target behavior, not a claim that the features are implemented.

Implementation order and acceptance gates: [milestone checklist](../implementation-checklist.md).

## R1. Actors and authority

| Actor | Scope | Target authority |
| --- | --- | --- |
| Visitor | Approved public content and own onboarding submission | Public sermons/events/live links, visitor registration, secure account claiming |
| Church Member | Own records and authorized public/joined content | Own profile/family details, events/RSVP, own giving, joined groups and conversations |
| Group / Ministry Leader | Explicitly assigned groups only | Join-request decisions, group moderation, group events and coordinator appointments |
| Assistant Pastor / Staff | Church-wide operations | Authorized member management, attendance, events, announcements, broadcasts, group oversight and general giving totals |
| Super Admin / Lead Pastor / Main Admin | Church-wide administration | Appoint pastors/admins, manage groups and permissions, oversee member data and financial reporting, audited administrative overrides |
| Designated Finance Officer | Explicit finance appointment | Approved donor reporting, reconciliation and exports; no unrelated pastoral or messaging access |
| Welcome Team / Usher / Volunteer Coordinator | Explicit task assignment | Visitor follow-up, attendance or activity coordination only; no implied pastor/admin rights |

Content editing and assigned pastoral-care duties remain separate capabilities.
Giving a volunteer a task must not grant unrestricted member, finance or care access.
Accounts can hold more than one assignment, each with its own scope.

### R1.1 Permission matrix

“Own” means the caller's records; “group” means a currently assigned group.
All successful reads must also enforce the relevant privacy/audience policy.

| Action | Super Admin | Assistant Pastor / Staff | Group Leader | Member |
| --- | --- | --- | --- | --- |
| Church administration and member management | Church-wide | Operational subset | Group subset | Own profile only |
| Appoint/revoke Super Admin, Pastor or Finance Officer | Yes, protected privileged flow | No | No | No |
| Appoint group leaders/coordinators | Yes | Within delegated operational authority | Activity coordinators from own roster only | No |
| Create/archive groups | Yes | Only if delegated | No implicit right | No |
| Accept/reject group join requests | Yes | Yes | Own group | No |
| Remove/flag group posts, comments or media | Yes | Yes | Own group | Own content or report only |
| Create events and announcements | Yes | Church-wide | Own group | No |
| Assign event/activity leaders | Yes | Yes | Own group and eligible roster | No |
| View attendance / perform check-in | Yes | Yes | Only if explicitly assigned | Own records / permitted self-check-in |
| Send church-wide/emergency push | Yes | Yes | No; group-only messaging if granted | No |
| View general church giving totals | Yes | Aggregates only | No | No |
| View individual donor records and finance exports | Yes | No, unless separately appointed Finance Officer | No | Own giving only |
| View personal giving history | Own | Own | Own | Own |
| Participate in group chats | Authorized group access | Authorized group access | Assigned/joined groups | Joined groups |
| Edit personal/family information | Own and authorized administration | Authorized operations | No blanket family access | Own/authorized family relationships |

### R1.2 Delegation and immediate effects

- Only Super Admins may promote someone to Pastor/Admin or appoint finance access.
  This follows the explicit matrix's “No” for Assistant Pastor promotions. The
  general “Super Admin or Pastor” wording permits delegated operational/group
  appointments, not an Assistant Pastor promoting peers or administrators.
- Validate the actor, target, assignable role and exact scope on the server. Never
  trust a profile-edit field, frontend menu or role claim supplied by the caller.
- Apply grants and revocations to subsequent requests using the existing session.
  The member keeps the same account/login. Refresh dashboard capabilities after
  a change; a role tag in the browser is not authorization.
- Require fresh authentication and MFA for high-impact administration. Google-only
  accounts reauthenticate with their provider; they do not acquire a local password
  automatically.
- Record actor, target, before/after assignment, scope, reason and time atomically.
  Prevent self-escalation and accidental removal of the last active Super Admin.
- Appoint the first Super Admin through an explicitly approved operator procedure.
  Never promote the first signup or automatically upgrade existing technical roles.
- Overrides remain audited and bounded by security rules: no bypass of verified
  payment events, MFA, secret handling or immutable audit history.

### R1.3 Relationship to current implementation

Current `access_administrator`, `system_administrator`, `content_editor` and scoped
`ministry_leader` are technical roles with limited implemented capabilities.
`access_administrator` currently delegates editors/leaders; `system_administrator`
currently grants sermon editing. Neither is today's implementation of universal
Super Admin. The new church-wide hierarchy, finance access, MFA and operational
dashboards remain future work. Preserve current assignments in additive migrations;
approve any mapping before increasing an existing account's authority.

## R2. Required operational workflows

### W01 — Promote a member to Assistant Pastor / Staff

Super Admin opens User Management, searches/selects a member, reviews current
assignments, selects the new role/scope, reauthenticates when required and saves.
Persist an audited assignment on the existing account. The member's next authorized
request gains the new capabilities; the UI refreshes its role tag and dashboard.
Reject unauthorized actors, self-escalation and invalid target/scope combinations.

### W02 — Decide a group join request

Assigned leader, Staff or Super Admin opens the group's Pending Requests, accepts or
declines a request, and supplies a reason when policy requires it. Acceptance adds
one active roster entry and creates an in-app confirmation transactionally. Repeated
or concurrent decisions must not duplicate membership or notifications. Nonmembers
and leaders of another group cannot inspect or decide the request.

### W03 — Moderate group content

An authorized moderator selects a post/comment/media item, chooses Delete/Flag and
records the reason. Remove the item from ordinary feeds and attachment access
immediately for all members. Keep an audited moderation record. “Permanently removed
from the feed” does not require destroying audit evidence in the same transaction;
physical purge, retention and appeal/restore rules must be settled before release.
Restrict leader authority to the assigned group; persist visibility before emitting
realtime notifications.

### W04 — Create a group event and assign a leader

Group leader enters title, time/timezone, location and description; selects an
eligible coordinator from the group's current roster; then publishes. Show the
event in the authorized group calendar and create an assignment notification.
Revalidate roster eligibility and actor scope on save, including concurrent changes.

### W05 — View an event, RSVP and export a calendar entry

Member reads authorized event details, chooses attendee count and confirms RSVP.
Persist Registered status with capacity enforced transactionally. Support repeat-safe
registration/cancellation and the approved waitlist/guest policy. Offer a standard
calendar export for Apple/Google calendars; downloading an export does not prove the
member granted access to, or saved it in, their native calendar.

### W06 — Welcome a visitor / new member

Visitor opens a PWA/Welcome Desk QR link and submits name, phone, email and optional
family, prayer and baptism information under explicit consent/privacy notices.
Create the visitor/member record and classify it in New Visitors for follow-up.
Send a welcome email/SMS through the approved provider and notify the authorized
Welcome Team using a durable outbox.

Keep identity verification and account claiming secure: never silently attach an
existing email to a new caller, bypass OTP, email plaintext passwords or give staff
access to optional private prayer details by default. A new-member list is not an
automatic grant of private group-chat access. Support duplicate submissions safely.

### W07 — Give tithes/offerings and obtain a receipt

Member selects Tithe, Offering, Building Fund or Special Seed; enters a valid amount
and currency; and uses an approved provider checkout or verified transfer workflow.
Show own transaction status/history and issue a digital receipt only after verified
payment confirmation. Update authorized finance reports. Browser redirects cannot
mark a transaction paid. Handle duplicate/out-of-order webhooks, refunds, failures
and reconciliation. Staff totals must not reveal individual donor details.

### W08 — Check in for a service / Sunday School

Authorized Usher scans a member's opaque/appropriately protected PWA QR code or
searches the permitted member fields (name/phone), selects the service and confirms
check-in. Support children through an authorized family/guardian relationship and
appropriate Sunday School permissions. Persist one attendance record per member
and service with a timestamp; update authorized dashboard counts. Settle self-check-in,
QR expiry/replay and child collection rules before enabling those flows.

### W09 — Send an emergency push broadcast

Staff/Super Admin composes a title/body, selects All Members, a group or Staff Only,
reviews the authorized audience and sends. Persist the broadcast and each recipient's
in-app notification before worker delivery. Send Web Push only to opted-in active
subscriptions. Record delivery outcomes/retries without duplicates, remove expired
subscriptions, and retain inbox access when device permission or delivery fails.
Do not promise that every browser/device will display every push.

## R3. Data protection, financial privacy and recovery

- **S01 — Encryption:** use TLS, encrypted storage/backups and an explicit sensitive-
  field encryption design for member contact/family information, giving and care
  data. Use maintained OSS/provider cryptography, managed keys, rotation/versioning
  and a reviewed lookup/search design. Never invent cryptographic primitives.
  Existing Argon2id passwords and encrypted OTP outbox do not mean all profile
  fields are already encrypted: current account names/emails are ordinary columns.
- **S02 — Privacy enforcement:** authorize every object, collection, export, report
  and WebSocket action. Protect directory opt-in; staff member management is a
  distinct permission from the ordinary member directory. Keep private content out
  of logs, push previews and unrelated analytics.
- **S03 — Finance:** only Super Admins and explicitly designated Finance Officers
  access individual donor reports. Staff may access approved general totals; all
  members can access their own giving. Never store full card numbers/security codes.
- **S04 — Recovery:** verified, expiring, single-use email recovery with attempt/rate
  limits and generic public responses. Super Admin assisted recovery is a controlled,
  audited ownership-verification process, not password disclosure or unrestricted
  impersonation. Apply the approved session-revocation policy.
- **S05 — Audit/retention:** record privileged role changes, moderation, sensitive
  reads/exports, financial actions and assisted recovery. Define retention, deletion,
  backup and purge behavior before relevant features launch.
- **S06 — Family/children:** explicitly model guardian authority and consent; shared
  names/emails/households do not establish authority to view or change another
  account. Restrict child check-in and pickup operations.

## R4. Decisions to resolve at the relevant milestone

Do not block unrelated work on these now, and do not silently enable the undecided
behavior. The checklist identifies the gates that require each decision.

| Decision | Required before |
| --- | --- |
| First Super Admin identity, privileged reauthentication/MFA and legacy-role mapping | New privileged-role rollout (M1) |
| Staff-visible member fields, contact/family encryption, consent and retention | Expanded member administration/onboarding (M2) |
| Whether Super Admin has exceptional access to private DMs/care; purpose, approval and audit conditions | Care (M9) and private messaging (M10) |
| Group privacy/join policies, moderation retention and appeals | Group/community rollout (M4–M5) |
| Church timezone, capacity, guest counts, waitlist/cancellation and calendar policy | Events (M6) |
| Self-check-in, QR lifecycle and child guardian/pickup policy | Attendance (M7) |
| SMS, Web Push, object storage/scanning and livestream providers/budgets | Provider-dependent slices (M2–M3, M5, M11) |
| Giving provider/country/currency, direct-transfer verification, receipt/refund and finance retention rules | Giving sandbox (M12) |
| Production budget, persistent database/backups, restore targets and capacity targets | Launch gate (M13) |
