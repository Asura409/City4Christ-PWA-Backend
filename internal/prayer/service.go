package prayer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrForbidden = errors.New("prayer request action forbidden")
	ErrInvalid   = errors.New("invalid prayer request")
	ErrConflict  = errors.New("prayer request state conflict")
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func (s *Service) Create(ctx context.Context, actor string, input CreateInput) (Request, error) {
	input.Body = strings.TrimSpace(input.Body)
	if !validID(actor) || !utf8.ValidString(input.Body) || len([]rune(input.Body)) < 1 || len([]rune(input.Body)) > 3000 || strings.ContainsRune(input.Body, 0) {
		return Request{}, ErrInvalid
	}
	switch input.Visibility {
	case "community", "private":
		if input.GroupID != "" {
			return Request{}, ErrInvalid
		}
	case "group":
		if !validID(input.GroupID) {
			return Request{}, ErrInvalid
		}
		active, err := s.repo.IsActiveGroupMember(ctx, actor, input.GroupID)
		if err != nil {
			return Request{}, err
		}
		if !active {
			return Request{}, ErrForbidden
		}
	default:
		return Request{}, ErrInvalid
	}
	return s.repo.Create(ctx, actor, input)
}

func (s *Service) List(ctx context.Context, actor string, options Options) (Page, error) {
	if !validID(actor) {
		return Page{}, ErrForbidden
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 || len(options.Cursor) > 2048 {
		return Page{}, ErrInvalid
	}
	if options.Status == "" {
		options.Status = "open"
	}
	if options.Status != "open" && options.Status != "answered" {
		return Page{}, ErrInvalid
	}
	var after *Cursor
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil {
			return Page{}, ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor *Cursor
		if decoder.Decode(&cursor) != nil || cursor == nil || decoder.Decode(new(any)) != io.EOF || cursor.CreatedAt.IsZero() || !validID(cursor.ID) {
			return Page{}, ErrInvalid
		}
		after = cursor
	}
	items, err := s.repo.List(ctx, actor, options, after)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, actor, requestID string) (Request, error) {
	if !validID(actor) {
		return Request{}, ErrForbidden
	}
	if !validID(requestID) {
		return Request{}, ErrNotFound
	}
	return s.repo.Get(ctx, actor, requestID)
}

func (s *Service) SetStatus(ctx context.Context, actor, requestID, status string) (bool, error) {
	if !validID(actor) {
		return false, ErrForbidden
	}
	if !validID(requestID) {
		return false, ErrNotFound
	}
	if status != "answered" && status != "archived" {
		return false, ErrInvalid
	}
	return s.repo.SetStatus(ctx, actor, requestID, status)
}

func (s *Service) Pray(ctx context.Context, actor, requestID string) (Acknowledgement, error) {
	if !validID(actor) {
		return Acknowledgement{}, ErrForbidden
	}
	if !validID(requestID) {
		return Acknowledgement{}, ErrNotFound
	}
	changed, err := s.repo.Pray(ctx, actor, requestID)
	return Acknowledgement{Changed: changed}, err
}
