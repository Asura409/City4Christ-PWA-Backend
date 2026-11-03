# Member profiles and privacy: first slice

Members can read their own account/profile, edit first name and last name, and manage one profile photo. The caller is always taken from a verified active Bearer session. There is no account-ID parameter on profile edits.

## API

| Method | Route | Behavior |
| --- | --- | --- |
| GET | /api/v1/me | Existing own-account fields plus directoryVisible |
| PATCH | /api/v1/me | Update only firstName, lastName and/or directoryVisible |
| PUT | /api/v1/me/avatar | Associate an owned, clean `profile_photo` upload with the current account |
| DELETE | /api/v1/me/avatar | Remove the current account's photo; safe to repeat |

Upload photos using the existing media flow: reserve with purpose `profile_photo`, upload the JPEG, PNG or WebP file to its short-lived signed URL, complete the upload and wait for scan status `available`. Profile photos are limited to 5 MiB. Send `{"uploadId":"..."}` to the PUT route. The server checks ownership, purpose, scan status and active account; raw object keys are never accepted. The profile response contains an optional short-lived signed `avatar` attachment for its owner. Replacing or removing a photo queues its old object for cleanup once no feature references it.

Example PATCH body:

```json
{"firstName":"Grace","lastName":"Member","directoryVisible":false}
```

The response is the current profile in data, including id, email, firstName, lastName, emailVerified, hasPassword and directoryVisible. Email is returned only to its owner here; this does not authorize exposing it in a later directory. Password hashes, tokens and staff assignments are not part of this response.

Names are trimmed, must contain 1–100 Unicode characters and cannot contain control characters. Omitted/null fields stay unchanged; at least one non-null editable field is required. directoryVisible=false is an explicit opt-out, not an omitted value. Partial edits preserve concurrently changed fields that this request did not supply; same-field edits use the last committed value.

PATCH requires application/json, an allowed Origin and an active verified Bearer session. It is limited to 30 attempts per member per minute. GET is not blocked by the write limit. JSON bodies have the existing 16 KiB cap. Missing/expired/revoked sessions fail with 401; invalid fields return 422 with error.fields. Unknown fields and wrong JSON types return 400 with field guidance. Email, password, verification status, role, account ID and ownership cannot be changed here. Email/password changes need separate security flows.

## Privacy and migration

Migration 00010 adds directory_visible NOT NULL DEFAULT false. Existing and new members start hidden. It preserves existing names, credentials, verification and role assignments; no account is seeded or reset. Tests rehearse upgrading an existing version-9 account, repeat the migration and verify its editorial role survives. A privacy-only edit preserves legacy name formatting.

The authenticated member directory shows all verified, active members, with only names and an optional short-lived signed photo. Email addresses and other account/security fields remain private. The legacy `directory_visible` profile field is retained for compatibility but no longer controls member search visibility. Ministries, biographies, recovery and email changes are separate slices.

## TDD and verification

Repository/service tests first failed on missing implementation. Global-app tests failed with 405 before PATCH was wired. Tests cover real PostgreSQL persistence, partial-update concurrency, account isolation, default privacy/opt-out, protected-field rejection, invalid names, session revocation and write limits. Tests run only against disposable PostgreSQL.

Executed verification: the full `go test -race ./... -count=1 -timeout=5m` suite passed. Final targeted tests also executed the Swagger examples through the global app. `go vet`, formatting/whitespace checks, Swagger initializer checks and Redocly 2.63.0 OpenAPI validation passed. The local Docker build and smoke test passed with disposable PostgreSQL. This does not claim live staging or browser acceptance for the profile changes.
