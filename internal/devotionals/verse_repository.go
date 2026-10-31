package devotionals

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

const verseColumns = `id,to_char(display_date,'YYYY-MM-DD'),text,scripture_reference,translation,status,version,published_at,created_at,updated_at`

func scanVerse(row scanner) (Verse, error) {
	var v Verse
	err := row.Scan(&v.ID, &v.DisplayDate, &v.Text, &v.ScriptureReference, &v.Translation, &v.Status, &v.Version, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "daily_verses_published_date" {
		err = ErrVerseDateConflict
	}
	return v, err
}
func verseAudit(ctx context.Context, tx *sql.Tx, actor string, v Verse, action string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_verse_audit_events(verse_id,actor_id,action,version,occurred_at) VALUES($1,$2,$3,$4,$5)`, v.ID, actor, action, v.Version, now)
	return err
}
func (r *Repository) createVerse(ctx context.Context, actor string, c VerseContent, now time.Time) (Verse, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Verse{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Verse{}, err
	}
	v, err := scanVerse(tx.QueryRowContext(ctx, `INSERT INTO daily_verses(display_date,text,scripture_reference,translation,created_at,updated_at) VALUES($1::date,$2,$3,$4,$5,$5) RETURNING `+verseColumns, c.DisplayDate, c.Text, c.ScriptureReference, c.Translation, now))
	if err != nil {
		return Verse{}, err
	}
	if err = verseAudit(ctx, tx, actor, v, "created", now); err != nil {
		return Verse{}, err
	}
	return v, tx.Commit()
}
func (r *Repository) updateVerse(ctx context.Context, actor, id string, u VerseUpdate, now time.Time) (Verse, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Verse{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Verse{}, err
	}
	v, err := scanVerse(tx.QueryRowContext(ctx, `SELECT `+verseColumns+` FROM daily_verses WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Verse{}, err
	}
	if v.Version != u.Version {
		return Verse{}, ErrConflict
	}
	for _, f := range []struct {
		input  *string
		target *string
	}{{u.DisplayDate, &v.DisplayDate}, {u.Text, &v.Text}, {u.ScriptureReference, &v.ScriptureReference}, {u.Translation, &v.Translation}} {
		if f.input != nil {
			*f.target = *f.input
		}
	}
	v.VerseContent, err = normalizeVerse(v.VerseContent)
	if err != nil {
		return Verse{}, err
	}
	action := "updated"
	if u.Status != nil && *u.Status != v.Status {
		v.Status = *u.Status
		if v.Status == "published" {
			v.PublishedAt = &now
			action = "published"
		} else {
			v.PublishedAt = nil
			action = "withdrawn"
		}
	}
	v, err = scanVerse(tx.QueryRowContext(ctx, `UPDATE daily_verses SET display_date=$2::date,text=$3,scripture_reference=$4,translation=$5,status=$6,published_at=$7,version=version+1,updated_at=$8 WHERE id=$1 RETURNING `+verseColumns, id, v.DisplayDate, v.Text, v.ScriptureReference, v.Translation, v.Status, v.PublishedAt, now))
	if err != nil {
		return Verse{}, err
	}
	if err = verseAudit(ctx, tx, actor, v, action, now); err != nil {
		return Verse{}, err
	}
	return v, tx.Commit()
}
func (r *Repository) verseEditorial(ctx context.Context, actor, id string) (Verse, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Verse{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Verse{}, err
	}
	return scanVerse(tx.QueryRowContext(ctx, `SELECT `+verseColumns+` FROM daily_verses WHERE id=$1`, id))
}
func (r *Repository) listVersesEditorial(ctx context.Context, actor string, o Options) (Page[Verse], error) {
	p := Page[Verse]{Items: []Verse{}}
	tx, err := r.begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return p, err
	}
	at, id, err := decodeCursor(o.Cursor)
	if err != nil {
		return p, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+verseColumns+` FROM daily_verses WHERE ($1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, at, id, o.Limit+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanVerse(rows)
		if err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Items) > o.Limit {
		p.Items = p.Items[:o.Limit]
		last := p.Items[len(p.Items)-1]
		p.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return p, nil
}
