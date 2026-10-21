# Protected church-role management

This is the implemented local M1 appointment slice, not an approval to appoint a
real account or release it. Existing technical assignments and staging data are
preserved. Migration17 activates the catalog definitions but grants nothing.

## Implemented authority

| Actor | Read role candidates/assignments | Protected global appointments | Scoped ministry-leader appointments | Sermon publishing |
| --- | --- | --- | --- | --- |
| Super Admin | Yes | Super Admin, Assistant Pastor, Staff, Finance Officer | Registered ministry only | Yes |
| Assistant Pastor / Staff | Yes | No | Registered ministry only | Yes |
| Ministry Leader | No church-wide lookup | No | No delegation | Only with a separate editor assignment |
| Finance Officer | No operational lookup by this role | No | No | Only with a separate editor assignment |
| Legacy Access Administrator | Existing legacy role lookup only | No | Existing legacy editor/leader flow only | Only with a separate editor assignment |
| Member / other verified account | Own access/profile only | No | No | No |

Roles are additive, with no automatic conversion of legacy administrators. Church
membership is not inferred from verification: a formal member/visitor lifecycle
belongs to M2. Finance Officer is an assignable identity with no invented finance
capabilities; individual giving/reporting endpoints must enforce their own future
policy. Events, groups, attendance, messaging and confidential care are not enabled
by this migration.

Protected global roles use `super_administrator`, `assistant_pastor`, `staff` and
`finance_officer`, all at empty scope. Operational appointments currently permit
only `ministry_leader` at a registered canonical, nonzero UUID. Registering a scope
is not creating a ministry/group. Existing editor assignment remains separate.

## API and security boundary

- `GET /api/v1/admin/role-candidates?query=...&limit=20&cursor=...` searches verified
  accounts by literal name/email substring (3–100 characters, escaped wildcards).
  Limit1–50; stable account-UUID cursor, reused with the same query. Results contain
  only `id`, `firstName`, `lastName`, `email`, `mfaEnabled`; no phone/address,
  donor data or authenticator secrets. Administrative lookup does not alter public
  directory opt-in.
- `GET /api/v1/admin/accounts/{accountId}/church-roles` reads current assignments.
- `POST` to that path grants; `POST .../church-roles/revoke` revokes. Body:
  `{"role":"staff","scope":"","reason":"Approved staff appointment"}`.
  Effective changes return `data.changed=true`; repeats return false without a new
  audit. Unknown fields, target IDs in bodies, unknown/duplicate query parameters,
  invalid roles/scopes and empty/overlong reasons are rejected.

All routes require an active verified Bearer session and current database
capabilities; cookies alone cannot authorize them. Shared limits are30 requests
per account/minute and120/IP/minute. Responses are `no-store`. Mutations require
an exact allowed Origin and JSON and use **fresh primary authentication plus
mandatory current-session MFA**, even when no factor has yet been enrolled.
Password and Google-only accounts use the existing identity confirmation panels;
no local password is invented for Google accounts. Primary and MFA proofs last at
most five minutes, are session-bound, and are checked after blocking locks.

Protected grant targets must also be verified and have confirmed application MFA.
Pending enrollment is insufficient. MFA disable returns409 `mfa_protected_role`
while any protected church role remains. Password reset preserves the factor and
revokes sessions; it cannot remove this protection.

Self-grants and other self-changes are prohibited, except explicit Super Admin
self-resignation when another verified MFA-enrolled Super Admin remains. The final
active administrator cannot be removed (`409 last_administrator`). All role
mutations—including legacy delegation—serialize before authorization row locks.
Last-admin checks freeze administrator accounts and commit role/audit together.
Revocation takes effect with the existing JWT on the next request. Audit failure
rolls back the change; no unaudited appointment is accepted.

Future account deletion, suspension, unverification and assisted factor removal
must participate in this last-admin invariant before those operations are added.
The current account API exposes none of those operations.

## Explicit first administrator bootstrap

No browser endpoint, migration or server startup bootstraps an administrator.
The owner must identify and approve the exact existing account before this command
is executed against a non-test database. First, that account signs up, verifies
email and confirms authenticator enrollment. Apply normal forward migrations first.
Then an authorized operator with the existing private API environment executes:

```sh
api bootstrap-super-admin --account-id ACCOUNT_UUID --confirm-account-id ACCOUNT_UUID --reason "Owner-approved first administrator"
```

Both IDs must match and be canonical/nonzero. This command uses the existing
private database configuration, never accepts a database URL as an argument, never
runs migrations, and never prints credentials. It records a singleton bootstrap
audit and assignment atomically. Any existing bootstrap record or Super Admin
prevents reuse; subsequent appointments use the protected API. A failed audit or
ineligible target changes nothing. No real bootstrap has been performed here.

## Frontend and release

The React interface is preserved. Profile links open the connected staff page,
which keeps legacy editor controls separate from church appointments. Authorized
staff search/select a verified target, inspect appointments, supply role/scope and
reason, explicitly confirm and submit. Selection is cleared when the search or
account changes; late responses cannot revive logged-out state. Security errors
show confirmation guidance and require explicit resubmission, not automatic retry.

Release backend1.10/migration17 before the corresponding frontend. No new runtime
secret or hosting variable is required. Do not reset data, silently promote an
access administrator, disable protected MFA to recover an account, or treat a
passing automated test as real Google/provider acceptance.

## Verification evidence

Strict TDD observed missing service/guard symbols, missing bootstrap storage, global
HTTP404s and missing OpenAPI paths before implementation. A real desktop/mobile
appointment test initially failed on its missing build-tagged operator fixture,
then passed once that isolated fixture was added. Review reproduced a frontend
permission-refresh bug (Staff selection changed to Super Admin); failing browser
tests drove the correction and confirmation invalidation. The old Docker image
failed the new migration smoke assertion, and the rebuilt image passed.

Verified locally on21 October2026: full `go test -p 1 ./... -count=1 -timeout=5m`,
plus targeted race tests across access/app/auth for church roles, mandatory MFA,
legacy delegation, lock waits and proof expiry. All passed. `go vet ./...`, Swagger
configuration, formatting/whitespace, documentation links and OpenAPI1.10 validation
passed. Redocly reports the four existing license/health/reset-example warnings.

Frontend unit/build checks passed24 cases. All ten real API browser workflows
passed against the global app with disposable PostgreSQL, including React account
flows, Google-only confirmation, MFA and church grant/revoke with an unchanged JWT.
The final combined mocked browser regression passed all50 desktop/mobile scenarios,
including the permission-refresh selection/confirmation regression.
Docker image `city4christ:church-roles-verify` built and passed disposable smoke
checks, upgrade preservation, empty bootstrap/administrator checks, anonymous route
denials and clean API/worker shutdown. Nothing here establishes remote CI, real
Google acceptance, a staging release or a real administrator appointment.
