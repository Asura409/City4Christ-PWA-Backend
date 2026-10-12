# Render staging foundation

This is the registration/verification foundation, not completed authentication.
Login, tokens, Google OIDC, account/IP limits, the mobile auth page, and protected
staging inbox remain pending. Keep registration disabled until those prerequisites
and the signup policy are resolved.

## Verification

Run `go test -race ./... -count=1 -timeout=5m` with PostgreSQL binaries installed.
Startup tests use a private disposable cluster and verify that forward migrations
preserve an existing account, the global HTTP app serves health routes, and shutdown
works. No test consumes a staging DATABASE_URL.

On a host with Docker:

```sh
docker build --tag city4christ:verify .
bash scripts/docker-smoke.sh city4christ:verify
```

The smoke check creates uniquely named disposable containers/network, tests forward
migrations twice with account preservation, readiness, closed registration, the
nonroot API user, disabled worker persistence, and graceful shutdown. It removes only
its own containers, anonymous database volume, and network. GitHub Actions runs these
commands after Go tests. CI has not run until changes are pushed.

Docker build and the smoke checks passed locally on ARM64 with Docker 29.1.3
and Buildx 0.30.1 on 2026-10-12. The image was built with the combined CA secret
shown below. Smoke checks used PostgreSQL 16 in a disposable container and preserved
an account across repeated forward migrations. The readiness check explicitly uses
TCP so PostgreSQL's temporary initialization server cannot cause an early start.
GitHub Actions on AMD64, live Render deployment, and live Resend sending remain
unverified.

Managed cloud builds can supply the public combined CA bundle without adding it
to image layers:

```sh
docker build --secret id=proxy_ca,src=/etc/ssl/certs/ca-certificates.crt --tag city4christ:verify .
```

## Blueprint creation (manual, after verification and push)

1. Review `render.yaml`: paid API/worker plans, paid persistent PostgreSQL with 15GB
   storage, and Oregon region. Review Render's displayed prices before creating them.
2. Commit and push the reviewed files to GitHub. Nothing in this setup pushes or deploys.
3. Render Dashboard: **New > Blueprint**, select the repository and reviewed branch,
   and use `render.yaml` as the Blueprint path.
4. Enter `RESEND_API_KEY` privately in Render, and a comma-separated
   `EMAIL_RECIPIENT_ALLOWLIST` containing only authorized staging recipients.
   Do not put the key in Git or chat.
5. Review the resources and click **Deploy Blueprint** only when ready to deploy.
6. Set Blueprint **Auto Sync** to **No**. Service auto-deploy triggers are already off.
7. Check the API `/health/ready` endpoint and service logs. Email delivery initially
   remains disabled; no test email is sent automatically.

The API's pre-deploy command is `/app/api migrate`: forward pending migrations only,
with no reset or seed. Normal API and worker startup never runs migrations.
PostgreSQL external access is blocked; services use its internal connection string.
The Blueprint generates one stable 32-byte base64 verification key on the API and
references that same key from the worker. Preserve it across restarts/redeploys;
changing it invalidates existing encrypted mail and verification challenges.

The sender is `City4christ <info@city4christ.org>`. Enabling live delivery later requires
`EMAIL_DELIVERY_MODE=resend` on the worker and a valid recipient allowlist. Keep the
Blueprint's value aligned with any reviewed configuration change before syncing.
No staging reset job, destructive migration command, or automatic seed is configured.

References: [Render Blueprint specification](https://render.com/docs/blueprint-spec),
[Blueprint creation](https://render.com/docs/infrastructure-as-code),
[Docker services](https://render.com/docs/docker).
