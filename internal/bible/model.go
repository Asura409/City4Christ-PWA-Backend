// Package bible provides bounded public passage lookup behind a provider adapter.
package bible

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid     = errors.New("invalid Bible lookup")
	ErrUnavailable = errors.New("Bible lookup unavailable")
	ErrRateLimited = errors.New("Bible lookup rate limited")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return ErrInvalid.Error() }
func (e *ValidationError) Unwrap() error { return ErrInvalid }
func invalid(field, message string) error {
	return &ValidationError{Fields: map[string]string{field: message}}
}

type Translation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Language  string `json:"language"`
	License   string `json:"license"`
	CanImport bool   `json:"canImport"`
}

var catalog = []Translation{{"web", "World English Bible", "English", "Public domain", true}, {"webbe", "World English Bible, British Edition", "English (UK)", "Public domain", true}, {"asv", "American Standard Version (1901)", "English", "Public domain", true}}

func Translations() []Translation { return append([]Translation{}, catalog...) }
func findTranslation(id string) (Translation, bool) {
	for _, t := range catalog {
		if t.ID == id {
			return t, true
		}
	}
	return Translation{}, false
}

type Query struct{ Reference, Translation string }
type Verse struct {
	BookID   string `json:"bookId"`
	BookName string `json:"bookName"`
	Chapter  int    `json:"chapter"`
	Number   int    `json:"number"`
	Text     string `json:"text"`
}
type Passage struct {
	Reference   string      `json:"reference"`
	Translation Translation `json:"translation"`
	Text        string      `json:"text"`
	Verses      []Verse     `json:"verses"`
	Source      string      `json:"source"`
	SourceURL   string      `json:"sourceUrl"`
}
type Provider interface {
	Lookup(context.Context, Query) (Passage, error)
}
type Gate func(context.Context) (bool, error)

func normalize(q Query) (Query, error) {
	if !utf8.ValidString(q.Reference) || utf8.RuneCountInString(q.Reference) > 128 {
		return q, invalid("reference", "Use a short Bible reference, such as John 3:16 or Psalm 23.")
	}
	digits := false
	for _, r := range q.Reference {
		if unicode.IsControl(r) || !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" .,:;'-", r)) {
			return q, invalid("reference", "Use an English book name, chapter and optional verse range.")
		}
		if r >= '0' && r <= '9' {
			digits = true
		}
	}
	q.Reference = strings.Join(strings.Fields(q.Reference), " ")
	if q.Reference == "" || !digits {
		return q, invalid("reference", "Include a chapter number, such as John 3:16 or Psalm 23.")
	}
	q.Translation = strings.ToLower(strings.TrimSpace(q.Translation))
	if q.Translation == "" {
		q.Translation = "web"
	}
	if _, ok := findTranslation(q.Translation); !ok {
		return q, invalid("translation", "Choose a supported translation.")
	}
	return q, nil
}
func clone(p Passage) Passage { p.Verses = append([]Verse(nil), p.Verses...); return p }
