# City4Christ-PWA-Backend

Go and PostgreSQL backend for the City4Christ PWA. Implementation proceeds in small slices using TDD.

Planning: [church roles, operational workflows and privacy requirements](docs/requirements/church-operations-addendum.md), and the [start-to-finish implementation checklist with milestones](docs/implementation-checklist.md). The checklist distinguishes the implemented foundation from future church-wide features.

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

Implemented: registration, Argon2id passwords, email verification and resend, encrypted Resend outbox, password login, ten-minute access JWTs, thirty-day rotating refresh cookies with replay revocation, account/IP limits, logout, `/api/v1/me`, Google OIDC with explicit account linking, and [email password recovery](docs/password-recovery.md) with expiring single-use links and session revocation. Public signup defaults to disabled. Google-only accounts have no local password.

Interactive API reference: open `/docs/` on your API origin. Download the OpenAPI 3.0.3 contract at `/openapi.json`; [source specification](internal/app/apidocs/openapi.json) and [frontend usage guide](docs/api-documentation.md) are included.

See [auth API and security configuration](docs/auth-sessions.md), [TDD evidence](docs/auth-foundation.md), and [temporary mobile preview](docs/temporary-preview.md). Tests use disposable PostgreSQL. Docker and CI configuration are included; this update does not deploy or modify staging.

OTP defaults are eight digits, a ten-minute lifetime, five verification attempts per issued code, and a sixty-second resend cooldown. The app requires a securely generated, persistent 32-byte `VerificationKey` to enable auth operations. Missing or invalid keys return HTTP 503 for auth operations (closed registration still returns 403). There is no generated fallback key; do not regenerate it on application restarts. Only test fixtures use a fixed sample key.

See [OTP API and delivery notes](docs/email-verification.md) for endpoint behavior and pending delivery work.

See [Resend onboarding](docs/resend.md) for domain verification, secret configuration, staging recipient restrictions, and the worker command. Live sending defaults to disabled.

See [Render staging setup and verification](docs/render-staging.md) for deployment prerequisites and disposable Docker checks.

For automatic, CI-gated deployment to the Hostinger VPS, see
[Hostinger VPS deployment](docs/hostinger-vps-deployment.md). The workflow keeps
the current email worker separate and uses the VPS Docker logs for API request
diagnostics.

The first [sermon catalog slice](docs/sermon-catalog.md) adds public published-only metadata listing and detail, filters, and cursor pagination. The [publishing slice](docs/sermon-publishing.md) adds role-checked draft/publish controls, atomic audit records, version checks and canonical YouTube links. [Member sermon tools](docs/sermon-member-tools.md) cover private notes and uploaded-media playback progress. No content or privileged account is seeded automatically.

See [access management](docs/access-management.md) for scoped permissions, operator-provisioned access administrators and audited role delegation.

See [member profiles and privacy](docs/member-profiles.md) for own-profile edits and directory visibility, and [request validation](docs/api-validation.md) for field-specific errors and improved Swagger examples.

The [managed Daily Verse](docs/daily-verse.md) replaces Home’s static verse with an editor-published selection for the fixed GMT+1 church day.

The [devotional publishing foundation](docs/devotional-publishing.md) adds editorial drafts, immediate publication, withdrawal and a published reader view.

The [pastoral booking foundation](docs/pastoral-bookings.md) adds explicit pastor profiles, availability, approval-based requests and private member/pastor queues. No real pastor is appointed automatically. [Appointment email delivery](docs/pastoral-email-delivery.md) adds transactional updates and scheduled reminders through the existing worker.

See [the member directory](docs/member-directory.md) for authenticated name search, opt-in visibility, cursor pagination and response privacy.

See [frontend browser integration](docs/frontend-browser-integration.md) for Google browser completion and optional Playwright checks through the real app with disposable PostgreSQL. The frontend checkout is an independent repository.

See [church community posts](docs/posts.md) for the authenticated church-wide text feed and its current API contract and scope.

See [church permissions and current-access snapshots](docs/church-permissions.md) for role-derived navigation, exact ministry scopes and the protected church-role rollout plan.

See [church events](docs/events.md), [prayer wall](docs/prayer-wall.md), and [direct messaging](docs/messaging.md) for the connected API contracts and current scope.

See [in-app notifications](docs/notifications.md) for the private inbox API and current delivery scope.

Bible lookup and Daily Bread integration: [API and editorial workflow](docs/bible-lookup.md).

Group workflow completion: [group content](docs/group-content.md). Notification preferences, broadcasts and optional push: [delivery and worker rollout](docs/notification-delivery.md).
