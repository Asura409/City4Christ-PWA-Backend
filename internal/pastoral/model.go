// Package pastoral owns explicit pastor profiles, availability and appointments.
package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

var (
	ErrForbidden = errors.New("pastoral action forbidden")
	ErrNotFound  = errors.New("pastoral resource not found")
	ErrInvalid   = errors.New("invalid pastoral input")
	ErrConflict  = errors.New("pastoral state conflict")
)

type Profile struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Bio       string `json:"bio"`
	TimeZone  string `json:"timeZone"`
	Listed    bool   `json:"listed"`
}
type ProfileInput struct {
	AccountID string `json:"accountId"`
	Bio       string `json:"bio"`
	TimeZone  string `json:"timeZone"`
	Listed    *bool  `json:"listed,omitempty"`
}
type Slot struct {
	Available bool      `json:"available"`
	ID        string    `json:"id"`
	PastorID  string    `json:"pastorId"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	TimeZone  string    `json:"timeZone"`
}
type SlotInput struct {
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
}
type Options struct {
	Limit  int
	Cursor string
}
type Page[T any] struct {
	Items      []T
	NextCursor string
}
type Guard func(context.Context, *sql.Tx) error
type Repository struct{ db *sql.DB }
type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func NewService(repo *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}
func validID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}
func options(o Options) (Options, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 || (o.Cursor != "" && !validID(o.Cursor)) {
		return o, ErrInvalid
	}
	return o, nil
}
func finishPage[T any](items []T, limit int, id func(T) string) Page[T] {
	p := Page[T]{Items: items}
	if len(items) > limit {
		p.Items = items[:limit]
		p.NextCursor = id(p.Items[limit-1])
	}
	return p
}
func cleanBio(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || len([]rune(value)) > 1000 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return "", ErrInvalid
	}
	return value, nil
}
