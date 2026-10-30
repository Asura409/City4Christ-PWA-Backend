package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const bookingColumns = `b.id,b.slot_id,b.pastor_id,b.member_id,pa.first_name,pa.last_name,ma.first_name,ma.last_name,b.starts_at,b.ends_at,p.time_zone,b.status,b.version,b.created_at,b.updated_at`
const bookingJoins = ` FROM pastor_bookings b JOIN pastor_profiles p ON p.account_id=b.pastor_id JOIN accounts pa ON pa.id=b.pastor_id JOIN accounts ma ON ma.id=b.member_id `

func scanBooking(row interface{ Scan(...any) error }) (Booking, error) {
	var b Booking
	err := row.Scan(&b.ID, &b.SlotID, &b.PastorID, &b.MemberID, &b.PastorFirstName, &b.PastorLastName, &b.MemberFirstName, &b.MemberLastName, &b.StartsAt, &b.EndsAt, &b.TimeZone, &b.Status, &b.Version, &b.CreatedAt, &b.UpdatedAt)
	b.StartsAt = b.StartsAt.UTC()
	b.EndsAt = b.EndsAt.UTC()
	b.CreatedAt = b.CreatedAt.UTC()
	b.UpdatedAt = b.UpdatedAt.UTC()
	return b, err
}
func readBooking(ctx context.Context, tx *sql.Tx, id string) (Booking, error) {
	return scanBooking(tx.QueryRowContext(ctx, `SELECT `+bookingColumns+bookingJoins+` WHERE b.id=$1`, id))
}
func verifiedMember(ctx context.Context, tx *sql.Tx, actor string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL FOR SHARE`, actor).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	return err
}
func (r *Repository) Request(ctx context.Context, actor, slotID string, now time.Time) (Booking, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Booking{}, err
	}
	defer tx.Rollback()
	var pastor string
	err = tx.QueryRowContext(ctx, `SELECT pastor_id FROM pastor_slots WHERE id=$1`, slotID).Scan(&pastor)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	if err != nil {
		return Booking{}, err
	}
	if actor == pastor {
		return Booking{}, ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pastoral-member:'||$1,0))`, actor); err != nil {
		return Booking{}, err
	}
	if err = verifiedMember(ctx, tx, actor); err != nil {
		return Booking{}, err
	}
	if err = lockPastor(ctx, tx, pastor, true); err != nil {
		if errors.Is(err, ErrForbidden) {
			err = ErrNotFound
		}
		return Booking{}, err
	}
	var starts, ends time.Time
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT starts_at,ends_at,active FROM pastor_slots WHERE id=$1`, slotID).Scan(&starts, &ends, &active); err != nil {
		return Booking{}, err
	}
	if !active || !starts.After(now) {
		return Booking{}, ErrConflict
	}
	existing, err := scanBooking(tx.QueryRowContext(ctx, `SELECT `+bookingColumns+bookingJoins+` WHERE b.slot_id=$1 AND b.status IN ('pending','approved')`, slotID))
	if err == nil {
		if existing.MemberID == actor {
			return existing, tx.Commit()
		}
		return Booking{}, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Booking{}, err
	}
	var conflict bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pastor_bookings WHERE member_id=$1 AND status IN ('pending','approved') AND starts_at<$3 AND ends_at>$2)`, actor, starts, ends).Scan(&conflict); err != nil {
		return Booking{}, err
	}
	if conflict {
		return Booking{}, ErrConflict
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO pastor_bookings(slot_id,pastor_id,member_id,starts_at,ends_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6) RETURNING id`, slotID, pastor, actor, starts, ends, now).Scan(&id)
	if err != nil {
		return Booking{}, err
	}
	b, err := readBooking(ctx, tx, id)
	if err != nil {
		return Booking{}, err
	}
	if err = r.bookingEvent(ctx, tx, b, actor, "requested", now); err != nil {
		return Booking{}, err
	}
	return b, tx.Commit()
}
func (r *Repository) Bookings(ctx context.Context, actor string, assigned bool, o Options) ([]Booking, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	column := "b.member_id"
	if assigned {
		column = "b.pastor_id"
		err = lockPastor(ctx, tx, actor, false)
	} else {
		err = verifiedMember(ctx, tx, actor)
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+bookingColumns+bookingJoins+` WHERE `+column+`=$1 AND ($2::uuid IS NULL OR b.id>$2) ORDER BY b.id LIMIT $3`, actor, cursor(o.Cursor), o.Limit+1)
	if err != nil {
		return nil, err
	}
	items := []Booking{}
	for rows.Next() {
		b, e := scanBooking(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return items, tx.Commit()
}
