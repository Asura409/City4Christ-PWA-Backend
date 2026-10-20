# Member profiles and privacy: first slice

Members can read their own account/profile and edit first name, last name and their directory visibility preference. The caller is always taken from a verified active Bearer session. There is no account-ID parameter on the edit route and no public profile/directory endpoint in this slice.

## API

| Method | Route | Behavior |
| --- | --- | --- |
| GET | /api/v1/me | Existing own-account fields plus directoryVisible |
| PATCH | /api/v1/me | Update only firstName, lastName and/or directoryVisible |

Example PATCH body:

```json
{"firstName":"Grace","lastName":"Member","directoryVisible":false}
```

The response is the current profile in data, including id, email, firstName, lastName, emailVerified, hasPassword and directoryVisible. Email is returned only to its owner here; this does not authorize exposing it in a later directory. Password hashes, tokens and staff assignments are not part of this response.

Names are trimmed, must contain 1–100 Unicode characters and cannot contain control characters. Omitted/null fields stay unchanged; at least one non-null editable field is required. directoryVisible=false is an explicit opt-out, not an omitted value. Partial edits preserve concurrently changed fields that this request did not supply; same-field edits use the last committed value.

PATCH requires application/json, an allowed Origin and an active verified Bearer session. It is limited to 30 attempts per member per minute. GET is not blocked by the write limit. JSON bodies have the existing 16 KiB cap. Missing/expired/revoked sessions fail with 401; invalid fields return 422 with error.fields. Unknown fields and wrong JSON types return 400 with field guidance. Email, password, verification status, role, account ID and ownership cannot be changed here. Email/password changes need separate security flows.

## Privacy and migration

Migration 00010 adds directory_visible NOT NULL DEFAULT false. Existing and new members start hidden. It preserves existing names, credentials, verification and role assignments; no account is seeded or reset. Tests rehearse upgrading an existing version-9 account, repeat the migration and verify its editorial role survives. A privacy-only edit preserves legacy name formatting.

Opting in currently stores a preference only. The future member directory must filter out hidden members server-side, show only approved fields and keep email addresses private by default. Ministries, directory search, biography/avatar uploads, recovery, email changes and account deletion are later slices.

## TDD and verification

Repository/service tests first failed on missing implementation. Global-app tests failed with 405 before PATCH was wired. Tests cover real PostgreSQL persistence, partial-update concurrency, account isolation, default privacy/opt-out, protected-field rejection, invalid names, session revocation and write limits. Tests run only against disposable PostgreSQL.

Executed verification: the full `go test -race ./... -count=1 -timeout=5m` suite passed. Final targeted tests also executed the Swagger examples through the global app. `go vet`, formatting/whitespace checks, Swagger initializer checks and Redocly 2.63.0 OpenAPI validation passed. The local Docker build and smoke test passed with disposable PostgreSQL. This does not claim live staging or browser acceptance for the profile changes.
