package messaging

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

func insertDirectMessageAttachments(ctx context.Context, tx *sql.Tx, messageID string, items []media.Attachment) error {
	return insertMessageAttachments(ctx, tx, "direct_message_media_attachments", messageID, items)
}

func insertGroupMessageAttachments(ctx context.Context, tx *sql.Tx, messageID string, items []media.Attachment) error {
	return insertMessageAttachments(ctx, tx, "group_chat_message_media_attachments", messageID, items)
}

func insertMessageAttachments(ctx context.Context, tx *sql.Tx, table, messageID string, items []media.Attachment) error {
	for i, item := range items {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+"(message_id,upload_id,position) VALUES($1,$2,$3)", messageID, item.ID, i); err != nil {
			return err
		}
	}
	return nil
}

func directMessageAttachmentIDs(ctx context.Context, tx *sql.Tx, messageID string) ([]string, error) {
	return messageAttachmentIDs(ctx, tx, "direct_message_media_attachments", messageID)
}

func groupMessageAttachmentIDs(ctx context.Context, tx *sql.Tx, messageID string) ([]string, error) {
	return messageAttachmentIDs(ctx, tx, "group_chat_message_media_attachments", messageID)
}

func messageAttachmentIDs(ctx context.Context, tx *sql.Tx, table, messageID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT upload_id::text FROM "+table+" WHERE message_id=$1 ORDER BY position", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) directMessageAttachments(ctx context.Context, messageIDs []string) (map[string][]media.Attachment, error) {
	return r.loadMessageAttachments(ctx, "direct_message_media_attachments", messageIDs)
}

func (r *Repository) groupMessageAttachments(ctx context.Context, messageIDs []string) (map[string][]media.Attachment, error) {
	return r.loadMessageAttachments(ctx, "group_chat_message_media_attachments", messageIDs)
}

func (r *Repository) loadMessageAttachments(ctx context.Context, table string, messageIDs []string) (map[string][]media.Attachment, error) {
	items := make(map[string][]media.Attachment, len(messageIDs))
	if len(messageIDs) == 0 {
		return items, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := "SELECT a.message_id::text,u.id::text,u.actual_content_type,u.actual_size,u.available_object_key FROM " + table +
		" a JOIN media_uploads u ON u.id=a.upload_id WHERE a.message_id IN (" + strings.Join(placeholders, ",") +
		") AND u.status='available' AND u.available_object_key IS NOT NULL ORDER BY a.message_id,a.position"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var item media.Attachment
		if err := rows.Scan(&messageID, &item.ID, &item.ContentType, &item.Size, &item.ObjectKey); err != nil {
			return nil, err
		}
		items[messageID] = append(items[messageID], item)
	}
	return items, rows.Err()
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
