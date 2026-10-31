package notifications

import (
	"context"
	"database/sql"
)

func (s *Service) Broadcasts(ctx context.Context, actor, group string) ([]Broadcast, error) {
	if group != "" && !validID(group) {
		return nil, ErrInvalid
	}
	tx, err := s.repo.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = broadcastPermission(ctx, tx, actor, group); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,title,body,status,version,recipient_count,created_at,sent_at FROM notification_broadcasts WHERE group_id IS NOT DISTINCT FROM NULLIF($1,'')::uuid ORDER BY created_at DESC,id DESC LIMIT 50`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Broadcast{}
	for rows.Next() {
		b := Broadcast{BroadcastInput: BroadcastInput{GroupID: group, Audience: "church"}}
		if group != "" {
			b.Audience = "group"
		}
		if err = rows.Scan(&b.ID, &b.Title, &b.Body, &b.Status, &b.Version, &b.RecipientCount, &b.CreatedAt, &b.SentAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, tx.Commit()
}
