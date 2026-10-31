package app

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPushRoutesBindSubscriptionToAuthenticatedSession(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, token := f.account(t, "", false)
	w := f.request("GET", "/api/v1/notifications/push/config", token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.request("POST", "/api/v1/notifications/push/subscriptions", token, `{}`)
	if w.Code != 503 {
		t.Fatal("disabled push", w.Code)
	}
	key, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	public := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	f.handler = New(f.db, Config{AppOrigin: f.origin, VerificationKey: make([]byte, 32), Now: func() time.Time { return f.now }, PushEnabled: true, PushPublicKey: public})
	body := fmt.Sprintf(`{"endpoint":"https://fcm.googleapis.com/fcm/send/test-device","keys":{"p256dh":%q,"auth":%q}}`, public, base64.RawURLEncoding.EncodeToString(make([]byte, 16)))
	w = f.request("POST", "/api/v1/notifications/push/subscriptions", token, body)
	if w.Code != 201 || strings.Contains(w.Body.String(), "endpoint") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.request("DELETE", "/api/v1/notifications/push/subscriptions", token, `{"endpoint":"https://fcm.googleapis.com/fcm/send/test-device"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
