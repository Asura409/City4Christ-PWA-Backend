package profiles

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "invalid profile fields" }

type Service struct {
	repo   *Repository
	signer media.DownloadSigner
}

func NewService(repo *Repository, signers ...media.DownloadSigner) *Service {
	var signer media.DownloadSigner
	if len(signers) > 0 {
		signer = signers[0]
	}
	return &Service{repo: repo, signer: signer}
}
func validActor(actor string) bool {
	id, err := uuid.Parse(actor)
	return err == nil && id != uuid.Nil && id.String() == actor
}
func (s *Service) Get(ctx context.Context, actor string) (Profile, error) {
	if !validActor(actor) {
		return Profile{}, ErrNotFound
	}
	p, err := s.repo.Get(ctx, actor)
	if err != nil {
		return Profile{}, err
	}
	return s.signProfileAvatar(ctx, p)
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
	p, err := s.repo.Update(ctx, actor, in)
	if err != nil {
		return Profile{}, err
	}
	return s.signProfileAvatar(ctx, p)
}

func (s *Service) SetAvatar(ctx context.Context, actor, uploadID string) (Profile, error) {
	if !validActor(actor) || !validActor(uploadID) {
		return Profile{}, ErrInvalidAvatarUpload
	}
	p, err := s.repo.SetAvatar(ctx, actor, uploadID)
	if err != nil {
		return Profile{}, err
	}
	return s.signProfileAvatar(ctx, p)
}

func (s *Service) RemoveAvatar(ctx context.Context, actor string) (Profile, error) {
	if !validActor(actor) {
		return Profile{}, ErrNotFound
	}
	p, err := s.repo.RemoveAvatar(ctx, actor)
	if err != nil {
		return Profile{}, err
	}
	return s.signProfileAvatar(ctx, p)
}

func (s *Service) signProfileAvatar(ctx context.Context, profile Profile) (Profile, error) {
	if !profile.avatarUploadID.Valid {
		return profile, nil
	}
	item, err := s.repo.avatar(ctx, profile.avatarUploadID.String)
	if err != nil {
		return Profile{}, err
	}
	if item == nil || s.signer == nil {
		return profile, nil
	}
	signed, err := media.SignAttachments(ctx, s.signer, []media.Attachment{*item})
	if err != nil {
		return Profile{}, err
	}
	profile.Avatar = &signed[0]
	return profile, nil
}
