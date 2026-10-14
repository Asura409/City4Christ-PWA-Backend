# Temporary HTTPS auth preview

`cmd/auth-preview` is a test-only executable that serves the existing global API,
a responsive mobile interface at `/auth-test`, and a protected local OTP inbox.
It is separate from the production API entrypoint and Render Blueprint. Never
point it at production or staging data. It enables registration and exposes
verification codes to the authenticated preview operator.

The entire preview requires HTTP Basic authentication with username `preview`
and a random `PREVIEW_PASSWORD` of at least 24 characters. Every response is
`no-store`; CSP prevents framing and restricts scripts and requests to this origin.
Writes require an exact `Origin` match with the configured HTTPS `APP_ORIGIN`.
Bearer requests to `/api/v1/me` and Google linking use the application's session authentication; the remaining preview interface requires Basic authentication.
The preview binds only to `127.0.0.1` on `PORT` (default 10000).

Required environment:

- `DATABASE_URL`: dedicated disposable test database, never staging.
- `VERIFICATION_KEY_BASE64`: random 32-byte base64 key for this test instance.
- `PREVIEW_PASSWORD`: random preview gate credential.
- `APP_ORIGIN`: exact generated HTTPS Quick Tunnel origin, without trailing slash.
- `PORT`: loopback API port.

No email worker is started and no provider is called. Use synthetic accounts
under `example.invalid`. The mobile page uses relative URLs with same-origin
credentials. The test inbox decrypts the existing encrypted outbox using the
trusted verification adapter; successful verification removes the queued email.

Cloudflared was installed from Cloudflare's official GitHub release 2026.10.0,
ARM64 binary, with release SHA256 verified. The temporary tunnel forwards to
`http://127.0.0.1:10000`. Its URL changes when the tunnel is recreated; update
`APP_ORIGIN` and restart the preview API after any URL change.

Current runtime is in tmux session `c4c-auth-preview` with `postgres`, `tunnel`,
and `api` windows. Attach with `tmux attach -t c4c-auth-preview` (detach using
Ctrl-b, then d). The private state directory path is recorded in
`/tmp/c4c-preview-active.path`; it contains `url.txt`, mode-600 `credentials.txt`
and `preview.env`, process logs, the test-only database, binaries, and
`verification.json` with HTTPS test results. Do not commit these runtime files
or include credentials in shared logs. Processes survive terminal disconnects;
they do not automatically restart after VPS reboot. Stop the tunnel and API
with Ctrl-C in their respective tmux windows when testing is finished.

Verified through the public HTTPS origin: protected page and inbox, registration,
wrong-origin rejection, resend cooldown response, incorrect OTP rejection,
successful OTP verification, single-use replay rejection, and inbox deletion.
The source now includes login, refresh, logout and `/me` controls. Existing running
preview binaries must be rebuilt and the dedicated test database forward-migrated
before those controls are available. Google requires separate OAuth configuration;
the disposable preview does not configure a provider by default.


Source: [Cloudflare Quick Tunnels](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/),
[official downloads](https://developers.cloudflare.com/tunnel/downloads/).
