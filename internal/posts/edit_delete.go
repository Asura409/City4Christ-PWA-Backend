package posts

import (
	"context"
	"database/sql"
	"errors"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

// UpdateInput replaces the member-owned post text and attachment list.
type UpdateInput struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
}

func (r *Repository) Update(ctx context.Context, authorID, postID string, in UpdateInput, signer media.DownloadSigner) (Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Post{}, err
	}
	defer tx.Rollback()
	var ownedID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM church_posts WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL FOR UPDATE`, postID, authorID).Scan(&ownedID)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	attachments, err := media.LoadAttachableTx(ctx, tx, authorID, string(media.PurposePostAttachment), in.AttachmentIDs)
	if err != nil {
		if errors.Is(err, media.ErrInvalidAttachment) {
			return Post{}, ErrInvalid
		}
		return Post{}, err
	}
	attachments, err = media.SignAttachments(ctx, signer, attachments)
	if err != nil {
		return Post{}, err
	}
	p, err := scan(tx.QueryRowContext(ctx, `UPDATE church_posts p SET body=$1
FROM accounts a WHERE p.id=$2 AND p.author_id=$3 AND p.deleted_at IS NULL AND a.id=p.author_id
RETURNING p.id,p.author_id,a.first_name,a.last_name,p.body,p.created_at,
 (SELECT count(*) FROM post_reactions pr WHERE pr.post_id=p.id AND pr.reaction='amen'),
 EXISTS(SELECT 1 FROM post_reactions pr WHERE pr.post_id=p.id AND pr.account_id=$3 AND pr.reaction='amen'),
 (SELECT count(*) FROM post_comments pc WHERE pc.post_id=p.id)`, in.Body, postID, authorID))
	if err != nil {
		return Post{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM post_media_attachments WHERE post_id=$1`, postID); err != nil {
		return Post{}, err
	}
	if err = insertAttachments(ctx, tx, "post_media_attachments", "post_id", p.ID, attachments); err != nil {
		return Post{}, err
	}
	p.Attachments = attachments
	if err = tx.Commit(); err != nil {
		return Post{}, err
	}
	return p, nil
}

func (r *Repository) Delete(ctx context.Context, authorID, postID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE church_posts SET body='[Post deleted]',deleted_at=now()
WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL`, postID, authorID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	for _, query := range []string{
		`DELETE FROM post_media_attachments WHERE post_id=$1`,
		`DELETE FROM post_comments WHERE post_id=$1`,
		`DELETE FROM post_reactions WHERE post_id=$1`,
	} {
		if _, err := tx.ExecContext(ctx, query, postID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) Update(ctx context.Context, authorID, postID string, in UpdateInput) (Post, error) {
	var err error
	in.Body, err = normalizePostBody(in.Body)
	if !validID(authorID) || !validID(postID) || err != nil {
		return Post{}, ErrInvalid
	}
	if media.ValidateAttachmentIDs(in.AttachmentIDs, media.MaxPostAttachments) != nil {
		return Post{}, ErrInvalid
	}
	return s.repo.Update(ctx, authorID, postID, in, s.signer)
}

func (s *Service) Delete(ctx context.Context, authorID, postID string) error {
	if !validID(authorID) || !validID(postID) {
		return ErrInvalid
	}
	return s.repo.Delete(ctx, authorID, postID)
}
