package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestDirectMessagingRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{41}, 32)
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Direct','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	firstID, firstToken := account("http-dm-first@example.invalid")
	secondID, secondToken := account("http-dm-second@example.invalid")
	staffID, staffToken := account("http-dm-staff@example.invalid")
	_, outsiderToken := account("http-dm-outside@example.invalid")
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator','')`, staffID); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	origin := "https://api.example.invalid"
	h := New(db, Config{VerificationKey: key, AppOrigin: origin})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.10:1234"
		r.Header.Set("Origin", origin)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/conversations", "", ""); w.Code != 401 {
		t.Fatalf("anonymous list = %d %s", w.Code, w.Body.String())
	}
	created := request("POST", "/api/v1/conversations", firstToken, `{"recipientId":"`+secondID+`"}`)
	if created.Code != 201 {
		t.Fatalf("create conversation = %d %s", created.Code, created.Body.String())
	}
	var conv struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &conv); err != nil || conv.Data.ID == "" {
		t.Fatalf("conversation response %s (%v)", created.Body.String(), err)
	}
	path := "/api/v1/conversations/" + conv.Data.ID + "/messages"
	messageBody := `{"clientMessageId":"7219a6ea-b436-48ca-9d75-b2b8f5dd18ef","body":"A private prayer request"}`
	if w := request("POST", path, firstToken, messageBody); w.Code != 201 {
		t.Fatalf("send = %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", path, firstToken, messageBody); w.Code != 200 {
		t.Fatalf("retry = %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", path, secondToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "A private prayer request") {
		t.Fatalf("recipient history = %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", path, outsiderToken, ""); w.Code != 404 {
		t.Fatalf("outsider history = %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", path, staffToken, ""); w.Code != 404 {
		t.Fatalf("staff history = %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/conversations", secondToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), firstID) {
		t.Fatalf("participant list = %d %s", w.Code, w.Body.String())
	}
	read := request("PUT", "/api/v1/conversations/"+conv.Data.ID+"/read", secondToken, `{"throughMessageId":""}`)
	if read.Code != 200 {
		t.Fatalf("mark read = %d %s", read.Code, read.Body.String())
	}
	if strings.Contains(logs.String(), "A private prayer request") {
		t.Fatal("request logs contained message body")
	}
	if strings.Contains(created.Body.String(), "http-dm-first@example.invalid") {
		t.Fatal("conversation leaked participant email")
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
}

func TestMessagingOpenAPIRequiresBearerForEveryRoute(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"/api/v1/conversations":                           {"get", "post"},
		"/api/v1/conversations/{conversationId}/messages": {"get", "post"},
		"/api/v1/conversations/{conversationId}/read":     {"put"},
	}
	for path, methods := range want {
		for _, method := range methods {
			operation := spec.Paths[path][method]
			if operation == nil {
				t.Fatalf("missing contract %s %s", method, path)
			}
			security, ok := operation["security"].([]any)
			if !ok || len(security) != 1 {
				t.Fatalf("%s %s lacks bearer security", method, path)
			}
		}
	}
}
