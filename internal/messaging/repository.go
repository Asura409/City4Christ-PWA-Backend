package messaging

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateConversation(ctx context.Context, actor, recipient string, now time.Time) (Conversation, bool, error) {
	low, high := actor, recipient
	if high < low {
		low, high = high, low
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Conversation{}, false, err
	}
	defer tx.Rollback()
	var actorVerified, recipientVerified bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false),COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$2),false)`, actor, recipient).Scan(&actorVerified, &recipientVerified); err != nil {
		return Conversation{}, false, err
	}
	if !actorVerified {
		return Conversation{}, false, ErrForbidden
	}
	if !recipientVerified {
		return Conversation{}, false, ErrNotFound
	}
	id := uuid.NewString()
	created := true
	err = tx.QueryRowContext(ctx, `INSERT INTO direct_conversations(id,participant_low,participant_high,created_at) VALUES($1,$2,$3,$4) ON CONFLICT(participant_low,participant_high) DO NOTHING RETURNING id`, id, low, high, now.UTC()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = tx.QueryRowContext(ctx, `SELECT id FROM direct_conversations WHERE participant_low=$1 AND participant_high=$2`, low, high).Scan(&id)
	}
	if err != nil {
		return Conversation{}, false, err
	}
	if created {
		for _, member := range []string{low, high} {
			if _, err = tx.ExecContext(ctx, `INSERT INTO direct_conversation_members(conversation_id,account_id,joined_at) VALUES($1,$2,$3)`, id, member, now.UTC()); err != nil {
				return Conversation{}, false, err
			}
		}
	}
	conversation, err := conversationFor(ctx, tx, id, actor)
	if err != nil {
		return Conversation{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Conversation{}, false, err
	}
	return conversation, created, nil
}

func (r *Repository) ListConversations(ctx context.Context, actor string) ([]Conversation, error) {
	var verified bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,other.id,other.first_name,other.last_name,c.created_at,me.last_read_sequence,peer.last_read_sequence,
 COALESCE(unread.count,0),last.id,last.conversation_id,last.sender_id,last.client_message_id,last.body,last.sequence,last.created_at
 FROM direct_conversations c
 JOIN direct_conversation_members me ON me.conversation_id=c.id AND me.account_id=$1
 JOIN accounts self ON self.id=me.account_id AND self.email_verified_at IS NOT NULL
 JOIN direct_conversation_members peer ON peer.conversation_id=c.id AND peer.account_id<>$1
 JOIN accounts other ON other.id=peer.account_id AND other.email_verified_at IS NOT NULL
 LEFT JOIN LATERAL (SELECT count(*)::int AS count FROM direct_messages m WHERE m.conversation_id=c.id AND m.sender_id<>$1 AND m.sequence>me.last_read_sequence) unread ON true
 LEFT JOIN LATERAL (SELECT id,conversation_id,sender_id,client_message_id,body,sequence,created_at FROM direct_messages m WHERE m.conversation_id=c.id ORDER BY sequence DESC LIMIT 1) last ON true
 ORDER BY COALESCE(last.sequence,0) DESC,c.created_at DESC,c.id`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Conversation{}
	for rows.Next() {
		var c Conversation
		var lastID, lastConversationID, lastSenderID, lastClientID, lastBody sql.NullString
		var lastSequence sql.NullInt64
		var lastAt sql.NullTime
		if err = rows.Scan(&c.ID, &c.PartnerID, &c.PartnerFirstName, &c.PartnerLastName, &c.CreatedAt, &c.MyLastReadSequence, &c.PartnerReadSequence, &c.UnreadCount, &lastID, &lastConversationID, &lastSenderID, &lastClientID, &lastBody, &lastSequence, &lastAt); err != nil {
			return nil, err
		}
		if lastID.Valid {
			c.LastMessage = &Message{ID: lastID.String, ConversationID: lastConversationID.String, SenderID: lastSenderID.String, ClientMessageID: lastClientID.String, Body: lastBody.String, Sequence: lastSequence.Int64, CreatedAt: lastAt.Time}
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *Repository) SendMessage(ctx context.Context, conversationID, actor, clientMessageID, body string, now time.Time) (Message, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, false, err
	}
	defer tx.Rollback()
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return Message{}, false, err
	}
	if !verified {
		return Message{}, false, ErrForbidden
	}
	var participant bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM direct_conversation_members WHERE conversation_id=$1 AND account_id=$2)`, conversationID, actor).Scan(&participant); err != nil {
		return Message{}, false, err
	}
	if !participant {
		return Message{}, false, ErrNotFound
	}
	message := Message{ID: uuid.NewString(), ConversationID: conversationID, SenderID: actor, ClientMessageID: clientMessageID, Body: body, CreatedAt: now.UTC()}
	err = tx.QueryRowContext(ctx, `INSERT INTO direct_messages(id,conversation_id,sender_id,client_message_id,body,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(conversation_id,sender_id,client_message_id) DO NOTHING RETURNING sequence`, message.ID, conversationID, actor, clientMessageID, body, message.CreatedAt).Scan(&message.Sequence)
	duplicate := false
	if errors.Is(err, sql.ErrNoRows) {
		duplicate = true
		err = tx.QueryRowContext(ctx, `SELECT id,conversation_id,sender_id,client_message_id,body,sequence,created_at FROM direct_messages WHERE conversation_id=$1 AND sender_id=$2 AND client_message_id=$3`, conversationID, actor, clientMessageID).Scan(&message.ID, &message.ConversationID, &message.SenderID, &message.ClientMessageID, &message.Body, &message.Sequence, &message.CreatedAt)
		if err == nil && message.Body != body {
			return Message{}, false, ErrIdempotencyConflict
		}
	}
	if err != nil {
		return Message{}, false, err
	}
	if !duplicate {
		if err = messageNoticesTx(ctx, tx, message.ID, actor, conversationID, ""); err != nil {
			return Message{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Message{}, false, err
	}
	return message, duplicate, nil
}

func (r *Repository) ListMessages(ctx context.Context, conversationID, actor string, beforeSequence int64, limit int) (MessagePage, error) {
	var allowed bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM direct_conversation_members m JOIN accounts a ON a.id=m.account_id AND a.email_verified_at IS NOT NULL WHERE m.conversation_id=$1 AND m.account_id=$2)`, conversationID, actor).Scan(&allowed); err != nil {
		return MessagePage{}, err
	}
	if !allowed {
		return MessagePage{}, ErrNotFound
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,conversation_id,sender_id,client_message_id,body,sequence,created_at FROM (
 SELECT id,conversation_id,sender_id,client_message_id,body,sequence,created_at FROM direct_messages WHERE conversation_id=$1 AND ($2=0 OR sequence<$2) ORDER BY sequence DESC LIMIT $3
 ) page ORDER BY sequence`, conversationID, beforeSequence, limit+1)
	if err != nil {
		return MessagePage{}, err
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.ClientMessageID, &m.Body, &m.Sequence, &m.CreatedAt); err != nil {
			return MessagePage{}, err
		}
		items = append(items, m)
	}
	if err = rows.Err(); err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{Items: items}
	if len(items) > limit {
		page.NextBeforeSequence = items[1].Sequence
		page.Items = items[1:]
	}
	return page, nil
}

func (r *Repository) MarkRead(ctx context.Context, conversationID, actor, throughMessageID string, now time.Time) (ReadReceipt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReadReceipt{}, err
	}
	defer tx.Rollback()
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return ReadReceipt{}, err
	}
	if !verified {
		return ReadReceipt{}, ErrForbidden
	}
	var participant bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM direct_conversation_members WHERE conversation_id=$1 AND account_id=$2)`, conversationID, actor).Scan(&participant); err != nil {
		return ReadReceipt{}, err
	}
	if !participant {
		return ReadReceipt{}, ErrNotFound
	}
	var target int64
	if throughMessageID == "" {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(sequence),0) FROM direct_messages WHERE conversation_id=$1`, conversationID).Scan(&target); err != nil {
			return ReadReceipt{}, err
		}
	} else {
		err = tx.QueryRowContext(ctx, `SELECT sequence FROM direct_messages WHERE id=$1 AND conversation_id=$2`, throughMessageID, conversationID).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			return ReadReceipt{}, ErrNotFound
		}
		if err != nil {
			return ReadReceipt{}, err
		}
	}
	receipt := ReadReceipt{ConversationID: conversationID}
	var readAt sql.NullTime
	err = tx.QueryRowContext(ctx, `UPDATE direct_conversation_members SET last_read_at=CASE WHEN $3>last_read_sequence THEN $4 ELSE last_read_at END,last_read_sequence=GREATEST(last_read_sequence,$3) WHERE conversation_id=$1 AND account_id=$2 RETURNING last_read_sequence,last_read_at`, conversationID, actor, target, now.UTC()).Scan(&receipt.LastReadSequence, &readAt)
	if readAt.Valid {
		receipt.LastReadAt = &readAt.Time
	}
	if err != nil {
		return ReadReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return ReadReceipt{}, err
	}
	return receipt, nil
}

func conversationFor(ctx context.Context, tx *sql.Tx, id, actor string) (Conversation, error) {
	var c Conversation
	var lastID, lastConversationID, lastSenderID, lastClientID, lastBody sql.NullString
	var lastSequence sql.NullInt64
	var lastAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT c.id,other.id,other.first_name,other.last_name,c.created_at,me.last_read_sequence,peer.last_read_sequence,
 COALESCE(unread.count,0),last.id,last.conversation_id,last.sender_id,last.client_message_id,last.body,last.sequence,last.created_at
 FROM direct_conversations c
 JOIN direct_conversation_members me ON me.conversation_id=c.id AND me.account_id=$2
 JOIN direct_conversation_members peer ON peer.conversation_id=c.id AND peer.account_id<>$2
 JOIN accounts other ON other.id=peer.account_id AND other.email_verified_at IS NOT NULL
 LEFT JOIN LATERAL (SELECT count(*)::int AS count FROM direct_messages m WHERE m.conversation_id=c.id AND m.sender_id<>$2 AND m.sequence>me.last_read_sequence) unread ON true
 LEFT JOIN LATERAL (SELECT id,conversation_id,sender_id,client_message_id,body,sequence,created_at FROM direct_messages m WHERE m.conversation_id=c.id ORDER BY sequence DESC LIMIT 1) last ON true
 WHERE c.id=$1`, id, actor).Scan(&c.ID, &c.PartnerID, &c.PartnerFirstName, &c.PartnerLastName, &c.CreatedAt, &c.MyLastReadSequence, &c.PartnerReadSequence, &c.UnreadCount, &lastID, &lastConversationID, &lastSenderID, &lastClientID, &lastBody, &lastSequence, &lastAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	if lastID.Valid {
		c.LastMessage = &Message{ID: lastID.String, ConversationID: lastConversationID.String, SenderID: lastSenderID.String, ClientMessageID: lastClientID.String, Body: lastBody.String, Sequence: lastSequence.Int64, CreatedAt: lastAt.Time}
	}
	return c, nil
}
