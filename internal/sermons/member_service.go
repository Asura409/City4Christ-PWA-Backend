package sermons

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidMemberInput = errors.New("invalid sermon member input")

type MemberService struct {
	repo *Repository
	now  func() time.Time
}

func NewMemberService(repo *Repository, now func() time.Time) *MemberService {
	if now == nil {
		now = time.Now
	}
	return &MemberService{repo: repo, now: now}
}

func (s *MemberService) GetProgress(ctx context.Context, accountID, sermonID string) (Progress, error) {
	if !canonicalID(accountID) || !canonicalID(sermonID) {
		return Progress{}, ErrNotFound
	}
	return s.repo.GetProgress(ctx, accountID, sermonID, s.now())
}

func (s *MemberService) SaveProgress(ctx context.Context, accountID, sermonID string, position int) (Progress, error) {
	if !canonicalID(accountID) || !canonicalID(sermonID) {
		return Progress{}, ErrNotFound
	}
	if position < 0 || position > 86400 {
		return Progress{}, ErrInvalidMemberInput
	}
	return s.repo.SaveProgress(ctx, accountID, sermonID, position, s.now())
}

func (s *MemberService) GetNote(ctx context.Context, accountID, sermonID string) (*Note, error) {
	if !canonicalID(accountID) || !canonicalID(sermonID) {
		return nil, ErrNotFound
	}
	return s.repo.GetNote(ctx, accountID, sermonID, s.now())
}

func (s *MemberService) SaveNote(ctx context.Context, accountID, sermonID, body string) (Note, error) {
	body = strings.TrimSpace(body)
	if !canonicalID(accountID) || !canonicalID(sermonID) {
		return Note{}, ErrNotFound
	}
	if !utf8.ValidString(body) || utf8.RuneCountInString(body) == 0 || utf8.RuneCountInString(body) > 10000 {
		return Note{}, ErrInvalidMemberInput
	}
	return s.repo.SaveNote(ctx, accountID, sermonID, body, s.now())
}

func (s *MemberService) DeleteNote(ctx context.Context, accountID, sermonID string) error {
	if !canonicalID(accountID) || !canonicalID(sermonID) {
		return ErrNotFound
	}
	return s.repo.DeleteNote(ctx, accountID, sermonID, s.now())
}
