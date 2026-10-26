package media

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DownloadURLTTL       = 30 * time.Minute
	MaxPostAttachments   = 4
	MaxSermonAttachments = 10
)

var ErrInvalidAttachment = errors.New("media attachment is not available to this owner")

// DownloadSigner creates short-lived read URLs for clean objects in private storage.
type DownloadSigner interface {
	PresignDownload(context.Context, string, time.Duration) (SignedRequest, error)
}

// Attachment exposes a signed URL while keeping storage keys internal.
type Attachment struct {
	ID          string    `json:"id"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ObjectKey   string    `json:"-"`
}

func ValidateAttachmentIDs(ids []string, limit int) error {
	if len(ids) > limit {
		return ErrInvalidAttachment
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id {
			return ErrInvalidAttachment
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidAttachment
		}
		seen[id] = struct{}{}
	}
	return nil
}

// LoadAttachableTx locks and verifies every upload in the request. Purpose,
// ownership, clean scan status, and an available object key are all required.
func LoadAttachableTx(ctx context.Context, tx *sql.Tx, ownerID, purpose string, ids []string) ([]Attachment, error) {
	items := make([]Attachment, 0, len(ids))
	for _, id := range ids {
		var item Attachment
		err := tx.QueryRowContext(ctx, `SELECT id::text,actual_content_type,actual_size,available_object_key
			FROM media_uploads WHERE id=$1 AND owner_id=$2 AND purpose=$3 AND status='available'
			AND available_object_key IS NOT NULL FOR UPDATE`, id, ownerID, purpose).
			Scan(&item.ID, &item.ContentType, &item.Size, &item.ObjectKey)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidAttachment
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func SignAttachments(ctx context.Context, signer DownloadSigner, items []Attachment) ([]Attachment, error) {
	if len(items) == 0 {
		return []Attachment{}, nil
	}
	if signer == nil {
		return nil, ErrMediaUnavailable
	}
	for i := range items {
		signed, err := signer.PresignDownload(ctx, items[i].ObjectKey, DownloadURLTTL)
		if err != nil {
			return nil, err
		}
		items[i].URL = signed.URL
		items[i].ExpiresAt = signed.ExpiresAt.UTC()
	}
	return items, nil
}
