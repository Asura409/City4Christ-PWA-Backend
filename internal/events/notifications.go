package events

import (
	"context"
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"strconv"
)

func eventChangeNoticesTx(ctx context.Context, tx *sql.Tx, event Event) error {
	rows, err := tx.QueryContext(ctx, `SELECT r.account_id::text FROM event_rsvps r JOIN accounts a ON a.id=r.account_id WHERE r.event_id=$1 AND a.email_verified_at IS NOT NULL AND c4c_group_content_visible(NULLIF($2,'')::uuid,r.account_id)`, event.ID, event.GroupID)
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
		if _, err = notifications.NewRepository(nil).CreateTx(ctx, tx, notifications.CreateInput{AccountID: id, Kind: "event.updated", Title: "Event registration update", Body: "An event you registered for has changed. Open Events for the latest information.", ResourcePath: "/#/events", GroupID: event.GroupID, DedupKey: "event:" + event.ID + ":" + strconv.Itoa(event.Version)}); err != nil {
			return err
		}
	}
	return nil
}
