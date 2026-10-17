# Local sermon publishing audit

Scope: editor authorization, publication transitions, partial updates, concurrent edits, audit atomicity, media links, and the Swagger contract. All changes and review artifacts remain inside the project. No staging account, role, data or service was changed during this audit.

## Finding fixed

**Partial updates rewrote fields that were not supplied.** Publishing or editing one field passed the entire stored record through trimming/canonicalization. That could remove paragraph formatting from an existing summary, or change legacy titles/speaker labels during a publish-only request.

The update path now validates the merged record while applying normalization only to explicitly supplied fields. Omitted fields retain their stored values. `TestPatchPreservesFieldsNotSupplied` first failed on the old behavior, then passed after the fix. The API guide and OpenAPI description now state this behavior.

## Additional checks

- A pending role revocation cannot be bypassed by creating content; after it commits the same actor is forbidden. The regression uses a real PostgreSQL role lock and bounded request context.
- Editor writes reach 429 with a retry hint. The limit does not block reads or transfer to a second editor account.
- Existing tests cover verified active sessions, member denial, revoked roles/sessions, allowed Origin enforcement, privilege-field rejection, malformed media URLs and database-level media constraints.
- Concurrent edits of the same version yield one success and one conflict. Failed audit insertion rolls back both draft creation and edits.
- Drafts and future publications remain hidden; publishing, scheduling and unpublishing use explicit server-side transitions. Timestamp precision and UTC serialization boundaries are tested.
- Swagger's initializer supports GET/POST/PATCH without persisting authorization or enabling an external validator.

## Executed verification

- `go test -race ./... -count=1 -timeout=5m` passed, including repository and global application tests against disposable PostgreSQL.
- `go vet ./...`, formatting and whitespace checks passed.
- OpenAPI validation with Redocly 2.63.0 and the Swagger initializer test passed.
- `docker build -t city4christ:publishing-audit .` and `bash scripts/docker-smoke.sh city4christ:publishing-audit` passed. Smoke checks used disposable PostgreSQL and did not touch staging.

## Operational limits

Role grants remain deliberate operator actions; no account is promoted automatically. Video checks validate supported YouTube URL syntax, not ownership or remote availability. The audit table has no application edit/delete endpoint; a privileged database operator remains trusted. Audit foreign keys must be considered by future retention/account-deletion features.

No live browser acceptance or remote CI run is claimed by this local audit. Publishing the feature branch does not merge PR #8 or deploy these changes.
