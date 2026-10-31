package devotionals

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"time"
)

const columns = `id,title,body,scripture_reference,author,published_at,status,version,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Entry, error) {
	var e Entry
	err := row.Scan(&e.ID, &e.Title, &e.Body, &e.ScriptureReference, &e.Author, &e.PublishedAt, &e.Status, &e.Version, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return e, err
}
func checkEditor(ctx context.Context, tx *sql.Tx, actor string) error {
	err := access.CheckTx(ctx, tx, actor, "devotionals.manage", "")
	if errors.Is(err, access.ErrForbidden) {
		return ErrForbidden
	}
	return err
}
func (r *Repository) begin(ctx context.Context) (*sql.Tx, error) {
	if r.db == nil {
		return nil, ErrUnavailable
	}
	return r.db.BeginTx(ctx, nil)
}
func (r *Repository) canEdit(ctx context.Context, actor string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkEditor(ctx, tx, actor)
}
func audit(ctx context.Context, tx *sql.Tx, actor string, e Entry, action string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO devotional_audit_events(devotional_id,actor_id,action,version,occurred_at) VALUES($1,$2,$3,$4,$5)`, e.ID, actor, action, e.Version, now)
	return err
}
func (r *Repository) create(ctx context.Context, actor string, c Content, now time.Time) (Entry, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Entry{}, err
	}
	e, err := scan(tx.QueryRowContext(ctx, `INSERT INTO devotionals(title,body,scripture_reference,author,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5) RETURNING `+columns, c.Title, c.Body, c.ScriptureReference, c.Author, now))
	if err != nil {
		return Entry{}, err
	}
	if err = audit(ctx, tx, actor, e, "created", now); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}
func (r *Repository) update(ctx context.Context, actor, id string, u Update, now time.Time) (Entry, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Entry{}, err
	}
	e, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM devotionals WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Entry{}, err
	}
	if e.Version != u.Version {
		return Entry{}, ErrConflict
	}
	for _, f := range []struct {
		input  *string
		target *string
	}{{u.Title, &e.Title}, {u.Body, &e.Body}, {u.ScriptureReference, &e.ScriptureReference}, {u.Author, &e.Author}} {
		if f.input != nil {
			*f.target = *f.input
		}
	}
	e.Content, err = normalize(e.Content)
	if err != nil {
		return Entry{}, err
	}
	action := "updated"
	if u.Status != nil && *u.Status != e.Status {
		e.Status = *u.Status
		if e.Status == "published" {
			e.PublishedAt = &now
			action = "published"
		} else {
			e.PublishedAt = nil
			action = "withdrawn"
		}
	}
	e, err = scan(tx.QueryRowContext(ctx, `UPDATE devotionals SET title=$2,body=$3,scripture_reference=$4,author=$5,status=$6,published_at=$7,version=version+1,updated_at=$8 WHERE id=$1 RETURNING `+columns, id, e.Title, e.Body, e.ScriptureReference, e.Author, e.Status, e.PublishedAt, now))
	if err != nil {
		return Entry{}, err
	}
	if err = audit(ctx, tx, actor, e, action, now); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}
func (r *Repository) editorial(ctx context.Context, actor, id string) (Entry, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actor); err != nil {
		return Entry{}, err
	}
	return scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM devotionals WHERE id=$1`, id))
}
func (r *Repository) published(ctx context.Context, id string, now time.Time) (PublishedEntry, error) {
	if r.db == nil {
		return PublishedEntry{}, ErrUnavailable
	}
	e, err := scan(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM devotionals WHERE id=$1 AND status='published' AND published_at<=$2`, id, now))
	return e.PublishedEntry, err
}
