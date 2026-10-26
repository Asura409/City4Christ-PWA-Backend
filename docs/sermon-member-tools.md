# Sermon playback progress and private notes

Members with an active verified session can save playback position and one
private note for each published sermon. Draft or scheduled sermons return the
same not-found response as unknown sermons. Account IDs always come from the
Bearer session; request bodies cannot select another member.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/me/sermons/{sermonId}/progress` | Get own position; an unsaved position is returned as zero. |
| PUT | `/api/v1/me/sermons/{sermonId}/progress` | Set own position in integer seconds, from zero through 86400. Seeking backward is allowed. |
| GET | `/api/v1/me/sermons/{sermonId}/notes` | Get own note, or `data: null` if none exists. |
| PUT | `/api/v1/me/sermons/{sermonId}/notes` | Create or replace one private note, up to 10000 Unicode characters. |
| DELETE | `/api/v1/me/sermons/{sermonId}/notes` | Idempotently remove only the caller's note. |

Write requests require `Authorization: Bearer <access-token>` and an allowed
browser `Origin`. Notes and progress are stored in separate account/sermon rows;
they are not included in public sermon responses, directory results, or staff
editorial views. Account and sermon deletion cascade through the foreign keys.

The React Word Hub resumes uploaded audio/video from the saved position and saves
updates while the member listens. External YouTube embeds cannot report playback
position to the app, so progress tracking applies to uploaded media elements.

Migration `00033_sermon_member_tools.sql` is additive and preserves existing
accounts and sermons.
