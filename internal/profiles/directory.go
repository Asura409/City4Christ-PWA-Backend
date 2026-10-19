package profiles

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidDirectory = errors.New("invalid directory options")

type DirectoryValidationError struct{ Fields map[string]string }

func (e *DirectoryValidationError) Error() string { return ErrInvalidDirectory.Error() }
func (e *DirectoryValidationError) Unwrap() error { return ErrInvalidDirectory }
func invalidDirectory(field, message string) error {
	return &DirectoryValidationError{Fields: map[string]string{field: message}}
}

// DirectoryMember intentionally excludes private account/security fields.
type DirectoryMember struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}
type DirectoryOptions struct {
	Limit         int
	Query, Cursor string
}
type DirectoryCursor struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Query   string `json:"query"`
}
type DirectoryPage struct {
	Items      []DirectoryMember
	NextCursor string
}

func (s *Service) Directory(ctx context.Context, actor string, o DirectoryOptions) (DirectoryPage, error) {
	if _, err := s.Get(ctx, actor); err != nil {
		return DirectoryPage{}, err
	}
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 {
		return DirectoryPage{}, invalidDirectory("limit", "Use an integer from 1 to 100.")
	}
	if len(o.Cursor) > 2048 {
		return DirectoryPage{}, invalidDirectory("cursor", "Use a nextCursor returned by the directory.")
	}
	if !utf8.ValidString(o.Query) || utf8.RuneCountInString(o.Query) > 200 || strings.ContainsFunc(o.Query, unicode.IsControl) {
		return DirectoryPage{}, invalidDirectory("query", "Search names with up to 200 characters and no control characters.")
	}

	o.Query = strings.TrimSpace(o.Query)
	var after *DirectoryCursor
	if o.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		if err != nil {
			return DirectoryPage{}, invalidDirectory("cursor", "Use a nextCursor from the same name search.")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&after) != nil || after == nil || decoder.Decode(new(any)) != io.EOF || after.Version != 1 || !validActor(after.ID) || after.Query != o.Query {
			return DirectoryPage{}, invalidDirectory("cursor", "Use a nextCursor from the same name search.")
		}
	}
	items, err := s.repo.ListDirectory(ctx, o, after)
	if err != nil {
		return DirectoryPage{}, err
	}
	page := DirectoryPage{Items: items}
	if len(items) > o.Limit {
		page.Items = items[:o.Limit]
		raw, err := json.Marshal(DirectoryCursor{Version: 1, ID: page.Items[len(page.Items)-1].ID, Query: o.Query})
		if err != nil {
			return DirectoryPage{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

// ListDirectory filters privacy in SQL on every page, independently of cursors.
// UUID ordering remains stable when members rename themselves.
func (r *Repository) ListDirectory(ctx context.Context, o DirectoryOptions, after *DirectoryCursor) ([]DirectoryMember, error) {
	var id *string
	if after != nil {
		id = &after.ID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,first_name,last_name FROM accounts
 WHERE directory_visible AND email_verified_at IS NOT NULL
 AND ($1='' OR strpos(lower(first_name||' '||last_name),lower($1))>0)
 AND ($2::uuid IS NULL OR id>$2::uuid)
 ORDER BY id ASC LIMIT $3`, o.Query, id, o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DirectoryMember{}
	for rows.Next() {
		var member DirectoryMember
		if err = rows.Scan(&member.ID, &member.FirstName, &member.LastName); err != nil {
			return nil, err
		}
		items = append(items, member)
	}
	return items, rows.Err()
}
