# Church permissions: policy and first implementation slice

The first M1 slice exposes existing effective staff access through
`GET /api/v1/me/access` and uses it for frontend navigation and staff controls.
It adds no role assignments, automatic promotions or new appointment powers.
The subsequent catalog slice adds migration 14 with role definitions only.
Super Admin, Assistant Pastor/Staff and Finance Officer activation remains
behind the privileged-security/bootstrap work in M1.2–M1.4.

The owner’s [requirements addendum](requirements/church-operations-addendum.md)
controls the target policy. The [current access-management contract](access-management.md)
continues to govern existing role grants and revocations.

## Scope and authority

- **Global:** an explicit church-wide capability; persisted legacy scope is `""`.
- **Ministry/group:** one registered canonical, nonzero ministry UUID. Assignments
  in two groups are two scopes, never a global grant. A scope registry entry is
  groundwork; it does not create a group or chat.
- **Own:** the authenticated account plus a verified ownership relationship. Never
  accept another account ID as proof of ownership. Family access additionally
  requires an explicit guardian/family relationship and field policy.
- **Task:** an assigned event, service, visit or care activity. The domain must
  validate the task relationship and resource lifecycle; an arbitrary UUID grants
  nothing. Task assignment storage/routes are future work.

Roles are additive assignments, not a rank comparison. Staff does not inherit
individual-donor access; a ministry leader has no global authority. Church-wide
role names never imply unrestricted direct-message or confidential-care access.
An authenticated/verified account is not automatically confirmed church membership.

## Named capability plan

Only the three permissions in the first table are implemented in the centralized
role checker today. The second table reserves target names for future domains;
unknown names currently deny access and are not advertised as usable permissions.

| Implemented permission | Assignment | Exact scope |
| --- | --- | --- |
| `sermons.manage` | `content_editor`, legacy `system_administrator` | Global |
| `roles.manage` | `access_administrator` | Global; existing editor/leader delegation only |
| `ministry.members.manage` | `ministry_leader` | Assigned registered ministry only; ministry routes remain future work |

| Planned capability | Super Admin | Assistant Pastor / Staff | Ministry Leader / task assignee | Member |
| --- | --- | --- | --- | --- |
| `roles.privileged.manage` | Global, protected appointment flow | No | No | No |
| `roles.operational.manage` | Global | Only approved operational appointments | No role delegation; activity coordinators use the event capability | No |
| `members.operational.read` / `members.operational.manage` | Authorized church-wide fields | Authorized operational fields | No church-wide member administration; roster access uses the ministry capability | Own profile baseline |
| `groups.create` / `groups.archive` | Global | Explicit additional delegation only | No implicit grant | No |
| `ministry.members.manage` | Church-wide oversight after activation | Church-wide oversight after activation | Assigned ministry only | No |
| `groups.content.moderate` | Global, audited | Global, audited | Assigned ministry only | Own content/reporting policy |
| `events.manage` / `events.coordinators.assign` | Global | Church-wide | Assigned ministry; coordinator must be eligible in its roster | Own RSVP only |
| `attendance.read` / `attendance.check_in` | Global | Church-wide | Explicit service/task assignment | Own records/permitted self-check-in |
| `broadcasts.send` | Global | Church-wide | No church-wide broadcast | No |
| `giving.totals.read` | Global | Aggregates only | No | No |
| `giving.individual.read` / `giving.export` | Protected finance access | Separate Finance Officer assignment required | Separate Finance Officer assignment required | Own giving only |
| `visitors.follow_up` | Global | Authorized operations | Explicit Welcome Team task | Own intake only |
| `pastoral.care.manage` | Future audited exceptional-access policy | Separately assigned care duties | Assigned pastor/care task only | Own booking/request |

Public sermons and existing authenticated own-profile/directory access remain
separate from staff-role capabilities. Future group participation, giving, RSVP,
prayer and messaging also require domain ownership/audience/membership checks;
this table does not implement those routes or override privacy rules.

Reserved role identifiers for the next lifecycle slice are `super_administrator`,
`assistant_pastor`, `staff` and `finance_officer`. Keep existing technical roles
until an explicitly approved mapping is ready. A verified account alone does not
acquire one of these roles. Membership/visitor state belongs to the member domain.

## Protected delegation rollout

1. Add role definitions and assignment constraints without converting existing
   accounts or expanding technical-role capabilities.
2. Implement [fresh reauthentication](reauthentication.md), MFA and audited recovery controls. Google-only
   accounts reauthenticate with Google and keep no local password by default.
3. Obtain the owner's explicit first-Super-Admin account approval, then bootstrap
   through the approved operator procedure. Do not infer it from the first signup,
   email domain or existing access-administrator assignment.
4. Add privileged appointment operations requiring `roles.privileged.manage` and
   the protected authentication context. Do not route them through the legacy,
   less-powerful `roles.manage` operation. Staff can never appoint pastors/admins
   or finance officers through an operational or scoped role.
5. Prevent self-escalation and removal/deactivation of the last active Super Admin.
   Concurrent changes must serialize the invariant, not independently count two
   administrators and remove both. Commit assignment changes and audit atomically.
6. Add eligible-member lookup and User Management UI, then verify every actor,
   target, scope and still-valid session before owner-approved live rollout.

Migration 14 implements the definition groundwork for step 1. The
`role_definitions` table records display names, global/ministry scope, assignment
availability and delegation policy for the four existing technical roles plus
four reserved church roles. Its foreign key preserves the connection between
assignments and known definitions. Existing role/scope constraints and server
policy remain authoritative; changing catalog metadata alone cannot activate a
reserved role or grant a permission. No accounts, assignments or audit events are
seeded. Historical audit names deliberately remain independent of the catalog.

`assignment_enabled` means the role participates in the existing assignment
model, not that every administrator may grant it. `legacy_access_admin` permits
the existing editor/scoped-leader delegation; `legacy_operator` retains the
existing operator-only administrator procedure. `protected_pending` is inactive.
Verified accounts are not seeded with a Member role: church membership remains a
separate domain decision.

The migration is forward-only and repeatable through the existing migration
runner. Deploying it requires no environment change or reset. The protected
assignment lifecycle and remaining rollout steps stay open milestone work.
No privileged bootstrap or appointment is performed by the current-access endpoint.

## Current-access contract

`GET /api/v1/me/access` requires a verified active Bearer session. It accepts no
query parameters or target account ID. A refresh cookie alone does not authorize
it. Limits are 60 requests/account/minute and 120 requests/IP/minute.

```json
{
  "data": {
    "roles": [{"role":"content_editor","scope":""}],
    "capabilities": [{"permission":"sermons.manage","scope":""}]
  }
}
```

Role-free verified accounts receive two empty arrays. Assignments are ordered by
role and scope; capabilities are deduplicated and ordered by permission and scope.
Unsupported or invalid role/scope combinations confer no effective roles or
capabilities. Responses have `Cache-Control: no-store` and contain no email,
credentials, audit reasons or other accounts' assignments. Account verification
and assignments are read in one PostgreSQL statement.

This is display data, not a credential. The same server policy defines both the
snapshot and `CheckTx`; each protected action still checks current database roles
inside its transaction. Committed grants/revocations appear on subsequent requests
with the same JWT. Snapshot reads can reflect their statement's in-flight state;
they do not reserve authorization for a later action.

The frontend keeps snapshots only in memory, initially hides staff controls, shows
own role/scope labels on profile/settings/staff pages, and provides Refresh
permissions. It refreshes on page entry and returning to a visible/focused tab
(automatic events are coalesced and throttled to once per 15 seconds). Explicit
refresh bypasses that throttle. Failed/malformed lookups clear visible staff
permissions; logout invalidates late responses. This does not provide realtime
push of role changes to an already-focused tab. Server denials always take effect
independently of navigation visibility. Membership-scoped groundwork does not show
publishing/delegation tools or claim that ministry management is implemented.

The authorization boundary follows the
[OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html):
least privilege, deny by default and a server check on each protected action.

## Verification and release boundary

TDD started with missing repository/service symbols, followed by global-app 404s,
a missing frontend state module, a browser failure showing the member's editor
form, and a missing OpenAPI path. The corresponding tests were run before those
implementations. One browser attempt used the wrong repository path and another
served an old dist build; neither was treated as a successful verification. The
final browser runs rebuilt/served the canonical app.

Verified locally: PostgreSQL repository/service tests, same-JWT global-app grants
and revocation, unknown-role/scope denial, per-account and cross-account IP limits,
revoked/unverified-session rejection, non-cacheable own-only responses and generic
storage failures. The full affected-package race command passed:

```sh
go test -race -p 1 ./internal/access ./internal/app ./internal/sermons -count=1 -timeout=10m
```

Sixteen frontend unit tests, twenty mocked desktop/mobile browser scenarios and
four real-API workflows passed. The real workflows verify permission refresh
without a new JWT, UI hiding after revocation and an actual 403 for the revoked
publishing action. They use disposable PostgreSQL and loopback-only test fixtures;
no staging data or real emails are involved.

Vet, formatting/whitespace, Swagger configuration, Redocly 2.63.0 contract validation,
Docker build and disposable PostgreSQL smoke checks passed. Redocly reported the
existing license/health-response/reset-example warnings; no new current-access
contract warning was reported. The image rejects anonymous `/api/v1/me/access`
requests and the migration smoke preserves its disposable account.

Release the backend's 1.7.0 contract before the connected frontend. No additional
environment variable or database migration is needed for this slice. This document
records implementation/local checks; it does not establish remote CI, deployment,
new-role activation or live appointment acceptance.
