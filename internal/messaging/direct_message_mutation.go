package messaging

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

func (r *Repository) EditOwnMessage(ctx context.Context, actor, conversationID, messageID, body string, now time.Time) (Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	var verified bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return Message{}, err
	}
	if !verified {
		return Message{}, ErrForbidden
	}
	message, err := lockOwnDirectMessage(ctx, tx, actor, conversationID, messageID)
	if err != nil {
		return Message{}, err
	}
	if message.Deleted || !message.CreatedAt.Add(DirectMessageMutationWindow).After(now) {
		return Message{}, ErrMutationWindowExpired
	}
	now = now.UTC()
	if err := tx.QueryRowContext(ctx, `UPDATE direct_messages SET body=$2,edited_at=$3 WHERE id=$1 RETURNING body,edited_at`, messageID, body, now).Scan(&message.Body, &message.EditedAt); err != nil {
		return Message{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO direct_message_events(message_id,actor_id,action,occurred_at) VALUES($1,$2,'edited',$3)`, messageID, actor, now); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (r *Repository) DeleteOwnMessage(ctx context.Context, actor, conversationID, messageID string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var verified bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return false, err
	}
	if !verified {
		return false, ErrForbidden
	}
	message, err := lockOwnDirectMessage(ctx, tx, actor, conversationID, messageID)
	if err != nil {
		return false, err
	}
	if message.Deleted || !message.CreatedAt.Add(DirectMessageMutationWindow).After(now) {
		return false, ErrMutationWindowExpired
	}
	now = now.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE direct_messages SET body='',deleted_at=$2,deleted_by=$3 WHERE id=$1`, messageID, now, actor); err != nil {
		return false, err
	}
	if err := media.DetachDirectMessageAttachmentsTx(ctx, tx, messageID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO direct_message_events(message_id,actor_id,action,occurred_at) VALUES($1,$2,'deleted',$3)`, messageID, actor, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func lockOwnDirectMessage(ctx context.Context, tx *sql.Tx, actor, conversationID, messageID string) (Message, error) {
	var message Message
	var editedAt sql.NullTime
	var deletedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id::text,conversation_id::text,sender_id::text,client_message_id::text,body,sequence,created_at,edited_at,deleted_at
		FROM direct_messages WHERE id=$1 AND conversation_id=$2 AND sender_id=$3 FOR UPDATE`, messageID, conversationID, actor).
		Scan(&message.ID, &message.ConversationID, &message.SenderID, &message.ClientMessageID, &message.Body, &message.Sequence, &message.CreatedAt, &editedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	if editedAt.Valid {
		value := editedAt.Time.UTC()
		message.EditedAt = &value
	}
	message.Deleted = deletedAt.Valid
	return message, nil
}
