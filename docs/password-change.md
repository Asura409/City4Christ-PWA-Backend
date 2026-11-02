# Signed-in password change

A verified account with a local password can change it from the signed-in security
settings by proving its current password. This is separate from the emailed
[forgot-password recovery flow](password-recovery.md).

Google-only accounts have no local password and keep using Google. This operation
does not create a password for them. An account with both Google and a local
password must still supply its current local password, including when it signed
in through Google.

## API

`POST /api/v1/auth/password/change` requires a verified Bearer access token, an
allowed `Origin` and `Content-Type: application/json`. Browser requests must use
`credentials: 'include'` so the response can clear the refresh cookie. Passwords
and access tokens must remain private and must never appear in URLs or logs.

```json
{
  "currentPassword": "your current private password",
  "newPassword": "a different new private password"
}
```

The new password must be valid UTF-8, contain at least 15 Unicode characters,
occupy at most 1024 UTF-8 bytes, and differ from the current password. Passwords
are preserved exactly, including spaces. The body accepts only these two fields. Malformed UTF-8 and unpaired Unicode
surrogate escapes are rejected rather than replaced with another character.
The server enforces five attempts per account and per client IP per 15 minutes,
in addition to the existing shared authentication IP limits.

Success returns `200` with `{"data":{"status":"password_changed"}}` and clears
the browser-managed Secure, HttpOnly refresh cookie. One transaction replaces
the Argon2id hash, invalidates outstanding reset links, revokes **all** account
sessions including the caller, and queues an encrypted password-change email
notice. It returns no access token or replacement session. The frontend must clear
its in-memory credentials and require a new sign-in on every successful change.
Other existing tabs and devices must sign in again when their next request is
rejected. An email delivery failure does not undo a committed password change.

| Status | Meaning |
| --- | --- |
| 400 | Malformed JSON or unknown fields |
| 401 | Missing, expired, revoked or otherwise invalid session |
| 403 | Disallowed browser origin |
| 409 `local_password_required` | The account has no local password |
| 422 `current_password_invalid` | Current local password is incorrect |
| 422 `validation_failed` | Replacement password is invalid or unchanged; check `fields.newPassword` |
| 429 | Account or IP attempt limit reached |
| 503 | Required dependency unavailable |

All responses are `Cache-Control: no-store`. Standard request-size and JSON
content-type errors still apply. The endpoint is documented in `/openapi.json`
and `/docs/`.

## Delivery and rollout

This slice reuses the existing encrypted recovery outbox and password-change
template. It needs no new secret, environment variable, database reset or provider.
The existing recovery-capable email worker must be running to deliver notices;
provider acceptance alone does not establish inbox receipt. Deploy and test only
within the owner's authorization. Local tests use disposable PostgreSQL and fake
delivery, with no messages sent to real recipients.

Automated verification and actual staging/provider acceptance are separate. This
document describes the contract and does not claim a deployment or live password
change has occurred.
