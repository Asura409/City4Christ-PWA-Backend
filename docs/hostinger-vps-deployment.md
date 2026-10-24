# Hostinger VPS API deployment

`.github/workflows/deploy.yml` deploys only after the `Test` workflow succeeds on
`main`. CI exports the exact Docker image it built and smoke-tested. The deploy
workflow downloads that run's artifact, verifies its SHA-256, and sends it to the
VPS over SSH. The VPS loads the image, applies forward migrations, replaces only
the API container, and checks `/health/ready`. It does not compile Go on the VPS,
deploy or stop the existing email worker, reset PostgreSQL, or send database
credentials through GitHub Actions. The SSH key is restricted to a fixed receiver
command; it cannot open a shell, forward ports, or use agent/X11 forwarding.

## GitHub Actions settings

In the backend repository, open **Settings → Environments**, create an environment
named `hostinger-vps`, and restrict deployments to the `main` branch. Do not add a
required reviewer if unattended deployment after CI is desired. Then open that
environment's **Environment secrets** and **Environment variables** settings.
Add these variables:

| Variable | Value |
| --- | --- |
| `VPS_DEPLOY_ENABLED` | Set to `true` only after VPS preparation is complete; leave unset to keep VPS deployment disabled |
| `VPS_HOST` | Hostinger VPS public hostname or IPv4 address |
| `VPS_USER` | Dedicated SSH deployment username, such as `c4cdeploy` |

Add these environment secrets:

| Secret | Value |
| --- | --- |
| `VPS_SSH_PRIVATE_KEY` | Private SSH key for the deployment user; never commit or paste it into chat |
| `VPS_SSH_KNOWN_HOSTS` | Verified SSH host-key line(s) for the VPS |

Create a dedicated SSH key pair for deployments. Install only its public key in
the deployment user's `~/.ssh/authorized_keys` with the forced-command restrictions
below. Obtain the host public key with `ssh-keyscan` if useful, then verify its
fingerprint against the Hostinger VPS console before saving its known-hosts line.
The workflow requires strict host-key checking and does not trust keys discovered
during a deployment.

Generate this key on a trusted computer, not inside a GitHub Actions run, and do
not reuse the administrator's login key. For example:

```sh
ssh-keygen -t ed25519 -f city4christ-actions -C city4christ-actions -N ''
```

Put `city4christ-actions.pub` on the deployment user's forced-command key line;
put the private `city4christ-actions` file in the `VPS_SSH_PRIVATE_KEY` environment
secret.

To prepare `VPS_SSH_KNOWN_HOSTS`, you can collect the public host key, then verify
its fingerprint using the Hostinger console before storing the line:

```sh
ssh-keyscan -H -t ed25519 "$VPS_HOST" > city4christ-known-hosts
ssh-keygen -lf city4christ-known-hosts
```

## One-time VPS preparation

Use the Hostinger console or an existing administrator SSH session to:

1. Install Docker Engine in [rootless mode](https://docs.docker.com/engine/security/rootless/)
   for the deployment user. Install `curl`, `tar`, Bash, `uidmap` and the
   rootless networking prerequisites. Enable the user Docker service at boot with
   `systemctl --user enable --now docker` and `sudo loginctl enable-linger c4cdeploy`.
   Do not add this user to the `docker` group and do not grant it `sudo`.
2. Create a dedicated deployment user with SSH public-key login. Install the
   repository's `scripts/deploy-vps.sh` once as a root-owned receiver:

   ```sh
   sudo install -o root -g root -m 0755 \
     ~/City4Christ-PWA-Backend/scripts/deploy-vps.sh \
     /usr/local/sbin/c4c-deploy-receiver
   ```

   Adjust the source path if the checkout is elsewhere. This receiver must remain
   owned by root so a deployment cannot replace its own restrictions.
3. Add the deployment public key as a single line in the user's
   `~/.ssh/authorized_keys`, prefixed as follows (replace the key text):

   ```text
   restrict,command="/usr/local/sbin/c4c-deploy-receiver" ssh-ed25519 AAAA... github-actions-city4christ
   ```

   Ensure no other key is installed for this deployment-only account. The forced
   command accepts only `deploy <40-character-commit-sha>` and reads the image
   archive from SSH standard input. Use a separate administrator account for
   maintenance and reading logs.
4. Create `~/.config/city4christ/api.env` for the deployment user, owned by that
   user and mode `0600`. The deployment user and rootless Docker daemon need to
   read it; it is not accessible to other users.
5. Configure DNS and an HTTPS reverse proxy for the API domain. Proxy it to
   `http://127.0.0.1:3000`. The API container binds only to loopback; do not expose
   port 3000 directly to the internet. Open only SSH and the proxy's HTTPS ports
   in the VPS firewall.

Before configuring CI, check the server architecture with `uname -m`. The current
image built and smoke-tested by GitHub Actions targets `x86_64`.

## API runtime environment

Put one `KEY=value` per line in `~/.config/city4christ/api.env`. Do not wrap
values in `export` statements. Keep the file on the VPS; these are not GitHub
Actions secrets.

```dotenv
APP_ENV=production
PORT=10000
DATABASE_URL=<PostgreSQL connection URL with TLS verification>
VERIFICATION_KEY_BASE64=<the existing stable 32-byte base64 key>
APP_ORIGIN=https://api.example.org
CORS_ORIGINS=https://city4christ-pwa.vercel.app,https://zeetechdot.github.io
REFRESH_COOKIE_SAME_SITE=none
REGISTRATION_ENABLED=true
TRUSTED_PROXY_CIDRS=
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
```

Replace `api.example.org` with the real API HTTPS origin and list the exact
frontend origins, without paths or trailing slashes. Set `REGISTRATION_ENABLED`
to the intended signup policy. Set both Google values only if Google sign-in is
enabled; the Google OAuth redirect URI must then be
`https://api.example.org/api/v1/auth/google/callback`.

Keep `VERIFICATION_KEY_BASE64` exactly the same as the existing email worker's
key. Do not generate a new value when moving the API: this key protects auth
signing and encrypted pending mail. The API uses the same PostgreSQL database as
the worker. If PostgreSQL remains on Render, confirm the database permits the
Hostinger server's current outbound IP; the existing `51.170.141.205/32` entry
works only if that is still this VPS's egress address. Preserve TLS verification
in `DATABASE_URL`.

Because the API runs behind rootless Docker's port forwarder, verify the source
address the container sees before setting `TRUSTED_PROXY_CIDRS`. An incorrect
value affects IP-based rate limits; never use `0.0.0.0/0`. If left empty, all
clients share the proxy's IP rate-limit bucket until the trusted proxy address is
configured.

The email delivery variables (`RESEND_API_KEY`, `EMAIL_FROM`, and
`EMAIL_RECIPIENT_ALLOWLIST`) belong to the existing email worker. This workflow
does not change its process or environment. If the API is pointed to a different
database or verification key from that worker, OTP and recovery mail will not
work correctly.

## Logs and checks

The API emits one structured JSON log for each non-health HTTP request, with
request ID, method, path, status and duration. It does not log query strings,
headers, request bodies, passwords, tokens, or email addresses. On the VPS:

```sh
sudo -u c4cdeploy env \
  XDG_RUNTIME_DIR=/run/user/$(id -u c4cdeploy) \
  DOCKER_HOST=unix:///run/user/$(id -u c4cdeploy)/docker.sock \
  docker logs --since 15m city4christ-api
sudo -u c4cdeploy env \
  XDG_RUNTIME_DIR=/run/user/$(id -u c4cdeploy) \
  DOCKER_HOST=unix:///run/user/$(id -u c4cdeploy)/docker.sock \
  docker logs --since 15m --follow city4christ-api
curl -fsS https://api.example.org/health/ready
```

For a failed login, search the request ID returned in the API error. Docker keeps
the active container logs in bounded 10 MB files (five files). Logs are local to
the VPS; configure a separate log shipper only if centralized retention is
needed.

CI currently builds the image on an x86_64 Linux runner. The receiver checks that
the VPS reports `x86_64` before replacing the API. If `uname -m` reports
`aarch64`, the deployment stops safely and the CI build must be configured for
ARM64 before deploying there.

Set the frontend hosting `API_URL` to the new API HTTPS origin and rebuild the
frontend after DNS/TLS and API readiness are confirmed. If the frontend stays on
`vercel.app`, some browsers may block the cross-site refresh cookie despite
`SameSite=None`; using a frontend custom domain on the same site as the API gives
more reliable persistent sessions. A live migration from Render should be
scheduled only after the VPS API and shared database are verified; this workflow
does not change the Render service or its database.
