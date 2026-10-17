package sermons

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const editorialColumns = "id,title,summary,speaker,series,topic,preached_at,published_at,video_url,version"

func scanEditorial(row scanner, now time.Time) (EditorialSermon, error) {
	var s EditorialSermon
	err := row.Scan(&s.ID, &s.Title, &s.Summary, &s.Speaker, &s.Series, &s.Topic, &s.PreachedAt, &s.PublishedAt, &s.VideoURL, &s.Version)
	s.PreachedAt = s.PreachedAt.UTC()
	if s.PublishedAt != nil {
		utc := s.PublishedAt.UTC()
		s.PublishedAt = &utc
	}
	s.Status = editorialStatus(s.PublishedAt, now)
	return s, err
}
func checkEditor(ctx context.Context, tx *sql.Tx, actorID string) error {
	if !canonicalID(actorID) {
		return ErrForbidden
	}
	var role string
	err := tx.QueryRowContext(ctx, `SELECT r.role FROM account_roles r JOIN accounts a ON a.id=r.account_id WHERE r.account_id=$1 AND a.email_verified_at IS NOT NULL AND r.role IN ('content_editor','system_administrator') ORDER BY r.role LIMIT 1 FOR SHARE OF r,a`, actorID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	return err
}
func (r *Repository) canEdit(ctx context.Context, actorID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkEditor(ctx, tx, actorID)
}
func (r *Repository) createDraft(ctx context.Context, actorID string, input DraftInput, now time.Time) (EditorialSermon, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EditorialSermon{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actorID); err != nil {
		return EditorialSermon{}, err
	}
	result, err := scanEditorial(tx.QueryRowContext(ctx, `INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,video_url) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+editorialColumns, input.Title, input.Summary, input.Speaker, input.Series, input.Topic, input.PreachedAt, input.VideoURL), now)
	if err != nil {
		return EditorialSermon{}, err
	}
	if err = audit(ctx, tx, actorID, result, "created", now); err != nil {
		return EditorialSermon{}, err
	}
	if err = tx.Commit(); err != nil {
		return EditorialSermon{}, err
	}
	return result, nil
}
func (r *Repository) getEditorial(ctx context.Context, actorID, id string, now time.Time) (EditorialSermon, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EditorialSermon{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actorID); err != nil {
		return EditorialSermon{}, err
	}
	result, err := scanEditorial(tx.QueryRowContext(ctx, "SELECT "+editorialColumns+" FROM sermons WHERE id=$1", id), now)
	if errors.Is(err, sql.ErrNoRows) {
		return EditorialSermon{}, ErrNotFound
	}
	return result, err
}
func (r *Repository) update(ctx context.Context, actorID, id string, patch UpdateInput, now time.Time) (EditorialSermon, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EditorialSermon{}, err
	}
	defer tx.Rollback()
	if err = checkEditor(ctx, tx, actorID); err != nil {
		return EditorialSermon{}, err
	}
	current, err := scanEditorial(tx.QueryRowContext(ctx, "SELECT "+editorialColumns+" FROM sermons WHERE id=$1 FOR UPDATE", id), now)
	if errors.Is(err, sql.ErrNoRows) {
		return EditorialSermon{}, ErrNotFound
	}
	if err != nil {
		return EditorialSermon{}, err
	}
	if current.Version != patch.Version {
		return EditorialSermon{}, ErrVersionConflict
	}
	for _, f := range []struct {
		target *string
		value  *string
	}{{&current.Title, patch.Title}, {&current.Summary, patch.Summary}, {&current.Speaker, patch.Speaker}, {&current.Series, patch.Series}, {&current.Topic, patch.Topic}, {&current.VideoURL, patch.VideoURL}} {
		if f.value != nil {
			*f.target = *f.value
		}
	}
	if patch.PreachedAt != nil {
		current.PreachedAt = *patch.PreachedAt
	}
	normalized, err := normalizeDraft(current.DraftInput)
	if err != nil {
		return EditorialSermon{}, err
	}
	// Validate the merged record, but normalize only fields explicitly patched.
	// Legacy formatting must survive publish-only and unrelated metadata edits.
	for _, f := range []struct {
		target     *string
		supplied   *string
		normalized string
	}{
		{&current.Title, patch.Title, normalized.Title},
		{&current.Summary, patch.Summary, normalized.Summary},
		{&current.Speaker, patch.Speaker, normalized.Speaker},
		{&current.Series, patch.Series, normalized.Series},
		{&current.Topic, patch.Topic, normalized.Topic},
		{&current.VideoURL, patch.VideoURL, normalized.VideoURL},
	} {
		if f.supplied != nil {
			*f.target = f.normalized
		}
	}
	if patch.PreachedAt != nil {
		current.PreachedAt = normalized.PreachedAt
	}
	action := "updated"
	if patch.Status != nil {
		switch *patch.Status {
		case "draft":
			if current.PublishedAt != nil {
				action = "unpublished"
			}
			current.PublishedAt = nil
		case "published":
			if current.PublishedAt == nil || current.PublishedAt.After(now) {
				current.PublishedAt = &now
				action = "published"
			}
		case "scheduled":
			at := patch.PublishAt.UTC()
			current.PublishedAt = &at
			action = "scheduled"
		}
	}
	result, err := scanEditorial(tx.QueryRowContext(ctx, `UPDATE sermons SET title=$2,summary=$3,speaker=$4,series=$5,topic=$6,preached_at=$7,published_at=$8,video_url=$9,version=version+1 WHERE id=$1 RETURNING `+editorialColumns, id, current.Title, current.Summary, current.Speaker, current.Series, current.Topic, current.PreachedAt, current.PublishedAt, current.VideoURL), now)
	if err != nil {
		return EditorialSermon{}, err
	}
	if err = audit(ctx, tx, actorID, result, action, now); err != nil {
		return EditorialSermon{}, err
	}
	if err = tx.Commit(); err != nil {
		return EditorialSermon{}, err
	}
	return result, nil
}
func audit(ctx context.Context, tx *sql.Tx, actorID string, s EditorialSermon, action string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sermon_audit_events(sermon_id,actor_id,action,version,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, s.ID, actorID, action, s.Version, s.Status, now)
	return err
}
