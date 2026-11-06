package messaging

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

const maxMessageRunes = 4000
const maxPageSize = 100
const DirectMessageMutationWindow = 10 * time.Minute

var (
	ErrForbidden             = errors.New("messaging action forbidden")
	ErrNotFound              = errors.New("conversation not found")
	ErrInvalid               = errors.New("invalid messaging input")
	ErrIdempotencyConflict   = errors.New("message idempotency conflict")
	ErrMutationWindowExpired = errors.New("direct message mutation window expired")
)

type Service struct {
	repo           *Repository
	now            func() time.Time
	downloadSigner media.DownloadSigner
}

func NewService(repo *Repository, now func() time.Time, downloadSigners ...media.DownloadSigner) *Service {
	if now == nil {
		now = time.Now
	}
	var signer media.DownloadSigner
	if len(downloadSigners) > 0 {
		signer = downloadSigners[0]
	}
	return &Service{repo: repo, now: now, downloadSigner: signer}
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}
func (s *Service) CreateConversation(ctx context.Context, actor, recipient string) (Conversation, bool, error) {
	if !validID(actor) || !validID(recipient) || actor == recipient {
		return Conversation{}, false, ErrInvalid
	}
	c, created, err := s.repo.CreateConversation(ctx, actor, recipient, s.now())
	if err != nil {
		return Conversation{}, false, err
	}
	c, err = s.signPartnerAvatar(ctx, c)
	return c, created, err
}
func (s *Service) ListConversations(ctx context.Context, actor string) ([]Conversation, error) {
	if !validID(actor) {
		return nil, ErrInvalid
	}
	items, err := s.repo.ListConversations(ctx, actor)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i], err = s.signPartnerAvatar(ctx, items[i])
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) signPartnerAvatar(ctx context.Context, c Conversation) (Conversation, error) {
	if c.partnerAvatar == nil {
		return c, nil
	}
	signed, err := media.SignAttachments(ctx, s.downloadSigner, []media.Attachment{*c.partnerAvatar})
	if err != nil {
		return Conversation{}, err
	}
	c.PartnerAvatar = &signed[0]
	return c, nil
}
func (s *Service) SendMessage(ctx context.Context, actor, conversationID, clientMessageID, body string) (Message, bool, error) {
	return s.SendMessageWithAttachments(ctx, actor, conversationID, clientMessageID, body, nil)
}

func (s *Service) SendMessageWithAttachments(ctx context.Context, actor, conversationID, clientMessageID, body string, attachmentIDs []string) (Message, bool, error) {
	if !validID(actor) || !validID(conversationID) || !validID(clientMessageID) || !utf8.ValidString(body) {
		return Message{}, false, ErrInvalid
	}
	if media.ValidateAttachmentIDs(attachmentIDs, media.MaxMessageAttachments) != nil {
		return Message{}, false, ErrInvalid
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxMessageRunes || strings.ContainsRune(body, 0) || strings.ContainsFunc(body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return Message{}, false, ErrInvalid
	}
	message, duplicate, err := s.repo.SendMessageWithAttachments(ctx, conversationID, actor, clientMessageID, body, attachmentIDs, s.now())
	if err != nil {
		return Message{}, false, err
	}
	items := []Message{message}
	if err := s.hydrateDirectMessages(ctx, items); err != nil {
		return Message{}, false, err
	}
	message = items[0]
	return message, duplicate, nil
}

func (s *Service) EditOwnMessage(ctx context.Context, actor, conversationID, messageID, body string) (Message, error) {
	if !validID(actor) || !validID(conversationID) || !validID(messageID) || !utf8.ValidString(body) {
		return Message{}, ErrInvalid
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxMessageRunes || strings.ContainsRune(body, 0) || strings.ContainsFunc(body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return Message{}, ErrInvalid
	}
	message, err := s.repo.EditOwnMessage(ctx, actor, conversationID, messageID, body, s.now())
	if err != nil {
		return Message{}, err
	}
	items := []Message{message}
	if err := s.hydrateDirectMessages(ctx, items); err != nil {
		return Message{}, err
	}
	return items[0], nil
}

func (s *Service) DeleteOwnMessage(ctx context.Context, actor, conversationID, messageID string) (bool, error) {
	if !validID(actor) || !validID(conversationID) || !validID(messageID) {
		return false, ErrInvalid
	}
	return s.repo.DeleteOwnMessage(ctx, actor, conversationID, messageID, s.now())
}
func (s *Service) ListMessages(ctx context.Context, actor, conversationID string, beforeSequence int64, limit int) (MessagePage, error) {
	return s.listMessages(ctx, actor, conversationID, "", beforeSequence, limit)
}

func (s *Service) SearchMessages(ctx context.Context, actor, conversationID, query string, beforeSequence int64, limit int) (MessagePage, error) {
	query = strings.TrimSpace(query)
	if !validSearchQuery(query) {
		return MessagePage{}, ErrInvalid
	}
	return s.listMessages(ctx, actor, conversationID, query, beforeSequence, limit)
}

func (s *Service) listMessages(ctx context.Context, actor, conversationID, query string, beforeSequence int64, limit int) (MessagePage, error) {
	if !validID(actor) || !validID(conversationID) || beforeSequence < 0 {
		return MessagePage{}, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > maxPageSize {
		return MessagePage{}, ErrInvalid
	}
	page, err := s.repo.SearchMessages(ctx, conversationID, actor, query, beforeSequence, limit)
	if err != nil {
		return MessagePage{}, err
	}
	if err := s.hydrateDirectMessages(ctx, page.Items); err != nil {
		return MessagePage{}, err
	}
	return page, nil
}

func validSearchQuery(query string) bool {
	length := utf8.RuneCountInString(query)
	return length >= 2 && length <= 120 && !strings.ContainsRune(query, 0) && !strings.ContainsFunc(query, unicode.IsControl)
}

func (s *Service) hydrateDirectMessages(ctx context.Context, messages []Message) error {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	attachments, err := s.repo.directMessageAttachments(ctx, ids)
	if err != nil {
		return err
	}
	for i := range messages {
		items := attachments[messages[i].ID]
		signed, err := media.SignAttachments(ctx, s.downloadSigner, items)
		if err != nil {
			return err
		}
		messages[i].Attachments = signed
		messages[i].AttachmentCount = len(signed)
	}
	return nil
}
func (s *Service) MarkRead(ctx context.Context, actor, conversationID, throughMessageID string) (ReadReceipt, error) {
	if !validID(actor) || !validID(conversationID) || (throughMessageID != "" && !validID(throughMessageID)) {
		return ReadReceipt{}, ErrInvalid
	}
	return s.repo.MarkRead(ctx, conversationID, actor, throughMessageID, s.now())
}

func (s *Service) ListGroupConversations(ctx context.Context, actor string) ([]GroupConversation, error) {
	if !validID(actor) {
		return nil, ErrInvalid
	}
	return s.repo.ListGroupConversations(ctx, actor)
}

func (s *Service) SendGroupMessage(ctx context.Context, actor, groupID, clientMessageID, body string) (GroupMessage, bool, error) {
	return s.SendGroupMessageWithAttachments(ctx, actor, groupID, clientMessageID, body, nil)
}

func (s *Service) SendGroupMessageWithAttachments(ctx context.Context, actor, groupID, clientMessageID, body string, attachmentIDs []string) (GroupMessage, bool, error) {
	if !validID(actor) || !validID(groupID) || !validID(clientMessageID) || !utf8.ValidString(body) {
		return GroupMessage{}, false, ErrInvalid
	}
	if media.ValidateAttachmentIDs(attachmentIDs, media.MaxMessageAttachments) != nil {
		return GroupMessage{}, false, ErrInvalid
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxMessageRunes || strings.ContainsRune(body, 0) || strings.ContainsFunc(body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return GroupMessage{}, false, ErrInvalid
	}
	message, duplicate, err := s.repo.SendGroupMessageWithAttachments(ctx, groupID, actor, clientMessageID, body, attachmentIDs, s.now())
	if err != nil {
		return GroupMessage{}, false, err
	}
	items := []GroupMessage{message}
	if err := s.hydrateGroupMessages(ctx, items); err != nil {
		return GroupMessage{}, false, err
	}
	message = items[0]
	return message, duplicate, nil
}

func (s *Service) ListGroupMessages(ctx context.Context, actor, groupID string, beforeSequence int64, limit int) (GroupMessagePage, error) {
	return s.listGroupMessages(ctx, actor, groupID, "", beforeSequence, limit)
}

func (s *Service) SearchGroupMessages(ctx context.Context, actor, groupID, query string, beforeSequence int64, limit int) (GroupMessagePage, error) {
	query = strings.TrimSpace(query)
	if !validSearchQuery(query) {
		return GroupMessagePage{}, ErrInvalid
	}
	return s.listGroupMessages(ctx, actor, groupID, query, beforeSequence, limit)
}

func (s *Service) listGroupMessages(ctx context.Context, actor, groupID, query string, beforeSequence int64, limit int) (GroupMessagePage, error) {
	if !validID(actor) || !validID(groupID) || beforeSequence < 0 {
		return GroupMessagePage{}, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > maxPageSize {
		return GroupMessagePage{}, ErrInvalid
	}
	page, err := s.repo.SearchGroupMessages(ctx, groupID, actor, query, beforeSequence, limit)
	if err != nil {
		return GroupMessagePage{}, err
	}
	if err := s.hydrateGroupMessages(ctx, page.Items); err != nil {
		return GroupMessagePage{}, err
	}
	return page, nil
}

func (s *Service) hydrateGroupMessages(ctx context.Context, messages []GroupMessage) error {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	attachments, err := s.repo.groupMessageAttachments(ctx, ids)
	if err != nil {
		return err
	}
	authors, err := s.repo.groupMessageAuthors(ctx, ids)
	if err != nil {
		return err
	}
	for i := range messages {
		author := authors[messages[i].ID]
		messages[i].SenderFirstName, messages[i].SenderLastName, messages[i].senderAvatar = author.SenderFirstName, author.SenderLastName, author.senderAvatar
		if messages[i].senderAvatar != nil {
			signed, e := media.SignAttachments(ctx, s.downloadSigner, []media.Attachment{*messages[i].senderAvatar})
			if e != nil {
				return e
			}
			messages[i].SenderAvatar = &signed[0]
		}
	}
	for i := range messages {
		items := attachments[messages[i].ID]
		if messages[i].Deleted {
			items = nil
		}
		signed, err := media.SignAttachments(ctx, s.downloadSigner, items)
		if err != nil {
			return err
		}
		messages[i].Attachments = signed
		messages[i].AttachmentCount = len(signed)
	}
	return nil
}

func (s *Service) MarkGroupRead(ctx context.Context, actor, groupID, throughMessageID string) (GroupReadReceipt, error) {
	if !validID(actor) || !validID(groupID) || (throughMessageID != "" && !validID(throughMessageID)) {
		return GroupReadReceipt{}, ErrInvalid
	}
	return s.repo.MarkGroupRead(ctx, groupID, actor, throughMessageID, s.now())
}

func (s *Service) DeleteGroupMessage(ctx context.Context, actor, groupID, messageID string) (bool, error) {
	if !validID(actor) || !validID(groupID) || !validID(messageID) {
		return false, ErrInvalid
	}
	return s.repo.DeleteGroupMessage(ctx, groupID, actor, messageID, s.now())
}
