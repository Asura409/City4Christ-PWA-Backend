# Free Render preview

`render.yaml` creates one Free Docker web service and one Free PostgreSQL database, from GitHub branch `main`. It creates no paid worker, disk, or pre-deploy hook. The API automatically deploys commits on `main` after GitHub CI checks pass. This is temporary preview hosting, not persistent staging.

Render Free PostgreSQL has 1 GB storage, expires after 30 days, and has no backups. After expiration it becomes inaccessible; Render deletes it after the subsequent 14-day upgrade grace period. Export test data before expiration. Never automatically recreate/reset the database. Only one free PostgreSQL database is allowed per workspace. The free API sleeps after 15 minutes idle and has monthly usage limits.

The email worker is omitted because Render has no free worker plan. The API still queues encrypted verification emails, but this Blueprint does not deliver them. Keep registration disabled until a worker is deliberately hosted elsewhere (for example on the existing VPS with secure database connectivity), or a separate free-compatible delivery design is implemented. The protected inbox/mobile interface remains available only in the dedicated test-preview executable.

## Creation

1. Commit and push the reviewed changes to GitHub `main` after tests/CI pass. Local changes are not available to Render until pushed.
2. Render Dashboard: New > Blueprint; choose the repository, branch `main`, and `render.yaml`.
3. Review the proposed resources: both must show Free; there must be no worker. If the previous attempt created resources, inspect them first. Do not delete or replace an existing database to retry this setup.
4. Apply this configuration with Blueprint Manual Sync if Auto Sync is off, or set the API Settings > Auto-Deploy to After CI Checks Pass. Verify `/health/ready` after deployment. Blueprint Auto Sync controls configuration updates separately from service code deployments.
5. APP_ORIGIN is taken from the API's Render URL. The GitHub Pages and `https://city4christ-pwa.vercel.app` frontend origins are allowed; refresh cookies use SameSite=None and Secure. See [auth configuration](auth-sessions.md).
6. The database allowlist retains the approved VPS worker address `51.170.141.205/32`. Keep it in the Blueprint so synchronization does not remove delivery access; do not add other addresses or use a global range without approval.
7. Set `REGISTRATION_ENABLED` deliberately during initial Blueprint creation: use `false` until delivery is ready. It uses `sync: false`, so later Blueprint updates preserve the operator's current setting. Existing staging signup remains enabled; this release must not close it. Render's [Blueprint specification](https://render.com/docs/blueprint-spec) documents that `sync: false` entries are ignored when updating an existing Blueprint.

The Docker command `/app/api migrate-and-serve` applies forward migrations and starts the API only after they succeed. Migration reruns preserve data. No reset or seed command exists. Normal `/app/api` startup still does not migrate; `/app/api migrate` applies migrations and exits.

The generated verification key is persistent configuration: preserve it across redeployments. Google remains disabled until both OAuth credentials are configured privately. No Resend key is required by this free Blueprint.

## Local verification

```sh
go test -race ./cmd/api -count=1
docker build --tag city4christ:verify .
bash scripts/docker-smoke.sh city4christ:verify
```

Tests use disposable PostgreSQL, including migration reruns with existing accounts. The smoke script creates and cleans up only its own disposable containers, network and database volume. It checks migration-and-serve, readiness, disabled registration, nonroot execution, and graceful shutdown. No staging credentials are consumed.

References: [Free limits](https://render.com/docs/free), [deploy hooks](https://render.com/docs/deploys), [Blueprint specification](https://render.com/docs/blueprint-spec).
