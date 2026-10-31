// Package devotionals manages plain-text devotional publication and visibility.
package devotionals

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrForbidden   = errors.New("devotional editor permission required")
	ErrNotFound    = errors.New("devotional not found")
	ErrConflict    = errors.New("devotional version conflict")
	ErrInvalid     = errors.New("invalid devotional input")
	ErrUnavailable = errors.New("devotionals unavailable")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return ErrInvalid.Error() }
func (e *ValidationError) Unwrap() error { return ErrInvalid }
func invalid(field, message string) error {
	return &ValidationError{Fields: map[string]string{field: message}}
}

type Content struct {
	Title              string `json:"title"`
	Body               string `json:"body"`
	ScriptureReference string `json:"scriptureReference"`
	Author             string `json:"author"`
}
type PublishedEntry struct {
	ID string `json:"id"`
	Content
	PublishedAt *time.Time `json:"publishedAt"`
}
type Entry struct {
	PublishedEntry
	Status    string    `json:"status"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Update struct {
	Version            int     `json:"version"`
	Title              *string `json:"title"`
	Body               *string `json:"body"`
	ScriptureReference *string `json:"scriptureReference"`
	Author             *string `json:"author"`
	Status             *string `json:"status"`
}
type Options struct {
	Limit  int
	Cursor string
}
type Page[T any] struct {
	Items      []T
	NextCursor string
}
type Repository struct{ db *sql.DB }
type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func NewService(r *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: r, now: now}
}
func (s *Service) CanEdit(ctx context.Context, actor string) error { return s.repo.canEdit(ctx, actor) }
