package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestRealtimeTicketRouteRequiresBearerAndIssuesNoStoreTicket(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{33}, 32)
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime@example.invalid','Real','Time',now()) RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://api.example.invalid"
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	request := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/realtime/tickets", nil)
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(""); w.Code != 401 {
		t.Fatalf("anonymous ticket request = %d %s, want 401", w.Code, w.Body.String())
	}
	w := request(tokens.AccessToken)
	if w.Code != 201 {
		t.Fatalf("issue ticket = %d %s, want 201", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
	var response struct {
		Data struct {
			Ticket   string    `json:"ticket"`
			Expires  time.Time `json:"expiresAt"`
			Lifetime int       `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Ticket == "" || response.Data.Lifetime != 30 || !response.Data.Expires.Equal(now.Add(30*time.Second)) {
		t.Fatalf("ticket response = %+v, want an opaque 30-second ticket", response.Data)
	}
	if strings.Contains(w.Body.String(), accountID) {
		t.Fatal("ticket response leaked account ID")
	}
}
