package pastoral

import (
	"context"
	"time"
)

const slotColumns = `s.id,s.pastor_id,s.starts_at,s.ends_at,p.time_zone,NOT EXISTS(SELECT 1 FROM pastor_bookings b WHERE b.slot_id=s.id AND b.status IN ('pending','approved'))`

func scanSlot(row interface{ Scan(...any) error }) (Slot, error) {
	var s Slot
	err := row.Scan(&s.ID, &s.PastorID, &s.StartsAt, &s.EndsAt, &s.TimeZone, &s.Available)
	s.StartsAt = s.StartsAt.UTC()
	s.EndsAt = s.EndsAt.UTC()
	return s, err
}
func (r *Repository) CreateSlot(ctx context.Context, actor string, in SlotInput, now time.Time) (Slot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Slot{}, err
	}
	defer tx.Rollback()
	if err = lockPastor(ctx, tx, actor, true); err != nil {
		return Slot{}, err
	}
	var overlap bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pastor_slots WHERE pastor_id=$1 AND active AND starts_at<$3 AND ends_at>$2)`, actor, in.StartsAt, in.EndsAt).Scan(&overlap); err != nil {
		return Slot{}, err
	}
	if overlap {
		return Slot{}, ErrConflict
	}
	slot, err := scanSlot(tx.QueryRowContext(ctx, `WITH created AS(INSERT INTO pastor_slots(pastor_id,starts_at,ends_at) VALUES($1,$2,$3) RETURNING *) SELECT `+slotColumns+` FROM created s JOIN pastor_profiles p ON p.account_id=s.pastor_id`, actor, in.StartsAt.UTC(), in.EndsAt.UTC()))
	if err != nil {
		return slot, err
	}
	if err = audit(ctx, tx, actor, actor, "slot", slot.ID, "created", now); err != nil {
		return Slot{}, err
	}
	return slot, tx.Commit()
}
func (r *Repository) Availability(ctx context.Context, pastor string, from, to time.Time, o Options, now time.Time) ([]Slot, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+slotColumns+` FROM pastor_slots s JOIN pastor_profiles p ON p.account_id=s.pastor_id JOIN accounts a ON a.id=p.account_id WHERE p.account_id=$1 AND p.listed AND `+eligiblePastor+` AND s.active AND NOT EXISTS(SELECT 1 FROM pastor_bookings b WHERE b.slot_id=s.id AND b.status IN ('pending','approved')) AND s.starts_at>$2 AND s.starts_at>=$3 AND s.ends_at<=$4 AND ($5::uuid IS NULL OR s.id>$5) ORDER BY s.id LIMIT $6`, pastor, now, from.UTC(), to.UTC(), cursor(o.Cursor), o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Slot{}
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, slot)
	}
	return items, rows.Err()
}

func (r *Repository) OwnSlots(ctx context.Context, actor string, from, to time.Time, o Options, now time.Time) ([]Slot, error) {
	if _, err := r.OwnProfile(ctx, actor); err != nil {
		if err == ErrNotFound {
			return nil, ErrForbidden
		}
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+slotColumns+` FROM pastor_slots s JOIN pastor_profiles p ON p.account_id=s.pastor_id JOIN accounts a ON a.id=p.account_id WHERE p.account_id=$1 AND `+eligiblePastor+` AND s.active AND s.starts_at>$2 AND s.starts_at>=$3 AND s.ends_at<=$4 AND ($5::uuid IS NULL OR s.id>$5) ORDER BY s.id LIMIT $6`, actor, now, from.UTC(), to.UTC(), cursor(o.Cursor), o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Slot{}
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, slot)
	}
	return items, rows.Err()
}
