package events

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (r *Repository) Create(ctx context.Context, actor string, in CreateInput, now time.Time) (Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	event, err := r.createTx(ctx, tx, actor, in, now)
	if err != nil {
		return Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) createTx(ctx context.Context, tx *sql.Tx, actor string, in CreateInput, now time.Time) (Event, error) {
	var event Event
	err := tx.QueryRowContext(ctx, `
		INSERT INTO church_events(id,title,description,location,starts_at,time_zone,capacity,status,version,created_by,created_at,updated_at,group_id)
		VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,'draft',1,$7,$8,$8,NULLIF($9,'')::uuid)
		RETURNING id,title,description,location,starts_at,time_zone,capacity,status,version,created_at,updated_at,COALESCE(group_id::text,'')`,
		in.Title, in.Description, in.Location, in.StartsAt, in.TimeZone, in.Capacity, actor, now, in.GroupID,
	).Scan(&event.ID, &event.Title, &event.Description, &event.Location, &event.StartsAt, &event.TimeZone, &event.Capacity, &event.Status, &event.Version, &event.CreatedAt, &event.UpdatedAt, &event.GroupID)
	if err != nil {
		return Event{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_audit_events(event_id,actor_id,action,version,created_at) VALUES($1,$2,'created',1,$3)`, event.ID, actor, now); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) Update(ctx context.Context, actor, eventID string, in UpdateInput, now time.Time) (Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	event, err := r.updateTx(ctx, tx, actor, eventID, in, now)
	if err != nil {
		return Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) updateTx(ctx context.Context, tx *sql.Tx, actor, eventID string, in UpdateInput, now time.Time) (Event, error) {
	var currentStatus string
	var currentVersion int
	if err := tx.QueryRowContext(ctx, `SELECT status,version FROM church_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&currentStatus, &currentVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Event{}, ErrNotFound
		}
		return Event{}, err
	}
	if currentVersion != in.Version {
		return Event{}, ErrVersionConflict
	}
	if currentStatus == "cancelled" && in.Status != "cancelled" {
		return Event{}, ErrConflict
	}
	var booked int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(attendee_count),0)::int FROM event_rsvps WHERE event_id=$1`, eventID).Scan(&booked); err != nil {
		return Event{}, err
	}
	if in.Capacity != nil && *in.Capacity < booked {
		return Event{}, ErrFullCapacity
	}
	var event Event
	err := tx.QueryRowContext(ctx, `
		UPDATE church_events SET title=$1,description=$2,location=$3,starts_at=$4,time_zone=$5,capacity=$6,status=$7,version=version+1,updated_at=$8
		WHERE id=$9 AND version=$10
		RETURNING id,title,description,location,starts_at,time_zone,capacity,status,version,created_at,updated_at,COALESCE(group_id::text,'')`,
		in.Title, in.Description, in.Location, in.StartsAt, in.TimeZone, in.Capacity, in.Status, now, eventID, in.Version,
	).Scan(&event.ID, &event.Title, &event.Description, &event.Location, &event.StartsAt, &event.TimeZone, &event.Capacity, &event.Status, &event.Version, &event.CreatedAt, &event.UpdatedAt, &event.GroupID)
	if err != nil {
		return Event{}, err
	}
	action := "edited"
	if in.Status != currentStatus {
		action = in.Status
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_audit_events(event_id,actor_id,action,version,created_at) VALUES($1,$2,$3,$4,$5)`, eventID, actor, action, event.Version, now); err != nil {
		return Event{}, err
	}
	if currentStatus == "published" {
		if err = eventChangeNoticesTx(ctx, tx, event); err != nil {
			return Event{}, err
		}
	}
	return event, nil
}

func (r *Repository) List(ctx context.Context, viewer string, now time.Time) ([]Event, error) {
	return r.list(ctx, viewer, now, false)
}
func (r *Repository) AdminList(ctx context.Context, actor string, now time.Time) ([]Event, error) {
	return r.list(ctx, actor, now, true)
}
func (r *Repository) list(ctx context.Context, viewer string, now time.Time, admin bool) ([]Event, error) {
	var result []Event
	err := func() error {
		tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := func() error {
			result, err = r.listTx(ctx, tx, viewer, now, admin)
			return err
		}(); err != nil {
			return err
		}
		return tx.Commit()
	}()
	return result, err
}

func (r *Repository) listTx(ctx context.Context, tx *sql.Tx, viewer string, now time.Time, admin bool, groupIDs ...string) ([]Event, error) {
	groupID := ""
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	query := `SELECT e.id,e.title,e.description,e.location,e.starts_at,e.time_zone,e.capacity,e.status,e.version,e.created_at,e.updated_at,
		COALESCE(sum(r.attendee_count),0)::int,
		COALESCE(max(r.attendee_count) FILTER (WHERE r.account_id=NULLIF($1,'')::uuid),0)::int,COALESCE(e.group_id::text,'')
		FROM church_events e LEFT JOIN event_rsvps r ON r.event_id=e.id`
	if admin {
		query += ` WHERE e.group_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid GROUP BY e.id ORDER BY e.starts_at,e.id LIMIT 100`
	} else {
		query += ` WHERE e.status='published' AND c4c_group_content_visible(e.group_id,NULLIF($1,'')::uuid) AND ($2='' OR e.group_id=NULLIF($2,'')::uuid) GROUP BY e.id ORDER BY e.starts_at,e.id LIMIT 100`
	}
	rows, err := tx.QueryContext(ctx, query, viewer, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.Description, &event.Location, &event.StartsAt, &event.TimeZone, &event.Capacity, &event.Status, &event.Version, &event.CreatedAt, &event.UpdatedAt, &event.AttendeeCount, &event.MyAttendeeCount, &event.GroupID); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) Detail(ctx context.Context, eventID, viewer string, now time.Time) (Event, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	var event Event
	err = tx.QueryRowContext(ctx, `SELECT e.id,e.title,e.description,e.location,e.starts_at,e.time_zone,e.capacity,e.status,e.version,e.created_at,e.updated_at,
		COALESCE(sum(r.attendee_count),0)::int,COALESCE(max(r.attendee_count) FILTER (WHERE r.account_id=NULLIF($2,'')::uuid),0)::int,COALESCE(e.group_id::text,'')
		FROM church_events e LEFT JOIN event_rsvps r ON r.event_id=e.id WHERE e.id=$1 AND e.status='published' AND c4c_group_content_visible(e.group_id,NULLIF($2,'')::uuid) GROUP BY e.id`, eventID, viewer).
		Scan(&event.ID, &event.Title, &event.Description, &event.Location, &event.StartsAt, &event.TimeZone, &event.Capacity, &event.Status, &event.Version, &event.CreatedAt, &event.UpdatedAt, &event.AttendeeCount, &event.MyAttendeeCount, &event.GroupID)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) RSVP(ctx context.Context, eventID, accountID string, attendees int, now time.Time) (RSVP, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RSVP{}, err
	}
	defer tx.Rollback()
	var status string
	var starts time.Time
	var capacity sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT status,starts_at,capacity FROM church_events WHERE id=$1 AND c4c_group_content_visible(group_id,$2::uuid) FOR UPDATE`, eventID, accountID).Scan(&status, &starts, &capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RSVP{}, ErrNotFound
		}
		return RSVP{}, err
	}
	if status != "published" || !starts.After(now) {
		return RSVP{}, ErrRegistrationClosed
	}
	var previous int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT attendee_count FROM event_rsvps WHERE event_id=$1 AND account_id=$2),0)`, eventID, accountID).Scan(&previous); err != nil {
		return RSVP{}, err
	}
	if previous == attendees {
		var result RSVP
		err = tx.QueryRowContext(ctx, `SELECT event_id,attendee_count,updated_at FROM event_rsvps WHERE event_id=$1 AND account_id=$2`, eventID, accountID).Scan(&result.EventID, &result.AttendeeCount, &result.UpdatedAt)
		if err != nil {
			return RSVP{}, err
		}
		if err = tx.Commit(); err != nil {
			return RSVP{}, err
		}
		return result, nil
	}
	if capacity.Valid {
		var booked int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(attendee_count),0)::int FROM event_rsvps WHERE event_id=$1`, eventID).Scan(&booked); err != nil {
			return RSVP{}, err
		}
		if booked-previous+attendees > int(capacity.Int64) {
			return RSVP{}, ErrFullCapacity
		}
	}
	var result RSVP
	err = tx.QueryRowContext(ctx, `INSERT INTO event_rsvps(event_id,account_id,attendee_count,created_at,updated_at) VALUES($1,$2,$3,$4,$4)
		ON CONFLICT(event_id,account_id) DO UPDATE SET attendee_count=EXCLUDED.attendee_count,updated_at=EXCLUDED.updated_at
		RETURNING event_id,attendee_count,updated_at`, eventID, accountID, attendees, now).
		Scan(&result.EventID, &result.AttendeeCount, &result.UpdatedAt)
	if err != nil {
		return RSVP{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_rsvp_audit_events(event_id,account_id,action,attendee_count,created_at) VALUES($1,$2,'confirmed',$3,$4)`, eventID, accountID, attendees, now); err != nil {
		return RSVP{}, err
	}
	if err = tx.Commit(); err != nil {
		return RSVP{}, err
	}
	return result, nil
}

func (r *Repository) CancelRSVP(ctx context.Context, eventID, accountID string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status string
	var starts time.Time
	if err = tx.QueryRowContext(ctx, `SELECT status,starts_at FROM church_events WHERE id=$1 AND c4c_group_content_visible(group_id,$2::uuid) FOR UPDATE`, eventID, accountID).Scan(&status, &starts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if status != "published" || !starts.After(now) {
		return false, ErrRegistrationClosed
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM event_rsvps WHERE event_id=$1 AND account_id=$2`, eventID, accountID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO event_rsvp_audit_events(event_id,account_id,action,attendee_count,created_at) VALUES($1,$2,'cancelled',0,$3)`, eventID, accountID, now); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return count > 0, nil
}
