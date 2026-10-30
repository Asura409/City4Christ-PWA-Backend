package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"strconv"
	"time"
)

func (r *Repository) Transition(ctx context.Context, actor, id string, in Decision, member bool, now time.Time) (Booking, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Booking{}, err
	}
	defer tx.Rollback()
	column := "pastor_id"
	if member {
		column = "member_id"
	}
	var pastor string
	err = tx.QueryRowContext(ctx, `SELECT pastor_id FROM pastor_bookings WHERE id=$1 AND `+column+`=$2`, id, actor).Scan(&pastor)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	if err != nil {
		return Booking{}, err
	}
	if err = lockPastorForTransition(ctx, tx, pastor, member); err != nil {
		return Booking{}, err
	}
	if member {
		if err = verifiedMember(ctx, tx, actor); err != nil {
			return Booking{}, err
		}
	}
	b, err := readBooking(ctx, tx, id)
	if err != nil {
		return b, err
	}
	if b.Status == in.Status {
		return b, tx.Commit()
	}
	if !b.StartsAt.After(now) || (in.Version != 0 && in.Version != b.Version) || b.Status == "cancelled" || b.Status == "declined" || (in.Status != "cancelled" && b.Status != "pending") {
		return Booking{}, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE pastor_bookings SET status=$2,version=version+1,updated_at=$3 WHERE id=$1`, id, in.Status, now)
	if err != nil {
		return Booking{}, err
	}
	b, err = readBooking(ctx, tx, id)
	if err != nil {
		return b, err
	}
	if err = r.bookingEvent(ctx, tx, b, actor, in.Status, now); err != nil {
		return Booking{}, err
	}
	return b, tx.Commit()
}
func lockPastorForTransition(ctx context.Context, tx *sql.Tx, pastor string, member bool) error {
	if !member {
		return lockPastor(ctx, tx, pastor, false)
	}
	// Members retain cancellation rights when a pastor is unlisted or revoked.
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pastoral:'||$1,0))`, pastor)
	return err
}
func (r *Repository) bookingEvent(ctx context.Context, tx *sql.Tx, b Booking, actor, action string, now time.Time) error {
	if err := audit(ctx, tx, actor, b.PastorID, "booking", b.ID, action, now); err != nil {
		return err
	}
	recipient := b.MemberID
	if actor == b.MemberID {
		recipient = b.PastorID
	}
	_, err := notifications.NewRepository(r.db).CreateTx(ctx, tx, notifications.CreateInput{AccountID: recipient, Kind: "pastoral." + action, Title: "Pastoral appointment update", Body: "Open your private appointments to review an update.", ResourcePath: "/#/pastors", DedupKey: "pastoral:" + b.ID + ":" + strconv.Itoa(b.Version)})
	if err != nil {
		return err
	}
	return enqueueBookingEmails(ctx, tx, b, action, now)
}
func (r *Repository) WithdrawSlot(ctx context.Context, actor, id string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockPastor(ctx, tx, actor, false); err != nil {
		return err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT active FROM pastor_slots WHERE id=$1 AND pastor_id=$2`, id, actor).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return tx.Commit()
	}
	var held bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pastor_bookings WHERE slot_id=$1 AND status IN ('pending','approved'))`, id).Scan(&held); err != nil {
		return err
	}
	if held {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pastor_slots SET active=false WHERE id=$1`, id); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, actor, "slot", id, "withdrawn", now); err != nil {
		return err
	}
	return tx.Commit()
}
