// Package messaging implements participant-only, durable one-to-one conversations.
package messaging

import (
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

type Conversation struct {
	ID                  string            `json:"id"`
	PartnerID           string            `json:"partnerId"`
	PartnerFirstName    string            `json:"partnerFirstName"`
	PartnerLastName     string            `json:"partnerLastName"`
	LastMessage         *Message          `json:"lastMessage,omitempty"`
	UnreadCount         int               `json:"unreadCount"`
	MyLastReadSequence  int64             `json:"myLastReadSequence"`
	PartnerReadSequence int64             `json:"partnerReadSequence"`
	CreatedAt           time.Time         `json:"createdAt"`
	PartnerAvatar       *media.Attachment `json:"partnerAvatar,omitempty"`
	partnerAvatar       *media.Attachment
}

type Message struct {
	ID              string             `json:"id"`
	ConversationID  string             `json:"conversationId"`
	SenderID        string             `json:"senderId"`
	ClientMessageID string             `json:"clientMessageId"`
	Body            string             `json:"body"`
	Attachments     []media.Attachment `json:"attachments,omitempty"`
	AttachmentCount int                `json:"attachmentCount,omitempty"`
	Sequence        int64              `json:"sequence"`
	CreatedAt       time.Time          `json:"createdAt"`
	EditedAt        *time.Time         `json:"editedAt,omitempty"`
	Deleted         bool               `json:"deleted,omitempty"`
}

type MessagePage struct {
	Items              []Message `json:"items"`
	NextBeforeSequence int64     `json:"nextBeforeSequence,omitempty"`
}

type ReadReceipt struct {
	ConversationID   string     `json:"conversationId"`
	LastReadSequence int64      `json:"lastReadSequence"`
	LastReadAt       *time.Time `json:"lastReadAt,omitempty"`
}

type SendResult struct {
	Message   Message `json:"message"`
	Duplicate bool    `json:"duplicate"`
}

type GroupConversation struct {
	GroupID            string        `json:"groupId"`
	GroupName          string        `json:"groupName"`
	Audience           string        `json:"audience"`
	LastMessage        *GroupMessage `json:"lastMessage,omitempty"`
	UnreadCount        int           `json:"unreadCount"`
	MyLastReadSequence int64         `json:"myLastReadSequence"`
	CanModerate        bool          `json:"canModerate"`
}

type GroupMessage struct {
	ID              string             `json:"id"`
	GroupID         string             `json:"groupId"`
	SenderID        string             `json:"senderId"`
	ClientMessageID string             `json:"clientMessageId"`
	Body            string             `json:"body"`
	Attachments     []media.Attachment `json:"attachments,omitempty"`
	AttachmentCount int                `json:"attachmentCount,omitempty"`
	Sequence        int64              `json:"sequence"`
	CreatedAt       time.Time          `json:"createdAt"`
	Deleted         bool               `json:"deleted"`
	SenderFirstName string             `json:"senderFirstName,omitempty"`
	SenderLastName  string             `json:"senderLastName,omitempty"`
	SenderAvatar    *media.Attachment  `json:"senderAvatar,omitempty"`
	senderAvatar    *media.Attachment
}

type GroupMessagePage struct {
	Items              []GroupMessage `json:"items"`
	NextBeforeSequence int64          `json:"nextBeforeSequence,omitempty"`
}

type GroupReadReceipt struct {
	GroupID          string     `json:"groupId"`
	LastReadSequence int64      `json:"lastReadSequence"`
	LastReadAt       *time.Time `json:"lastReadAt,omitempty"`
}
