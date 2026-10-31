# Managed Daily Verse — fixed GMT+1

The owner selected GMT+1 on 31 October 2026. The church day uses **fixed
UTC+01:00**, with no daylight-saving changes and no dependency on a phone's or
PostgreSQL session's timezone. Midnight is always 23:00 UTC on the previous UTC
calendar date. For example, `2026-10-31T22:59:59Z` belongs to 31 October;
`2026-10-31T23:00:00Z` belongs to 1 November.

## Publication and privacy

Editors with the existing global `devotionals.manage` permission create dated
verse drafts and explicitly publish them. No new roles, delegation rules, secrets,
worker are added. Editors can supply text manually or use the optional [Bible lookup](bible-lookup.md) helper. Looking up or copying a passage never saves or publishes it automatically.

Any number of drafts may share a date. PostgreSQL permits only **one published
verse per display date**, including under concurrent publication attempts. A
collision returns `409 verse_date_conflict` without replacing the existing verse;
withdraw it or choose another date. PATCH requires the current version. Stale
versions return `409 version_conflict` and are not retried automatically.

Publication approves the verse for its assigned church date. Future dates remain
hidden until that day; past dates are not used as a fallback. Changing the text or
date of a published record changes its public selection immediately. Saving it as
draft withdraws it. Existing browser views revalidate on refresh/focus/day rollover;
there is no push invalidation or recall of already displayed text.

Writes and audit metadata share a transaction. Audit failure rolls back the verse
change. Audit rows retain actor, verse, action, version and UTC time, without a copy
of the text. Permission and account verification are checked again in PostgreSQL
for every editorial operation, including inside mutation transactions.

## API

All paths start with `/api/v1`. Every response uses the existing envelope and
`Cache-Control: no-store`.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/daily-verse` | Public selection for the server's current church date |
| POST | `/admin/daily-verses` | Create a dated draft; 201 with Location |
| GET | `/admin/daily-verses` | Editorial list, including drafts and future dates |
| GET | `/admin/daily-verses/{verseId}` | Editorial detail and current version |
| PATCH | `/admin/daily-verses/{verseId}` | Edit, publish or withdraw |

Create example (choose the date returned by `/daily-verse` for immediate testing):

```json
{
  "displayDate": "2026-11-01",
  "text": "An editor-supplied verse.",
  "scriptureReference": "Book 1:1",
  "translation": ""
}
```

Display date is a valid `YYYY-MM-DD` calendar date, years 0001–9999, rather than a
timestamp. Text is required plain text, 1–4000 Unicode characters. Scripture
reference is required, 1–200 characters. Optional translation/attribution allows
100 characters. Fields are trimmed; unsupported controls are rejected, while text
newlines and tabs are allowed. HTML is displayed as text.

Publish with `{"version":1,"status":"published"}`. Withdraw using the latest version
and status draft. Editable content/date fields may accompany status. The server
controls publication timestamps and actor identity; unknown fields are rejected.

An empty day returns **200**, not 404:

```json
{
  "data": {
    "date": "2026-11-01",
    "timeZone": "UTC+01:00",
    "serverTime": "2026-10-31T23:00:00Z",
    "nextDayAt": "2026-11-01T23:00:00Z",
    "verse": null
  }
}
```

A selected public verse contains only id, displayDate, text, scriptureReference and
translation. Editorial status/version/actor metadata is absent. Public requests
cannot override date/timezone or preview future content with query parameters.
There is no public history or draft-detail endpoint in this slice.

Editorial routes require a verified Bearer session and global permission. Mutations
also require an allowed Origin. List limits are 20 by default, maximum 100, with an
opaque createdAt/id descending cursor. Cursors grant no authority. Unknown or
duplicate query parameters are rejected; writes/detail/today accept no query.
Daily Verse shares the existing devotional quotas: 120 public reads/IP/minute,
60 editorial reads/account/minute, 30 writes/account/minute, and the editorial IP
ceilings. Validation returns field-specific 422; auth, availability and other
error conventions remain unchanged. OpenAPI version is 1.19.0.

## Frontend and release

The Home Daily bread card now uses the API. Empty days, loading and service failure
have distinct states; there is no hardcoded verse or previous-day fallback. The
client schedules revalidation from nextDayAt minus serverTime, accounting for
elapsed request time, so the device wall clock does not select a church day.
It clears the old verse before fetching, revalidates when the tab regains focus,
and offers Refresh daily verse. Offline/service failures show an error without
retaining an expired verse. Browser scheduling can be delayed while suspended;
returning to the tab triggers revalidation.

The dated editor is under Devotionals → Manage daily verses, alongside the existing
devotional editor, with the same permission gate. Shared paginated-content state
is separated under `src/content`; Daily Verse components, transport and timing
logic live under `src/dailyverse`. Drafts and daily content are not persisted in
localStorage or service-worker API caches.

Release the additive migration 43/backend routes before the new Home/editor UI.
There is no new configuration or permission-parser migration. The current VPS
email worker is unaffected. The implementation does not deploy, reset staging,
seed a verse or assign a live role. Staging acceptance stays with the testing team.
Scheduled publication times for long-form devotionals remain a separate feature;
dated Daily Verse visibility needs no cron job or background publisher.

## Verified local checks

TDD checks first failed for the missing service, HTTP routes, client clock model,
Home integration and OpenAPI paths. Full devotional/Daily Verse and migration
packages then passed with the race detector against disposable PostgreSQL. Global
HTTP checks passed for day selection, permissions, field validation, conflicts,
withdrawal, query rejection, origins, shared quotas and the OpenAPI contract.

All 41 frontend unit tests passed. The combined Daily Verse/devotional/React
browser run passed 38 desktop/mobile scenarios. The real Go API workflow passed
on desktop and mobile with separate synthetic editor/member sessions and no live
email or content creation. Go vet, formatting, diff checks, Swagger configuration
and the production frontend build passed. OpenAPI validates with the same eight
existing warnings.

The Docker image built and its disposable PostgreSQL smoke checks passed, including
repeatable forward migrations preserving a fixture record. These results do not
claim a fresh full-repository test run or live acceptance. Nothing was committed,
pushed, merged or deployed as part of this implementation slice.
