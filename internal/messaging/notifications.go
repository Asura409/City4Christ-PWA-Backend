package messaging

import (
	"context"
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
)

func messageNoticesTx(ctx context.Context, tx *sql.Tx, message, actor, conversation, group string) error {
	query := `SELECT m.account_id::text FROM direct_conversation_members m JOIN accounts a ON a.id=m.account_id WHERE m.conversation_id=$1 AND m.account_id<>$2 AND a.email_verified_at IS NOT NULL`
	target := conversation
	kind := "message.direct"
	if group != "" {
		query = `SELECT account_id::text FROM group_memberships WHERE group_id=$1 AND account_id<>$2 AND c4c_group_content_visible(group_id,account_id)`
		target = group
		kind = "message.group"
	}
	rows, err := tx.QueryContext(ctx, query, target, actor)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if _, err = notifications.NewRepository(nil).CreateTx(ctx, tx, notifications.CreateInput{AccountID: id, Kind: kind, Title: "New message", Body: "Open your messages to view a new update.", ResourcePath: "/#/messages", GroupID: group, DedupKey: "message:" + message}); err != nil {
			return err
		}
	}
	return nil
}
