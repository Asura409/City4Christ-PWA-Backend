# Auth API

All routes use `/api/v1`. JSON successes use `data`; errors use `error` with a request ID. Auth writes require an allowed `Origin`. Use `credentials: 'include'` in browser requests; keep the access token in memory and send `Authorization: Bearer <token>` to `/me` and Google linking.

| Method | Route | Request |
| --- | --- | --- |
| POST | `/auth/register` | firstName, lastName, email, password |
| POST | `/auth/verification/verify` | email, code |
| POST | `/auth/verification/resend` | email |
| POST | `/auth/login` | email, password |
| POST | `/auth/refresh` | Refresh cookie; no token in JSON |
| POST | `/auth/logout` | Refresh cookie |
| GET | `/me` | Authorization header |
| GET | `/auth/google/start` | Returns provider URL and browser-binding cookie |
| POST | `/auth/google/link` | Authorization header; recent login required |
| GET | `/auth/google/callback` | Provider code/state and binding cookie |

Login and Google callback return `accessToken`, `tokenType`, and `expiresIn`. Access tokens last ten minutes. Refresh tokens last thirty days from initial login, rotate on every use, and appear only in a Secure, HttpOnly, host-only cookie. Reusing a consumed refresh token revokes the session, including its access tokens. Serialize refresh requests in the frontend. Logout revokes the cookie's session and clears the cookie.

Google uses maintained coreos OIDC and oauth2 libraries with signed ID-token validation, issuer/audience/expiry/nonce checks and PKCE S256. An existing email is never automatically linked. Sign in to that account and explicitly link within five minutes of login; the Google email must match. Google accounts created through signup have no local password. Callback returns JSON rather than putting tokens in a redirect URL; frontend integration must handle completion and then refresh through the cookie.

## Configuration

Set `APP_ORIGIN` to the exact HTTPS backend origin. Set `CORS_ORIGINS` to additional exact frontend origins, without paths. `REFRESH_COOKIE_SAME_SITE=lax` is the default; use `none` for a separate frontend origin. Browser third-party cookie restrictions can still prevent cross-site sessions; a shared site/domain avoids that restriction. Blueprint staging allows `https://zeetechdot.github.io` and explicitly uses `none`.

Preserve `VERIFICATION_KEY_BASE64`: JWT signing uses a separately derived key. Rotating this master secret invalidates existing access tokens and encrypted pending mail. Never commit secrets. `REGISTRATION_ENABLED=false` closes both password and new Google signup while allowing existing users to log in.

To enable Google, privately configure both `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`. Register the exact Google web-client redirect URI `APP_ORIGIN/api/v1/auth/google/callback`. Discovery failure prevents configured Google startup rather than weakening validation. Without Google credentials its routes return 503.

Rate limits use atomic PostgreSQL counters: auth IP 60/minute; registration and resend account 5/15 minutes; login 10/15 minutes; verification 20/15 minutes. Limits return 429 and dependency failures return 503. Account keys are HMAC digests. `TRUSTED_PROXY_CIDRS` must contain only real ingress proxies; forwarded IP headers are ignored by default. Configure this for your hosting ingress so clients do not share a proxy's IP bucket.

Migrate with `/app/api migrate` before starting the updated API, or use `/app/api migrate-and-serve` for the free Blueprint. Migrations only move forward and do not reset or seed staging. Tests create their own disposable PostgreSQL databases. The protected OTP inbox is available only through `cmd/auth-preview`, never the production API.

Email-based password recovery is described in [password recovery](password-recovery.md). It uses public forgot/reset endpoints, verified local-password accounts, single-use links and complete session revocation. Google-only accounts keep using Google.
