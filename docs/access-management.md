# Access management foundation

Authentication proves identity; authorization checks the requested action and its scope in PostgreSQL. Roles are never accepted from JWT claims or profile edits. Unknown permissions deny access. No account is promoted automatically.

## Implemented policy

| Role | Permission | Scope | How appointed |
| --- | --- | --- | --- |
| content_editor | sermons.manage | Global | Access administrator or approved operator |
| system_administrator | sermons.manage (existing compatibility) | Global | Approved operator only |
| access_administrator | roles.manage | Global | Approved operator only |
| ministry_leader | ministry.members.manage | One registered ministry UUID | Access administrator or approved operator |

Access administrators do not inherit publishing rights. System administrators do not inherit delegation rights. The API never grants/revokes access_administrator or system_administrator, and rejects finance/care/unknown roles. Sensitive appointments remain operator-managed until fresh authentication, MFA and approval controls are implemented. This is not a complete implementation of every PRD staff role.

A member cannot delegate. No actor can grant or revoke their own roles through this API. Grants require a verified target; revocation remains possible for an unverified target. Every authorization requires a verified actor. Session authentication also checks expiry/revocation. A role revocation applies to subsequent requests using the same access token. In-flight transactions hold role/account locks: a committed revocation prevents later authorization, while a previously authorized transaction may finish before revocation commits.

Ministry permissions are groundwork only: there are no ministry/member management routes yet. A ministry leader cannot act globally or in a different scope. An operator registers a ministry UUID in access_scopes before assigning its leader. When the ministry domain is implemented, creation/deletion must own this scope lifecycle and all membership writes must use CheckTx within the domain transaction. Registered scope IDs alone do not create a ministry or chat.

## HTTP contract

All routes require an active verified Bearer session with roles.manage. Reads and writes share a limit of 30 requests per administrator per minute. POST requires an allowed Origin and application/json; bodies are capped at 16 KiB and unknown fields are rejected.

| Method | Route | Result |
| --- | --- | --- |
| GET | /api/v1/admin/accounts/{accountId}/roles | Role/scope array; empty for no assignments or an absent account |
| POST | /api/v1/admin/accounts/{accountId}/roles | Grant; returns data.changed |
| POST | /api/v1/admin/accounts/{accountId}/roles/revoke | Revoke; returns data.changed |

Example grant/revoke body:

```json
{"role":"content_editor","scope":"","reason":"Approved sermon publishing appointment"}
```

For ministry_leader, scope must be a registered canonical nonzero UUID. Duplicate grant or absent-role revocation returns changed=false; only effective changes produce audit records. Reasons are trimmed, nonblank, valid Unicode, NUL-free and at most 500 characters. Invalid targets/roles/scopes return 422; self-assignment or unauthorized actors return 403. Storage failures return a generic 503.

## Bootstrap and audit

A trusted database operator provisions the first verified access administrator using an approved account UUID, outside the web API. Never bootstrap from the first signup, an email domain, a request body or a migration. Keep operator grants in the church's change record: direct operator SQL does not generate application audit events. Revoke a compromised access administrator through that operator process; the web API cannot revoke privileged roles or lock out the last administrator.

Migration 00009 preserves existing assignments, adds the global scope, scoped uniqueness, foreign keys and role/scope constraints. It seeds no accounts or assignments and never resets staging data.

The service locks the authorizing role/account, serializes target mutations with transaction-scoped advisory locks, and commits the role change and access_audit_events entry in one transaction. Audit insertion failure rolls back grants and revocations. Entries retain actor, target, role, scope, action, reason and server/database time. Do not put confidential care or finance information in reasons. No application route edits/deletes audit records. Database operators remain trusted; future account deletion must account for audit foreign keys.

## Verification

Tests first failed on missing access symbols, then the global routes failed with 404, and contract tests failed on missing OpenAPI paths. Tests use separate disposable PostgreSQL clusters. Coverage includes delegation denial, privileged role rejection, scope isolation, duplicate/concurrent grants, verified account rules, revocation, atomic audit rollback, and global-app session/Origin/JSON checks. No staging data is used.

The migration upgrade rehearsal applies versions 1–8, inserts an existing editor assignment, then applies version 9 twice. It verifies that publishing access survives and no access administrator is created.

Executed checks: race tests passed across all packages with tests (access/app/sermons and cmd/auth/googleoidc/mailresend batches), including the final HTTP and upgrade regressions; go vet, formatting, whitespace, Swagger initializer and Redocly 2.63.0 OpenAPI validation passed. Docker build and smoke verification passed against disposable PostgreSQL. No remote CI, live deployment or production acceptance is claimed for this slice.

See [the access audit](access-management-audit.md) for the reproduced concurrency finding and its fix.
