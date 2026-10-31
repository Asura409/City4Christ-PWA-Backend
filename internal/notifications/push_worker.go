package notifications

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"time"
)

const pushPreview = `{"title":"City4Christ","body":"You have a new update. Open the app to view it.","url":"/#/notifications"}`

type PushSender interface {
	Send(context.Context, PushSubscription, []byte) (int, error)
}
type PushWorker struct {
	db     *sql.DB
	sender PushSender
	now    func() time.Time
}

func NewPushWorker(db *sql.DB, sender PushSender, now func() time.Time) *PushWorker {
	if now == nil {
		now = time.Now
	}
	return &PushWorker{db: db, sender: sender, now: now}
}
func (w *PushWorker) RunOnce(ctx context.Context) (int, error) {
	if w.sender == nil {
		return 0, ErrInvalid
	}
	processed := 0
	for range 20 {
		ok, err := w.one(ctx)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, nil
		}
		processed++
	}
	return processed, nil
}
func (w *PushWorker) one(ctx context.Context) (bool, error) {
	now := w.now().UTC()
	token := uuid.NewString()
	var id, subID string
	var attempts int
	err := w.db.QueryRowContext(ctx, `WITH ready AS(SELECT id FROM notification_push_jobs WHERE (state='pending' AND available_at<=$1) OR (state='processing' AND lease_until<=$1) ORDER BY available_at,id LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE notification_push_jobs j SET state='processing',lease_until=$1+interval '30 seconds',lease_token=$2,attempts=attempts+1 FROM ready WHERE j.id=ready.id RETURNING j.id,j.subscription_id,j.attempts`, now, token).Scan(&id, &subID, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var sub PushSubscription
	var eligible bool
	err = w.db.QueryRowContext(ctx, `SELECT s.endpoint,s.p256dh,s.auth_secret,(COALESCE(p.push_enabled,false) AND `+pushCategorySQL+` AND a.email_verified_at IS NOT NULL AND a.id=n.account_id AND sess.revoked_at IS NULL AND sess.expires_at>$2 AND n.read_at IS NULL AND c4c_group_content_visible(n.group_id,n.account_id) AND j.created_at>$2-interval '24 hours') FROM notification_push_jobs j JOIN notifications n ON n.id=j.notification_id JOIN notification_push_subscriptions s ON s.id=j.subscription_id JOIN accounts a ON a.id=s.account_id JOIN auth_sessions sess ON sess.id=s.session_id LEFT JOIN notification_preferences p ON p.account_id=a.id WHERE j.id=$1`, id, now).Scan(&sub.Endpoint, &sub.Keys.P256dh, &sub.Keys.Auth, &eligible)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	state := "cancelled"
	available := now
	if eligible && validSubscription(sub) {
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		status, sendErr := w.sender.Send(sendCtx, sub, []byte(pushPreview))
		cancel()
		switch {
		case sendErr == nil && (status == 201 || status == 202):
			state = "delivered"
		case sendErr == nil && (status == 404 || status == 410):
			_, err = w.db.ExecContext(ctx, `DELETE FROM notification_push_subscriptions WHERE id=$1`, subID)
			return true, err
		case sendErr != nil || status == 429 || status >= 500:
			state = "pending"
			if attempts >= 6 {
				state = "failed"
			}
			delay := time.Minute * time.Duration(1<<min(attempts-1, 6))
			available = now.Add(delay)
		default:
			state = "failed"
		}
	}
	_, err = w.db.ExecContext(ctx, `UPDATE notification_push_jobs SET state=$3,available_at=$4,lease_until=NULL,lease_token=NULL,finished_at=CASE WHEN $3='pending' THEN NULL ELSE $5::timestamptz END WHERE id=$1 AND lease_token=$2`, id, token, state, available, now)
	return true, err
}
