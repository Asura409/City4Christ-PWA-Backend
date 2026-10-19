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

## TDD evidence

Repository/service tests first failed on absent directory symbols. The global-app route test failed with 404 before wiring. An additional failing test caught empty error.fields for invalid parameters, and contract tests failed on the missing route/DTO. Tests run against disposable PostgreSQL and cover literal/name-only search, exclusion of hidden/unverified accounts, stable pagination, malformed/cross-search cursors, privacy on cursor replay, account and shared-IP limits, revoked sessions and the restricted response fields.

Executed verification: full `go test -race ./... -count=1 -timeout=5m`, focused directory regressions, go vet, formatting/whitespace checks, Swagger initializer, Redocly 2.63.0 OpenAPI validation, Docker build and disposable PostgreSQL smoke verification passed locally. No live staging acceptance is claimed for this unpushed directory slice.
