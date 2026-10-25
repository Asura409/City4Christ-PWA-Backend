# Media storage setup

Cloudflare R2 is the selected object store for post and sermon attachments. The
backend currently contains a tested signing adapter in `internal/media`; upload
and download API routes, metadata persistence, content inspection, and frontend
flows have not been connected. Do not give the bucket a public read policy or
enable uploads until those checks are implemented.

ClamAV on the VPS is the selected malware scanner. The backend now has a small
`ClamAVScanner` adapter which invokes `clamdscan --stream` with a bounded context
and distinguishes clean, infected, and scanner-error results. It is not wired to
uploads yet. The intended pipeline is: API records a pending private-object scan
job; the VPS worker downloads that object and scans it through the local ClamAV
daemon; only a clean result can make the media attachable. A timeout, missing
scanner, or daemon error must leave the upload unavailable. Keep `clamd` on a
Unix socket or loopback only; never open its unauthenticated TCP port to the
Internet. The official ClamAV docs describe `clamdscan` as the standard client
and explain that TCP `clamd` is not encrypted or authenticated:
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
4. Configure the bucket's CORS policy for the exact deployed frontend origins and
   the required `PUT`/`GET` methods and request headers once the browser upload
   contract is finalized. Avoid wildcard origins when cookies or authorization
   are involved.

The R2 signer accepts the account ID, bucket name, Access Key ID and Secret Access
Key at construction. Keep these values in the API/worker host's secret environment
settings, never in Git, frontend variables, logs, or chat. Do not configure these
on Render yet: application startup does not read them and no media HTTP endpoints
or worker exist yet. The VPS worker must have read access to pending objects; the
API needs only the object operations required to reserve and finalize them.

## Current signing limits

The adapter signs private-object `PUT` and `GET` requests for at most five minutes.
Upload signatures bind the declared `Content-Type` and `Content-Length`, and object
keys are restricted to safe relative paths. This is only the storage adapter:
before a signed upload can be issued, the API must authorize the actor and parent
resource, reserve a server-generated key, enforce per-file and per-user quotas,
and persist pending metadata. On completion it must inspect the actual object
size/type/content, reject mismatches, and attach it transactionally. The app must
serve private media through short-lived signed GET URLs. Public media requires a
separate explicit product policy and must not be exposed by changing the bucket to
public.

Cloudflare R2 references: [S3-compatible API setup](https://developers.cloudflare.com/r2/get-started/s3/),
[presigned URLs](https://developers.cloudflare.com/r2/api/s3/presigned-urls/),
[CORS](https://developers.cloudflare.com/r2/buckets/cors/), and
[pricing](https://developers.cloudflare.com/r2/pricing/).
