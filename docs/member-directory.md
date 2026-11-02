# Member directory

GET /api/v1/members lists only verified accounts whose owners explicitly set directoryVisible=true. A verified signed-in member can browse while keeping their own profile hidden. Visitors and expired/revoked sessions cannot browse. There is no member-detail-by-ID route in this slice.

## Frontend integration

Send Authorization: Bearer with an access token. The response contains only id, firstName and lastName per member. Never populate a directory from the private /me response: email, hasPassword, verification state and role assignments are not directory fields.

To opt in or out, the member updates their own preference:

```json
{"directoryVisible":true}
```

Send that body to PATCH /api/v1/me using the member's session and allowed Origin. Existing and new accounts stay hidden unless they opt in. A committed opt-out excludes the account from subsequent directory queries, including continuation pages. A response already delivered or a read already in flight cannot be recalled by this setting.

Directory request examples:

- /api/v1/members
- /api/v1/members?query=grace&limit=20
- /api/v1/members?query=grace&limit=20&cursor=URL_ENCODED_NEXT_CURSOR

Search is a literal case-insensitive substring over first name, one space and last name. Input is trimmed and bounded to 200 characters without control characters. Email is not searched. Percent and underscore characters do not act as SQL wildcards. Use URL encoding for query/cursor values.

```json
{
  "data":[{"id":"11111111-1111-4111-8111-111111111111","firstName":"Grace","lastName":"Member"}],
  "meta":{"nextCursor":""}
}
```

Default page size is 20; valid limits are 1–100. Results use UUID ascending order, not alphabetical or signup order, so renaming does not change the pagination key. Use the returned nextCursor as an opaque value with the same trimmed query. Reset the cursor when changing the search. An empty cursor marks the last page. Empty results are data=[], not null.

The cursor is a position, not a permission or security token. Constructing/changing one cannot bypass SQL visibility checks. The service rejects malformed/oversized cursors, unsupported versions and a cursor from another search. Each page reads current data: new accounts and name/visibility changes can alter search membership between requests; this is not a snapshot or a guarantee of enumerating a changing directory exactly once.

Only query, limit and cursor are accepted, once each. Invalid options return 422 validation_failed with error.fields. Requests are limited to 60/member/minute and 120/IP/minute. A 429 includes Retry-After; a member's directory limit does not block their own profile reads or edits. Responses use Cache-Control: no-store.

## Persistence and scope

Migration 00011 adds a partial index on IDs of verified, directory-visible accounts. It changes no account, role or privacy setting and runs forward-only. It does not create accounts, opt members in or reset staging data.

This slice provides directory search/listing only. Ministries, group memberships, direct messaging and public profile pages remain later domains; listing a member does not grant access to any of their private data.

## Staff member lookup

`GET /api/v1/admin/members?query=...` is a separate, read-only operational lookup
for Super Admin, Assistant Pastor, and Staff. It searches verified, active
accounts by literal case-insensitive substring in name or email and returns
`id`, `firstName`, `lastName`, `email`, and `joinedAt`. Results are paginated by
opaque UUID cursor, limited to 50 rows per page, and excluded for unverified or
deletion-requested accounts. Each returned email is recorded as an audited
search result; the query text is not copied into the audit log. The frontend
shows names and join dates first, then fetches details only when a staff member
selects a person.

`GET /api/v1/admin/members/{accountId}` returns the same basic fields for one
verified, active account. Every detail response records an audit event naming
the staff account, member account, `email` field and time. The response is not
sent if that audit insert fails. The detail includes a profile `version` used
for conflict-safe name corrections. Both operations are rate-limited and use
`Cache-Control: no-store`. They do not expose password state, MFA state, role
assignments, sessions, private profile preferences, phone/address fields or
family/care records. Staff member editing, additional sensitive fields,
field-specific policies and exports remain future work. See `GET /openapi.json`
for the request and response contract.

`PATCH /api/v1/admin/members/{accountId}` allows Super Admin, Assistant Pastor,
and Staff to correct only `firstName` and/or `lastName`. Require the latest
`expectedVersion` from the detail read and a 1–500 character reason. A stale
version returns 409; concurrent member self-edits also advance the version. The
change and a reasoned audit event commit together, and an audit failure rolls the
change back. Do not put care information in the reason. Email, passwords,
contact, family and care information are not editable through this endpoint.

`GET /api/v1/admin/members/{accountId}/access-audit` is restricted to Super
Administrators. It pages through the audited search-result, detail-read and name
correction events for one verified, active member. Entries show the staff account
ID, action, disclosed or changed field names, optional reason, and timestamp;
they contain no email value or credentials. Use the returned numeric
`nextCursor` only to continue the same member's history. The endpoint is
rate-limited, non-cacheable, and has no export operation. Each audit-history
review is itself recorded as `audit_view`; those review records are excluded
from the displayed events.

## TDD evidence

Repository/service tests cover member lookup audit writes, audited detail reads,
audit failure preventing disclosure, staff-only access and exclusion of inactive
accounts. Global-app tests cover detail lookup, unknown/inactive member handling,
authorization and rate limits; frontend browser tests confirm private email is
shown only after selecting a member and that detail is fetched on demand.

For the audited-detail slice, local verification used the full `internal/access`
package plus focused global-app member/OpenAPI tests against disposable
PostgreSQL, `go vet`, OpenAPI JSON parsing, frontend `npm test`, production build,
and desktop/mobile Playwright flows. No live staging acceptance is claimed for
this unpushed slice.
