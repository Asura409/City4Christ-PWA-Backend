# Media storage setup

Cloudflare R2 is the selected object store for post and sermon attachments. The
backend has a tested R2 adapter, PostgreSQL upload/scan-job repository,
authenticated upload reservation/completion/status routes, post and sermon
attachment associations, short-lived signed read URLs, and a standalone
media scanning worker. Completion checks R2's reported size and content type;
the worker independently checks object metadata and actual file bytes, scans
with ClamAV, and promotes clean bytes to a unique immutable key. Download
routes and attachments expose only short-lived signed reads after a clean scan.
The bucket remains private; do not give it a public read policy.

The worker uses the leased PostgreSQL queue, downloads to a mode-0600 temporary
file, checks magic-byte MIME against the allowed type, and invokes the existing
`ClamAVScanner` (`clamdscan --stream`) with a bounded context. A clean file is
uploaded under a unique `available/<upload-id>/<random-id>.<extension>` key;
only then does a lease-checked transaction make that key available. The original
presigned key is kept separate; after finalization, deletion is attempted. If a
lease has expired, its worker cannot publish the object and removes its own
promotion. MIME mismatches and malware are rejected and deletion is attempted;
storage failures keep rejected source objects private and unavailable.
Storage/scanner failures remain unavailable and retry with bounded attempts.
Keep `clamd` on a Unix socket
or loopback only; never open its unauthenticated TCP port to the Internet. The
official ClamAV docs describe `clamdscan` as the standard client and explain that
TCP `clamd` is not encrypted or authenticated:
[ClamAV scanning](https://docs.clamav.net/manual/Usage/Scanning.html) and
[ClamD protocol](https://docs.clamav.net/manual/Usage/ClamdProtocol.html).

## Create the R2 resources

1. In the Cloudflare dashboard, open **R2 Object Storage** and create a bucket,
   for example `city4christ-media-staging`. Keep it private and in the account's
   default location.
2. Create an R2 API token scoped only to this bucket, with **Object Read & Write**.
   Do not use the account-wide Global API Key. Store the Access Key ID and Secret
   Access Key in a password manager; the secret is only shown at creation.
3. Record the Cloudflare Account ID from the R2 overview. The S3 endpoint is
   `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`; the Go signer uses region `auto`.
4. Configure bucket CORS for the exact deployed frontend origins, `PUT` and
   `GET` methods, and the `Content-Type` request header. Avoid wildcard origins.
   Set frontend build variable `MEDIA_ORIGIN` to
   `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`; the React build adds only this
   R2 origin to `connect-src`, `img-src`, and `media-src`.

The API reads `R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, and
`R2_SECRET_ACCESS_KEY`. Configure all four or leave all four unset. The worker
reads the same account and bucket names plus `MEDIA_R2_ACCESS_KEY_ID` and
`MEDIA_R2_SECRET_ACCESS_KEY`; its distinct token must have bucket-scoped Object
Read & Write permission because it reads uploads, writes clean immutable objects,
and deletes temporary/rejected objects. Never put either token in Git, frontend
variables, logs, or chat. Keep API R2 settings unset until the worker is deployed
and verified; otherwise uploads queue and remain unavailable.

## VPS worker setup

The Go worker is a static binary in the API image, but runs as a separate
systemd user service on the VPS so `clamdscan` can reach the host ClamAV Unix
socket. It does not run schema migrations or reset data. The currently installed
SSH forced-command deploy script must be updated from the reviewed
`scripts/deploy-vps.sh` before automated worker activation; preserve its forced
command and root-owned permissions.

1. Install and start `clamav-daemon` and `clamav-freshclam` on the VPS. Confirm
   `/run/clamav/clamd.ctl` is active, `clamdscan` works locally, and the daemon
   uses a Unix socket only. Keep `TCPSocket` disabled; do not expose port 3310.
2. Add the VPS deployment user to the `clamav` group, enable a lingering user
   service manager with `sudo loginctl enable-linger <deploy-user>`, then start a
   fresh login and confirm `id -nG` includes `clamav`. Plan this group refresh
   around the existing rootless Docker service.
3. Create `$HOME/.config/city4christ/media-worker.env` from
   `.env.media-worker.example` with mode `0600`. Use the private database URL
   and a distinct R2 Object Read & Write token scoped only to the media bucket.
   Keep `MEDIA_SCAN_ENABLED=false` until ClamAV and the database are verified;
   then explicitly set it to `true`. Never share this token with the API. Set it
   back to `false` and deploy to stop an already running worker.
4. On a later normal VPS deployment, the updated forced-command deploy script
   installs the worker binary and user service only when that private env file
   explicitly enables scanning. Check it with
   `systemctl --user status city4christ-media-worker` and
   `journalctl --user -u city4christ-media-worker -n 50`. Logs contain only a
   generic processing error code. Configure the API's R2 settings only after a
   test upload has scanned and reported `available`.

The worker processes one job at a time, uses a five-minute database lease and a
two-minute ClamAV timeout, and retries temporary failures up to five times. Its
mode-0600 temporary files are limited by the validated 100 MiB upload maximum
and removed after processing. The API and worker must use the same PostgreSQL
database and R2 bucket.

## Current signing limits

The adapter signs private-object `PUT` requests for at most five minutes and signed
`GET` URLs for at most one hour (content attachments use 30 minutes).
Upload signatures bind the declared `Content-Type` and `Content-Length`, and object
keys are restricted to safe relative paths. The API accepts JPEG, PNG, WebP, GIF,
MP3, M4A, WAV, OGG, MP4, and WebM with maximum sizes of 20 MiB for images, 50 MiB
for audio, and 100 MiB for video. It reserves owner-scoped keys, requires the
allowed Origin on writes, limits requests by account and IP, checks R2 object
metadata on completion, and exposes status only to the owner. Posts accept up to
four owned, clean uploads; sermons accept up to ten and keep them private until
publication. Attachment responses include media ID, verified MIME type, byte
size, signed URL, and URL expiry, never a raw object-key field. The React post
composer uploads images, audio, and video directly to R2, waits for the malware
scan, and attaches only cleared upload IDs. Per-account storage quotas remain
future work. Public media requires a separate explicit policy and
must not be exposed by changing the bucket to public.

## API contract

- `POST /api/v1/uploads/presign` reserves an upload and returns a five-minute R2
  `PUT` URL and required headers.
- `POST /api/v1/uploads/{uploadId}/complete` verifies R2 metadata and queues one
  durable scan job. Send `{}` as the JSON request body.
- `GET /api/v1/uploads/{uploadId}` returns status only to the reservation owner.
- `POST /api/v1/posts` accepts optional `attachmentIds` after their uploads reach
  `available`; `GET /api/v1/posts` returns short-lived signed URLs.
- Sermon draft creation and update accept `attachmentIds`. Public sermon list and
  detail return signed media only after publication.

The API reference is served at `/docs/` and `/openapi.json`. Direct browser uploads
need a bucket CORS rule for the exact frontend origins, `PUT`, and the signed
headers; do not manually set the browser-forbidden `Content-Length` header.

Cloudflare R2 references: [S3-compatible API setup](https://developers.cloudflare.com/r2/get-started/s3/),
[presigned URLs](https://developers.cloudflare.com/r2/api/s3/presigned-urls/),
[CORS](https://developers.cloudflare.com/r2/buckets/cors/), and
[pricing](https://developers.cloudflare.com/r2/pricing/).
