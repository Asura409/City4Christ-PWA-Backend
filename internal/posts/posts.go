// Package posts implements the authenticated church-wide member feed.
package posts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalid = errors.New("invalid post or feed options")
)

type Post struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"authorId"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateInput struct {
	Body string `json:"body"`
}

type Options struct {
	Limit  int
	Cursor string
}

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type Page struct {
	Items      []Post `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, author string, in CreateInput) (Post, error) {
	return scan(r.db.QueryRowContext(ctx, `WITH created AS (
 INSERT INTO church_posts(author_id,body) VALUES($1,$2)
 RETURNING id,author_id,body,created_at
)
SELECT p.id,p.author_id,a.first_name,a.last_name,p.body,p.created_at
FROM created p JOIN accounts a ON a.id=p.author_id`, author, in.Body))
}

func (r *Repository) List(ctx context.Context, o Options, after *Cursor) ([]Post, error) {
	var at *time.Time
	var id *string
	if after != nil {
		at, id = &after.CreatedAt, &after.ID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.author_id,a.first_name,a.last_name,p.body,p.created_at
 FROM church_posts p JOIN accounts a ON a.id=p.author_id
 WHERE ($1::timestamptz IS NULL OR (p.created_at,p.id)<($1::timestamptz,$2::uuid))
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`, at, id, o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Post, 0)
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func scan(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.FirstName, &p.LastName, &p.Body, &p.CreatedAt)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, err
}

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func (s *Service) Create(ctx context.Context, author string, in CreateInput) (Post, error) {
	in.Body = strings.TrimSpace(in.Body)
	if !validID(author) || !utf8.ValidString(in.Body) || len([]rune(in.Body)) < 1 || len([]rune(in.Body)) > 5000 || strings.ContainsRune(in.Body, 0) {
		return Post{}, ErrInvalid
	}
	return s.repo.Create(ctx, author, in)
}

func (s *Service) List(ctx context.Context, o Options) (Page, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 || len(o.Cursor) > 2048 {
		return Page{}, ErrInvalid
	}
	var after *Cursor
	if o.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		if err != nil {
			return Page{}, ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var c *Cursor
		if decoder.Decode(&c) != nil || c == nil || decoder.Decode(new(any)) != io.EOF || c.CreatedAt.IsZero() || !validID(c.ID) {
			return Page{}, ErrInvalid
		}
		after = c
	}
	items, err := s.repo.List(ctx, o, after)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > o.Limit {
		page.Items = items[:o.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
