# Implementation status audit — 1 November 2026

This reconciles the [milestone checklist](implementation-checklist.md), original
`City4Christ_PWA_Backend_PRD.docx`, [owner operations addendum](requirements/church-operations-addendum.md),
current backend source and the separate frontend repository. It adds no product scope.
The historical checklist combines code, provider rollout and acceptance in single
checkboxes; those boxes are not a count of missing features.

## Status definitions and evidence

- **Implemented:** source and relevant tests/UI exist; not a fresh test-pass claim.
- **Deployed:** merged source is released, subject to runtime configuration.
- **Acceptance pending:** a provider, authenticated workflow, device or operational
  condition has not been established by this audit.
- **Missing/partial:** required behavior is absent or incomplete in inspected code.
- **Deferred/superseded:** explicitly excluded from current scope or replaced by an
  agreed design; do not implement automatically.

Released backend main is `bc4150b821a472328303d2acd08aa239de7eeee2`,
frontend main is `fbcf127cf5d2ab20a64c454c2df5b3a0788f50f4`.
[Backend main CI](https://github.com/Asura409/City4Christ-PWA-Backend/actions/runs/41286772050)
and [frontend main CI](https://github.com/Asura409/City4Christ-PWA/actions/runs/41286778968)
were rechecked as successful. The backend retry followed a Docker Hub 502;
its successful run includes PostgreSQL race tests, Docker build and smoke checks.
No full local suite was rerun for this documentation audit. Frontend CI runs a
selected browser-test list, not every browser spec; a green run is not fresh
coverage of every domain workflow.

Read-only live checks during this audit observed API OpenAPI version 1.22.0,
readiness 200, visitor configuration 200 (`enabled:false`, retention 90 days,
policy `visitor-welcome-v1`), and anonymous staff visitor listing 401.
The preceding release verified the deployed welcome screen at desktop/mobile sizes.
These are narrow checks, not blanket authenticated/provider acceptance.

## Reconciled milestone status

| ID | Implemented in released source | Remaining code or decisions | Acceptance/rollout limits |
| --- | --- | --- | --- |
| M0 Auth | Password/Google auth, OTP, sessions/refresh/replay, logout, recovery, CORS and rate limits | Document final cookie/domain and production database strategy | Current-device Google/linking and full session/email acceptance remain separate; do not discard prior user confirmations |
| M1 Permissions | Protected church roles, fresh authentication, app MFA, audited grants/revocations, last-admin safeguards, role UI | Factor-change alerts and controlled assisted recovery | This audit does not establish live first-admin bootstrap status; historical “no real account promoted” text is not current evidence |
| M2 Members/visitors | Own name/privacy profile, opt-in directory; encrypted consented visitor intake, staff review/status/withdrawal, 90-day expiry; forgot/reset password | Expanded member/contact administration; family/guardian model; secure visitor account claiming; explicit volunteer follow-up assignments; verified welcome delivery; change-email/password and account deletion | Visitor intake remains disabled pending dedicated private key/enablement; expanded contact/family access and retention need decisions |
| M3 Notifications | Inbox/read-all, preferences, broadcasts/review/send, Web Push subscriptions/jobs/worker, generic message/event/group/visitor notices | Verified visitor welcome delivery and task-specific triggers; operational delivery visibility/retention where required | Real Web Push needs keys, worker configuration and device acceptance; presence of code does not prove these are enabled |
| M4 Groups | Directory, join/leave/request/decide, rosters, group administration/archive/restore, scoped leader permissions and membership inbox notices | No missing foundation feature established in this audit | Complete actor/group boundary and device acceptance remain release gates |
| M5 Posts/media | Church/group posts, edits/deletion, comments/reactions, reports/moderation, R2 signed uploads, ClamAV scan jobs, clean post/sermon attachments | Profile avatars; general abandoned/unattached-object cleanup policy/jobs; moderation report/audit retention policy | Live R2/CORS/scanner acceptance is not established; rejected/processed source cleanup already exists, so cleanup is partially implemented |
| M6 Events | Church/group publication, RSVP/capacity/cancel, calendar export, scoped coordinators and recipient notices | No missing agreed foundation behavior established | Current authenticated/device/provider acceptance remains separate |
| M7 Attendance | No attendance domain or connected check-in UI found | Service/session model, assigned ushers, protected lookup/QR, repeat-safe check-in/corrections/counts, family/Sunday School flow | Self-check-in, QR expiry/replay and guardian/pickup policies must be resolved before those flows |
| M8 Prayer | Audience/anonymity, acknowledgements, encouragements, owner status, reports/moderation and expiry | Prayer attachments and associated privacy/media tests | Live media acceptance also applies; expiry archival is not physical text deletion |
| M9 Pastoral care | Pastor publication/directory, slots, held approval-based booking, cancellation, private queues, email updates/reminders | Wider service/blocked-period rules, confidential notes, assigned-care dashboard extensions and related access/retention decisions | Existing requester/assigned-pastor boundary remains; real reminders/worker/device acceptance is separate |
| M10 Messaging | Direct/group durable REST messaging, read markers, participant/current-membership-scoped search, moderation, session-bound WSS metadata/reconnect catch-up, ephemeral typing/presence, and scanned private attachments using the R2/ClamAV media path | Direct-message sender deletion policy, general unattached-object cleanup, and live media acceptance remain open | Multi-instance fanout is deferred; durable writes use REST, not the original proposed WS-send contract; end-to-end encryption is not implemented |
| M11 Word/home/PWA | Sermon series/notes/progress, devotionals, GMT+1 daily verse, Bible lookup; basic home name/verse/navigation; SW/offline groundwork | Managed YouTube live state/scheduling, full home aggregation, authorized cross-domain search, finished install/offline/update UI and removal of legacy samples | External YouTube playhead tracking is not supported by current progress design; SW groundwork is not finished PWA acceptance |
| M12 Giving | Give A Little checkout link builder, giving screen and return screen; no PWA payment-success claim | Verify final hosted handoff/return experience | Ledger, provider webhooks, PWA receipts/history and finance reports are superseded by owner-selected hosted integration |
| M13 Operations | CI, Docker smoke, additive migrations, health routes, redacted logging foundations and deployment tooling | Durable production hosting/backups/restore drill, actionable monitoring, complete privacy/security/load/outage review and release approval | Passing CI and staging readiness do not close these operational gates |

## Source traceability

Backend route wiring is in [app.go](../internal/app/app.go). Domain modules and
contracts give finer scope and limits:

- [Auth/session rules](auth-sessions.md), [recovery](password-recovery.md),
  [church-role management](church-role-management.md) and [MFA](mfa.md).
- [Member profiles](member-profiles.md), [directory](member-directory.md) and
  [visitor welcome](visitor-welcome.md). Expanded member/family administration is
  distinct from an opt-in directory or a visitor form.
- [Group content](group-content.md), [notification delivery](notification-delivery.md),
  [posts](posts.md), [media storage](media-storage.md), [events](events.md) and
  [prayer](prayer-wall.md).
- [Pastoral bookings](pastoral-bookings.md), [appointment emails](pastoral-email-delivery.md),
  [messaging](messaging.md), [sermon member tools](sermon-member-tools.md),
  [devotionals](devotional-publishing.md), [daily verse](daily-verse.md) and
  [Bible lookup](bible-lookup.md).

In the independent frontend repository, `src/App.jsx` connects auth/profile,
sermons, groups/posts/events, prayer, messages, bookings, notifications, giving,
devotionals/Bible and visitor screens. `src/screens/HomeScreen.jsx` is a partial
home implementation. `src/screens/GivingScreen.jsx`, `GivingReturnScreen.jsx`
and `src/giving/checkout.js` establish the hosted giving scope. Profile currently
covers name/directory preferences/Google linking rather than expanded family/contact
management. Legacy `/staff.html`, `/profile.html` and `/members.html` retain
connected role/security/directory tools; absence of React equivalents is not absence
of those features. Legacy live/get-app HTML also ships and needs review as part of
M11; a shipped sample page is not implemented live management or finished install UX.
`frontend/src/screens/NewUserScreen.jsx` still describes ministry membership and
preferences as future, despite their connected screens. The legacy get-app page
retains a store placeholder. These are existing UI cleanup gaps, not new domains.

## Scope provenance

The original PRD prioritizes auth/profile, community/groups, prayer, events/RSVP,
pastoral booking, sermon catalog/progress/notes, inbox and direct messaging for
MVP. Families, volunteer task delegation and attendance are approved operations
addendum scope, not prerequisites silently added to that original minimal MVP.
Keep the original MVP and the broader operations backlog distinct when selecting
the public release boundary.

## Requirements versus recommendations

Welcome Team task delegation comes from addendum R1 and checklist M2.6; it was not
an invented new domain. The next-slice recommendation was an implementation order,
not an approval to expand scope. Current intake already supports authorized staff
follow-up; volunteer assignment is a narrower future permission model.

Original PRD account deletion must be tracked explicitly even though it was omitted
from detailed M2 tasks. Its deletion/retention policy must be settled before release.
Original messaging follow-ups now include participant-scoped ephemeral typing,
chat-scoped online presence, participant-scoped search, and private scanned
attachments. Shared attachment objects are detached from moderator-removed group
messages; periodic cleanup for unattached R2 objects remains open.
REST writes plus WSS metadata are the current design; shared fanout remains deferred
until multiple API instances are needed.

SMS is not approved for implementation merely because W06 mentions it: provider,
consent and costs remain undecided. Giving backend scope is explicitly superseded.
No exceptional access to private DMs or pastoral records is granted by this audit.

## Remaining work order to choose from

These are existing backlog choices, not newly authorized implementation:

1. Complete member/visitor onboarding in small slices: approved member fields and
   access rules, then staff administration, safe claiming and task delegation.
2. Build attendance after member/guardian and check-in policies are ready.
3. Finish remaining original PWA features: managed YouTube links, home/search,
   install/update UI, avatars/prayer/message media where approved.
4. Complete care/account-management extensions with their required privacy rules.
5. Finish provider rollout and M13 operational gates before public launch.

Provider setup and acceptance can proceed independently of unrelated code, but
features with shared schema/permissions or unresolved policies should not be
implemented simultaneously just to increase concurrency. Testing-team ownership
of device acceptance does not make those gates complete.

## Subsequent local account slice — not deployed

Signed-in password change is now implemented locally in both repositories; it is
not part of the released commits named above. It adds the Profile form and
`POST /api/v1/auth/password/change`, current-password proof, strict Unicode/input
validation, account/IP limits, all-session revocation, reset-link invalidation and
the existing encrypted changed-password email notice. Google-only accounts remain
without local passwords. See [contract](password-change.md). Change-email,
assisted recovery, factor alerts and account deletion remain separate gaps.

## Subsequent reviewed-deletion slice — 2 November, not deployed

Owner deferred email changes and selected reviewed account-deletion requests with
immediate sign-in disablement, session revocation and directory hiding. The local
implementation includes the self-service Profile flow, Super Admin review queue
and audited detail, fresh primary/MFA checks, last-effective-admin protection,
pastoral-commitment blocking and active-state gates across authentication and
relevant delivery/visibility paths. See [account-deletion contract](account-deletion.md).
Permanent erasure, reactivation and a completion deadline are not implemented or
promised. Existing source release IDs above do not include this local work.

### Verified local evidence for reviewed requests

- Full account-deletion domain race suite passed (197.150s); targeted pending-account
  access race passed (24.392s). The final full access normal suite passed (76.008s).
- All 42 auth top-level cases have normal passing evidence across completed cases
  and retries; the remaining 27-case retry passed (221.614s). Both credential/delivery
  gate cases passed with race detection, and the optimized refresh/deletion race
  passed separately (9.140s).
- Final global-app deletion, password-change/input and protected-role checks passed
  (110.428s). Notification pending-account race checks passed (94.072s); pastoral
  pending-account race checks passed (18.086s); full profiles race suite passed
  (27.423s).
- Frontend unit suite passed 50/50; selected desktop/mobile regressions passed
  36/36 (18 deletion, six password and 12 existing React cases). Both actual Go API
  desktop/mobile request→disabled-sign-in browser cases passed (35.758s), using
  disposable PostgreSQL and synthetic data.
- Final frontend and Docker builds passed. Container smoke verified migration47,
  repeated forward migration preservation, readiness, anonymous route denials and
  disabled workers. Vet, Swagger, OpenAPI1.24 validation (eight existing warnings),
  formatting and whitespace checks passed.

Initial broad auth/access race runs and several concurrent database setups timed
out under VPS CPU/disk contention; those invocations are not counted as passing.
Database-heavy checks were staggered, and affected cases were retried. No complete
`go test -race ./...` run is claimed for this local slice. No provider acceptance,
real email, staging mutation or deployment was performed. A later rollout must
apply migration47 before installing the new auth/email-worker binaries.
