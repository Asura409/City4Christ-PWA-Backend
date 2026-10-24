package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestNotificationsThroughGlobalAppArePrivateAndReadIdempotently(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("n", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	createAccount := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Notice','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	owner, ownerToken := createAccount("notice-owner@example.invalid")
	_, otherToken := createAccount("notice-other@example.invalid")
	repository := notifications.NewRepository(db)
	if _, err := repository.Create(ctx, notifications.CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Request approved", Body: "You joined Choir.", ResourcePath: "/#/groups/11111111-1111-4111-8111-111111111111"}); err != nil {
		t.Fatal(err)
	}
	handler := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if method != "GET" {
			r.Header.Set("Origin", "https://api.example")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/notifications", "", ""); w.Code != 401 {
		t.Fatalf("anonymous list status=%d body=%s", w.Code, w.Body.String())
	}
	feed := request("GET", "/api/v1/notifications?limit=20", ownerToken, "")
	if feed.Code != 200 || !strings.Contains(feed.Body.String(), "Request approved") || strings.Contains(feed.Body.String(), "notification-account") {
		t.Fatalf("owner feed status=%d body=%s", feed.Code, feed.Body.String())
	}
	var response struct {
		Data []notifications.Notification `json:"data"`
	}
	if err := json.Unmarshal(feed.Body.Bytes(), &response); err != nil || len(response.Data) != 1 {
		t.Fatalf("owner feed decode=%+v err=%v", response, err)
	}
	id := response.Data[0].ID
	if w := request("GET", "/api/v1/notifications/unread-count", ownerToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"unreadCount":1`) {
		t.Fatalf("unread count status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("PUT", "/api/v1/notifications/"+id+"/read", otherToken, `{}`); w.Code != 404 {
		t.Fatalf("other member read private notification: status=%d body=%s", w.Code, w.Body.String())
	}
	for index, want := range []bool{true, false} {
		w := request("PUT", "/api/v1/notifications/"+id+"/read", ownerToken, `{}`)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":`+strings.ToLower(map[bool]string{true: "true", false: "false"}[want])) {
			t.Fatalf("mark read #%d status=%d body=%s", index, w.Code, w.Body.String())
		}
	}
	if w := request("GET", "/api/v1/notifications/unread-count", ownerToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"unreadCount":0`) {
		t.Fatalf("unread count after read status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNotificationsOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]struct {
			Security []any `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ path, method string }{{"/api/v1/notifications", "get"}, {"/api/v1/notifications/unread-count", "get"}, {"/api/v1/notifications/{notificationId}/read", "put"}} {
		operation, ok := spec.Paths[route.path][route.method]
		if !ok || len(operation.Security) == 0 {
			t.Fatalf("missing or unsecured notifications route %s %s", route.method, route.path)
		}
	}
}
