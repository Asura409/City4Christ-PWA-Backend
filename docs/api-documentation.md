# Interactive API reference

Open `/docs/` on the API origin to browse Swagger UI. `/openapi.json` is the downloadable OpenAPI 3.0.3 contract; its source is `internal/app/apidocs/openapi.json`. Both are embedded into the Go API and Docker image. The UI uses pinned, locally served Swagger UI assets; it does not call an external validator or CDN. Their version, checksum and license are in `internal/app/apidocs/VENDOR.md`.

After this change is deployed, the staging links are:

- https://city4christ-staging-api.onrender.com/docs/
- https://city4christ-staging-api.onrender.com/openapi.json

## Using Try it out

Requests execute against the API origin you opened. Use synthetic test data; these are real requests and can change accounts. Registration stays closed unless explicitly enabled. OTP emails require a running delivery worker; Google endpoints require OAuth credentials.

1. Register, then verify the OTP through the verification endpoint. Neither operation establishes a session.
2. Log in. The response contains an access token; the browser automatically stores the Secure, HttpOnly refresh cookie.
3. Select Authorize and enter only the access token in **bearerAuth**, without the Bearer prefix. Try GET `/api/v1/me`.
4. Try refresh; the browser sends and replaces the refresh cookie. Copy the new access token into Authorize when needed. Serialize refresh requests: replaying an old token revokes the session.
5. Try logout; it revokes the refresh-cookie session and clears its cookie. Swagger's in-memory access token remains in its Authorize box until cleared, but the server rejects the revoked session.

Swagger UI sends credentials automatically and does not persist authorization across reloads. HttpOnly cookies cannot be read or manually set through Swagger's cookie security input; leave those inputs empty. Browser Cookie headers, Origin and CORS restrictions still apply. Use the same-origin HTTPS docs for cookie-based testing. HTTP localhost cannot reliably exercise these Secure cookies.

Google start/link returns a provider URL; navigate the browser to it to complete the flow. Callback returns JSON and sets the refresh cookie, without redirecting to the frontend. Returning to the docs and refreshing can issue an access token for the resulting session. Read [auth flow details](auth-sessions.md) for linking rules.

For the actual frontend, use `credentials: 'include'` and keep access tokens in memory. Send `Authorization: Bearer <accessToken>` for protected calls. Only explicitly configured frontend origins are allowed.

## Maintaining the contract

Update the specification when changing request fields, responses, routes or security behavior. Route tests exercise docs through the global app; OpenAPI validation can be run with Node 22.12 or newer:

```sh
npx --yes @redocly/cli@2.63.0 lint internal/app/apidocs/openapi.json --extends minimal
```

This contract documents the implemented auth/health API. It does not describe future PRD features or imply that live email/Google provider acceptance has passed.
