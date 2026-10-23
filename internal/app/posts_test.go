package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPostsThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("p", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Post','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	_, memberToken := account("posts@example.invalid")
	_, otherToken := account("reader@example.invalid")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/v1/posts", memberToken, `{"body":"Welcome to Sunday service"}`, "https://api.example"); w.Code != 201 {
		t.Fatalf("create post: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/posts", otherToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Welcome to Sunday service") {
		t.Fatalf("member feed: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ path, body string }{{"/api/v1/posts", `{"body":""}`}, {"/api/v1/posts?limit=0", ""}, {"/api/v1/posts?admin=true", ""}} {
		if w := request("POST", "/api/v1/posts", memberToken, tc.body, "https://api.example"); tc.body != "" && w.Code != 422 {
			t.Fatalf("invalid post: %d %s", w.Code, w.Body.String())
		}
		if tc.body == "" {
			if w := request("GET", tc.path, otherToken, "", ""); w.Code != 422 {
				t.Fatalf("invalid feed query %s: %d", tc.path, w.Code)
			}
		}
	}
	if w := request("GET", "/api/v1/posts", "", "", ""); w.Code != 401 {
		t.Fatalf("anonymous feed: %d", w.Code)
	}
	if w := request("POST", "/api/v1/posts", memberToken, `{"body":"No origin"}`, ""); w.Code != 403 {
		t.Fatalf("write without origin: %d", w.Code)
	}
}

func TestPostsContractRequiresMemberAuthentication(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	route := spec.Paths["/api/v1/posts"]
	if route == nil || route["get"] == nil || route["post"] == nil {
		t.Fatal("posts API contract is missing")
	}
	for _, method := range []string{"get", "post"} {
		security, ok := route[method]["security"].([]any)
		if !ok || len(security) != 1 {
			t.Fatalf("%s posts endpoint must require bearer authentication", method)
		}
	}
}
