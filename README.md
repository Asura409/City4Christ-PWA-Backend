# City4Christ-PWA-Backend

Go and PostgreSQL backend for the City4Christ PWA. Implementation proceeds in small slices using TDD.

Requires Go 1.27 or newer and PostgreSQL server binaries. On Ubuntu:

```sh
sudo apt-get install postgresql
```

Run as a non-root user:

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
```

Tests start their own PostgreSQL cluster and database in a private temporary directory, with TCP disabled, and remove them afterward. They never read `DATABASE_URL` or connect to staging. Set `TEST_POSTGRES_BIN` to the directory containing `initdb` and `pg_ctl` if those binaries are outside PATH or Ubuntu's usual installation paths. Missing binaries fail tests rather than skipping integration coverage.

Implemented: registration, Argon2id passwords, email verification and resend, encrypted Resend outbox, password login, ten-minute access JWTs, thirty-day rotating refresh cookies with replay revocation, account/IP limits, logout, `/api/v1/me`, and Google OIDC with explicit account linking. Public signup defaults to disabled. Google-only accounts have no local password.

Interactive API reference: open `/docs/` on your API origin. Download the OpenAPI 3.0.3 contract at `/openapi.json`; [source specification](internal/app/apidocs/openapi.json) and [frontend usage guide](docs/api-documentation.md) are included.

See [auth API and security configuration](docs/auth-sessions.md), [TDD evidence](docs/auth-foundation.md), and [temporary mobile preview](docs/temporary-preview.md). Tests use disposable PostgreSQL. Docker and CI configuration are included; this update does not deploy or modify staging.

OTP defaults are eight digits, a ten-minute lifetime, five verification attempts per issued code, and a sixty-second resend cooldown. The app requires a securely generated, persistent 32-byte `VerificationKey` to enable auth operations. Missing or invalid keys return HTTP 503 for auth operations (closed registration still returns 403). There is no generated fallback key; do not regenerate it on application restarts. Only test fixtures use a fixed sample key.

See [OTP API and delivery notes](docs/email-verification.md) for endpoint behavior and pending delivery work.

See [Resend onboarding](docs/resend.md) for domain verification, secret configuration, staging recipient restrictions, and the worker command. Live sending defaults to disabled.

See [Render staging setup and verification](docs/render-staging.md) for deployment prerequisites and disposable Docker checks.

The first [sermon catalog slice](docs/sermon-catalog.md) adds public published-only metadata listing and detail, filters, and cursor pagination. The [publishing slice](docs/sermon-publishing.md) adds role-checked draft/publish controls, atomic audit records, version checks and canonical YouTube links. No content or privileged account is seeded automatically.
