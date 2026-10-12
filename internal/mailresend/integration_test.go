package mailresend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/app"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestGlobalRegistrationThroughResendToVerification(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{42}, 32)
	application := app.New(db, app.Config{RegistrationEnabled: true, VerificationKey: key, Now: func() time.Time { return now }})
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/auth/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		application.ServeHTTP(w, r)
		return w
	}
	signup := request("register", `{"email":"member@example.com","firstName":"Ada","lastName":"Member","password":"a sufficiently long password"}`)
	if signup.Code != 201 {
		t.Fatalf("signup=%d", signup.Code)
	}
	now = now.Add(time.Minute)
	if w := request("verification/resend", `{"email":"member@example.com"}`); w.Code != 202 {
		t.Fatalf("resend=%d", w.Code)
	}
	var calls atomic.Int32
	codes := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct{ Text string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		matches := regexp.MustCompile(`code is: ([0-9]{8})`).FindStringSubmatch(body.Text)
		if len(matches) != 2 {
			t.Error("missing OTP")
		} else {
			codes <- matches[1]
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"accepted-email"}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	sender, err := newSender("test-key", "City4Christ <noreply@example.com>", &http.Client{Transport: redirectTransport{target: target}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewVerificationService(db, auth.VerificationConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := service.DeliverNext(ctx, sender); err != nil || !processed {
		t.Fatalf("delivery: %v", err)
	}
	if processed, err := service.DeliverNext(ctx, sender); err != nil || processed || calls.Load() != 1 {
		t.Fatal("duplicate or replaced email dispatched")
	}
	code := <-codes
	body, _ := json.Marshal(map[string]string{"email": "member@example.com", "code": code})
	if w := request("verification/verify", string(body)); w.Code != 200 {
		t.Fatalf("verification=%d", w.Code)
	}
}
