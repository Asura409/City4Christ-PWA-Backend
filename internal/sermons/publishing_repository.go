package sermons

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"time"
)

const editorialColumns = "id,title,summary,speaker,series,topic,preached_at,published_at,video_url,image_url,version"

func scanEditorial(row scanner, now time.Time) (EditorialSermon, error) {
	var s EditorialSermon
	err := row.Scan(&s.ID, &s.Title, &s.Summary, &s.Speaker, &s.Series, &s.Topic, &s.PreachedAt, &s.PublishedAt, &s.VideoURL, &s.ImageURL, &s.Version)
	s.PreachedAt = s.PreachedAt.UTC()
	if s.PublishedAt != nil {
		utc := s.PublishedAt.UTC()
		s.PublishedAt = &utc
	}
	s.Status = editorialStatus(s.PublishedAt, now)
	return s, err
}
func checkEditor(ctx context.Context, tx *sql.Tx, actorID string) error {
	err := access.CheckTx(ctx, tx, actorID, "sermons.manage", "")
	if errors.Is(err, access.ErrForbidden) {
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
	result, err := scanEditorial(tx.QueryRowContext(ctx, `INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,video_url,image_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+editorialColumns, input.Title, input.Summary, input.Speaker, input.Series, input.Topic, input.PreachedAt, input.VideoURL, input.ImageURL), now)
	if err != nil {
		return EditorialSermon{}, err
	}
	attachments, err := media.LoadAttachableTx(ctx, tx, actorID, string(media.PurposeSermonAttachment), input.AttachmentIDs)
	if err != nil {
		if errors.Is(err, media.ErrInvalidAttachment) {
			return EditorialSermon{}, invalidField("attachmentIds", "Choose media uploaded by this editor and cleared by scanning.")
		}
		return EditorialSermon{}, err
	}
	if err = replaceSermonAttachments(ctx, tx, result.ID, attachments); err != nil {
		return EditorialSermon{}, err
	}
	result.AttachmentIDs = attachmentIDs(attachments)
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
	if err == nil {
		result.AttachmentIDs, err = sermonAttachmentIDs(ctx, tx, id)
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
	}{{&current.Title, patch.Title}, {&current.Summary, patch.Summary}, {&current.Speaker, patch.Speaker}, {&current.Series, patch.Series}, {&current.Topic, patch.Topic}, {&current.VideoURL, patch.VideoURL}, {&current.ImageURL, patch.ImageURL}} {
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
		{&current.ImageURL, patch.ImageURL, normalized.ImageURL},
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
	result, err := scanEditorial(tx.QueryRowContext(ctx, `UPDATE sermons SET title=$2,summary=$3,speaker=$4,series=$5,topic=$6,preached_at=$7,published_at=$8,video_url=$9,image_url=$10,version=version+1 WHERE id=$1 RETURNING `+editorialColumns, id, current.Title, current.Summary, current.Speaker, current.Series, current.Topic, current.PreachedAt, current.PublishedAt, current.VideoURL, current.ImageURL), now)
	if err != nil {
		return EditorialSermon{}, err
	}
	if err = audit(ctx, tx, actorID, result, action, now); err != nil {
		return EditorialSermon{}, err
	}
	if patch.AttachmentIDs != nil {
		attachments, loadErr := media.LoadAttachableTx(ctx, tx, actorID, string(media.PurposeSermonAttachment), *patch.AttachmentIDs)
		if loadErr != nil {
			if errors.Is(loadErr, media.ErrInvalidAttachment) {
				return EditorialSermon{}, invalidField("attachmentIds", "Choose media uploaded by this editor and cleared by scanning.")
			}
			return EditorialSermon{}, loadErr
		}
		if err = replaceSermonAttachments(ctx, tx, id, attachments); err != nil {
			return EditorialSermon{}, err
		}
		result.AttachmentIDs = attachmentIDs(attachments)
	} else {
		result.AttachmentIDs, err = sermonAttachmentIDs(ctx, tx, id)
		if err != nil {
			return EditorialSermon{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return EditorialSermon{}, err
	}
	return result, nil
}

func replaceSermonAttachments(ctx context.Context, tx *sql.Tx, sermonID string, items []media.Attachment) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM sermon_media_attachments WHERE sermon_id=$1", sermonID); err != nil {
		return err
	}
	for position, item := range items {
		if _, err := tx.ExecContext(ctx, "INSERT INTO sermon_media_attachments(sermon_id,upload_id,position) VALUES($1,$2,$3)", sermonID, item.ID, position); err != nil {
			return err
		}
	}
	return nil
}
func attachmentIDs(items []media.Attachment) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
func sermonAttachmentIDs(ctx context.Context, tx *sql.Tx, sermonID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT upload_id::text FROM sermon_media_attachments WHERE sermon_id=$1 ORDER BY position", sermonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func audit(ctx context.Context, tx *sql.Tx, actorID string, s EditorialSermon, action string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sermon_audit_events(sermon_id,actor_id,action,version,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, s.ID, actorID, action, s.Version, s.Status, now)
	return err
}
