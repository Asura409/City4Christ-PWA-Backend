# Implementation checklist and milestones

Planning baseline: 20 October 2026. Sources: the original
`City4Christ_PWA_Backend_PRD.docx` and the owner's
[church operations addendum](requirements/church-operations-addendum.md).

This is the execution backlog, not a promise that proposed endpoints/features
already exist. Checked baseline items are implemented and have prior local/CI
evidence; production acceptance is tracked separately. A milestone closes only
when its exit gate has evidence. No dates or provider spend are assumed.

## Current baseline

- [x] **B1:** Email/password registration/login, verification OTP, Argon2id, rate
  limits, access JWTs, rotating refresh cookies/replay handling, logout and `/me`.
- [x] **B2:** Google OIDC and explicit linking; allowlisted browser returns released
  in backend PR #12. Full interactive Google acceptance is still a separate check.
- [x] **B3:** Own-name/profile edits, directory opt-in and authenticated opt-in name
  search. This is not full member/family/visitor administration.
- [x] **B4:** Public sermon catalog/detail and staff draft/edit/publish/unpublish/
  schedule with version checks and audit. Progress, private notes and live remain.
- [x] **B5:** Limited technical-role delegation, scoped authorization groundwork
  and audit. The new church-role hierarchy is not implemented yet.
- [x] **B6:** Go/PostgreSQL, forward migrations, Docker, required GitHub CI,
  Render staging and a Vercel frontend with `API_URL`.
- [x] **B7:** Modular connected frontend pages and desktop/mobile Playwright checks,
  including real API flows against disposable PostgreSQL.
- [x] **B8:** VPS email-worker access restored through PR #6 and freshly verified
  with a read-only `SELECT 1` over `sslmode=verify-full`; the worker is active.
  Received-email acceptance remains separate in M0.4.

## Execution rules for every feature slice

1. Name a small behavior and its authorization/privacy boundary; settle only the
   product decisions that behavior needs.
2. Write meaningful repository tests against a fresh disposable PostgreSQL database,
   service tests, handler tests and routes through the global app. Run and record
   the expected failure before implementing missing behavior.
3. Implement the smallest complete vertical slice; rerun the tests to green.
4. Refactor into focused domain modules and rerun relevant checks. Never combine
   unrelated features in a large handler/service file.
5. Connect the real frontend interaction and its loading/error/empty states. Test
   desktop/mobile behavior, server denials and API integration; do not fake success
   or enable design-only buttons for an unimplemented domain.
6. Update OpenAPI/examples, migration notes, permissions and this checklist. Rehearse
   additive upgrades with existing records; never reset/seed staging automatically.
7. Use descriptive scoped commits, review the final diff, push/merge only within
   authorization, wait for required CI and verify the actual deployment afterward.
   Backend and frontend remain separate Git repositories.

For reversible documentation/configuration edits, use appropriate consistency/schema
checks rather than artificial tests that repeat the implementation. Every domain
slice still requires the repository/service/HTTP/global-app TDD sequence above.

## Milestone overview

| Milestone | Deliverable | Depends on | Owner requirements |
| --- | --- | --- | --- |
| M0 | Stabilize deployed auth and email infrastructure | Existing baseline | Existing auth, S04 |
| M1 | Church roles, delegation and privileged security | M0 | R1, W01, S02–S05 |
| M2 | Members, families, visitor onboarding and recovery | M1 | W06, S01–S06 |
| M3 | Durable notification inbox and push/broadcast delivery | M1–M2 | W06, W09 |
| M4 | Ministries/groups, rosters and join decisions | M1–M3 | W02 |
| M5 | Community feed, media and scoped moderation | M4 | W03 |
| M6 | Events, coordinators, RSVP and calendar export | M3–M5 | W04–W05 |
| M7 | Service attendance and Sunday School check-in | M2, M6 | W08, S06 |
| M8 | Prayer wall and privacy-aware moderation | M3–M5 | Original PRD, S02 |
| M9 | Pastor directory, availability and confidential bookings | M1–M3 | Original PRD, S01–S05 |
| M10 | Durable messaging, group chat and realtime | M3–M5 | Original PRD, S02 |
| M11 | Remaining Word/live, home/search and install experience | M3, M5–M6 | Original PRD |
| M12 | Giving sandbox, ledger, receipts and finance dashboards | M1–M3 | W07, S01–S05 |
| M13 | Security, resilience and production release | Completed release scope | Entire PRD and addendum |

This is the default sequence. Groups, events and attendance form the next coherent
church-operations increment after roles/member/notification foundations. Giving has
its own sandbox/live gate and must not delay unrelated beta feedback. A reduced MVP
must explicitly record deferred features rather than marking them complete.

## M0 — Stabilize the foundation

- [x] **M0.1:** Merge the approved `51.170.141.205/32` restoration through PR #6,
  preserving current signup/CORS/free plans. PR and main CI passed; merge `2d00ffe`.
- [x] **M0.2:** Verify restored access with a fresh read-only database connection
  from the VPS over verified TLS and worker health. The Blueprint declares only
  the approved `/32`; no other hosts or credential changes were introduced.
- [ ] **M0.3:** Verify API readiness, exact Vercel/GitHub origins and rejection of
  lookalike origins against the deployed commit. Readiness and the origin checks
  passed on 20 October; exact Render deployment identity still needs confirmation
  when management tools are available.
- [ ] **M0.4:** Complete a fresh registration → received OTP → verify → login →
  refresh → `/me` → logout acceptance run using an approved test account. Confirm
  Google signup/login/linking interactively without posting tokens/secrets in chat.
- [ ] **M0.5:** Record remaining third-party-cookie/browser limitations and the
  durable database/backup migration needed before production.

**Exit:** working delivery and auth acceptance have explicit evidence. A healthy
worker process, successful DB connection or simulated Google provider alone is not
proof of received email or completed real-provider login.

## M1 — Church roles and permission management

- [ ] **M1.1:** Translate R1 into named capabilities and global/group/task scopes;
  document grantable roles, sensitive read/export permissions and deny-by-default.
- [ ] **M1.2:** Add role definitions/assignment lifecycle with additive migrations.
  Preserve technical editor/admin assignments without silently making anyone a
  Super Admin. Resolve first-Super-Admin identity and mapping before live rollout.
- [ ] **M1.3:** Implement owner-approved Super Admin bootstrap, last-admin protection,
  fresh reauthentication and MFA for privileged role/recovery/finance actions.
- [ ] **M1.4:** Implement audited role read/grant/revoke and eligible-member lookup.
  Only Super Admin can appoint pastors/admins/finance officers; Staff delegates only
  permitted operational appointments. Prevent self-escalation and scope inflation.
- [ ] **M1.5:** Expose current capabilities for frontend navigation. Apply revocation
  on the same existing JWT/session and refresh UI capabilities without a new account.
- [ ] **M1.6:** Add User Management/role-change UI and test every actor/target/scope,
  concurrent last-admin changes, rollback and role revocation with a still-valid JWT.

**Exit:** W01 works end to end; members/leaders/staff cannot promote unauthorized
roles, and no real account gains privileged access without the approved procedure.

## M2 — Member, family, visitor and recovery workflows

- [ ] **M2.1:** Decide and document member fields, directory/staff visibility,
  consent, retention and sensitive-field encryption/key/lookup design (S01–S02).
- [ ] **M2.2:** Add contact details, family/guardian relationships, member/visitor
  lifecycle and consent records. Preserve existing accounts and directory opt-outs.
- [ ] **M2.3:** Build paginated operational member search/detail with field-level
  authorization and sensitive-read/export audit. No public phone/address lookup.
- [ ] **M2.4:** Implement private visitor intake and QR entry, duplicate handling,
  New Visitors follow-up list and secure verification/account claiming. Existing
  emails must not be claimed or linked by an unauthenticated form submission.
- [ ] **M2.5:** Implement forgot/reset password with expiring single-use recovery,
  generic responses, attempt/rate limits and session revocation; add separately
  authorized change-email/change-password flows and audited assisted recovery.
- [ ] **M2.6:** Implement authorized own-family editing and welcome-team follow-up
  UI. Defer avatar upload until the media pipeline is available in M5.
- [ ] **M2.7:** Test cross-member/family access denial, consent/privacy serialization,
  email uniqueness, recovery replay and upgrade preservation using real PostgreSQL.

**Exit:** W06 intake/identity flow and member management work without verification
bypass or private-field leaks. Welcome delivery is completed through M3.

## M3 — Notifications, delivery and emergency broadcasts

- [ ] **M3.1:** Add reusable transactional domain outbox/in-app inbox records,
  per-recipient delivery state, retry/backoff and failed-delivery visibility.
- [ ] **M3.2:** Implement own inbox, unread count, read/read-all and preferences;
  keep delivery failure independent of the committed domain action.
- [ ] **M3.3:** Implement Web Push subscription/revocation with browser opt-in,
  maintained libraries, protected provider credentials and expired-subscription cleanup.
- [ ] **M3.4:** Implement draft/review/send broadcast and authorized audience
  selection. Staff/Super Admin may broadcast church-wide; leaders cannot widen a
  group audience. Audit recipients/counts without leaking sensitive payloads.
- [ ] **M3.5:** Wire welcome-team/visitor welcome events; select SMS only when its
  provider, consent and costs are approved. Use inbox fallback and safe push previews.
- [ ] **M3.6:** Add notifications/preferences/broadcast UIs and test duplicate jobs,
  provider outage, unsubscribed devices, recipient isolation and safe retries.

**Exit:** W09 works for opted-in supported devices; W06 welcome/follow-up delivery
is durable and observable. Provider failure never reports a false successful send.

## M4 — Groups / ministries and membership

- [ ] **M4.1:** Implement group lifecycle, visibility/join policy, scopes, multiple
  leader assignments and roster/membership records; align existing ministry scopes.
- [ ] **M4.2:** Implement authorized group list/detail, join/request/leave and
  pending-request review; reject cross-group or inactive-leader access.
- [ ] **M4.3:** Implement atomic accept/decline decisions, unique active membership,
  safe concurrent decisions and recipient notifications through M3.
- [ ] **M4.4:** Implement church-wide Staff/Super Admin oversight with audit and
  approved group create/archive/restore behavior; no automatic private-channel access.
- [ ] **M4.5:** Connect My Groups, rosters and Pending Requests in the frontend;
  test leaders of group A against every read/write operation on group B.

**Exit:** W02 works; approval updates roster/inbox once and membership determines
subsequent private content access server-side.

## M5 — Community, uploads and moderation

- [ ] **M5.1:** Choose storage/scanning provider and implement short-lived upload
  permissions, size/type/content validation, private object access and completion.
- [ ] **M5.2:** Implement audience-filtered feed, posts, comments, reactions and
  own-content edits/deletion with cursor pagination and retry-safe operations.
- [ ] **M5.3:** Implement reports and authorized group/church moderation actions
  with reasons, audit, immediate feed/attachment removal and agreed retention/purge.
- [ ] **M5.4:** Add secure profile avatars using the completed media pipeline;
  garbage-collect unattached/rejected objects under an explicit cleanup policy.
- [ ] **M5.5:** Connect community/group feed/composer/report/moderation pages;
  test ownership, group boundaries, removed content/media URLs and concurrent changes.

**Exit:** W03 works for posts/comments/media; hidden content is unavailable through
detail routes, pagination, search and attachments, not merely hidden in the UI.

## M6 — Events, activity leaders and RSVP

- [ ] **M6.1:** Resolve timezone, guests/counts, capacity/waitlist, cancellation and
  publication policy. Keep timezone explicit and store instants consistently in UTC.
- [ ] **M6.2:** Implement public published and authorized group event list/detail,
  drafts/publication and scoped editor routes with update/conflict handling.
- [ ] **M6.3:** Implement coordinator assignments from the current eligible group
  roster and in-app assignment notifications; recheck membership transactionally.
- [ ] **M6.4:** Implement own RSVP/cancel/status, bounded attendee counts,
  transactionally enforced capacity, optional waitlist and repeat-safe retries.
- [ ] **M6.5:** Implement safe calendar export with stable event IDs and correct
  timezone/updates; connect event management and member registration UIs.
- [ ] **M6.6:** Test oversubscription races, duplicate RSVPs, cancellation capacity,
  removed coordinators, cross-group access and DST/timezone/calendar cases.

**Exit:** W04–W05 work from creation/assignment to Registered status and calendar
export, with no overselling capacity or unauthorized coordinator appointments.

## M7 — Attendance and Sunday School

- [ ] **M7.1:** Resolve service/session model, self-check-in rules, QR lifecycle,
  guardian/child access and Sunday School pickup/release policy.
- [ ] **M7.2:** Implement scoped Usher/check-in assignment, authorized member
  name/phone search and protected member QR lookup without exposing contact lists.
- [ ] **M7.3:** Implement transactional check-in, duplicate prevention, timestamped
  attendance, audited corrections and authorized aggregate counts.
- [ ] **M7.4:** Implement permitted family/child check-in and volunteer/member UIs;
  notify/stream updated counts only to authorized dashboard users.
- [ ] **M7.5:** Test replay/duplicate scans, revoked volunteers, cross-service scope,
  wrong-guardian access and concurrent check-in/correction operations.

**Exit:** W08 works; one correct attendance record is stored and child/family
information remains restricted to the permitted relationship and task.

## M8 — Prayer wall

- [ ] **M8.1:** Implement own/community/group/private requests, expiry/archive,
  anonymous display, encouragements, “I prayed” and reports.
- [ ] **M8.2:** Separate public anonymity from controlled moderator attribution;
  audit sensitive access and preserve visibility on every collection/detail route.
- [ ] **M8.3:** Connect prayer filters/composer/acknowledgements/moderation to the
  real API; use safe inbox/push previews and maintained upload controls if enabled.
- [ ] **M8.4:** Test another member's private prayer, group leakage, anonymous
  author serialization, duplicate acknowledgements and removed-content visibility.

**Exit:** original PRD prayer workflows work without exposing identity or private
requests through APIs, notifications, logs, search or pagination.

## M9 — Pastor directory and confidential care

- [ ] **M9.1:** Approve who may read care details, exceptional Super Admin access,
  service types, booking/cancellation/escalation rules and note retention.
- [ ] **M9.2:** Implement active pastor directory, timezone-aware availability,
  blocked periods, unique slot reservation and own booking/cancellation/status.
- [ ] **M9.3:** Implement assigned-care dashboards and confidential notes with
  encryption, need-to-know access and audited sensitive reads/writes.
- [ ] **M9.4:** Implement durable confirmations/reminders with safe previews;
  connect member booking and staff availability/care pages.
- [ ] **M9.5:** Test simultaneous booking, timezones, cancellations, reassignment,
  unrelated staff access, reminder duplicates and encrypted-field upgrades.

**Exit:** one pastor/slot cannot be double-booked; unrelated staff/leaders cannot
read private purposes or notes.

## M10 — Messaging and realtime

- [ ] **M10.1:** Approve DM/group privacy, moderation/retention and any exceptional
  staff access before implementing it. Do not infer access to every private DM from
  a broad dashboard role.
- [ ] **M10.2:** Implement participant-only conversations/history/search, durable
  message send, stable client-message idempotency and monotonic read receipts.
- [ ] **M10.3:** Bind group conversations to active membership and moderation;
  validate attachment ownership/visibility and limit abusive message operations.
- [ ] **M10.4:** Add short-lived single-use WSS tickets, per-event authorization,
  bounded frames/rates, heartbeat, revocation, ephemeral typing/presence and backpressure.
- [ ] **M10.5:** Connect conversation/group-chat UIs; recover missed durable history
  through REST after reconnect. Add Redis only when measured multi-instance needs it.
- [ ] **M10.6:** Test unauthorized room joins, removed members, same-token role
  revocation, duplicate sends, reconnect recovery, out-of-order receipts and outages.

**Exit:** committed messages survive reconnect; private history remains participant-
restricted; realtime failure does not prevent authoritative REST reads/writes.

## M11 — Complete Word/live and the home/install experience

- [ ] **M11.1:** Add published series, own playback progress and private sermon
  notes, preserving the already implemented catalog/publishing behavior.
- [ ] **M11.2:** Add managed devotionals/daily verse and approved live provider
  metadata/scheduling; never stream video through the app WebSocket.
- [ ] **M11.3:** Implement personalized home aggregation and authorized cross-domain
  search only for domains whose visibility rules and UI routes are implemented.
- [ ] **M11.4:** Finish install/offline/update UX and notification/media access
  behavior. Remove unconnected store links and keep private API responses uncached.
- [ ] **M11.5:** Connect remaining Word/live/home/search interfaces and test private
  notes/progress isolation, publication changes, access-aware search and SW upgrades.

**Exit:** no hardcoded personal identity or fake functionality remains in released
screens; private content is absent from browser caches and unrelated search results.

## M12 — Giving and finance, sandbox first

- [ ] **M12.1:** Approve provider, country/currency, guest/anonymous giving,
  categories, direct-transfer verification, receipts/refunds and retention/export policy.
- [ ] **M12.2:** Add money-in-minor-units validation, categories, pending checkout,
  actor-scoped idempotency, verified raw-body webhook adapters and event deduplication.
- [ ] **M12.3:** Implement auditable ledger/status transitions, reconciliation,
  refunds, digital receipts and member-owned giving history.
- [ ] **M12.4:** Implement Staff aggregate totals, Super Admin/Finance Officer donor
  reports and audited exports; do not expose donor-level breakdowns through totals.
- [ ] **M12.5:** Connect sandbox checkout, pending/failure/success states, receipts
  and finance dashboards; never collect card secrets in this backend/frontend.
- [ ] **M12.6:** Test forged signatures, duplicate/out-of-order events, redirect
  spoofing, amount/currency mismatch, double processing and cross-donor authorization.
- [ ] **M12.7:** Obtain explicit live-payment approval only after sandbox acceptance,
  reconciliation/receipt checks and the finance access review pass.

**Exit:** W07 completes with verified provider confirmation, accurate private history
and authorized finance views. No real charges occur during automated tests.

## M13 — Release security and operational readiness

- [ ] **M13.1:** Run the complete actor/scope/privacy matrix across REST, realtime,
  exports, notifications, search and media; review all privileged audit paths.
- [ ] **M13.2:** Verify managed encryption/keys, secret rotation, recovery/MFA,
  retention/deletion/export behavior and consent against the approved policies.
- [ ] **M13.3:** Move to durable approved hosting/database/backups; perform a restore
  drill and migration rehearsal with existing data. Never reset staging as a shortcut.
- [ ] **M13.4:** Add actionable redacted logs/metrics/alerts for readiness, delivery
  queue lag, failed jobs, webhooks, database capacity and restore/backup failures.
- [ ] **M13.5:** Confirm PRD provisional targets (5,000 accounts, 500 concurrent
  sessions, ordinary-read P95 <500 ms, writes <1 s excluding providers, 99.5% monthly
  availability), then load-test and revise capacity/budget based on measured evidence.
- [ ] **M13.6:** Rehearse provider, worker, socket and database outages; document
  rollback/forward-fix, incident ownership, smoke checks and data-preservation steps.
- [ ] **M13.7:** Complete beta acceptance on supported mobile/desktop browsers;
  record unresolved issues, final domain/cookie configuration and owner release approval.

**Exit:** the explicitly selected release scope meets its acceptance criteria,
production operations are verified, and deployment/real payments occur only within
the owner's authorization. Deferred features stay visible in this backlog.

## Code organization

Keep the modular monolith. Reuse current `internal/auth`, `access`, `profiles` and
`sermons`. Add focused `groups`, `community`, `notifications`, `media`, `events`,
`attendance`, `prayer`, `care`, `messaging`, `live`, `giving` and `search` packages
as their slices begin. Separate repositories, services/policies, HTTP adapters,
provider adapters and tests within each domain. Keep `internal/app` focused on
composition/global routing; move growing domain adapters into organized subfolders
without creating dependency cycles. Do not create all empty packages in advance.

Keep frontend API transport/shared components separate from domain pages. No
database/provider secret belongs in frontend configuration; `API_URL` is public.

## Progress and evidence log

For each completed task, record its ID, PR/commit, test command, verified result,
deployment ID and acceptance evidence when applicable. Keep failures and unresolved
provider checks explicit. Never store passwords, OTPs, access/refresh tokens or
provider keys in this document.

User-facing progress reports use a JSON map with `commands`, `purpose`, `status`,
`failure_reason` (null when none), and `next`. Report command results after observing
them; do not claim that a queued check, merge or deploy has completed.

### Recorded evidence

| Date | Task | Evidence | Limit |
| --- | --- | --- | --- |
| 2026-10-20 | M0.1 | [PR #6 merged](https://github.com/Asura409/City4Christ-PWA-Backend/pull/6); [main CI passed](https://github.com/Asura409/City4Christ-PWA-Backend/actions/runs/39414728216) | Required checks include disposable PostgreSQL race tests, Docker build and migration smoke |
| 2026-10-20 | M0.2 | Fresh `SELECT 1` with `sslmode=verify-full` passed; worker active; API readiness healthy | No new test email sent; fresh OTP receipt and interactive Google acceptance remain open |
| 2026-10-20 | M0.3 (partial) | Vercel and GitHub preflights returned 204; lookalike origin returned 403; readiness healthy | Render management tools were unavailable in this resumed session; deployment identity was not directly queried |
