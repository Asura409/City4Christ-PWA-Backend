package notifications

import (
	"context"
)

func (s *Service) Broadcasts(ctx context.Context, actor, audience, group string) ([]Broadcast, error) {
	if (audience != "church" && audience != "group" && audience != "staff") || (audience != "group" && group != "") || (audience == "group" && !validID(group)) {
		return nil, ErrInvalid
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = broadcastPermission(ctx, tx, actor, group); err != nil {
		return nil, err
	}
	staffOnly := audience == "staff"
	rows, err := tx.QueryContext(ctx, `SELECT id,title,body,status,version,recipient_count,created_at,sent_at FROM notification_broadcasts WHERE group_id IS NOT DISTINCT FROM NULLIF($1,'')::uuid AND staff_only=$2 ORDER BY created_at DESC,id DESC LIMIT 50`, group, staffOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Broadcast{}
	for rows.Next() {
		b := Broadcast{BroadcastInput: BroadcastInput{GroupID: group, Audience: audience}}
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
