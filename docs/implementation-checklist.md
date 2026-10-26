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
  and audit. Protected church-role lifecycle is implemented locally; live bootstrap/release remains pending.
- [x] **B6:** Go/PostgreSQL, forward migrations, Docker, required GitHub CI,
  Render staging and a Vercel frontend with `API_URL`.
- [x] **B7:** Modular connected frontend pages and desktop/mobile Playwright checks,
  including real API flows against disposable PostgreSQL.
- [x] **B8:** VPS email-worker access restored through PR #6 and freshly verified
  with a read-only `SELECT 1` over `sslmode=verify-full`; the worker is active.
  Received-email acceptance remains separate in M0.4.
- [x] **B9:** Password recovery released through backend PR #13 (`46762e1`) and
  frontend PR #2 (`c643756`). The owner confirmed the emailed reset succeeded;
  read-only checks confirmed token consumption, revocation of the six prior
  active sessions and a new sign-in session. Used-link rejection is covered by
  automated tests; separate live replay confirmation is not claimed.


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

### Owner-directed product priority update (21 October 2026)

The owner has moved member-facing functionality ahead of member/family/visitor
onboarding. Keep the existing milestone IDs below for references, but follow this
delivery order:

1. Groups and membership requests/decisions, using existing verified accounts.
2. Community posts and shared media attachments, then extend sermon publishing to
   support images, audio and uploaded video alongside existing YouTube links.
3. Prayer wall with its own visibility and anonymity controls.
4. Durable direct and group messaging, then realtime delivery.
5. Giving in a payment sandbox; live checkout remains blocked until a provider,
   currency and webhook/reconciliation rules are approved.
6. Member, family and visitor onboarding after these member-facing features.

Group membership precedes group-specific posts and chats. Each slice keeps
its server-side audience/ownership checks and React interface. Media bytes stay
outside PostgreSQL behind a private object-storage adapter with short-lived signed
URLs. Cloudflare R2 is the selected provider; upload reservations and scanning
now validate limits and actual content, with clean files promoted privately.
Uploads remain disabled in a live environment until the VPS worker is installed
and verified and the approved access policy is in place. Sermon and post
attachments must use the same media policy. Push notifications are not a
prerequisite for durable group or
message state; record in-app events transactionally and add delivery separately.

This priority update supersedes the default milestone order in the table and the
paragraph below; onboarding is deferred rather than cancelled.

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

The table preserves the original milestone IDs and dependencies for traceability.
Use the owner-directed priority update above for current sequencing. Giving keeps
its sandbox/live gate and must not delay unrelated beta feedback. A reduced MVP
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

- [x] **M1.1:** Define the [named capability policy](church-permissions.md),
  global/ministry/own/task scope boundaries, protected delegation and finance/care
  separation. Protected church appointment capabilities are now implemented locally as
  described below; unimplemented finance/care/group/event capabilities remain denied.
- [x] **M1.2:** Local role definitions/assignment lifecycle is implemented with
  additive migrations14–17, preserving legacy roles and scoped assignments.
  Migration17 activates protected definitions without appointing any account.
  See [church-role management](church-role-management.md). Real rollout remains
  separate; existing access administrators never become Super Admin implicitly.
- [ ] **M1.3:** Fresh password/Google confirmation and app MFA are implemented;
  protected church-role mutations now require both proofs transactionally.
  Verified/enrolled target checks, MFA-disable prevention, explicit operator-only
  one-time bootstrap and concurrent last-admin protection are implemented locally.
  Self-resignation requires another effective Super Admin. Factor-change alerts,
  assisted recovery and owner-approved first real administrator/bootstrap acceptance
  remain open. No real account has been promoted and this milestone is not closed.
- [x] **M1.4:** Local audited protected role read/grant/revoke and bounded verified
  candidate lookup are implemented. Only Super Admin appoints pastors/admins/finance;
  Assistant Pastor/Staff appoint registered-scope ministry leaders only. Legacy
  delegation remains limited. Unknown future domain capabilities remain denied.
- [x] **M1.5:** Implement current-role snapshots and frontend gating/refresh.
  Local real-API desktop/mobile tests confirm same-JWT grant visibility, hiding
  after revocation and server denial. Race tests passed for access/app/sermons;
  16 frontend unit tests, 20 mocked browser scenarios, four real-API workflows,
  vet, contract validation and Docker smoke checks passed. Release verification
  is separate; new church-role release and staging acceptance remain separately gated.
- [ ] **M1.6:** The local role-change UI now has authorized candidate search,
  separate protected appointments, explicit confirmations, security panels and
  stale-selection protection, preserving the React interface and legacy controls.
  Repository/service/global HTTP tests cover actor/scope boundaries, concurrent
  last-admin changes, audit rollback and same-JWT revocation; desktop/mobile
  browser integration is implemented. Owner-approved staging bootstrap and manual
  acceptance remain pending; broader member administration belongs to M2.

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
- [ ] **M2.5:** Email forgot/reset password is released with expiring single-use
  links, generic responses, rate limits, session revocation and frontend forms.
  The owner confirmed live receipt/reset; see B9 and
  [password recovery](password-recovery.md). Separately implement authorized
  change-email/change-password flows and audited assisted recovery.
- [ ] **M2.6:** Implement authorized own-family editing and welcome-team follow-up
  UI. Defer avatar upload until the media pipeline is available in M5.
- [ ] **M2.7:** Test cross-member/family access denial, consent/privacy serialization,
  email uniqueness, recovery replay and upgrade preservation using real PostgreSQL.

**Exit:** W06 intake/identity flow and member management work without verification
bypass or private-field leaks. Welcome delivery is completed through M3.

## M3 — Notifications, delivery and emergency broadcasts

- [x] **M3.1 (foundation):** Add transactional, account-scoped inbox records with
  idempotent domain writes. Domain notification producers are being connected as
  their feature slices land.
- [x] **M3.2 (foundation):** React inbox, unread count and idempotent read action are
  connected. Preferences, read-all and notification-producing workflows remain.
- [ ] **M3.3:** Implement Web Push subscription/revocation with browser opt-in,
  maintained libraries, protected provider credentials and expired-subscription cleanup.
- [ ] **M3.4:** Implement draft/review/send broadcast and authorized audience
  selection. Staff/Super Admin may broadcast church-wide; leaders cannot widen a
  group audience. Audit recipients/counts without leaking sensitive payloads.
- [ ] **M3.5:** Wire welcome-team/visitor welcome events; select SMS only when its
  provider, consent and costs are approved. Use inbox fallback and safe push previews.
- [ ] **M3.6:** Complete notification preferences/broadcast UIs and test duplicate jobs,
  provider outage, unsubscribed devices, recipient isolation and safe retries.

**Exit:** M3 stays open until authorized domain triggers, delivery providers, retry
visibility and user preferences are implemented. Provider failure must never report
false successful delivery.

## M4 — Groups / ministries and membership

- [x] **M4.1 (foundation):** Add church/leadership audience and join policies,
  verified-member records, group-scoped access scopes, and audit events. Group
  archive and leader appointment lifecycle remain open.
- [x] **M4.2 (foundation):** Add authenticated visible-group listing, join/request,
  leave, and pending-request review. Church groups are visible to verified
  members; the leadership-audience group is hidden from ineligible accounts.
  Leader review checks the exact group scope.
- [x] **M4.3 (backend decision):** Approve/decline transitions and audit writes are
  transactional and repeat-safe; PostgreSQL tests cover racing approve/decline
  calls. M3 recipient notifications remain open.
- [x] **M4.4:** Implement React group administration and church-wide group
  create/archive/restore for Staff, Assistant Pastors, and Super Admins. Creation
  and reasoned lifecycle transitions are audited transactionally; leadership
  group visibility remains role-gated and creation/restoration remains Super
  Admin only. Verified with PostgreSQL app/service tests and desktop/mobile UI tests.
- [ ] **M4.5:** React directory, My Groups, group detail, rosters and Pending
  Requests are implemented locally. Run the browser suite and verify leaders of
  group A against every read/write operation on group B before checking this.

**Exit:** W02 works; approval updates the roster/inbox once and membership
determines subsequent group-specific content access server-side.

The first backend foundation is implemented on `feat/group-membership-foundation`.
The React group directory, group details, My Groups, pending requests, roster,
and staff group oversight are implemented locally. M4.5 still requires the
full cross-group read/write boundary audit; membership notifications remain open.

## M5 — Community, uploads and moderation

- [ ] **M5.1:** Choose storage/scanning provider and implement short-lived upload
  permissions, size/type/content validation, private object access and completion.
  Progress: R2 and ClamAV are selected; PostgreSQL upload reservations and durable
  leased scan jobs, authenticated presign/complete/status routes, magic-byte MIME
  validation, ClamAV scanning and immutable clean-object promotion are implemented.
  The VPS worker has not been deployed or acceptance-tested. Owner-scoped
  post/sermon associations and short-lived signed reads are implemented; live
  upload acceptance remains blocked on the private VPS scanner rollout and R2
  CORS configuration.
- [ ] **M5.2:** Implement audience-filtered feed, posts, comments, reactions and
  own-content edits/deletion with cursor pagination and retry-safe operations.
  Progress: church-wide posts, paginated reads, idempotent Amen reactions with
  per-viewer state, and member-visible comments with cursor pagination and
  owner-only deletion exist. Members can attach
  up to four of their own clean uploads, and React editors can attach up to ten
  clean uploads to sermon drafts before publishing. Post edit/delete remain open.
- [ ] **M5.3:** Implement reports and authorized group/church moderation actions
  with reasons, audit, immediate feed/attachment removal and agreed retention/purge.
- [ ] **M5.4:** Add secure profile avatars using the completed media pipeline;
  garbage-collect unattached/rejected objects under an explicit cleanup policy.
- [ ] **M5.5:** Connect community/group feed/composer/report/moderation pages;
  test ownership, group boundaries, removed content/media URLs and concurrent changes.

**Exit:** W03 works for posts/comments/media; hidden content is unavailable through
detail routes, pagination, search and attachments, not merely hidden in the UI.

## M6 — Events, activity leaders and RSVP

- [x] **M6.1:** MVP defaults: UTC instants plus IANA timezone, one RSVP per
  verified account with 1–10 attendees, optional capacity (unlimited when omitted), no waitlist, cancellation
  before start, church-wide publication. No group event audience in this slice.
- [x] **M6.2:** Public published church-wide list/detail and `events.manage` draft,
  edit, publish and cancel routes with version conflicts.
- [x] **M6.3a:** Group event coordinator assignments are restricted to active,
  verified members of an eligible church group; leaders can assign only within
  their own group scope. Repository/service/global-app checks cover scope and
  inactive or ineligible members.
- [x] **M6.3b:** A new coordinator assignment writes a deduplicated in-app notice
  for the assigned member in the same transaction. Repeat assignments do not
  duplicate it; the existing React notification inbox links to Events.
- [x] **M6.4:** Own RSVP/cancel, bounded attendee counts and transactionally enforced
  capacity; full events return a conflict, and repeat requests are safe.
- [x] **M6.5:** Calendar export returns a standards-based `.ics` file and the
  React event page links to it. Focused API and browser checks cover the download.
- [x] **M6.6:** Repository/service/global-app tests cover concurrent capacity, duplicate
  RSVP, cancellation and authorization. Browser tests cover listing, RSVP, full
  capacity and manager publication on desktop/mobile.

**Exit:** MVP endpoints and UI are implemented and focused checks passed. Group
event scope remains open; the latest PR CI run is the full-suite release gate.

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

- [x] **M8.1a:** Requests expire after 180 days and are soft-archived in bounded
  batches during prayer API activity. Expired pending reports are closed without
  deleting request text, acknowledgements or audit history.
- [ ] **M8.1b:** Prayer comments and attachment policy remain. Current visibility
  supports community, active-group and private requests plus owner answer/archive.
- [x] **M8.2:** Members can report visible community/group requests. Scoped
  moderators review them with reasoned hide/dismiss actions and audit records;
  report responses and queues omit reporter and author identities.
- [x] **M8.3:** React filters, composer, acknowledgements and owner status controls
  use the real API with loading, empty and error states.
- [x] **M8.4:** Disposable-PostgreSQL repository/service/global-app tests and
  desktop/mobile browser scenarios cover privacy boundaries, anonymity, status,
  acknowledgements, expiry and report moderation. Media tests remain.

**Exit:** Core reports, moderation and expiry are implemented. Prayer comments,
attachments and notification workflows remain before closure.

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

- [x] **M10.1:** Direct messages are participant-only; any verified member may start
  a one-to-one conversation. No staff backdoor. Group chat needs its own policy.
- [x] **M10.2:** Participant-only durable history, directory search, idempotent sends,
  monotonic read receipts and account/IP rate limits are implemented.
- [x] **M10.3:** Durable group conversations, active-membership access, read
  markers and scoped moderation are implemented. Messages are redacted on removal;
  membership is rechecked for reads, writes and history access.
- [ ] **M10.4:** Realtime/WSS tickets, reconnect history and bounded fanout remain.
- [x] **M10.5:** React conversation list, member search, send and read receipt use the
  real API with desktop/mobile browser scenarios.
- [ ] **M10.6:** Group REST lifecycle and authorization checks now cover active
  membership, leave/revocation and moderation. Realtime/WSS lifecycle and reconnect
  history remain. Focused direct-message tests cover participant privacy, retry
  behavior, receipts and rate limits.

**Exit:** Direct and group REST messaging slices are implemented; M10 stays open for
realtime delivery, reconnect behavior and their security tests.

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

- [ ] **M12.1:** Provider candidate is Give A Little and currency is GBP. The owner
  supplied the existing public campaign URL but has no merchant/webhook setup yet.
  Obtain a provider test campaign, webhook enablement/signing secret through a
  secure channel, and confirm guest/anonymous giving, categories, failure semantics,
  receipts/refunds and retention/export policy before implementation. No live checkout
  is wired into preview.
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


### Protected church-role implementation — local evidence (21 October2026)

Parallel tracks implemented access services, migration/MFA safeguards, operator
CLI and frontend controls; the parent integrated HTTP/OpenAPI and independently
reviewed the security boundary. All changes remain local, with no real bootstrap.
Full backend normal tests and targeted access/app/auth race tests passed. Frontend
unit/build checks passed24 cases; final mocked desktop/mobile checks passed50;
real global-app workflows with disposable PostgreSQL passed10. Docker build and
disposable migration/route/shutdown smoke passed. Vet, Swagger configuration,
formatting/whitespace, documentation links and OpenAPI1.10 validation passed
(with four preexisting contract warnings). See [full contract](church-role-management.md).

Staging release, owner-approved first Super Admin, real-provider/device acceptance,
factor-change alerts and assisted recovery remain explicit remaining gates.


### Prayer, events and messaging follow-ups — local feature slices (25 October 2026)

Backend event, prayer, messaging, access and global-app packages passed against
disposable PostgreSQL. The OpenAPI contract first failed on path-level parameter
arrays, then passed after parameters were moved to the operations; OpenAPI JSON and
Swagger configuration checks pass. Frontend unit tests passed (30), the production
build passed with the staging API_URL, and all 28 focused desktop/mobile Playwright
scenarios passed. No staging deployment, data reset, commit or push was performed;
the follow-up feature changes remain local. Full `go test ./...` was not run.
M6 remains open for notifications, group events and full-suite checks; M8 for
comments, media and notifications; M10 for realtime/reconnect. Give A Little is a
provider candidate only: test campaign and webhook setup are unavailable, so M12
remains blocked at that provider prerequisite.


### Event coordinator in-app notices — local slice (25 October 2026)

Assignment notices are inserted in the same database transaction as a new
coordinator assignment, deduplicated by the assignment audit event, and visible only
in the assigned member's inbox. Repeating the same assignment does not create a
second notice. Backend repository and global-app tests passed with disposable
PostgreSQL; the notification link passed desktop/mobile browser checks.
