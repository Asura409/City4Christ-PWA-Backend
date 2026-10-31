package messaging

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/google/uuid"
)

func (r *Repository) ListGroupConversations(ctx context.Context, actor string) ([]GroupConversation, error) {
	var verified bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT g.id,g.name,g.audience,COALESCE(r.last_read_sequence,0),
 COALESCE(unread.count,0),last.id,last.group_id,last.sender_id,last.client_message_id,last.body,last.sequence,last.created_at,last.deleted_at IS NOT NULL,
 (EXISTS(SELECT 1 FROM account_roles ar WHERE ar.account_id=$1 AND ar.scope='' AND ar.role IN ('super_administrator','assistant_pastor','staff'))
  OR EXISTS(SELECT 1 FROM account_roles ar WHERE ar.account_id=$1 AND ar.scope=g.id::text AND ar.role='ministry_leader')) AS can_moderate
 FROM groups g
 LEFT JOIN group_chat_reads r ON r.group_id=g.id AND r.account_id=$1
 LEFT JOIN LATERAL (SELECT count(*)::int AS count FROM group_chat_messages m WHERE m.group_id=g.id AND m.sender_id<>$1 AND m.sequence>COALESCE(r.last_read_sequence,0) AND m.deleted_at IS NULL) unread ON true
 LEFT JOIN LATERAL (SELECT id,group_id,sender_id,client_message_id,body,sequence,created_at,deleted_at FROM group_chat_messages m WHERE m.group_id=g.id ORDER BY sequence DESC LIMIT 1) last ON true
 WHERE g.archived_at IS NULL AND (
   (g.audience='church' AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.group_id=g.id AND gm.account_id=$1 AND gm.status='active'))
   OR (g.audience='leadership' AND EXISTS(SELECT 1 FROM account_roles ar WHERE ar.account_id=$1 AND ((ar.role IN ('super_administrator','assistant_pastor') AND ar.scope='') OR ar.role='ministry_leader')))
 ) ORDER BY COALESCE(last.sequence,0) DESC,g.created_at DESC,g.id`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []GroupConversation{}
	for rows.Next() {
		var item GroupConversation
		var lastID, lastGroupID, lastSenderID, lastClientID, lastBody sql.NullString
		var lastSequence sql.NullInt64
		var lastAt sql.NullTime
		var lastDeleted bool
		if err = rows.Scan(&item.GroupID, &item.GroupName, &item.Audience, &item.MyLastReadSequence, &item.UnreadCount,
			&lastID, &lastGroupID, &lastSenderID, &lastClientID, &lastBody, &lastSequence, &lastAt, &lastDeleted, &item.CanModerate); err != nil {
			return nil, err
		}
		if lastID.Valid {
			body := lastBody.String
			if lastDeleted {
				body = ""
			}
			item.LastMessage = &GroupMessage{ID: lastID.String, GroupID: lastGroupID.String, SenderID: lastSenderID.String, ClientMessageID: lastClientID.String, Body: body, Sequence: lastSequence.Int64, CreatedAt: lastAt.Time, Deleted: lastDeleted}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) SendGroupMessage(ctx context.Context, groupID, actor, clientMessageID, body string, now time.Time) (GroupMessage, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return GroupMessage{}, false, err
	}
	defer tx.Rollback()
	if _, err = r.groupChatAccess(ctx, tx, groupID, actor); err != nil {
		return GroupMessage{}, false, err
	}
	message := GroupMessage{ID: uuid.NewString(), GroupID: groupID, SenderID: actor, ClientMessageID: clientMessageID, Body: body, CreatedAt: now.UTC()}
	err = tx.QueryRowContext(ctx, `INSERT INTO group_chat_messages(id,group_id,sender_id,client_message_id,body,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(group_id,sender_id,client_message_id) DO NOTHING RETURNING sequence`, message.ID, groupID, actor, clientMessageID, body, message.CreatedAt).Scan(&message.Sequence)
	duplicate := false
	if errors.Is(err, sql.ErrNoRows) {
		duplicate = true
		err = tx.QueryRowContext(ctx, `SELECT id,group_id,sender_id,client_message_id,body,sequence,created_at,deleted_at IS NOT NULL FROM group_chat_messages WHERE group_id=$1 AND sender_id=$2 AND client_message_id=$3`, groupID, actor, clientMessageID).Scan(&message.ID, &message.GroupID, &message.SenderID, &message.ClientMessageID, &message.Body, &message.Sequence, &message.CreatedAt, &message.Deleted)
		if err == nil && message.Body != body && !message.Deleted {
			return GroupMessage{}, false, ErrIdempotencyConflict
		}
	}
	if err != nil {
		return GroupMessage{}, false, err
	}
	if !duplicate {
		if err = messageNoticesTx(ctx, tx, message.ID, actor, "", groupID); err != nil {
			return GroupMessage{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return GroupMessage{}, false, err
	}
	return message, duplicate, nil
}

func (r *Repository) ListGroupMessages(ctx context.Context, groupID, actor string, beforeSequence int64, limit int) (GroupMessagePage, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return GroupMessagePage{}, err
	}
	defer tx.Rollback()
	if _, err = r.groupChatAccess(ctx, tx, groupID, actor); err != nil {
		return GroupMessagePage{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,group_id,sender_id,client_message_id,body,sequence,created_at,deleted_at IS NOT NULL FROM (
 SELECT id,group_id,sender_id,client_message_id,body,sequence,created_at,deleted_at FROM group_chat_messages WHERE group_id=$1 AND ($2=0 OR sequence<$2) ORDER BY sequence DESC LIMIT $3
 ) page ORDER BY sequence`, groupID, beforeSequence, limit+1)
	if err != nil {
		return GroupMessagePage{}, err
	}
	items := []GroupMessage{}
	for rows.Next() {
		var item GroupMessage
		if err = rows.Scan(&item.ID, &item.GroupID, &item.SenderID, &item.ClientMessageID, &item.Body, &item.Sequence, &item.CreatedAt, &item.Deleted); err != nil {
			rows.Close()
			return GroupMessagePage{}, err
		}
		if item.Deleted {
			item.Body = ""
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return GroupMessagePage{}, err
	}
	rows.Close()
	page := GroupMessagePage{Items: items}
	if len(items) > limit {
		page.NextBeforeSequence = items[1].Sequence
		page.Items = items[1:]
	}
	if err = tx.Commit(); err != nil {
		return GroupMessagePage{}, err
	}
	return page, nil
}

func (r *Repository) MarkGroupRead(ctx context.Context, groupID, actor, throughMessageID string, now time.Time) (GroupReadReceipt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return GroupReadReceipt{}, err
	}
	defer tx.Rollback()
	if _, err = r.groupChatAccess(ctx, tx, groupID, actor); err != nil {
		return GroupReadReceipt{}, err
	}
	var target int64
	if throughMessageID == "" {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(sequence),0) FROM group_chat_messages WHERE group_id=$1 AND deleted_at IS NULL`, groupID).Scan(&target)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT sequence FROM group_chat_messages WHERE id=$1 AND group_id=$2`, throughMessageID, groupID).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			return GroupReadReceipt{}, ErrNotFound
		}
	}
	if err != nil {
		return GroupReadReceipt{}, err
	}
	receipt := GroupReadReceipt{GroupID: groupID}
	var readAt sql.NullTime
	err = tx.QueryRowContext(ctx, `INSERT INTO group_chat_reads(group_id,account_id,last_read_sequence,last_read_at) VALUES($1,$2,$3,$4)
 ON CONFLICT(group_id,account_id) DO UPDATE SET last_read_sequence=GREATEST(group_chat_reads.last_read_sequence,EXCLUDED.last_read_sequence),last_read_at=CASE WHEN EXCLUDED.last_read_sequence>group_chat_reads.last_read_sequence THEN EXCLUDED.last_read_at ELSE group_chat_reads.last_read_at END
 RETURNING last_read_sequence,last_read_at`, groupID, actor, target, now.UTC()).Scan(&receipt.LastReadSequence, &readAt)
	if readAt.Valid {
		receipt.LastReadAt = &readAt.Time
	}
	if err != nil {
		return GroupReadReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return GroupReadReceipt{}, err
	}
	return receipt, nil
}

func (r *Repository) DeleteGroupMessage(ctx context.Context, groupID, actor, messageID string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var audience string
	err = tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL FOR SHARE`, groupID).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return false, err
	}
	if !verified {
		return false, ErrForbidden
	}
	if audience == "leadership" {
		var eligible bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=$1 AND ((role IN ('super_administrator','assistant_pastor') AND scope='') OR role='ministry_leader'))`, actor).Scan(&eligible); err != nil {
			return false, err
		}
		if !eligible {
			return false, ErrNotFound
		}
	}
	globalErr := access.CheckTx(ctx, tx, actor, "groups.members.manage", "")
	canModerate := globalErr == nil
	if globalErr != nil && !errors.Is(globalErr, access.ErrForbidden) {
		return false, globalErr
	}
	if !canModerate {
		err = access.CheckTx(ctx, tx, actor, "ministry.members.manage", groupID)
		canModerate = err == nil
		if err != nil && !errors.Is(err, access.ErrForbidden) {
			return false, err
		}
	}
	if !canModerate {
		return false, ErrNotFound
	}
	res, err := tx.ExecContext(ctx, `UPDATE group_chat_messages SET body='',deleted_at=$3,deleted_by=$4 WHERE group_id=$1 AND id=$2 AND deleted_at IS NULL`, groupID, messageID, now.UTC(), actor)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 0 {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_chat_messages WHERE group_id=$1 AND id=$2)`, groupID, messageID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, ErrNotFound
		}
	}
	if changed == 1 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO group_chat_moderation_events(group_id,message_id,actor_id,action,created_at) VALUES($1,$2,$3,'deleted',$4)`, groupID, messageID, actor, now.UTC()); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return changed == 1, nil
}
