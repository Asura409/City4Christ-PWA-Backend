# Sermon catalog: first slice

The API exposes public, read-only sermon metadata independently of member sessions. Only rows whose `published_at` is non-null and at or before the current UTC time are visible. Draft, future, missing and malformed IDs all return the same 404 error.

## List

`GET /api/v1/sermons`

Optional query parameters:

| Parameter | Behavior |
| --- | --- |
| `query` | Literal case-insensitive substring of title or summary; at most 200 characters |
| `speaker` | Case-insensitive exact match; at most 200 characters |
| `series` | Case-insensitive exact series label; at most 200 characters |
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

This slice includes the `sermons` table, publication/filter indexes, repository, validated service, global routes, and OpenAPI documentation. Series and topic are optional labels represented by empty strings when absent; separate managed series records are deferred.

No content is automatically seeded or published. There is no public content-write route. Staging stays empty until content is intentionally added through a future authorized editor workflow. This migration applies forward only and does not alter existing accounts or auth records.

Editor roles, audited publishing, GET `/series`, approved media-provider references, playback progress and private notes remain future work. Before exposing media links or editor writes, agree provider/access policy and who may publish. No media is fetched or streamed by this slice.

## Executed TDD

Repository tests first failed on missing symbols, then against PostgreSQL with a missing sermons table. After the forward migration they passed. Service tests failed before implementation, then verified page boundaries, validation and cancellation. The global-app test failed with HTTP 404 before route wiring, then verified public reads, hidden drafts/future publications, filter validation and safe database outages. Contract coverage failed before the OpenAPI endpoints were added.

Every database test uses its own disposable PostgreSQL cluster; no staging connection or data reset is used.
