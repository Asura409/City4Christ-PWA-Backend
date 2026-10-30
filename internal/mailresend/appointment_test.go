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

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
)

func TestAppointmentEmailUsesPrivatePurpleTemplateAndProviderKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Subject, Text, Html string
			To                  []string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Idempotency-Key") != "pastoral-email/b8ac53d9-6b94-44ba-b173-3d665c14c631" {
			t.Error("missing stable provider key")
		}
		if len(body.To) != 1 || body.To[0] != "member@example.invalid" {
			t.Error("recipient leaked", body.To)
		}
		if !strings.Contains(body.Html, "#542361") || !strings.Contains(body.Html, "viewport") || !strings.Contains(body.Html, "https://frontend.example.invalid/#/pastors") {
			t.Error("template/link missing")
		}
		for _, secret := range []string{"pastorId", "memberId", "startsAt", "bookingId", "Bearer", "<script>"} {
			if strings.Contains(body.Html+body.Text+body.Subject, secret) {
				t.Error("private/unsafe content", secret)
			}
		}
		if calls == 2 && !strings.Contains(body.Text, "upcoming appointment") {
			t.Error("missing reminder")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"sent"}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	s, _ := newSender("test-key", "City4Christ <info@example.invalid>", &http.Client{Transport: redirectTransport{target: target}})
	m := pastoral.AppointmentEmail{Email: "member@example.invalid", AppURL: "https://frontend.example.invalid/#/pastors"}
	id := "b8ac53d9-6b94-44ba-b173-3d665c14c631"
	if err := s.SendAppointment(context.Background(), id, m); err != nil {
		t.Fatal(err)
	}
	m.Reminder = true
	if err := s.SendAppointment(context.Background(), id, m); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"http://frontend.example/#/pastors", "https://user:pass@frontend.example/#/pastors", "https://frontend.example/?token=secret#/pastors", "https://frontend.example/#/login", "javascript:alert(1)"} {
		m.AppURL = u
		err := s.SendAppointment(context.Background(), id, m)
		var failure *auth.EmailDeliveryError
		if !errors.As(err, &failure) || failure.Code != "invalid_email_payload" || calls != 2 {
			t.Fatal("invalid URL sent", u, err)
		}
	}
	m.AppURL = "https://frontend.example.invalid/#/pastors"
	m.Email = "bad\r\n@injected"
	if err := s.SendAppointment(context.Background(), id, m); err == nil || calls != 2 {
		t.Fatal("invalid recipient sent")
	}
}
