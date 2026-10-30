package pastoral

import (
	"context"
	"database/sql"
	"time"
)

// enqueueBookingEmails runs inside the booking mutation transaction. Delayed
// reminder rows need no separate scheduler or backfill of historical bookings.
func enqueueBookingEmails(ctx context.Context, tx *sql.Tx, b Booking, event string, now time.Time) error {
	recipients := []string{b.MemberID}
	if event == "requested" {
		recipients = []string{b.PastorID}
	}
	if event == "cancelled" {
		recipients = []string{b.MemberID, b.PastorID}
	}
	expires := now.Add(23 * time.Hour) // Stay inside the provider's 24-hour deduplication window.
	if (event == "requested" || event == "approved") && b.StartsAt.Before(expires) {
		expires = b.StartsAt
	}
	for _, recipient := range recipients {
		if err := enqueueEmail(ctx, tx, b, recipient, event, now, expires); err != nil {
			return err
		}
	}
	if event == "approved" {
		for _, reminder := range []struct {
			event  string
			before time.Duration
		}{{"reminder_24h", 24 * time.Hour}, {"reminder_1h", time.Hour}} {
			due := b.StartsAt.Add(-reminder.before)
			if !due.After(now) {
				continue
			} // Approval itself covers late confirmations.
			expiry := due.Add(23 * time.Hour)
			if b.StartsAt.Before(expiry) {
				expiry = b.StartsAt
			}
			for _, recipient := range []string{b.MemberID, b.PastorID} {
				if err := enqueueEmail(ctx, tx, b, recipient, reminder.event, due, expiry); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func enqueueEmail(ctx context.Context, tx *sql.Tx, b Booking, recipient, event string, due, expires time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO pastoral_email_outbox(booking_id,recipient_id,booking_version,event,scheduled_at,next_attempt_at,expires_at) VALUES($1,$2,$3,$4,$5,$5,$6) ON CONFLICT(booking_id,booking_version,event,recipient_id) DO NOTHING`, b.ID, recipient, b.Version, event, due, expires)
	return err
}
