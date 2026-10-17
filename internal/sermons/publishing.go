package sermons

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrForbidden       = errors.New("sermon editor permission required")
	ErrInvalidInput    = errors.New("invalid sermon input")
	ErrVersionConflict = errors.New("sermon version conflict")
)

type DraftInput struct {
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Speaker    string    `json:"speaker"`
	Series     string    `json:"series"`
	Topic      string    `json:"topic"`
	PreachedAt time.Time `json:"preachedAt"`
	VideoURL   string    `json:"videoUrl"`
}
type EditorialSermon struct {
	ID string `json:"id"`
	DraftInput
	PublishedAt *time.Time `json:"publishedAt"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
}
type UpdateInput struct {
	Version    int        `json:"version"`
	Title      *string    `json:"title"`
	Summary    *string    `json:"summary"`
	Speaker    *string    `json:"speaker"`
	Series     *string    `json:"series"`
	Topic      *string    `json:"topic"`
	PreachedAt *time.Time `json:"preachedAt"`
	VideoURL   *string    `json:"videoUrl"`
	Status     *string    `json:"status"`
	PublishAt  *time.Time `json:"publishAt"`
}

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func normalizeVideo(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", ErrInvalidInput
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return "", ErrInvalidInput
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", ErrInvalidInput
	}
	var id string
	switch strings.ToLower(u.Host) {
	case "youtube.com", "www.youtube.com":
		if u.Path != "/watch" || len(q["v"]) != 1 {
			return "", ErrInvalidInput
		}
		id = q.Get("v")
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	default:
		return "", ErrInvalidInput
	}
	if !videoID.MatchString(id) {
		return "", ErrInvalidInput
	}
	return "https://www.youtube.com/watch?v=" + id, nil
}
func normalizeDraft(input DraftInput) (DraftInput, error) {
	for _, f := range []struct {
		value    *string
		min, max int
	}{{&input.Title, 1, 200}, {&input.Summary, 0, 10000}, {&input.Speaker, 1, 200}, {&input.Series, 0, 200}, {&input.Topic, 0, 100}} {
		*f.value = strings.TrimSpace(*f.value)
		n := utf8.RuneCountInString(*f.value)
		if !utf8.ValidString(*f.value) || strings.ContainsRune(*f.value, 0) || n < f.min || n > f.max {
			return DraftInput{}, ErrInvalidInput
		}
	}
	if !validDate(input.PreachedAt) {
		return DraftInput{}, ErrInvalidInput
	}
	input.PreachedAt = input.PreachedAt.UTC().Truncate(time.Microsecond)
	var err error
	input.VideoURL, err = normalizeVideo(input.VideoURL)
	return input, err
}
func validDate(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func (s *Service) CanEdit(ctx context.Context, actorID string) error {
	return s.repo.canEdit(ctx, actorID)
}
func (s *Service) CreateDraft(ctx context.Context, actorID string, input DraftInput) (EditorialSermon, error) {
	input, err := normalizeDraft(input)
	if err != nil {
		return EditorialSermon{}, err
	}
	return s.repo.createDraft(ctx, actorID, input, s.now().UTC())
}
func (s *Service) GetEditorial(ctx context.Context, actorID, id string) (EditorialSermon, error) {
	if !canonicalID(id) {
		return EditorialSermon{}, ErrNotFound
	}
	return s.repo.getEditorial(ctx, actorID, id, s.now().UTC())
}
func (s *Service) Update(ctx context.Context, actorID, id string, input UpdateInput) (EditorialSermon, error) {
	if !canonicalID(id) {
		return EditorialSermon{}, ErrNotFound
	}
	if input.Version < 1 {
		return EditorialSermon{}, ErrInvalidInput
	}
	if input.Title == nil && input.Summary == nil && input.Speaker == nil && input.Series == nil && input.Topic == nil && input.PreachedAt == nil && input.VideoURL == nil && input.Status == nil {
		return EditorialSermon{}, ErrInvalidInput
	}
	now := s.now().UTC()
	if input.PublishAt != nil {
		at := input.PublishAt.UTC().Truncate(time.Microsecond)
		input.PublishAt = &at
	}
	if input.PublishAt != nil && (input.Status == nil || *input.Status != "scheduled") {
		return EditorialSermon{}, ErrInvalidInput
	}
	if input.Status != nil {
		switch *input.Status {
		case "draft", "published":
		case "scheduled":
			if input.PublishAt == nil || !validDate(*input.PublishAt) || !input.PublishAt.After(now) {
				return EditorialSermon{}, ErrInvalidInput
			}
		default:
			return EditorialSermon{}, ErrInvalidInput
		}
	}
	return s.repo.update(ctx, actorID, id, input, now)
}
func editorialStatus(at *time.Time, now time.Time) string {
	if at == nil {
		return "draft"
	}
	if at.After(now) {
		return "scheduled"
	}
	return "published"
}
