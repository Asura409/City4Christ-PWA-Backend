package notifications

import (
	"context"
	webpush "github.com/SherClockHolmes/webpush-go"
	"io"
	"net/http"
	"strings"
	"testing"
)

type pushHTTP func(*http.Request) (*http.Response, error)

func (f pushHTTP) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestVAPIDSenderUsesLibraryAndFixedEndpointPolicy(t *testing.T) {
	private, public, e := webpush.GenerateVAPIDKeys()
	if e != nil {
		t.Fatal(e)
	}
	called := false
	sender, e := NewVAPIDSender(PushConfig{Enabled: true, PublicKey: public, PrivateKey: private, Subject: "mailto:info@city4christ.org"}, pushHTTP(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.URL.Host != "fcm.googleapis.com" || r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Fatal("missing encrypted push headers")
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "private text") {
			t.Fatal("plaintext push")
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	status, e := sender.Send(context.Background(), testSubscription(t), []byte("private text"))
	if e != nil || status != 201 || !called {
		t.Fatal(status, e)
	}
	bad := testSubscription(t)
	bad.Endpoint = "https://example.invalid/ssrf"
	called = false
	if _, e = sender.Send(context.Background(), bad, nil); e == nil || called {
		t.Fatal("unsafe endpoint sent", e)
	}
	if _, e = NewVAPIDSender(PushConfig{Enabled: true, PublicKey: public, PrivateKey: "invalid", Subject: "mailto:info@city4christ.org"}, nil); e == nil {
		t.Fatal("invalid key config")
	}
}
