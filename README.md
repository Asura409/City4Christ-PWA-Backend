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

Implemented: Argon2id hashing, account persistence, forward migrations, transactional registration, verification OTP/resend routes, encrypted email outbox, and global app health routes. Public signup defaults to disabled. Registration currently creates an unverified account without establishing a member session; OTP verification is implemented; the Resend adapter and opt-in delivery worker are implemented; live provider verification, sessions, and the rest of authentication are still pending. API and worker executables, a Dockerfile, and a staging Render Blueprint exist. Docker build and container smoke checks passed locally on ARM64; GitHub Actions and live Render deployment remain unverified.

See [auth foundation progress](docs/auth-foundation.md) for TDD evidence, remaining work and product decisions. GitHub Actions runs the same tests with disposable PostgreSQL; its remote execution has not been verified yet.

OTP defaults are eight digits, a ten-minute lifetime, five verification attempts per issued code, and a sixty-second resend cooldown. The app requires a securely generated, persistent 32-byte `VerificationKey` to enable auth operations. Missing or invalid keys return HTTP 503 for auth operations (closed registration still returns 403). There is no generated fallback key; do not regenerate it on application restarts. Only test fixtures use a fixed sample key.

See [OTP API and delivery notes](docs/email-verification.md) for endpoint behavior and pending delivery work.

See [Resend onboarding](docs/resend.md) for domain verification, secret configuration, staging recipient restrictions, and the worker command. Live sending defaults to disabled.

See [Render staging setup and verification](docs/render-staging.md) for deployment prerequisites and disposable Docker checks.
