# Access management audit

Scope: privilege escalation, role/scope boundaries, delegation, transaction locking, audit integrity, HTTP protections and forward migration compatibility. Only disposable PostgreSQL and test accounts were used locally.

## Finding and fix

Role mutations previously locked the target account FOR UPDATE. Ordinary account readers could block changes, and two access administrators appointing each other as editors could contend on each other's already-shared account locks. The new TestDelegationDoesNotRequireExclusiveAccountLocks failed with a context timeout before the fix.

Mutations now serialize per target using PostgreSQL transaction-scoped advisory locks and retain shared verified-account locks. Existing authorization readers stay compatible, duplicate grants remain serialized and role/account revocation locks remain effective. PostgreSQL's built-in hash selects a lock key; collisions only serialize unrelated targets. This lock is coordination, not authorization or cryptography.

The regression passes after the fix. Existing tests cover concurrent duplicate grants, pending administrator revocation, revocation with the same JWT, scope isolation, self-promotion denial, privileged-role rejection and atomic rollback on audit failure. Migration rehearsal preserves an existing editor assignment from version 8 through repeated version 9 migration runs without appointing an administrator.

## Boundaries

The API delegates only content_editor and registered ministry_leader assignments. Administrator, finance and care appointments stay outside it until their MFA/approval controls exist. Access administrators have no inherited content or sensitive-data permissions. Ministry scopes are a foundation for a future ministry module, not working ministry or chat features.

Application mutations and audit events commit together. Direct database operators remain trusted and must maintain their own appointment records. No first administrator is seeded or selected from signup order. An approved verified account must be named before a staging appointment; the audit does not choose one.

## Executed verification

After the lock fix, `go test -race ./internal/access ./internal/app ./internal/sermons -count=1 -timeout=5m` passed. `go vet ./...`, formatting/whitespace and Swagger initializer checks passed. The audited Docker image built and passed `scripts/docker-smoke.sh` using disposable PostgreSQL, including repeated migrations and account preservation. The unchanged OpenAPI contract had already passed Redocly 2.63.0 validation. Remote CI and live staging acceptance are separate checks.
