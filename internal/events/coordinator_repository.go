package events

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (r *Repository) assignCoordinatorTx(ctx context.Context, tx *sql.Tx, actor, eventID, groupID, accountID string, now time.Time) (Coordinator, int64, bool, error) {
	var item Coordinator
	err := tx.QueryRowContext(ctx, `INSERT INTO event_coordinators(event_id,group_id,account_id,assigned_by,assigned_at)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(event_id,account_id) DO UPDATE SET group_id=EXCLUDED.group_id,assigned_by=EXCLUDED.assigned_by,assigned_at=EXCLUDED.assigned_at
		WHERE event_coordinators.group_id IS DISTINCT FROM EXCLUDED.group_id
		RETURNING event_id,group_id,account_id,assigned_at`, eventID, groupID, accountID, actor, now).
		Scan(&item.EventID, &item.GroupID, &item.AccountID, &item.AssignedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT event_id,group_id,account_id,assigned_at FROM event_coordinators WHERE event_id=$1 AND account_id=$2`, eventID, accountID).
			Scan(&item.EventID, &item.GroupID, &item.AccountID, &item.AssignedAt)
		return item, 0, false, err
	}
	if err != nil {
		return Coordinator{}, 0, false, err
	}
	var auditID int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO event_coordinator_audit_events(event_id,group_id,account_id,actor_id,action,occurred_at) VALUES($1,$2,$3,$4,'assigned',$5) RETURNING id`, eventID, groupID, accountID, actor, now).Scan(&auditID); err != nil {
		return Coordinator{}, 0, false, err
	}
	return item, auditID, true, nil
}

func (r *Repository) coordinators(ctx context.Context, eventID string, admin bool) ([]Coordinator, error) {
	query := `SELECT c.event_id,c.group_id,g.name,c.account_id,a.first_name,a.last_name,c.assigned_at
		FROM event_coordinators c JOIN church_events e ON e.id=c.event_id
		JOIN groups g ON g.id=c.group_id JOIN accounts a ON a.id=c.account_id
		JOIN group_memberships gm ON gm.group_id=c.group_id AND gm.account_id=c.account_id
		WHERE c.event_id=$1 AND e.status='published' AND g.audience='church' AND g.archived_at IS NULL AND gm.status='active' AND a.email_verified_at IS NOT NULL
		ORDER BY g.name,a.first_name,a.last_name,a.id`
	if admin {
		query = `SELECT c.event_id,c.group_id,g.name,c.account_id,a.first_name,a.last_name,c.assigned_at
			FROM event_coordinators c JOIN church_events e ON e.id=c.event_id
			JOIN groups g ON g.id=c.group_id JOIN accounts a ON a.id=c.account_id
			JOIN group_memberships gm ON gm.group_id=c.group_id AND gm.account_id=c.account_id
			WHERE c.event_id=$1 AND e.status<>'cancelled' AND g.audience='church' AND g.archived_at IS NULL AND gm.status='active' AND a.email_verified_at IS NOT NULL
			ORDER BY g.name,a.first_name,a.last_name,a.id`
	}
	rows, err := r.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Coordinator{}
	for rows.Next() {
		var item Coordinator
		if err = rows.Scan(&item.EventID, &item.GroupID, &item.GroupName, &item.AccountID, &item.FirstName, &item.LastName, &item.AssignedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		var published bool
		err = r.db.QueryRowContext(ctx, `SELECT status='published' FROM church_events WHERE id=$1`, eventID).Scan(&published)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !published {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
