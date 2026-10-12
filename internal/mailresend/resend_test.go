package mailresend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.resend.com" || r.URL.Scheme != "https" {
		return nil, errors.New("SDK endpoint override was not blocked")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = rt.target.Scheme
	u.Host = rt.target.Host
	copy.URL = &u
	return http.DefaultTransport.RoundTrip(copy)
}

func TestResendAdapter(t *testing.T) {
	type requestCapture struct{ key, body string }
	captured := make(chan requestCapture, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing SDK authentication")
		}
		var body struct {
			From          string
			To            []string
			Subject, Text string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.From != "City4Christ <noreply@example.com>" || len(body.To) != 1 || body.To[0] != "member@example.com" || !strings.Contains(body.Text, "01234567") || !strings.Contains(body.Text, "2026-10-12T12:10:00Z") || body.Subject == "" {
			t.Error("unexpected verification message")
		}
		captured <- requestCapture{key: r.Header.Get("Idempotency-Key"), body: body.Text}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"email-123"}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	t.Setenv("RESEND_BASE_URL", "https://unexpected.example/")
	sender, err := newSender("test-key", "City4Christ <noreply@example.com>", &http.Client{Transport: redirectTransport{target: target}})
	if err != nil {
		t.Fatal(err)
	}
	message := auth.VerificationEmail{Email: "member@example.com", Code: "01234567", ExpiresAt: time.Date(2026, 10, 12, 12, 10, 0, 0, time.UTC)}
	const id = "b8ac53d9-6b94-44ba-b173-3d665c14c631"
	for range 2 {
		if err := sender.Send(context.Background(), id, message); err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-captured, <-captured
	if first.key != "email-verification/"+id || second.key != first.key || second.body != first.body {
		t.Fatal("retries must preserve key and payload")
	}
}

func TestResendErrorsAreSafeAndClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		retry  bool
		code   string
	}{{429, true, "provider_rate_limited"}, {503, true, "provider_unavailable"}, {403, false, "provider_rejected"}, {422, false, "provider_rejected"}, {409, true, "provider_unavailable"}, {302, false, "provider_rejected"}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"message":"secret-otp-and-api-key"}`))
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			sender, err := newSender("test-key", "noreply@example.com", &http.Client{Transport: redirectTransport{target: target}})
			if err != nil {
				t.Fatal(err)
			}
			err = sender.Send(context.Background(), "b8ac53d9-6b94-44ba-b173-3d665c14c631", auth.VerificationEmail{Email: "member@example.com", Code: "01234567", ExpiresAt: time.Now().Add(time.Minute)})
			var failure *auth.EmailDeliveryError
			if !errors.As(err, &failure) || failure.Retryable != tc.retry || failure.Code != tc.code {
				t.Fatalf("unexpected failure: %v", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "test-key") {
				t.Fatal("provider error leaked secrets")
			}
			if tc.status == 429 && failure.RetryAfter != 7*time.Second {
				t.Fatal("rate limit retry hint ignored")
			}
		})
	}
}

func TestResendConfigurationAndCancellation(t *testing.T) {
	for _, tc := range []struct{ key, from string }{{"", "noreply@example.com"}, {"test-key", ""}, {"test-key", "invalid"}} {
		if _, err := New(tc.key, tc.from); err == nil {
			t.Fatal("accepted invalid configuration")
		}
	}
	sender, err := New("test-key", "noreply@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sender.Send(ctx, "b8ac53d9-6b94-44ba-b173-3d665c14c631", auth.VerificationEmail{Email: "member@example.com", Code: "01234567", ExpiresAt: time.Now().Add(time.Minute)})
	if err == nil {
		t.Fatal("canceled request accepted")
	}
}
