package notifications

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"testing"
)

func testSubscription(t *testing.T) PushSubscription {
	t.Helper()
	key, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return PushSubscription{Endpoint: "https://fcm.googleapis.com/fcm/send/synthetic", Keys: PushKeys{P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16))}}
}

type pushStub struct {
	status, calls int
	payload       []byte
}

func (p *pushStub) Send(_ context.Context, _ PushSubscription, payload []byte) (int, error) {
	p.calls++
	p.payload = payload
	return p.status, nil
}
func TestPushConsentSessionBindingRetryAndExpiry(t *testing.T) {
	db, r, owner, other := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	sessions, e := auth.NewSessionService(db, auth.SessionConfig{Key: make([]byte, 32)})
	if e != nil {
		t.Fatal(e)
	}
	tokens, e := sessions.Start(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	_, sid, _, e := sessions.AuthenticateSession(ctx, tokens.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	sub := testSubscription(t)
	if _, e = s.Subscribe(ctx, owner, sid, sub); e != nil {
		t.Fatal(e)
	}
	foreign, e := sessions.Start(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	_, otherSID, _, _ := sessions.AuthenticateSession(ctx, foreign.AccessToken)
	if _, e = s.Subscribe(ctx, other, otherSID, sub); !errors.Is(e, ErrConflict) {
		t.Fatal("endpoint stolen", e)
	}
	bad := sub
	bad.Endpoint = "https://127.0.0.1/private"
	if _, e = s.Subscribe(ctx, owner, sid, bad); !errors.Is(e, ErrInvalid) {
		t.Fatal("SSRF", e)
	}
	if _, e = r.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Private title", Body: "Private secret"}); e != nil {
		t.Fatal(e)
	}
	sender := &pushStub{status: 201}
	worker := NewPushWorker(db, sender, nil)
	if n, e := worker.RunOnce(ctx); e != nil || n != 0 || sender.calls != 0 {
		t.Fatal("without consent", n, e)
	}
	pref := Preferences{PushEnabled: true, Groups: true, Events: true, Pastoral: true, Messages: true, Announcements: true}
	if _, e = s.SetPreferences(ctx, owner, pref); e != nil {
		t.Fatal(e)
	}
	notice, e := r.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Private title", Body: "Private secret", DedupKey: "push-1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Private title", DedupKey: "push-1"}); e != nil {
		t.Fatal(e)
	}
	sender.status = 503
	if n, e := worker.RunOnce(ctx); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if sender.calls != 1 {
		t.Fatal(sender.calls)
	}
	if string(sender.payload) != "{\"title\":\"City4Christ\",\"body\":\"You have a new update. Open the app to view it.\",\"url\":\"/#/notifications\"}" {
		t.Fatal("unsafe preview", string(sender.payload))
	}
	if _, e = db.Exec(`UPDATE notification_push_jobs SET available_at=now()-interval '1 second' WHERE notification_id=$1`, notice.ID); e != nil {
		t.Fatal(e)
	}
	sender.status = 410
	if _, e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM notification_push_subscriptions`).Scan(&count); e != nil || count != 0 {
		t.Fatal("expired device retained", count, e)
	}
	if _, e = s.Subscribe(ctx, owner, sid, sub); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "After logout"}); e != nil {
		t.Fatal(e)
	}
	if e = sessions.Logout(ctx, tokens.RefreshToken); e != nil {
		t.Fatal(e)
	}
	calls := sender.calls
	if _, e = worker.RunOnce(ctx); e != nil || sender.calls != calls {
		t.Fatal("logged out device received", sender.calls, e)
	}
}
