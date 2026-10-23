// Package sermons owns the public, published sermon catalog.
package sermons

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("sermon not found")

type Sermon struct {
	VideoURL    string    `json:"videoUrl,omitempty"`
	ImageURL    string    `json:"imageUrl,omitempty"`
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Speaker     string    `json:"speaker"`
	Series      string    `json:"series"`
	Topic       string    `json:"topic"`
	PreachedAt  time.Time `json:"preachedAt"`
	PublishedAt time.Time `json:"publishedAt"`
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
 AND ($4='' OR lower(series)=lower($4))
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
	return items, rows.Err()
}
