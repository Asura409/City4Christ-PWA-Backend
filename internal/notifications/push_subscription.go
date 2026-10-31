package notifications

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

type PushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}
type PushSubscription struct {
	Endpoint string   `json:"endpoint"`
	Keys     PushKeys `json:"keys"`
}

func validPushEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") || u.Path == "" || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	host := u.Hostname()
	return host == "fcm.googleapis.com" || host == "updates.push.services.mozilla.com" || host == "web.push.apple.com" || strings.HasSuffix(host, ".notify.windows.com")
}
func decodePushKey(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}
func validSubscription(s PushSubscription) bool {
	if !validPushEndpoint(s.Endpoint) || len(s.Keys.P256dh) > 100 || len(s.Keys.Auth) > 32 {
		return false
	}
	key, err := decodePushKey(s.Keys.P256dh)
	if err != nil {
		return false
	}
	if _, err = ecdh.P256().NewPublicKey(key); err != nil {
		return false
	}
	auth, err := decodePushKey(s.Keys.Auth)
	return err == nil && len(auth) == 16
}
func (s *Service) Subscribe(ctx context.Context, actor, session string, in PushSubscription) (string, error) {
	if !validID(actor) || !validID(session) || !validSubscription(in) {
		return "", ErrInvalid
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('push-account:'||$1,0))`, actor); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('push-endpoint:'||$1,0))`, in.Endpoint); err != nil {
		return "", err
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_sessions s JOIN accounts a ON a.id=s.account_id WHERE s.id=$1 AND s.account_id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND a.email_verified_at IS NOT NULL)`, session, actor, s.now()).Scan(&active); err != nil {
		return "", err
	}
	if !active {
		return "", ErrForbidden
	}
	var id, owner, oldSession, key, secret string
	var live bool
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.account_id,p.session_id,p.p256dh,p.auth_secret,(s.revoked_at IS NULL AND s.expires_at>$2) FROM notification_push_subscriptions p JOIN auth_sessions s ON s.id=p.session_id WHERE endpoint=$1 FOR UPDATE OF p`, in.Endpoint, s.now()).Scan(&id, &owner, &oldSession, &key, &secret, &live)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil {
		if owner != actor && live {
			return "", ErrConflict
		}
		if owner == actor && oldSession == session && key == in.Keys.P256dh && secret == in.Keys.Auth {
			return id, tx.Commit()
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM notification_push_subscriptions WHERE id=$1`, id); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM notification_push_subscriptions p USING auth_sessions s WHERE p.account_id=$1 AND s.id=p.session_id AND (s.revoked_at IS NOT NULL OR s.expires_at<=$2)`, actor, s.now()); err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM notification_push_subscriptions WHERE account_id=$1`, actor).Scan(&count); err != nil {
		return "", err
	}
	if count >= 10 {
		return "", ErrConflict
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO notification_push_subscriptions(account_id,session_id,endpoint,p256dh,auth_secret) VALUES($1,$2,$3,$4,$5) RETURNING id`, actor, session, in.Endpoint, in.Keys.P256dh, in.Keys.Auth).Scan(&id); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func (s *Service) Unsubscribe(ctx context.Context, actor, endpoint string) error {
	if !validID(actor) || !validPushEndpoint(endpoint) {
		return ErrInvalid
	}
	_, err := s.repo.db.ExecContext(ctx, `DELETE FROM notification_push_subscriptions WHERE account_id=$1 AND endpoint=$2`, actor, endpoint)
	return err
}

// Push preferences never suppress the durable in-app inbox.
const pushCategorySQL = `CASE WHEN n.kind IN ('church.broadcast','group.broadcast') THEN COALESCE(p.announcements,true) WHEN n.kind LIKE 'group.%' THEN COALESCE(p.groups,true) WHEN n.kind LIKE 'event.%' THEN COALESCE(p.events,true) WHEN n.kind LIKE 'pastoral.%' OR n.kind LIKE 'appointment.%' THEN COALESCE(p.pastoral,true) WHEN n.kind LIKE 'message.%' THEN COALESCE(p.messages,true) ELSE true END`

func queuePushTx(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_push_jobs(notification_id,subscription_id) SELECT n.id,s.id FROM notifications n JOIN notification_push_subscriptions s ON s.account_id=n.account_id JOIN auth_sessions a ON a.id=s.session_id JOIN notification_preferences p ON p.account_id=n.account_id WHERE n.id=$1 AND p.push_enabled AND a.revoked_at IS NULL AND a.expires_at>now() AND `+pushCategorySQL+` ON CONFLICT(notification_id,subscription_id) DO NOTHING`, id)
	return err
}
