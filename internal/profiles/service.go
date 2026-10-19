package profiles

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "invalid profile fields" }

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }
func validActor(actor string) bool {
	id, err := uuid.Parse(actor)
	return err == nil && id != uuid.Nil && id.String() == actor
}
func (s *Service) Get(ctx context.Context, actor string) (Profile, error) {
	if !validActor(actor) {
		return Profile{}, ErrNotFound
	}
	return s.repo.Get(ctx, actor)
}
func (s *Service) Update(ctx context.Context, actor string, in Update) (Profile, error) {
	if !validActor(actor) {
		return Profile{}, ErrNotFound
	}
	fields := map[string]string{}
	if in.FirstName == nil && in.LastName == nil && in.DirectoryVisible == nil {
		fields["body"] = "Supply firstName, lastName or directoryVisible."
	}
	for _, f := range []struct {
		key   string
		value **string
	}{{"firstName", &in.FirstName}, {"lastName", &in.LastName}} {
		if *f.value == nil {
			continue
		}
		raw := **f.value
		normalized := strings.TrimSpace(raw)
		if !utf8.ValidString(raw) || strings.ContainsFunc(raw, unicode.IsControl) || utf8.RuneCountInString(normalized) < 1 || utf8.RuneCountInString(normalized) > 100 {
			fields[f.key] = "Use between 1 and 100 characters without control characters."
		}
		*f.value = &normalized
	}
	if len(fields) > 0 {
		return Profile{}, &ValidationError{Fields: fields}
	}
	return s.repo.Update(ctx, actor, in)
}
