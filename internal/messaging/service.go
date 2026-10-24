package messaging

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxMessageRunes = 4000
const maxPageSize = 100

var (
	ErrForbidden           = errors.New("messaging action forbidden")
	ErrNotFound            = errors.New("conversation not found")
	ErrInvalid             = errors.New("invalid messaging input")
	ErrIdempotencyConflict = errors.New("message idempotency conflict")
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}
func (s *Service) CreateConversation(ctx context.Context, actor, recipient string) (Conversation, bool, error) {
	if !validID(actor) || !validID(recipient) || actor == recipient {
		return Conversation{}, false, ErrInvalid
	}
	return s.repo.CreateConversation(ctx, actor, recipient, s.now())
}
func (s *Service) ListConversations(ctx context.Context, actor string) ([]Conversation, error) {
	if !validID(actor) {
		return nil, ErrInvalid
	}
	return s.repo.ListConversations(ctx, actor)
}
func (s *Service) SendMessage(ctx context.Context, actor, conversationID, clientMessageID, body string) (Message, bool, error) {
	if !validID(actor) || !validID(conversationID) || !validID(clientMessageID) || !utf8.ValidString(body) {
		return Message{}, false, ErrInvalid
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxMessageRunes || strings.ContainsRune(body, 0) || strings.ContainsFunc(body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return Message{}, false, ErrInvalid
	}
	return s.repo.SendMessage(ctx, conversationID, actor, clientMessageID, body, s.now())
}
func (s *Service) ListMessages(ctx context.Context, actor, conversationID string, beforeSequence int64, limit int) (MessagePage, error) {
	if !validID(actor) || !validID(conversationID) || beforeSequence < 0 {
		return MessagePage{}, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > maxPageSize {
		return MessagePage{}, ErrInvalid
	}
	return s.repo.ListMessages(ctx, conversationID, actor, beforeSequence, limit)
}
func (s *Service) MarkRead(ctx context.Context, actor, conversationID, throughMessageID string) (ReadReceipt, error) {
	if !validID(actor) || !validID(conversationID) || (throughMessageID != "" && !validID(throughMessageID)) {
		return ReadReceipt{}, ErrInvalid
	}
	return s.repo.MarkRead(ctx, conversationID, actor, throughMessageID, s.now())
}
