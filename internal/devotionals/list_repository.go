package devotionals

import (
	"context"
	"database/sql"
	"time"
)

func readEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	items := []Entry{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
func (r *Repository) listPublished(ctx context.Context, o Options, now time.Time) (Page[PublishedEntry], error) {
	p := Page[PublishedEntry]{Items: []PublishedEntry{}}
	if r.db == nil {
		return p, ErrUnavailable
	}
	at, id, err := decodeCursor(o.Cursor)
	if err != nil {
		return p, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM devotionals WHERE status='published' AND published_at<=$1 AND ($2::timestamptz IS NULL OR (published_at,id)<($2,$3::uuid)) ORDER BY published_at DESC,id DESC LIMIT $4`, now, at, id, o.Limit+1)
	if err != nil {
		return p, err
	}
	entries, err := readEntries(rows)
	if err != nil {
		return p, err
	}
	if len(entries) > o.Limit {
		entries = entries[:o.Limit]
		last := entries[len(entries)-1]
		p.NextCursor = encodeCursor(*last.PublishedAt, last.ID)
	}
	for _, e := range entries {
		p.Items = append(p.Items, e.PublishedEntry)
	}
	return p, nil
}
func (r *Repository) listEditorial(ctx context.Context, actor string, o Options) (Page[Entry], error) {
	p := Page[Entry]{Items: []Entry{}}
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
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM devotionals WHERE ($1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, at, id, o.Limit+1)
	if err != nil {
		return p, err
	}
	p.Items, err = readEntries(rows)
	if err != nil {
		return p, err
	}
	if len(p.Items) > o.Limit {
		p.Items = p.Items[:o.Limit]
		last := p.Items[len(p.Items)-1]
		p.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return p, nil
}
