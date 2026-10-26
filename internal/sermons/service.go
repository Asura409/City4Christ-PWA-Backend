package sermons

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

var ErrInvalidOptions = errors.New("invalid sermon catalog options")

type Page struct {
	Items      []Sermon
	NextCursor string
}
type Service struct {
	repo   *Repository
	now    func() time.Time
	signer media.DownloadSigner
}

func NewService(repo *Repository, now func() time.Time, signers ...media.DownloadSigner) *Service {
	if now == nil {
		now = time.Now
	}
	s := &Service{repo: repo, now: now}
	if len(signers) > 0 {
		s.signer = signers[0]
	}
	return s
}
func (s *Service) Get(ctx context.Context, id string) (Sermon, error) {
	if !canonicalID(id) {
		return Sermon{}, ErrNotFound
	}
	item, err := s.repo.Get(ctx, id, s.now())
	if err != nil {
		return Sermon{}, err
	}
	item.Attachments, err = media.SignAttachments(ctx, s.signer, item.Attachments)
	return item, err
}
func canonicalID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}
func (s *Service) List(ctx context.Context, o Options) (Page, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 || len(o.Cursor) > 2048 {
		return Page{}, ErrInvalidOptions
	}
	for _, field := range []struct {
		value *string
		max   int
	}{{&o.Query, 200}, {&o.Speaker, 200}, {&o.Series, 200}, {&o.Topic, 100}} {
		if !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.max {
			return Page{}, ErrInvalidOptions
		}
		*field.value = strings.TrimSpace(*field.value)
	}
	var after *Cursor
	if o.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		if err != nil {
			return Page{}, ErrInvalidOptions
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var c *Cursor
		if decoder.Decode(&c) != nil || c == nil || decoder.Decode(new(any)) != io.EOF || c.PublishedAt.IsZero() || !canonicalID(c.ID) {
			return Page{}, ErrInvalidOptions
		}
		after = c
	}
	items, err := s.repo.List(ctx, o, after, s.now())
	if err != nil {
		return Page{}, err
	}
	for i := range items {
		items[i].Attachments, err = media.SignAttachments(ctx, s.signer, items[i].Attachments)
		if err != nil {
			return Page{}, err
		}
	}
	page := Page{Items: items}
	if len(items) > o.Limit {
		page.Items = items[:o.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(Cursor{PublishedAt: last.PublishedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
