# Fresh authentication for sensitive actions

This local implementation adds explicit password and Google confirmation without
signing out, changing accounts, issuing new application tokens or granting roles.
It is the fresh-authentication portion of M1.3, not MFA or privileged appointment.

## Contract

All three endpoints require an active verified Bearer session. They accept no
query parameters or target account/session identifiers. Responses are private
and `Cache-Control: no-store`. Refresh cookies alone never authorize them.

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/v1/auth/reauthentication` | No body | `fresh`, nullable `freshUntil`, `passwordAvailable`, `googleAvailable` |
| `POST /api/v1/auth/reauthentication/password` | `{"password":"current password"}` | Current confirmation status |
| `POST /api/v1/auth/reauthentication/google` | `{}` or `{"returnTo":"https://allowed-frontend/auth.html"}` | `{ "url": "https://accounts.google.com/..." }` |

POST requests require an exact allowed Origin and JSON. Password and Google
confirmation share five attempts per account per 15 minutes, including failures.
The existing authentication IP limit is 60 requests/minute. Status reads allow
60/account/minute and 120/IP/minute. Rate-limited requests return 429 and
`Retry-After`; storage failures return a generic 503.

A successful password sign-in establishes up to five minutes of freshness.
Explicit confirmation renews it only for that session, bounded by session expiry.
The server checks its persisted authentication time, not JWT `iat`, token refresh
time, session creation time or frontend state. Old sessions upgraded by migration
15 remain signed in but need confirmation before a sensitive action. Ordinary
Google sign-in does not establish this stronger freshness: it may reuse an old
Google SSO session without recent authentication evidence.

Password confirmation verifies the current Argon2id hash using the maintained
password library, then rechecks that hash under an account lock before persisting
the proof. Google-only accounts have no invented local password and must use
Google. A local account with an explicitly linked Google identity can use either
available method. The HTTP status response disables Google when no provider is
configured.

## Google browser round trip

1. POST to the Google confirmation endpoint with the existing Bearer token and
   optional allowlisted `auth.html` return URL. Preserve the Secure, HttpOnly
   `__Host-c4c_oidc` cookie and navigate to the returned Google URL.
2. The request includes random one-use state, nonce, PKCE S256, `max_age=0`, account
   selection and an essential `auth_time` claim request. It uses the existing
   configured `/api/v1/auth/google/callback`; no new provider redirect URI is needed.
3. The callback consumes only the browser-bound, unexpired confirmation flow. The
   maintained OIDC library verifies the signed identity token's issuer, audience,
   expiry and nonce. Confirmation additionally requires the **already-linked
   subject**, verified email claim and recent signed `auth_time`. Email equality
   alone is never identity proof. Missing, stale or future `auth_time` fails closed.
4. `auth_time` must be no earlier than flow start minus 30 seconds, no later than
   server time plus 30 seconds, and less than five minutes old. The proof deadline
   derives from that authentication time, capped at the current time/session
   expiry. Issuance time is never substituted for authentication time.
5. No account or external identity is created or linked. No application session
   starts and no refresh cookie is replaced. The browser returns to the stored,
   revalidated target with `reauthentication=complete` or
   `auth_error=reauthentication_failed`. These markers are hints only: fetch status
   to establish the current server state. Without a return target, success returns
   `{"data":{"status":"reauthenticated"}}`.

If a provider does not honor the freshness request or omits its signed `auth_time`,
confirmation is refused. An account chooser or consent screen alone is not fresh
authentication. Google documents optional `auth_time` and its claims request in
its [OIDC reference](https://developers.google.com/identity/openid-connect/reference).
Real Google provider acceptance remains a staging check after an authorized
release; local tests use a signed test issuer and a synthetic browser provider.

## Enforcement and frontend

Existing role grant/revoke routes require freshness in the same PostgreSQL
transaction as role authorization, mutation and audit. Stale proofs return
403 `reauthentication_required`; revoked sessions return 401 `invalid_session`.
Shared account/session locks serialize the sensitive action with password reset,
logout and refresh-replay revocation. Token, session and proof deadlines are
rechecked after waiting for these locks, so a wait cannot extend freshness. Confirmation does not bypass role or scope
checks and does not enable reserved church roles.

Existing Google linking accepts fresh password sign-in or explicit confirmation,
checks the deadline again at callback, and retains its same-email explicit-link
rules. Its stale-session response remains 401 `invalid_session` for compatibility.
Read-only role inspection, public content and ordinary own-profile edits do not
require step-up authentication.

The independent frontend displays confirmation controls on My profile, Settings,
Staff and the signed-in auth page. It shows only available methods, clears password
fields after success or failure, and stores no passwords or proof tokens in
browser storage or URLs. Expiry is displayed locally; every action still checks
the server. Wrong-password 401 responses are not automatically refreshed/retried.
Sensitive actions are not automatically resubmitted after confirmation.

Successful confirmations and freshness updates commit atomically with
`reauthentication_audit_events`; audit failure rolls back confirmation. No secrets,
provider tokens, passwords or authorization codes are stored in this audit table.
Existing login, session, role and audit data are preserved by forward migrations.

## Remaining milestone work

[Authenticator enrollment, verification and recovery](mfa.md) are now implemented
locally. Mandatory MFA authorization for future
privileged actions, first-Super-Admin bootstrap, concurrent last-admin protection,
and new church-role appointments remain unimplemented. Fresh authentication alone
must not authorize those flows. No new environment variables are required.

## Local verification evidence

Tests were introduced and executed before implementation: missing service methods,
404 routes, missing browser controls and missing OpenAPI contracts failed first.
Additional red tests reproduced stale role writes, ordinary SSO bypassing freshness,
and a deadline expiring while waiting for a session lock before those cases were
fixed. No test connects to staging or sends a real email.

Verified locally:

- `go test -race -p 1 ./... -count=1 -timeout=10m` passed across all backend
  packages. After the final lock-wait correction, the affected authentication and
  HTTP reauthentication tests passed again under the race detector, followed by
  the existing access-management and new global HTTP regression tests.
- `go vet ./...`, Go formatting, Swagger UI configuration and whitespace checks
  passed. OpenAPI 1.8.0 validated with four pre-existing warnings (license metadata,
  health-route 4xx conventions and the older password-reset example placeholder).
- Frontend: 17 unit tests and 26 mocked desktop/mobile browser scenarios passed.
- `go test -tags frontend_e2e ./internal/app -run '^TestFrontendWithRealAPI$'
  -count=1 -timeout=5m` passed on the final implementation: six desktop/mobile
  workflows against the global API, TLS loopback server and disposable PostgreSQL.
  Password and Google-only confirmation are covered alongside recovery and
  existing account/content workflows. Each scenario has a test-only synthetic IP;
  production rate limits remain unchanged.
- `docker build --tag city4christ:fresh-reauthentication-verify .` and
  `bash scripts/docker-smoke.sh city4christ:fresh-reauthentication-verify` passed,
  including forward migrations, data preservation, API protection and clean
  API/worker shutdown with a disposable container database.

These are local checks, not remote CI or deployed-provider acceptance. Backend and
frontend changes remain separate local worktrees. Nothing was pushed, merged,
deployed or applied to staging; no administrator was appointed.
