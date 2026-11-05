# Bible lookup and Daily Bread

The public frontend Bible page (`/#/bible`) accepts an English Bible reference,
version, verse range or chapter. Home and Word link to it. Daily Bread links open
its published reference and translation; an unsupported original translation
requires an explicit choice rather than silently displaying another version.

Initial versions: World English Bible (`web`), World English Bible British Edition
(`webbe`), American Standard Version 1901 (`asv`) and King James Version (`kjv`).
These are public domain. KJV is served from HelloAO's public JSON API; the other
initial versions use the existing public provider.
[Provider documentation](https://bible-api.com/) and
[publisher licensing](https://ebible.org/eng-web/) describe the source material.
When `YVP_APP_KEY` is configured, the backend uses the [YouVersion Platform API](https://developers.youversion.com/api/bibles)
and exposes exactly the English versions available to that App Key through the
same public backend endpoints. The App Key stays server-side. Without it, the
public-domain catalog above remains active.

## Public API

- `GET /api/v1/bible/translations`: supported versions, language, license and import eligibility.
- `GET /api/v1/bible/passages?reference=John%203%3A16-18&translation=web`: canonical reference, version, text, numbered verses and source link.

Translation defaults to `web`. References are bounded to 128 characters and use
English book names, chapter numbers and optional verses. Unknown, duplicated or
unsupported parameters return 422. No arbitrary upstream URLs are accepted.
Single-chapter books use chapter interpretation for references such as `Jude 1`.

The backend calls a fixed HTTPS provider endpoint with a five-second timeout,
redirects disabled, bounded response sizes and validated plain text. It retains
up to 128 successful lookups for one hour per process and coalesces simultaneous
identical requests. The persistent shared provider budget permits 12 uncached
requests per 30 seconds; caller IPs have a 60-request/minute limit. Lookup may
return 429 or 503; it never substitutes another translation on failure. Cached
data is lost on restart.

## KJV database import worker

The KJV tables are created by migration `00059_bible_kjv.sql`. The one-time
import worker downloads HelloAO chapters in order and upserts them into
`bible_verses`. It uses a PostgreSQL advisory lock and records its checkpoint in
`bible_import_jobs`, so it is safe to restart after a failure:

```text
Build command: go build -o bible-import-worker ./cmd/bible-import-worker
Start command: ./bible-import-worker
```

Configure the Render Background Worker with the same `DATABASE_URL` as the API.
After the worker completes, KJV lookups are served from PostgreSQL; before then,
the API safely uses HelloAO as a fallback.

## Daily Bread workflow

An editor with `devotionals.manage` can look up and copy a passage into the dated
Daily Verse form. The helper fills text, reference and translation name while
preserving the date. The editor reviews the fields, creates/saves a draft, then
publishes explicitly. Passages exceeding the 4,000-character editorial limit
remain readable but cannot be copied as one daily verse.

Saved Daily Bread content remains in PostgreSQL and is served independently of
the lookup provider. Imported fields are editable and do not claim immutable
source provenance. Reading a different version never changes the published verse.
No staging data is seeded or reset. This feature requires outbound HTTPS only.
