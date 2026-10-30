package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

type AppointmentEmail struct {
	Email, AppURL string
	Reminder      bool
}
type AppointmentEmailSender interface {
	SendAppointment(context.Context, string, AppointmentEmail) error
}
type EmailWorker struct {
	db     *sql.DB
	appURL string
	now    func() time.Time
}

func AppointmentAppURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(base, "\r\n") {
		return "", errors.New("appointment email app URL must be an HTTPS app address without credentials, query or fragment")
	}
	u.Fragment = "/pastors"
	return u.String(), nil
}
func NewEmailWorker(db *sql.DB, base string, now func() time.Time) (*EmailWorker, error) {
	appURL, err := AppointmentAppURL(base)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &EmailWorker{db: db, appURL: appURL, now: now}, nil
}

// DeliverNext serializes with booking mutations using the same provider lock.
// The row lock prevents competing workers from dispatching the same queue item.
// No SMTP/API calls happen in HTTP handlers or in the booking mutation transaction.
func (w *EmailWorker) DeliverNext(ctx context.Context, sender AppointmentEmailSender) (bool, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, booking, pastor, recipient, event string
	var version, attempts int
	var expires time.Time
	now := w.now().UTC()
	err = tx.QueryRowContext(ctx, `SELECT o.id,o.booking_id,b.pastor_id,o.recipient_id,o.event,o.booking_version,o.delivery_attempts,o.expires_at FROM pastoral_email_outbox o JOIN pastor_bookings b ON b.id=o.booking_id WHERE o.sent_at IS NULL AND o.failed_at IS NULL AND o.discarded_at IS NULL AND (o.next_attempt_at<=$1 OR o.expires_at<=$1) ORDER BY o.next_attempt_at,o.id LIMIT 1 FOR UPDATE OF o SKIP LOCKED`, now).Scan(&id, &booking, &pastor, &recipient, &event, &version, &attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('pastoral:'||$1,0))`, pastor).Scan(&locked); err != nil {
		return true, err
	}
	if !locked {
		return false, nil
	}
	discard := func() (bool, error) {
		_, err := tx.ExecContext(ctx, `UPDATE pastoral_email_outbox SET discarded_at=$2 WHERE id=$1`, id, w.now().UTC())
		if err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	b, err := readBooking(ctx, tx, booking)
	if err != nil {
		return true, err
	}
	reminder := strings.HasPrefix(event, "reminder_")
	expected := event
	if reminder {
		expected = "approved"
	}
	if event == "requested" {
		expected = "pending"
	}
	if b.Version != version || b.Status != expected || !w.now().Before(expires) {
		return discard()
	}
	// Never deliver a private provider update after pastoral authority is revoked.
	if recipient == pastor || event == "requested" || event == "approved" || reminder {
		var role string
		err = tx.QueryRowContext(ctx, `SELECT r.role FROM account_roles r JOIN accounts a ON a.id=r.account_id WHERE r.account_id=$1 AND r.scope='' AND r.role IN ('assistant_pastor','super_administrator') AND a.email_verified_at IS NOT NULL ORDER BY r.role LIMIT 1 FOR SHARE OF r,a`, pastor).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return discard()
		}
		if err != nil {
			return true, err
		}
	}
	var email string
	err = tx.QueryRowContext(ctx, `SELECT email FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL FOR SHARE`, recipient).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return discard()
	}
	if err != nil {
		return true, err
	}
	remaining := expires.Sub(w.now())
	if remaining <= 0 {
		return discard()
	}
	sendCtx, cancel := context.WithTimeout(ctx, min(10*time.Second, remaining))
	sendErr := sender.SendAppointment(sendCtx, id, AppointmentEmail{Email: email, AppURL: w.appURL, Reminder: reminder})
	cancel()
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if sendErr == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE pastoral_email_outbox SET sent_at=$2,delivery_attempts=delivery_attempts+1,last_error_code=NULL WHERE id=$1`, id, w.now().UTC()); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	failure := appointmentFailure(sendErr)
	attempts++
	delay := 5 * time.Second * time.Duration(1<<min(attempts-1, 4))
	if failure.RetryAfter > delay {
		delay = min(failure.RetryAfter, time.Hour)
	}
	var failedAt *time.Time
	now = w.now().UTC()
	if !failure.Retryable || attempts >= 5 {
		failedAt = &now
	}
	_, err = tx.ExecContext(ctx, `UPDATE pastoral_email_outbox SET delivery_attempts=$2,next_attempt_at=$3,failed_at=$4,last_error_code=$5 WHERE id=$1`, id, attempts, now.Add(delay), failedAt, failure.Code)
	if err != nil {
		return true, err
	}
	if err = tx.Commit(); err != nil {
		return true, err
	}
	return true, failure
}
func appointmentFailure(err error) *auth.EmailDeliveryError {
	var failure *auth.EmailDeliveryError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "provider_rate_limited", "provider_unavailable", "provider_rejected", "invalid_email_payload":
			copy := *failure
			return &copy
		}
	}
	return &auth.EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
}
