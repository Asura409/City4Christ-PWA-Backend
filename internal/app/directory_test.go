package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestMemberDirectoryThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("d", 32))
	origin := "https://api.example"
	now := time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	create := func(email string) (string, auth.SessionTokens) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,password_hash) VALUES($1,'Grace','Member',now(),'private-hash') RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens
	}
	_, reader := create("reader@example.invalid")
	memberID, member := create("listed@example.invalid")
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/members", "", ""); w.Code != 401 {
		t.Fatalf("visitor: %d %s", w.Code, w.Body.String())
	}
	w := request("GET", "/api/v1/members", reader.AccessToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("hidden defaults: %d %s", w.Code, w.Body.String())
	}
	if w = request("PATCH", "/api/v1/me", member.AccessToken, `{"directoryVisible":true}`); w.Code != 200 {
		t.Fatal("opt in failed")
	}
	w = request("GET", "/api/v1/members?query="+url.QueryEscape("grace member"), reader.AccessToken, "")
	var page struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			NextCursor string `json:"nextCursor"`
		} `json:"meta"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Data) != 1 || len(page.Data[0]) != 3 || page.Data[0]["id"] != memberID || strings.Contains(w.Body.String(), "@") || strings.Contains(w.Body.String(), "private-hash") {
		t.Fatalf("directory privacy: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("member data cacheable")
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=abc", "limit=1&limit=2", "email=listed@example.invalid", "cursor=bad", "query=bad%00name", "query=%zz"} {
		w = request("GET", "/api/v1/members?"+query, reader.AccessToken, "")
		if w.Code != 422 || strings.Contains(w.Body.String(), `"fields":{}`) {
			t.Fatalf("query %s: %d %s", query, w.Code, w.Body.String())
		}
	}
	if w = request("GET", "/api/v1/members/"+memberID, reader.AccessToken, ""); w.Code != 404 {
		t.Fatal("unplanned member detail route exposed")
	}
	if w = request("PATCH", "/api/v1/me", member.AccessToken, `{"directoryVisible":false}`); w.Code != 200 {
		t.Fatal("opt out failed")
	}
	if w = request("GET", "/api/v1/members", reader.AccessToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatal("opt-out not immediate")
	}
	limited := false
	for range 61 {
		w = request("GET", "/api/v1/members", reader.AccessToken, "")
		if w.Code == 429 {
			limited = true
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing retry hint")
			}
			break
		}
		if w.Code != 200 {
			t.Fatalf("limit: %d", w.Code)
		}
	}
	if !limited {
		t.Fatal("directory not rate limited")
	}
	if w = request("GET", "/api/v1/me", reader.AccessToken, ""); w.Code != 200 {
		t.Fatal("directory limit blocked own profile")
	}
	if w = request("GET", "/api/v1/members", member.AccessToken, ""); w.Code != 200 {
		t.Fatal("one reader's account limit blocked another")
	}
	_, freshReader := create("fresh-reader@example.invalid")
	ipLimited := false
	for range 60 {
		w = request("GET", "/api/v1/members", freshReader.AccessToken, "")
		if w.Code == 429 {
			ipLimited = true
			break
		}
		if w.Code != 200 {
			t.Fatalf("IP limit probe: %d", w.Code)
		}
	}
	if !ipLimited {
		t.Fatal("shared directory IP limit not enforced across accounts")
	}
	if err = sessions.Logout(ctx, member.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/members", member.AccessToken, ""); w.Code != 401 {
		t.Fatal("revoked session can browse")
	}
}
