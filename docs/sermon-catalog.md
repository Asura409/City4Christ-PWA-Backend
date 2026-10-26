# Sermon catalog: first slice

The API exposes public, read-only sermon metadata independently of member sessions. Only rows whose `published_at` is non-null and at or before the current UTC time are visible. Draft, future, missing and malformed IDs all return the same 404 error.

## List

### Published series

`GET /api/v1/sermons/series` returns each named series that has at least one
sermon published at the time of the request. Names are trimmed and grouped
case-insensitively. Each entry includes `name`, `sermonCount`, and
`latestPublishedAt`, ordered by latest publication first. Use the returned name
with the `series` filter below; filtering also ignores surrounding whitespace
and letter case.

`GET /api/v1/sermons`

Optional query parameters:

| Parameter | Behavior |
| --- | --- |
| `query` | Literal case-insensitive substring of title or summary; at most 200 characters |
| `speaker` | Case-insensitive exact match; at most 200 characters |
| `series` | Case-insensitive exact series label after trimming whitespace; at most 200 characters |
| `topic` | Case-insensitive exact topic label; at most 100 characters |
| `limit` | Integer 1–100; default 20 |
| `cursor` | Opaque continuation from the previous page; keep filters unchanged |

Ordering is descending `(publishedAt, id)`, with a one-row lookahead to determine continuation. Unknown or repeated parameters, malformed encodings, oversized filters, invalid limits and malformed cursors return 422. Cursors do not grant access; publication checks always apply.

```json
{
  "data": [
    {
      "id": "4a8394d5-ec5e-4a46-80bd-77f04a484e9a",
      "title": "Living in Faith",
      "summary": "A message about faith and community.",
      "speaker": "Pastor Joy",
      "series": "Foundations",
      "topic": "Faith",
      "preachedAt": "2026-10-11T14:00:00Z",
      "publishedAt": "2026-10-16T10:00:00Z"
    }
  ],
  "meta": { "nextCursor": "" }
}
```

An empty catalog returns `data: []`. An empty `nextCursor` marks the final page. New publications can appear ahead of a cursor; pagination is not a historical snapshot.

## Detail

`GET /api/v1/sermons/{sermonId}` returns `{ "data": { ...sermon metadata... } }`. UUIDs use their canonical lowercase representation. Storage failures return 503 with a generic error and request ID. Existing exact-origin CORS policy applies to browser requests.

## Scope and next slices

This slice includes the `sermons` table, publication/filter indexes, repository, validated service, global routes, and OpenAPI documentation. Series and topic are optional labels represented by empty strings when absent; separate managed series records are deferred. Optional YouTube links and Unsplash image URLs are returned by the catalog, with React rendering a thumbnail and privacy-enhanced video player.

No content is automatically seeded or published. There is no public content-write route. Authorized writes are available through the [editor workflow](sermon-publishing.md); no account receives permission automatically. The [member-tools slice](sermon-member-tools.md) adds private notes and progress for uploaded audio/video; progress for YouTube embeds is not available. All migrations apply forward only and do not alter existing accounts or auth records.

[Editor roles, audited publishing and validated media links](sermon-publishing.md) are implemented. `GET /api/v1/sermons/series` lists series with at least one currently published sermon, and the React Word Hub can filter the catalog by series. Member playback progress for YouTube embeds remains unavailable. No media is fetched or streamed by the API.

## Executed TDD

Repository tests first failed on missing symbols, then against PostgreSQL with a missing sermons table. After the forward migration they passed. Service tests failed before implementation, then verified page boundaries, validation and cancellation. The global-app test failed with HTTP 404 before route wiring, then verified public reads, hidden drafts/future publications, filter validation and safe database outages. Contract coverage failed before the OpenAPI endpoints were added.

The published-series repository, service and global-route tests use a disposable PostgreSQL cluster. The global-route test first returned 404; after adding the route it caught a whitespace-normalization mismatch in series filtering before that was fixed. The React desktop/mobile browser test first failed because the Word Hub had no series selector, then verified series discovery and filtering after the UI wiring. No staging connection or data reset is used.
