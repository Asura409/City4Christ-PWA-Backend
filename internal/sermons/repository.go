// Package sermons owns the public, published sermon catalog.
package sermons

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"time"
)

var ErrNotFound = errors.New("sermon not found")

type Sermon struct {
	VideoURL    string             `json:"videoUrl,omitempty"`
	ImageURL    string             `json:"imageUrl,omitempty"`
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Summary     string             `json:"summary"`
	Speaker     string             `json:"speaker"`
	Series      string             `json:"series"`
	Topic       string             `json:"topic"`
	PreachedAt  time.Time          `json:"preachedAt"`
	PublishedAt time.Time          `json:"publishedAt"`
	Attachments []media.Attachment `json:"attachments"`
}
type Options struct {
	Limit                                 int
	Cursor, Query, Speaker, Series, Topic string
}
type Cursor struct {
	PublishedAt time.Time `json:"publishedAt"`
	ID          string    `json:"id"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id,title,summary,speaker,series,topic,preached_at,published_at,video_url,image_url"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Sermon, error) {
	var s Sermon
	err := row.Scan(&s.ID, &s.Title, &s.Summary, &s.Speaker, &s.Series, &s.Topic, &s.PreachedAt, &s.PublishedAt, &s.VideoURL, &s.ImageURL)
	s.PreachedAt = s.PreachedAt.UTC()
	s.PublishedAt = s.PublishedAt.UTC()
	return s, err
}
func (r *Repository) Get(ctx context.Context, id string, now time.Time) (Sermon, error) {
	s, err := scan(r.db.QueryRowContext(ctx, "SELECT "+columns+" FROM sermons WHERE id=$1 AND published_at<=$2", id, now.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return Sermon{}, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Attachments, err = loadSermonAttachments(ctx, r.db, s.ID)
	return s, err
}
func (r *Repository) List(ctx context.Context, o Options, after *Cursor, now time.Time) ([]Sermon, error) {
	var at *time.Time
	var id *string
	if after != nil {
		at = &after.PublishedAt
		id = &after.ID
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+` FROM sermons
 WHERE published_at<=$1
 AND ($2='' OR strpos(lower(title || ' ' || summary),lower($2))>0)
 AND ($3='' OR lower(speaker)=lower($3))
 AND ($4='' OR lower(BTRIM(series))=lower($4))
 AND ($5='' OR lower(topic)=lower($5))
 AND ($6::timestamptz IS NULL OR (published_at,id)<($6::timestamptz,$7::uuid))
 ORDER BY published_at DESC,id DESC LIMIT $8`, now.UTC(), o.Query, o.Speaker, o.Series, o.Topic, at, id, o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Sermon, 0)
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Attachments, err = loadSermonAttachments(ctx, r.db, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func loadSermonAttachments(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, sermonID string) ([]media.Attachment, error) {
	rows, err := q.QueryContext(ctx, `SELECT u.id::text,u.actual_content_type,u.actual_size,u.available_object_key FROM sermon_media_attachments a JOIN media_uploads u ON u.id=a.upload_id WHERE a.sermon_id=$1 ORDER BY a.position`, sermonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]media.Attachment, 0)
	for rows.Next() {
		var item media.Attachment
		if err := rows.Scan(&item.ID, &item.ContentType, &item.Size, &item.ObjectKey); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
