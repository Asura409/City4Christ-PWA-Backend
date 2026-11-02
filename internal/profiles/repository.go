// Package profiles owns editable member information and privacy preferences.
package profiles

import (
	"context"
	"database/sql"
	"errors"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

var ErrNotFound = errors.New("profile not found")
var ErrInvalidAvatarUpload = errors.New("profile photo upload is not available to this owner")

type Profile struct {
	ID               string            `json:"id"`
	Email            string            `json:"email"`
	FirstName        string            `json:"firstName"`
	LastName         string            `json:"lastName"`
	EmailVerified    bool              `json:"emailVerified"`
	HasPassword      bool              `json:"hasPassword"`
	DirectoryVisible bool              `json:"directoryVisible"`
	Avatar           *media.Attachment `json:"avatar,omitempty"`
	avatarUploadID   sql.NullString
}
type Update struct {
	FirstName        *string `json:"firstName"`
	LastName         *string `json:"lastName"`
	DirectoryVisible *bool   `json:"directoryVisible"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id,email,first_name,last_name,email_verified_at IS NOT NULL,password_hash IS NOT NULL,directory_visible,avatar_upload_id::text"

func scan(row *sql.Row) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Email, &p.FirstName, &p.LastName, &p.EmailVerified, &p.HasPassword, &p.DirectoryVisible, &p.avatarUploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	return p, err
}

func (r *Repository) avatar(ctx context.Context, uploadID string) (*media.Attachment, error) {
	var item media.Attachment
	err := r.db.QueryRowContext(ctx, `SELECT id::text,actual_content_type,actual_size,available_object_key
		FROM media_uploads WHERE id=$1 AND purpose='profile_photo' AND status='available'`, uploadID).
		Scan(&item.ID, &item.ContentType, &item.Size, &item.ObjectKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) SetAvatar(ctx context.Context, actor, uploadID string) (Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback()
	var oldID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT avatar_upload_id::text FROM accounts
		WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FOR UPDATE`, actor).Scan(&oldID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, err
	}
	if _, err := media.LoadAttachableTx(ctx, tx, actor, string(media.PurposeProfilePhoto), []string{uploadID}); err != nil {
		if errors.Is(err, media.ErrInvalidAttachment) {
			return Profile{}, ErrInvalidAvatarUpload
		}
		return Profile{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET avatar_upload_id=$2 WHERE id=$1`, actor, uploadID); err != nil {
		return Profile{}, err
	}
	if oldID.Valid && oldID.String != uploadID {
		if err := media.QueueObjectCleanupIfUnreferencedTx(ctx, tx, oldID.String); err != nil {
			return Profile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Profile{}, err
	}
	return r.Get(ctx, actor)
}

func (r *Repository) RemoveAvatar(ctx context.Context, actor string) (Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback()
	var oldID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT avatar_upload_id::text FROM accounts
		WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FOR UPDATE`, actor).Scan(&oldID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET avatar_upload_id=NULL WHERE id=$1`, actor); err != nil {
		return Profile{}, err
	}
	if oldID.Valid {
		if err := media.QueueObjectCleanupIfUnreferencedTx(ctx, tx, oldID.String); err != nil {
			return Profile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Profile{}, err
	}
	return r.Get(ctx, actor)
}
func (r *Repository) Get(ctx context.Context, actor string) (Profile, error) {
	return scan(r.db.QueryRowContext(ctx, "SELECT "+columns+" FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL", actor))
}

// Atomic partial updates preserve concurrent edits to fields omitted by this request.
func (r *Repository) Update(ctx context.Context, actor string, in Update) (Profile, error) {
	return scan(r.db.QueryRowContext(ctx, `UPDATE accounts SET first_name=COALESCE($2,first_name),last_name=COALESCE($3,last_name),directory_visible=COALESCE($4,directory_visible),profile_version=profile_version+CASE WHEN $2 IS NOT NULL OR $3 IS NOT NULL THEN 1 ELSE 0 END WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL RETURNING `+columns, actor, in.FirstName, in.LastName, in.DirectoryVisible))
}
